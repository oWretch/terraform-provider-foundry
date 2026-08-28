package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

func TestRoutineRequestRoundTrip(t *testing.T) {
	t.Parallel()

	model := routineModel{
		Name:        types.StringValue("daily-summary"),
		Description: types.StringValue("test routine"),
		Enabled:     types.BoolValue(true),
		Trigger: routineTriggerModel{
			Type:           types.StringValue("schedule"),
			CronExpression: types.StringValue("0 7 * * 1-5"),
			TimeZone:       types.StringValue("UTC"),
		},
		ActionType: types.StringValue("invoke_agent_responses_api"),
		AgentName:  types.StringValue("my-agent"),
		Input:      types.StringValue("Summarize."),
	}

	request := model.request()
	if request.Enabled != true {
		t.Errorf("enabled = %v, want true", request.Enabled)
	}
	trigger, ok := request.Triggers[routineTriggerName]
	if !ok {
		t.Fatalf("trigger key %q missing", routineTriggerName)
	}
	if trigger.Type != "schedule" {
		t.Errorf("trigger type = %q", trigger.Type)
	}
	if trigger.CronExpression != "0 7 * * 1-5" {
		t.Errorf("cron_expression = %q", trigger.CronExpression)
	}
	if request.Action.AgentName != "my-agent" {
		t.Errorf("agent_name = %q", request.Action.AgentName)
	}
}

func TestRoutineApplyRoundTrip(t *testing.T) {
	t.Parallel()

	response := routineResponse{
		Name:        "daily-summary",
		Description: "test routine",
		Enabled:     true,
		Triggers: map[string]routineTriggerRequest{
			routineTriggerName: {Type: "timer", At: "2026-09-01T09:00:00Z"},
		},
		Action: routineActionRequest{
			Type:      "invoke_agent_invocations_api",
			AgentName: "my-agent",
			SessionID: "session-1",
		},
		CreatedAt: 1000,
		UpdatedAt: 2000,
	}

	var model routineModel
	model.apply(response)

	if model.Name.ValueString() != "daily-summary" {
		t.Errorf("name = %q", model.Name.ValueString())
	}
	if model.Trigger.Type.ValueString() != "timer" {
		t.Errorf("trigger type = %q", model.Trigger.Type.ValueString())
	}
	if model.Trigger.At.ValueString() != "2026-09-01T09:00:00Z" {
		t.Errorf("trigger at = %q", model.Trigger.At.ValueString())
	}
	if model.SessionID.ValueString() != "session-1" {
		t.Errorf("session_id = %q", model.SessionID.ValueString())
	}
	if model.CreatedAt.ValueInt64() != 1000 {
		t.Errorf("created_at = %d", model.CreatedAt.ValueInt64())
	}
}

func TestRoutineApplyEmptyDescriptionIsNull(t *testing.T) {
	t.Parallel()

	var model routineModel
	model.apply(routineResponse{
		Name: "r",
		Triggers: map[string]routineTriggerRequest{
			routineTriggerName: {Type: "timer", At: "2026-01-01T00:00:00Z"},
		},
		Action: routineActionRequest{Type: "invoke_agent_responses_api", AgentName: "a"},
	})

	if !model.Description.IsNull() {
		t.Error("empty description should be null")
	}
	if !model.Input.IsNull() {
		t.Error("empty input should be null")
	}
	if !model.SessionID.IsNull() {
		t.Error("empty session_id should be null")
	}
}

func TestRoutineSchemaAttributes(t *testing.T) {
	t.Parallel()

	r := NewRoutineResource()
	var schemaResp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", schemaResp.Diagnostics)
	}

	for _, name := range []string{"name", "description", "enabled", "trigger", "action_type", "agent_name", "input", "session_id", "created_at", "updated_at"} {
		if _, ok := schemaResp.Schema.Attributes[name]; !ok {
			t.Errorf("schema missing attribute %q", name)
		}
	}
}

func TestRoutineValidateConfig(t *testing.T) {
	t.Parallel()

	enabledClient, err := clients.New(clients.Config{
		AccountName:     "example",
		ProjectName:     "demo",
		Environment:     clients.EnvironmentPublic,
		PreviewFeatures: []string{routinesPreviewFeatureName},
		Auth:            clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "secret"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	disabledClient, err := clients.New(clients.Config{
		AccountName: "example",
		ProjectName: "demo",
		Environment: clients.EnvironmentPublic,
		Auth:        clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "secret"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name      string
		client    *clients.Client
		wantError bool
	}{
		{name: "enabled", client: enabledClient, wantError: false},
		{name: "disabled", client: disabledClient, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &routineResource{previewGate: previewGate{client: tt.client, feature: routinesPreviewFeature, name: routinesPreviewFeatureName}}
			resp := &resource.ValidateConfigResponse{}
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config{}}, resp)

			if resp.Diagnostics.HasError() != tt.wantError {
				t.Errorf("HasError() = %v, want %v (%v)", resp.Diagnostics.HasError(), tt.wantError, resp.Diagnostics)
			}
		})
	}
}

func TestRoutineDataSourceValidateConfig(t *testing.T) {
	t.Parallel()

	enabledClient, err := clients.New(clients.Config{
		AccountName:     "example",
		ProjectName:     "demo",
		Environment:     clients.EnvironmentPublic,
		PreviewFeatures: []string{routinesPreviewFeatureName},
		Auth:            clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "secret"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	disabledClient, err := clients.New(clients.Config{
		AccountName: "example",
		ProjectName: "demo",
		Environment: clients.EnvironmentPublic,
		Auth:        clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "secret"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name      string
		client    *clients.Client
		wantError bool
	}{
		{name: "enabled", client: enabledClient, wantError: false},
		{name: "disabled", client: disabledClient, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := &routineDataSource{previewGate: previewGate{client: tt.client, feature: routinesPreviewFeature, name: routinesPreviewFeatureName}}
			resp := &datasource.ValidateConfigResponse{}
			d.ValidateConfig(context.Background(), datasource.ValidateConfigRequest{Config: tfsdk.Config{}}, resp)

			if resp.Diagnostics.HasError() != tt.wantError {
				t.Errorf("HasError() = %v, want %v (%v)", resp.Diagnostics.HasError(), tt.wantError, resp.Diagnostics)
			}
		})
	}
}
