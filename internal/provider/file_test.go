package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestFileResponseApply(t *testing.T) {
	t.Parallel()

	encoded := `{"object":"file","id":"assistant-1","purpose":"assistants","filename":"probe.txt","bytes":14,"created_at":1787896146,"status":"processed"}`
	var response fileResponse
	if err := json.Unmarshal([]byte(encoded), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	var model fileModel
	model.apply(response)

	if model.ID.ValueString() != "assistant-1" {
		t.Errorf("id = %q", model.ID.ValueString())
	}
	if model.Bytes.ValueInt64() != 14 {
		t.Errorf("bytes = %d", model.Bytes.ValueInt64())
	}
	if model.CreatedAt.ValueInt64() != 1787896146 {
		t.Errorf("created_at = %d", model.CreatedAt.ValueInt64())
	}
	if model.Status.ValueString() != "processed" {
		t.Errorf("status = %q", model.Status.ValueString())
	}
}

func TestVectorStoreRequestRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	metadata, diags := types.MapValueFrom(ctx, types.StringType, map[string]string{"team": "support"})
	if diags.HasError() {
		t.Fatalf("build map: %v", diags)
	}

	model := vectorStoreModel{
		Name:             types.StringValue("tfprobe-vs"),
		Metadata:         metadata,
		ExpiresAfterDays: types.Int64Value(30),
	}

	var diagnostics diag.Diagnostics
	encoded, err := json.Marshal(model.request(ctx, &diagnostics))
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if diagnostics.HasError() {
		t.Fatalf("request diagnostics: %v", diagnostics)
	}

	var request vectorStoreRequest
	if err := json.Unmarshal(encoded, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if request.ExpiresAfter == nil || request.ExpiresAfter.Anchor != "last_active_at" || request.ExpiresAfter.Days != 30 {
		t.Errorf("expires_after = %+v", request.ExpiresAfter)
	}
	if request.Metadata["team"] != "support" {
		t.Errorf("metadata = %+v", request.Metadata)
	}
}

func TestVectorStoreRequestOmitsExpiry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	model := vectorStoreModel{
		Name:             types.StringValue("tfprobe-vs"),
		Metadata:         types.MapNull(types.StringType),
		ExpiresAfterDays: types.Int64Null(),
	}

	var diagnostics diag.Diagnostics
	encoded, err := json.Marshal(model.request(ctx, &diagnostics))
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if _, present := raw["expires_after"]; present {
		t.Error("expires_after should be omitted when unset")
	}
	if _, present := raw["metadata"]; present {
		t.Error("metadata should be omitted when null")
	}
}

func TestVectorStoreApplyEmptyMetadataIsNull(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	encoded := `{"id":"vs_1","object":"vector_store","name":"","status":"completed","usage_bytes":0,"created_at":1787896148,"file_counts":{"total":0},"metadata":{}}`
	var response vectorStoreResponse
	if err := json.Unmarshal([]byte(encoded), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	var diagnostics diag.Diagnostics
	var model vectorStoreModel
	model.apply(ctx, response, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("apply diagnostics: %v", diagnostics)
	}

	if !model.Metadata.IsNull() {
		t.Error("empty metadata should be null")
	}
	// An absent optional name must stay null so Terraform does not report a permanent diff.
	if !model.Name.IsNull() {
		t.Error("empty name should be null")
	}
	if model.FileCount.ValueInt64() != 0 {
		t.Errorf("file_count = %d", model.FileCount.ValueInt64())
	}
}

func TestVectorStoreFileResponseApply(t *testing.T) {
	t.Parallel()

	encoded := `{"id":"assistant-1","object":"vector_store.file","usage_bytes":0,"created_at":1787896156,"vector_store_id":"vs_1","status":"in_progress"}`
	var response vectorStoreFileResponse
	if err := json.Unmarshal([]byte(encoded), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	var model vectorStoreFileModel
	model.apply(response)
	if model.ID.ValueString() != "assistant-1" || model.Status.ValueString() != "in_progress" {
		t.Errorf("model = %+v", model)
	}
}

func TestSplitVectorStoreFileID(t *testing.T) {
	t.Parallel()

	vectorStoreID, fileID, err := splitVectorStoreFileID("vs_abc/assistant-xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vectorStoreID != "vs_abc" || fileID != "assistant-xyz" {
		t.Errorf("got %q, %q", vectorStoreID, fileID)
	}

	for _, invalid := range []string{"vs_abc", "vs_abc/", "/assistant-xyz", ""} {
		if _, _, err := splitVectorStoreFileID(invalid); err == nil {
			t.Errorf("expected error for %q", invalid)
		}
	}
}

// The vector store update endpoint echoes a new name, metadata, or expiry
// without persisting it, so those attributes must force replacement instead.
func TestVectorStoreConfigurableAttributesForceReplacement(t *testing.T) {
	t.Parallel()

	var response resource.SchemaResponse
	(&vectorStoreResource{}).Schema(context.Background(), resource.SchemaRequest{}, &response)

	for _, name := range []string{"name", "metadata", "expires_after_days"} {
		attribute, ok := response.Schema.Attributes[name]
		if !ok {
			t.Fatalf("attribute %q is missing", name)
		}
		if !requiresReplace(attribute) {
			t.Errorf("attribute %q must force replacement", name)
		}
	}
}

func requiresReplace(attribute schema.Attribute) bool {
	switch typed := attribute.(type) {
	case schema.StringAttribute:
		return len(typed.PlanModifiers) > 0
	case schema.MapAttribute:
		return len(typed.PlanModifiers) > 0
	case schema.Int64Attribute:
		return len(typed.PlanModifiers) > 0
	default:
		return false
	}
}
