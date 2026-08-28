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

func TestEvaluatorVersionRequestPromptType(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	metrics, diags := types.MapValueFrom(ctx, types.StringType, map[string]string{"score": "score"})
	if diags.HasError() {
		t.Fatalf("build metrics map: %v", diags)
	}

	model := evaluatorVersionModel{
		Name:       types.StringValue("my-evaluator"),
		Type:       types.StringValue("prompt"),
		PromptText: types.StringValue("Rate this: {{response}}"),
		Metrics:    metrics,
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
	definition, ok := decoded["definition"].(map[string]any)
	if !ok {
		t.Fatalf("expected definition object, got %v", decoded["definition"])
	}
	if definition["type"] != "prompt" {
		t.Errorf("type = %v, want prompt", definition["type"])
	}
	if definition["prompt_text"] != "Rate this: {{response}}" {
		t.Errorf("prompt_text = %v", definition["prompt_text"])
	}
	metricsField, ok := definition["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("expected metrics object, got %v", definition["metrics"])
	}
	if _, present := metricsField["score"]; !present {
		t.Error("expected metrics to contain score")
	}
}

func TestEvaluatorVersionApplyRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	response := evaluatorVersionResponse{
		ID:      "azureai://accounts/a/projects/p/evaluators/my-evaluator/versions/1",
		Name:    "my-evaluator",
		Version: "1",
		Definition: evaluatorDefinitionResponse{
			Type:     "code",
			CodeText: "def evaluate(): return 1",
			Metrics:  map[string]evaluatorMetric{"score": {Type: "ordinal"}},
		},
	}

	var model evaluatorVersionModel
	var diagnostics diag.Diagnostics
	model.apply(ctx, response, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("apply diagnostics: %v", diagnostics)
	}

	if model.Type.ValueString() != "code" {
		t.Errorf("type = %q", model.Type.ValueString())
	}
	if model.CodeText.ValueString() != "def evaluate(): return 1" {
		t.Errorf("code_text = %q", model.CodeText.ValueString())
	}
	if model.Version.ValueString() != "1" {
		t.Errorf("version = %q", model.Version.ValueString())
	}
	// An absent blob_uri must stay null so Terraform does not report a
	// permanent diff against an absent API field.
	if !model.BlobURI.IsNull() {
		t.Error("empty blob_uri should be null")
	}
}

func TestEvaluatorVersionResourceRequiresPreviewFeature(t *testing.T) {
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

	r := &evaluatorVersionResource{}
	r.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}

	var resp resource.ValidateConfigResponse
	r.ValidateResourceConfig(context.Background(), resource.ValidateConfigRequest{}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error when evaluations preview is not enabled")
	}
}

func TestEvaluatorVersionResourceAllowsEnabledPreviewFeature(t *testing.T) {
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

	r := &evaluatorVersionResource{}
	r.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}

	var resp resource.ValidateConfigResponse
	r.ValidateResourceConfig(context.Background(), resource.ValidateConfigRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected error when evaluations preview is enabled: %v", resp.Diagnostics)
	}
}

func TestEvaluatorVersionDataSourceRequiresPreviewFeature(t *testing.T) {
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

	d := &evaluatorVersionDataSource{}
	d.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}

	var resp datasource.ValidateConfigResponse
	d.ValidateDataSourceConfig(context.Background(), datasource.ValidateConfigRequest{}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error when evaluations preview is not enabled")
	}
}
