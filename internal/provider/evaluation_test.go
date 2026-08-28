package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

func TestEvaluationRequestUsesTopLevelItemSchema(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	model := evaluationModel{
		Name:                 types.StringValue("my-eval"),
		DataSourceItemSchema: types.StringValue(`{"type":"object"}`),
		TestingCriteria: []evaluationTestingCriterionModel{
			{
				Name:          types.StringValue("check"),
				EvaluatorName: types.StringValue("my-evaluator"),
			},
		},
	}

	var diagnostics diag.Diagnostics
	request := model.request(ctx, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("request diagnostics: %v", diagnostics)
	}

	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	dsConfig, ok := decoded["data_source_config"].(map[string]any)
	if !ok {
		t.Fatalf("expected data_source_config object, got %v", decoded["data_source_config"])
	}
	if _, present := dsConfig["item_schema"]; !present {
		t.Error("expected top-level item_schema in request")
	}

	criteria, ok := decoded["testing_criteria"].([]any)
	if !ok || len(criteria) != 1 {
		t.Fatalf("expected 1 testing criterion, got %v", decoded["testing_criteria"])
	}
	criterion := criteria[0].(map[string]any)
	if criterion["type"] != "azure_ai_evaluator" {
		t.Errorf("criterion type = %v, want azure_ai_evaluator", criterion["type"])
	}
}

// The service always echoes an empty top-level item_schema on responses; the
// schema actually posted comes back nested at data_source_config.schema.item.
// apply() must read from that nested location, not the top-level field.
func TestEvaluationApplyReadsNestedSchemaItem(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	response := evaluationResponse{
		ID:   "eval_abc123",
		Name: "my-eval",
		DataSourceConfig: evaluationDataSourceConfigResponse{
			Type: "custom",
			Schema: evaluationDataSourceSchemaWire{
				Item: json.RawMessage(`{"type":"object"}`),
			},
		},
	}

	var model evaluationModel
	var diagnostics diag.Diagnostics
	model.apply(ctx, response, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("apply diagnostics: %v", diagnostics)
	}

	if model.DataSourceItemSchema.ValueString() != `{"type":"object"}` {
		t.Errorf("data_source_item_schema = %q", model.DataSourceItemSchema.ValueString())
	}
	if model.ID.ValueString() != "eval_abc123" {
		t.Errorf("id = %q", model.ID.ValueString())
	}
}

func TestEvaluationResourceRequiresPreviewFeature(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName: "acct",
		ProjectName: "proj",
		Environment: clients.EnvironmentPublic,
		Auth:        clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	r := &evaluationResource{}
	r.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}

	var resp resource.ValidateConfigResponse
	r.ValidateResourceConfig(context.Background(), resource.ValidateConfigRequest{}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error when evaluations preview is not enabled")
	}
}

func TestEvaluationResourceAllowsEnabledPreviewFeature(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName:     "acct",
		ProjectName:     "proj",
		Environment:     clients.EnvironmentPublic,
		Auth:            clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
		PreviewFeatures: []string{evaluationsPreviewFeatureName},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	r := &evaluationResource{}
	r.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}

	var resp resource.ValidateConfigResponse
	r.ValidateResourceConfig(context.Background(), resource.ValidateConfigRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected error when evaluations preview is enabled: %v", resp.Diagnostics)
	}
}

func TestEvaluationDataSourceRequiresPreviewFeature(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName: "acct",
		ProjectName: "proj",
		Environment: clients.EnvironmentPublic,
		Auth:        clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	d := &evaluationDataSource{}
	d.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}

	var resp datasource.ValidateConfigResponse
	d.ValidateDataSourceConfig(context.Background(), datasource.ValidateConfigRequest{}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error when evaluations preview is not enabled")
	}
}

func TestEvaluationRuleRequestShape(t *testing.T) {
	t.Parallel()

	model := evaluationRuleModel{
		ID:          types.StringValue("tfrule1"),
		DisplayName: types.StringValue("My Rule"),
		EventType:   types.StringValue("responseCompleted"),
		AgentName:   types.StringValue("my-agent"),
		EvalID:      types.StringValue("eval_abc123"),
	}

	request := model.request()
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if decoded["eventType"] != "responseCompleted" {
		t.Errorf("eventType = %v", decoded["eventType"])
	}
	filter, ok := decoded["filter"].(map[string]any)
	if !ok || filter["agentName"] != "my-agent" {
		t.Errorf("filter.agentName = %v", decoded["filter"])
	}
	action, ok := decoded["action"].(map[string]any)
	if !ok {
		t.Fatalf("expected action object, got %v", decoded["action"])
	}
	if action["type"] != "continuousEvaluation" {
		t.Errorf("action.type = %v, want continuousEvaluation", action["type"])
	}
	if action["evalId"] != "eval_abc123" {
		t.Errorf("action.evalId = %v", action["evalId"])
	}
}

func TestEvaluationRuleApplyRoundTrip(t *testing.T) {
	t.Parallel()

	response := evaluationRuleResponse{
		ID:          "tfrule1",
		DisplayName: "My Rule",
		EventType:   "responseCompleted",
		Filter:      evaluationRuleFilterWire{AgentName: "my-agent"},
		Action:      evaluationRuleActionWire{Type: "continuousEvaluation", EvalID: "eval_abc123"},
	}

	var model evaluationRuleModel
	model.apply(response)

	if model.ID.ValueString() != "tfrule1" {
		t.Errorf("id = %q", model.ID.ValueString())
	}
	if model.AgentName.ValueString() != "my-agent" {
		t.Errorf("agent_name = %q", model.AgentName.ValueString())
	}
	if model.EvalID.ValueString() != "eval_abc123" {
		t.Errorf("eval_id = %q", model.EvalID.ValueString())
	}
	if !model.Description.IsNull() {
		t.Error("empty description should be null")
	}
}

func TestEvaluationRuleResourceRequiresPreviewFeature(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName: "acct",
		ProjectName: "proj",
		Environment: clients.EnvironmentPublic,
		Auth:        clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	r := &evaluationRuleResource{}
	r.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}

	var resp resource.ValidateConfigResponse
	r.ValidateResourceConfig(context.Background(), resource.ValidateConfigRequest{}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error when evaluations preview is not enabled")
	}
}

func TestEvaluationRuleResourceAllowsEnabledPreviewFeature(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName:     "acct",
		ProjectName:     "proj",
		Environment:     clients.EnvironmentPublic,
		Auth:            clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
		PreviewFeatures: []string{evaluationsPreviewFeatureName},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	r := &evaluationRuleResource{}
	r.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}

	var resp resource.ValidateConfigResponse
	r.ValidateResourceConfig(context.Background(), resource.ValidateConfigRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected error when evaluations preview is enabled: %v", resp.Diagnostics)
	}
}

func TestEvaluationRuleDataSourceRequiresPreviewFeature(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName: "acct",
		ProjectName: "proj",
		Environment: clients.EnvironmentPublic,
		Auth:        clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	d := &evaluationRuleDataSource{}
	d.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}

	var resp datasource.ValidateConfigResponse
	d.ValidateDataSourceConfig(context.Background(), datasource.ValidateConfigRequest{}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error when evaluations preview is not enabled")
	}
}
