package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestDatasetRequestRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tags, diags := types.MapValueFrom(ctx, types.StringType, map[string]string{"env": "test"})
	if diags.HasError() {
		t.Fatalf("build map: %v", diags)
	}

	model := datasetModel{
		Name:           types.StringValue("tfprobe-ds"),
		Version:        types.StringValue("1"),
		Type:           types.StringValue("uri_file"),
		ConnectionName: types.StringValue("storage"),
		DataURI:        types.StringValue("https://example.blob.core.windows.net/knowledge/probe.txt"),
		Description:    types.StringValue("probe dataset"),
		Tags:           tags,
	}

	var diagnostics diag.Diagnostics
	encoded, err := json.Marshal(model.request(ctx, &diagnostics))
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if diagnostics.HasError() {
		t.Fatalf("request diagnostics: %v", diagnostics)
	}

	var request datasetRequest
	if err := json.Unmarshal(encoded, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if request.Type != "uri_file" || request.ConnectionName != "storage" {
		t.Errorf("request = %+v", request)
	}
	if request.Tags["env"] != "test" {
		t.Errorf("tags = %+v", request.Tags)
	}
}

func TestDatasetRequestOmitsTags(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	model := datasetModel{
		Name:           types.StringValue("tfprobe-ds"),
		Version:        types.StringValue("1"),
		Type:           types.StringValue("uri_folder"),
		ConnectionName: types.StringValue("storage"),
		DataURI:        types.StringValue("https://example.blob.core.windows.net/knowledge/"),
		Description:    types.StringNull(),
		Tags:           types.MapNull(types.StringType),
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
	if _, present := raw["tags"]; present {
		t.Error("tags should be omitted when null")
	}
	if _, present := raw["description"]; present {
		t.Error("description should be omitted when null")
	}
}

func TestDatasetApplyEmptyTagsIsNull(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	encoded := `{"id":"azureai://accounts/x/data/tfprobe-ds/versions/1","name":"tfprobe-ds","version":"1","displayName":"tfprobe-ds","description":"","tags":{},"type":"uri_file","dataUri":"https://example.blob.core.windows.net/knowledge/probe.txt","isSingleFile":true,"connectionName":"storage","systemData":{"createdAt":"2026-01-01T00:00:00Z","lastModifiedAt":"2026-01-02T00:00:00Z"}}`
	var response datasetResponse
	if err := json.Unmarshal([]byte(encoded), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	var diagnostics diag.Diagnostics
	var model datasetModel
	model.apply(ctx, response, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("apply diagnostics: %v", diagnostics)
	}

	if !model.Tags.IsNull() {
		t.Error("empty tags should be null")
	}
	// An absent optional description must stay null so Terraform does not report a permanent diff.
	if !model.Description.IsNull() {
		t.Error("empty description should be null")
	}
	if !model.IsSingleFile.ValueBool() {
		t.Error("is_single_file should be true")
	}
	if model.CreatedAt.ValueString() != "2026-01-01T00:00:00Z" {
		t.Errorf("created_at = %q", model.CreatedAt.ValueString())
	}
	if model.ID.ValueString() != "azureai://accounts/x/data/tfprobe-ds/versions/1" {
		t.Errorf("id = %q", model.ID.ValueString())
	}
}

func TestIndexRequestRoundTrip(t *testing.T) {
	t.Parallel()

	model := indexModel{
		Name:           types.StringValue("tfprobe-idx"),
		Version:        types.StringValue("1"),
		Type:           types.StringValue("AzureSearch"),
		ConnectionName: types.StringValue("azure-ai-search"),
		IndexName:      types.StringValue("tfprobe-search-index"),
	}

	encoded, err := json.Marshal(model.request())
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var request indexRequest
	if err := json.Unmarshal(encoded, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if request.Type != "AzureSearch" || request.ConnectionName != "azure-ai-search" {
		t.Errorf("request = %+v", request)
	}
	if request.IndexName != "tfprobe-search-index" || request.Name != "tfprobe-idx" || request.Version != "1" {
		t.Errorf("request = %+v", request)
	}
}

func TestIndexApplyComposesID(t *testing.T) {
	t.Parallel()

	encoded := `{"type":"AzureSearch","connectionName":"azure-ai-search","indexName":"tfprobe-search-index","name":"tfprobe-idx","version":"1"}`
	var response indexResponse
	if err := json.Unmarshal([]byte(encoded), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	var model indexModel
	model.apply(response)
	if model.ID.ValueString() != "tfprobe-idx:1" {
		t.Errorf("id = %q", model.ID.ValueString())
	}
	if model.IndexName.ValueString() != "tfprobe-search-index" {
		t.Errorf("index_name = %q", model.IndexName.ValueString())
	}
}

func TestSplitNameVersion(t *testing.T) {
	t.Parallel()

	name, version, err := splitNameVersion("tfprobe-ds/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "tfprobe-ds" || version != "1" {
		t.Errorf("got %q, %q", name, version)
	}

	for _, invalid := range []string{"tfprobe-ds", "tfprobe-ds/", "/1", ""} {
		if _, _, err := splitNameVersion(invalid); err == nil {
			t.Errorf("expected error for %q", invalid)
		}
	}
}
