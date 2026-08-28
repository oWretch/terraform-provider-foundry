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

func TestEvaluationTaxonomyRequestShape(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	riskCategories, diags := types.ListValueFrom(ctx, types.StringType, []string{"ProhibitedActions"})
	if diags.HasError() {
		t.Fatalf("build risk categories list: %v", diags)
	}

	model := evaluationTaxonomyModel{
		Name:           types.StringValue("my-taxonomy"),
		AgentName:      types.StringValue("my-agent"),
		AgentVersion:   types.StringValue("1"),
		RiskCategories: riskCategories,
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
	taxonomyInput, ok := decoded["taxonomyInput"].(map[string]any)
	if !ok {
		t.Fatalf("expected taxonomyInput object, got %v", decoded["taxonomyInput"])
	}
	if taxonomyInput["type"] != "Agent" {
		t.Errorf("type = %v, want Agent", taxonomyInput["type"])
	}
	target, ok := taxonomyInput["target"].(map[string]any)
	if !ok {
		t.Fatalf("expected target object, got %v", taxonomyInput["target"])
	}
	if target["name"] != "my-agent" {
		t.Errorf("target.name = %v", target["name"])
	}
	riskCats, ok := taxonomyInput["riskCategories"].([]any)
	if !ok || len(riskCats) != 1 || riskCats[0] != "ProhibitedActions" {
		t.Errorf("riskCategories = %v", taxonomyInput["riskCategories"])
	}
}

// The service returns categories/subcategories in a stable, canonical order
// for a given risk-category selection; the model must preserve that order
// rather than re-sort, so plans stay deterministic across apply.
func TestEvaluationTaxonomyApplyPreservesCategoryOrder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	response := evaluationTaxonomyResponse{
		ID:      "azureai://accounts/a/projects/p/evaluationtaxonomies/my-taxonomy/versions/1.0",
		Name:    "my-taxonomy",
		Version: "1.0",
		TaxonomyCategories: []evaluationTaxonomyCategoryResponse{
			{ID: "cat-b", Name: "B"},
			{ID: "cat-a", Name: "A"},
		},
	}

	var model evaluationTaxonomyModel
	var diagnostics diag.Diagnostics
	model.apply(ctx, response, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("apply diagnostics: %v", diagnostics)
	}

	if len(model.Categories) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(model.Categories))
	}
	if model.Categories[0].ID.ValueString() != "cat-b" || model.Categories[1].ID.ValueString() != "cat-a" {
		t.Errorf("category order not preserved: %v", model.Categories)
	}
}

func TestEvaluationTaxonomyResourceRequiresPreviewFeature(t *testing.T) {
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

	r := &evaluationTaxonomyResource{}
	r.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}

	var resp resource.ValidateConfigResponse
	r.ValidateResourceConfig(context.Background(), resource.ValidateConfigRequest{}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error when evaluations preview is not enabled")
	}
}

func TestEvaluationTaxonomyResourceAllowsEnabledPreviewFeature(t *testing.T) {
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

	r := &evaluationTaxonomyResource{}
	r.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}

	var resp resource.ValidateConfigResponse
	r.ValidateResourceConfig(context.Background(), resource.ValidateConfigRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected error when evaluations preview is enabled: %v", resp.Diagnostics)
	}
}

func TestEvaluationTaxonomyDataSourceRequiresPreviewFeature(t *testing.T) {
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

	d := &evaluationTaxonomyDataSource{}
	d.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}

	var resp datasource.ValidateConfigResponse
	d.ValidateDataSourceConfig(context.Background(), datasource.ValidateConfigRequest{}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error when evaluations preview is not enabled")
	}
}
