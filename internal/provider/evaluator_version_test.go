package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

func TestEvaluatorVersionSafetyRubricRoundTrip(t *testing.T) {
	t.Parallel()

	const versionPath = "/api/projects/default/evaluators/reservation-safety/versions"
	const requestJSON = `{
		"evaluator_type":"custom",
		"categories":["safety"],
		"definition":{
			"type":"rubric","scoring":{"mode":"boolean"},
			"dimensions":[
				{"id":"privacy","description":"Discloses another guest's details.","allowed":false,"always_applicable":true},
				{"id":"booking","description":"Helps with a valid booking.","allowed":true,"always_applicable":false},
				{"id":"cancellation","description":"Explains the cancellation policy.","allowed":true}
			]
		}
	}`
	const responseJSON = `{
		"id":"azureai://accounts/test/projects/default/evaluators/reservation-safety/versions/1",
		"name":"reservation-safety","version":"1","categories":["safety"],
		"definition":{
			"type":"rubric","scoring":{"mode":"boolean"},
			"dimensions":[
				{"id":"privacy","description":"Discloses another guest's details.","allowed":false,"always_applicable":true},
				{"id":"booking","description":"Helps with a valid booking.","allowed":true,"always_applicable":false},
				{"id":"cancellation","description":"Explains the cancellation policy.","allowed":true,"always_applicable":false}
			],
			"metrics":{"allowed_behavior_violated":{},"disallowed_behavior_violated":{}}
		}
	}`
	var want map[string]any
	if err := json.Unmarshal([]byte(requestJSON), &want); err != nil {
		t.Fatal(err)
	}
	client := newAgentTestClient(t, []string{evaluationsPreviewFeatureName}, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Header.Get("Foundry-Features"); got != "Evaluations=V1Preview" {
			t.Errorf("preview header = %q", got)
		}
		switch {
		case req.Method == http.MethodPost && req.URL.Path == versionPath:
			var got map[string]any
			if err := json.NewDecoder(req.Body).Decode(&got); err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("create body = %#v, err = %v; want %#v", got, err, want)
			}
			w.WriteHeader(http.StatusCreated)
		case req.Method == http.MethodGet && req.URL.Path == versionPath+"/1":
		default:
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(responseJSON))
	}))

	ctx := context.Background()
	categories, diagnostics := types.ListValueFrom(ctx, types.StringType, []string{"safety"})
	if diagnostics.HasError() {
		t.Fatalf("build categories: %v", diagnostics)
	}
	plan := evaluatorVersionModel{
		Name:        types.StringValue("reservation-safety"),
		Type:        types.StringValue("rubric"),
		Categories:  categories,
		ScoringMode: types.StringValue("boolean"),
		Metrics:     types.MapUnknown(types.StringType),
		Dimensions: []evaluatorDimensionModel{
			{ID: types.StringValue("privacy"), Description: types.StringValue("Discloses another guest's details."), Allowed: types.BoolValue(false), AlwaysApplicable: types.BoolValue(true)},
			{ID: types.StringValue("booking"), Description: types.StringValue("Helps with a valid booking."), Allowed: types.BoolValue(true), AlwaysApplicable: types.BoolValue(false)},
			{ID: types.StringValue("cancellation"), Description: types.StringValue("Explains the cancellation policy."), Allowed: types.BoolValue(true), AlwaysApplicable: types.BoolUnknown()},
		},
	}

	gate := previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}
	r := &evaluatorVersionResource{previewGate: gate}
	var schemaResponse resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResponse)
	planState := tfsdk.State{Schema: schemaResponse.Schema}
	if diags := planState.Set(ctx, &plan); diags.HasError() {
		t.Fatalf("build plan: %v", diags)
	}
	create := resource.CreateResponse{State: tfsdk.State{Schema: schemaResponse.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Raw: planState.Raw, Schema: schemaResponse.Schema}}, &create)
	if create.Diagnostics.HasError() {
		t.Fatalf("create: %v", create.Diagnostics)
	}
	var created evaluatorVersionModel
	if diags := create.State.Get(ctx, &created); diags.HasError() {
		t.Fatalf("read created state: %v", diags)
	}
	plan.Dimensions[2].AlwaysApplicable = types.BoolValue(false)
	if !created.Categories.Equal(categories) || !created.ScoringMode.Equal(plan.ScoringMode) || !reflect.DeepEqual(created.Dimensions, plan.Dimensions) {
		t.Fatalf("unexpected rubric state: %+v", created)
	}
	metrics := created.Metrics.Elements()
	if len(metrics) != 2 || metrics["allowed_behavior_violated"] == nil || metrics["disallowed_behavior_violated"] == nil {
		t.Fatalf("unexpected service metrics: %v", metrics)
	}

	ds := &evaluatorVersionDataSource{previewGate: gate}
	config := evaluatorVersionModel{
		Name:       plan.Name,
		Version:    types.StringValue("1"),
		Categories: types.ListNull(types.StringType),
		Metrics:    types.MapNull(types.StringType),
	}
	read, diags := readAssetDataSource(t, ds, config)
	if diags.HasError() || !reflect.DeepEqual(read, created) {
		t.Fatalf("data source state = %+v, diagnostics = %v; want %+v", read, diags, created)
	}
}

func TestEvaluatorVersionLegacyRubricOmitsSafetyFields(t *testing.T) {
	t.Parallel()

	model := evaluatorVersionModel{
		Type: types.StringValue("rubric"),
		Dimensions: []evaluatorDimensionModel{
			{ID: types.StringValue("accuracy"), Description: types.StringValue("Gives an accurate answer."), Weight: types.Float64Value(2)},
		},
	}
	var diagnostics diag.Diagnostics
	request := model.request(context.Background(), &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("build request: %v", diagnostics)
	}
	got, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"definition":{"type":"rubric","dimensions":[{"id":"accuracy","description":"Gives an accurate answer.","weight":2}]}}`
	if string(got) != want {
		t.Errorf("legacy rubric request = %s; want %s", got, want)
	}
}

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
