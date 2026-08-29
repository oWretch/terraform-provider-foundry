package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

func TestToolboxModelApplyVersionRoundTrip(t *testing.T) {
	t.Parallel()

	response := toolboxVersionResponse{
		ID:          "toolboxver_abc123",
		Name:        "my-toolbox",
		Version:     "1",
		Description: "Toolbox with a skill reference",
		Tools:       []toolRequest{{Type: "web_search"}},
		Skills:      []toolboxSkillReference{{Type: "skill_reference", Name: "greeting"}},
	}

	var model toolboxModel
	model.applyVersion(response)

	if model.ID.ValueString() != "toolboxver_abc123" {
		t.Errorf("id = %q", model.ID.ValueString())
	}
	if model.Version.ValueString() != "1" {
		t.Errorf("version = %q", model.Version.ValueString())
	}
	if model.Description.ValueString() != "Toolbox with a skill reference" {
		t.Errorf("description = %q", model.Description.ValueString())
	}
	if len(model.Tools) != 1 || model.Tools[0].Type.ValueString() != "web_search" {
		t.Errorf("tools = %+v", model.Tools)
	}
	if len(model.Skills) != 1 || model.Skills[0].Name.ValueString() != "greeting" {
		t.Fatalf("skills = %+v", model.Skills)
	}
	if !model.Skills[0].Version.IsNull() {
		t.Error("unpinned skill reference version should be null")
	}
}

func TestToolboxModelApplyVersionEmptyDescriptionIsNull(t *testing.T) {
	t.Parallel()

	var model toolboxModel
	model.applyVersion(toolboxVersionResponse{ID: "v1", Version: "1"})

	if !model.Description.IsNull() {
		t.Error("empty description should be null")
	}
	if model.Skills != nil {
		t.Error("empty skills should be nil")
	}
}

func TestToolboxModelSkillReferencesEncodesPinnedVersion(t *testing.T) {
	t.Parallel()

	model := toolboxModel{
		Skills: []toolboxSkillReferenceModel{
			{Name: types.StringValue("greeting"), Version: types.StringValue("2")},
			{Name: types.StringValue("other"), Version: types.StringNull()},
		},
	}
	references := model.skillReferences()
	if len(references) != 2 {
		t.Fatalf("expected 2 references, got %d", len(references))
	}
	if references[0].Type != "skill_reference" || references[0].Name != "greeting" || references[0].Version != "2" {
		t.Errorf("pinned reference = %+v", references[0])
	}
	if references[1].Version != "" {
		t.Errorf("unpinned reference should have an empty version, got %q", references[1].Version)
	}

	encoded, err := json.Marshal(references[1])
	if err != nil {
		t.Fatalf("marshal reference: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal reference: %v", err)
	}
	if _, present := decoded["version"]; present {
		t.Error("unpinned skill reference should omit version, not send an empty string")
	}
}

func TestToolboxModelSkillReferencesNilWhenEmpty(t *testing.T) {
	t.Parallel()

	var model toolboxModel
	if model.skillReferences() != nil {
		t.Error("expected nil skill references for an empty skills list")
	}
}

func TestToolboxResourceRequiresPreviewFeature(t *testing.T) {
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

	r := &toolboxResource{}
	r.client = client
	r.feature = toolboxesFeature
	r.name = "toolboxes"

	var resp resource.ValidateConfigResponse
	r.ValidateResourceConfig(context.Background(), resource.ValidateConfigRequest{}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error when toolboxes preview is not enabled")
	}
}

func TestToolboxResourceAllowsEnabledPreviewFeature(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName:     "acct",
		ProjectName:     "proj",
		Environment:     clients.EnvironmentPublic,
		Auth:            clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
		PreviewFeatures: []string{toolboxesFeatureName},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	r := &toolboxResource{}
	r.client = client
	r.feature = toolboxesFeature
	r.name = "toolboxes"

	var resp resource.ValidateConfigResponse
	r.ValidateResourceConfig(context.Background(), resource.ValidateConfigRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected error when toolboxes preview is enabled: %v", resp.Diagnostics)
	}
}

func TestToolboxDataSourceRequiresPreviewFeature(t *testing.T) {
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

	d := &toolboxDataSource{}
	d.client = client
	d.feature = toolboxesFeature
	d.name = "toolboxes"

	var resp datasource.ValidateConfigResponse
	d.ValidateDataSourceConfig(context.Background(), datasource.ValidateConfigRequest{}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error when toolboxes preview is not enabled")
	}
}

func TestToolboxDataSourceAllowsEnabledPreviewFeature(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName:     "acct",
		ProjectName:     "proj",
		Environment:     clients.EnvironmentPublic,
		Auth:            clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
		PreviewFeatures: []string{toolboxesFeatureName},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	d := &toolboxDataSource{}
	d.client = client
	d.feature = toolboxesFeature
	d.name = "toolboxes"

	var resp datasource.ValidateConfigResponse
	d.ValidateDataSourceConfig(context.Background(), datasource.ValidateConfigRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected error when toolboxes preview is enabled: %v", resp.Diagnostics)
	}
}
