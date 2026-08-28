package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

func TestScheduleRequestCronRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	model := scheduleModel{
		ID:          types.StringValue("nightly-evaluation"),
		DisplayName: types.StringValue("Nightly evaluation"),
		Enabled:     types.BoolValue(true),
		Trigger: scheduleTriggerModel{
			Type:           types.StringValue("Cron"),
			CronExpression: types.StringValue("0 0 * * *"),
			TimeZone:       types.StringValue("UTC"),
		},
		Task: scheduleTaskModel{
			Type:          types.StringValue("Evaluation"),
			EvaluationID:  types.StringValue("eval-123"),
			EvaluationRun: types.StringValue(`{}`),
		},
		Tags:       types.MapNull(types.StringType),
		Properties: types.MapNull(types.StringType),
	}

	var diagnostics diag.Diagnostics
	request := model.request(ctx, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("request diagnostics: %v", diagnostics)
	}

	if request.Trigger.Type != "Cron" {
		t.Errorf("trigger type = %q", request.Trigger.Type)
	}
	if request.Trigger.Expression != "0 0 * * *" {
		t.Errorf("cron expression = %q", request.Trigger.Expression)
	}
	if request.Task.Type != "Evaluation" {
		t.Errorf("task type = %q", request.Task.Type)
	}
	if request.Task.EvalID != "eval-123" {
		t.Errorf("evalId = %q", request.Task.EvalID)
	}
	if string(request.Task.EvalRun) != "{}" {
		t.Errorf("evalRun = %q", request.Task.EvalRun)
	}
}

func TestScheduleRequestRecurrenceRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	hours, diags := types.ListValueFrom(ctx, types.Int64Type, []int64{9, 17})
	if diags.HasError() {
		t.Fatalf("build hours: %v", diags)
	}

	model := scheduleModel{
		ID:      types.StringValue("s1"),
		Enabled: types.BoolValue(true),
		Trigger: scheduleTriggerModel{
			Type:           types.StringValue("Recurrence"),
			Interval:       types.Int64Value(1),
			RecurrenceType: types.StringValue("Daily"),
			Hours:          hours,
		},
		Task: scheduleTaskModel{
			Type:          types.StringValue("Evaluation"),
			EvaluationID:  types.StringValue("eval-1"),
			EvaluationRun: types.StringValue(`{}`),
		},
		Tags:       types.MapNull(types.StringType),
		Properties: types.MapNull(types.StringType),
	}

	var diagnostics diag.Diagnostics
	request := model.request(ctx, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("request diagnostics: %v", diagnostics)
	}

	if request.Trigger.Interval != 1 {
		t.Errorf("interval = %d", request.Trigger.Interval)
	}
	if request.Trigger.Schedule == nil {
		t.Fatal("schedule was not set")
	}
	if request.Trigger.Schedule.Type != "Daily" {
		t.Errorf("recurrence type = %q", request.Trigger.Schedule.Type)
	}
	if len(request.Trigger.Schedule.Hours) != 2 || request.Trigger.Schedule.Hours[0] != 9 {
		t.Errorf("hours = %v", request.Trigger.Schedule.Hours)
	}
}

func TestScheduleApplyRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	response := scheduleResponse{
		ID:                 "s1",
		DisplayName:        "Nightly evaluation",
		Enabled:            true,
		ProvisioningStatus: "Succeeded",
		Trigger: scheduleTriggerRequest{
			Type:       "Cron",
			Expression: "0 0 * * *",
			TimeZone:   "UTC",
		},
		Task: scheduleTaskRequest{
			Type:    "Evaluation",
			EvalID:  "eval-123",
			EvalRun: []byte(`{}`),
		},
		Tags: map[string]string{"team": "quality"},
	}

	var model scheduleModel
	var diagnostics diag.Diagnostics
	model.apply(ctx, response, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("apply diagnostics: %v", diagnostics)
	}

	if model.ID.ValueString() != "s1" {
		t.Errorf("id = %q", model.ID.ValueString())
	}
	if model.ProvisioningStatus.ValueString() != "Succeeded" {
		t.Errorf("provisioning_status = %q", model.ProvisioningStatus.ValueString())
	}
	if model.Trigger.CronExpression.ValueString() != "0 0 * * *" {
		t.Errorf("cron_expression = %q", model.Trigger.CronExpression.ValueString())
	}
	if model.Task.EvaluationRun.ValueString() != "{}" {
		t.Errorf("evaluation_run = %q", model.Task.EvaluationRun.ValueString())
	}
	if model.Tags.IsNull() {
		t.Error("tags should not be null")
	}
	if !model.Properties.IsNull() {
		t.Error("empty properties should be null")
	}
	if !model.Trigger.RecurrenceType.IsNull() {
		t.Error("recurrence_type should be null for a Cron trigger")
	}
}

func TestScheduleSchemaAttributes(t *testing.T) {
	t.Parallel()

	r := NewScheduleResource()
	var schemaResp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", schemaResp.Diagnostics)
	}

	for _, name := range []string{"id", "display_name", "description", "enabled", "trigger", "task", "tags", "properties", "provisioning_status"} {
		if _, ok := schemaResp.Schema.Attributes[name]; !ok {
			t.Errorf("schema missing attribute %q", name)
		}
	}
}

func TestScheduleValidateConfig(t *testing.T) {
	t.Parallel()

	enabledClient, err := clients.New(clients.Config{
		AccountName:     "example",
		ProjectName:     "demo",
		Environment:     clients.EnvironmentPublic,
		PreviewFeatures: []string{schedulesPreviewFeatureName},
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

			r := &scheduleResource{previewGate: previewGate{client: tt.client, feature: schedulesPreviewFeature, name: schedulesPreviewFeatureName}}
			resp := &resource.ValidateConfigResponse{}
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config{}}, resp)

			if resp.Diagnostics.HasError() != tt.wantError {
				t.Errorf("HasError() = %v, want %v (%v)", resp.Diagnostics.HasError(), tt.wantError, resp.Diagnostics)
			}
		})
	}
}

func TestScheduleDataSourceValidateConfig(t *testing.T) {
	t.Parallel()

	enabledClient, err := clients.New(clients.Config{
		AccountName:     "example",
		ProjectName:     "demo",
		Environment:     clients.EnvironmentPublic,
		PreviewFeatures: []string{schedulesPreviewFeatureName},
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

			d := &scheduleDataSource{previewGate: previewGate{client: tt.client, feature: schedulesPreviewFeature, name: schedulesPreviewFeatureName}}
			resp := &datasource.ValidateConfigResponse{}
			d.ValidateConfig(context.Background(), datasource.ValidateConfigRequest{Config: tfsdk.Config{}}, resp)

			if resp.Diagnostics.HasError() != tt.wantError {
				t.Errorf("HasError() = %v, want %v (%v)", resp.Diagnostics.HasError(), tt.wantError, resp.Diagnostics)
			}
		})
	}
}
