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

func TestSkillModelApplyVersionRoundTrip(t *testing.T) {
	t.Parallel()

	response := skillVersionResponse{
		ID:          "skillver_abc123",
		SkillID:     "skill_abc123",
		Name:        "greeting",
		Version:     "1",
		Description: "Generate a personalized greeting for the user.",
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var decoded skillVersionResponse
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	var model skillModel
	model.applyVersion(decoded)

	if model.ID.ValueString() != "skillver_abc123" {
		t.Errorf("id = %q", model.ID.ValueString())
	}
	if model.SkillID.ValueString() != "skill_abc123" {
		t.Errorf("skill_id = %q", model.SkillID.ValueString())
	}
	if model.Version.ValueString() != "1" {
		t.Errorf("version = %q", model.Version.ValueString())
	}
	if model.Description.ValueString() != "Generate a personalized greeting for the user." {
		t.Errorf("description = %q", model.Description.ValueString())
	}
}

// An absent description must stay null so Terraform does not report a
// permanent diff against an absent API field, matching optionalString's
// contract used across every versioned resource in this provider.
func TestSkillModelApplyVersionEmptyDescriptionIsNull(t *testing.T) {
	t.Parallel()

	var model skillModel
	model.applyVersion(skillVersionResponse{ID: "v1", SkillID: "s1", Version: "1"})

	if !model.Description.IsNull() {
		t.Error("empty description should be null")
	}
}

func TestSkillInlineContentRequestOmitsEmptyFields(t *testing.T) {
	t.Parallel()

	request := skillVersionRequest{
		InlineContent: &skillInlineContent{
			Description:  "desc",
			Instructions: "",
		},
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	inline, ok := decoded["inline_content"].(map[string]any)
	if !ok {
		t.Fatalf("expected inline_content object, got %v", decoded["inline_content"])
	}
	if _, present := inline["instructions"]; present {
		t.Error("empty instructions should be omitted, not sent as an empty string")
	}
}

func TestSkillNamePatternValidatesServiceRules(t *testing.T) {
	t.Parallel()

	valid := []string{"greeting", "a", "a1", "my-skill", "skill-2"}
	for _, name := range valid {
		if !skillNamePattern.MatchString(name) {
			t.Errorf("expected %q to be a valid skill name", name)
		}
	}

	invalid := []string{"-leading", "trailing-", "Upper", "under_score", "double--hyphen", ""}
	for _, name := range invalid {
		if skillNamePattern.MatchString(name) {
			t.Errorf("expected %q to be an invalid skill name", name)
		}
	}
}

func TestSkillResourceRequiresPreviewFeature(t *testing.T) {
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

	r := &skillResource{}
	r.client = client
	r.feature = skillsFeature
	r.name = "skills"

	var resp resource.ValidateConfigResponse
	r.ValidateResourceConfig(context.Background(), resource.ValidateConfigRequest{}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error when skills preview is not enabled")
	}
}

func TestSkillResourceAllowsEnabledPreviewFeature(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName:     "acct",
		ProjectName:     "proj",
		Environment:     clients.EnvironmentPublic,
		Auth:            clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
		PreviewFeatures: []string{skillsFeatureName},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	r := &skillResource{}
	r.client = client
	r.feature = skillsFeature
	r.name = "skills"

	var resp resource.ValidateConfigResponse
	r.ValidateResourceConfig(context.Background(), resource.ValidateConfigRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected error when skills preview is enabled: %v", resp.Diagnostics)
	}
}

func TestSkillDataSourceRequiresPreviewFeature(t *testing.T) {
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

	d := &skillDataSource{}
	d.client = client
	d.feature = skillsFeature
	d.name = "skills"

	var resp datasource.ValidateConfigResponse
	d.ValidateDataSourceConfig(context.Background(), datasource.ValidateConfigRequest{}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error when skills preview is not enabled")
	}
}

func TestSkillDataSourceAllowsEnabledPreviewFeature(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName:     "acct",
		ProjectName:     "proj",
		Environment:     clients.EnvironmentPublic,
		Auth:            clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
		PreviewFeatures: []string{skillsFeatureName},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	d := &skillDataSource{}
	d.client = client
	d.feature = skillsFeature
	d.name = "skills"

	var resp datasource.ValidateConfigResponse
	d.ValidateDataSourceConfig(context.Background(), datasource.ValidateConfigRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected error when skills preview is enabled: %v", resp.Diagnostics)
	}
}

// A no-op sanity check that optionalString round-trips through skillModel's
// DefaultVersion/Version fields the same way other versioned resources use
// types.String, guarding against an accidental switch to a pointer type.
func TestSkillModelDefaultVersionIsPlainString(t *testing.T) {
	t.Parallel()

	model := skillModel{DefaultVersion: types.StringValue("2")}
	if model.DefaultVersion.ValueString() != "2" {
		t.Errorf("default_version = %q", model.DefaultVersion.ValueString())
	}
}
