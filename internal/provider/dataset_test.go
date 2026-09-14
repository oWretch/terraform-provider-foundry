package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestDatasetRequestUsesCurrentWireFields(t *testing.T) {
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

	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if raw["type"] != "uri_file" || raw["connectionName"] != "storage" || raw["dataUri"] == nil {
		t.Errorf("request = %#v", raw)
	}
	for _, stale := range []string{"name", "version", "displayName", "isSingleFile", "systemData"} {
		if _, present := raw[stale]; present {
			t.Errorf("request contains stale/read-only field %q", stale)
		}
	}
}

func TestDatasetRequestOmitsOptionalConnection(t *testing.T) {
	t.Parallel()

	model := datasetModel{
		Type:           types.StringValue("uri_file"),
		ConnectionName: types.StringNull(),
		DataURI:        types.StringValue("https://example.invalid/data.txt"),
		Description:    types.StringNull(),
		Tags:           types.MapNull(types.StringType),
	}
	var diagnostics diag.Diagnostics
	encoded, err := json.Marshal(model.request(context.Background(), &diagnostics))
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if diagnostics.HasError() {
		t.Fatalf("request diagnostics: %v", diagnostics)
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if _, present := raw["connectionName"]; present {
		t.Fatalf("request contains optional connectionName: %#v", raw)
	}
}

func TestDatasetApplyCurrentWireFields(t *testing.T) {
	t.Parallel()

	var response datasetResponse
	if err := json.Unmarshal([]byte(`{
		"dataUri":"azureml://datastores/workspaceblobstore/paths/knowledge/",
		"type":"uri_folder",
		"isReference":true,
		"connectionName":"workspaceblobstore",
		"id":"azureai://accounts/x/data/knowledge/versions/7",
		"name":"knowledge",
		"version":"7",
		"description":"knowledge files",
		"tags":{"team":"ai"}
	}`), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	var diagnostics diag.Diagnostics
	var model datasetModel
	model.apply(context.Background(), response, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("apply diagnostics: %v", diagnostics)
	}
	if model.Type.ValueString() != "uri_folder" || model.ConnectionName.ValueString() != "workspaceblobstore" {
		t.Errorf("model = %+v", model)
	}
	if !model.IsReference.ValueBool() {
		t.Error("is_reference should be true")
	}
	if model.ID.ValueString() != "azureai://accounts/x/data/knowledge/versions/7" {
		t.Errorf("id = %q", model.ID.ValueString())
	}
	if model.Description.ValueString() != "knowledge files" || model.Tags.IsNull() {
		t.Errorf("mutable fields = %q, %v", model.Description.ValueString(), model.Tags)
	}
}

func TestDatasetUpdateOnlySendsMutableFields(t *testing.T) {
	t.Parallel()

	model := datasetModel{
		Type:        types.StringValue("uri_file"),
		Description: types.StringNull(),
		Tags:        types.MapNull(types.StringType),
	}
	var diagnostics diag.Diagnostics
	encoded, err := json.Marshal(model.updateRequest(context.Background(), &diagnostics))
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if got := string(encoded); got != `{"type":"uri_file","description":null,"tags":null}` {
		t.Fatalf("request = %s", got)
	}
}

func TestDatasetRejectsUnsupportedType(t *testing.T) {
	t.Parallel()

	var diagnostics diag.Diagnostics
	var model datasetModel
	model.apply(context.Background(), datasetResponse{Type: "mltable"}, &diagnostics)
	if !diagnostics.HasError() || !strings.Contains(diagnostics.Errors()[0].Detail(), "uri_file") {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
}

func TestIndexRequestUsesCurrentAzureSearchFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	model := indexModel{
		Name:           types.StringValue("tfprobe-idx"),
		Version:        types.StringValue("1"),
		Type:           types.StringValue("AzureSearch"),
		ConnectionName: types.StringValue("azure-ai-search"),
		IndexName:      types.StringValue("tfprobe-search-index"),
		Description:    types.StringValue("search index"),
		Tags:           types.MapValueMust(types.StringType, map[string]attr.Value{"env": types.StringValue("test")}),
		FieldMapping: types.ObjectValueMust(indexFieldMappingAttributeTypes, map[string]attr.Value{
			"content_fields":  types.ListValueMust(types.StringType, []attr.Value{types.StringValue("content")}),
			"filepath_field":  types.StringValue("path"),
			"title_field":     types.StringValue("title"),
			"url_field":       types.StringValue("url"),
			"vector_fields":   types.ListValueMust(types.StringType, []attr.Value{types.StringValue("contentVector")}),
			"metadata_fields": types.ListValueMust(types.StringType, []attr.Value{types.StringValue("category")}),
		}),
	}

	var diagnostics diag.Diagnostics
	encoded, err := json.Marshal(model.request(ctx, &diagnostics))
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if diagnostics.HasError() {
		t.Fatalf("request diagnostics: %v", diagnostics)
	}

	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if raw["type"] != "AzureSearch" || raw["connectionName"] != "azure-ai-search" || raw["indexName"] != "tfprobe-search-index" {
		t.Errorf("request = %#v", raw)
	}
	if _, present := raw["name"]; present {
		t.Error("name belongs in the path, not the request body")
	}
	if _, present := raw["version"]; present {
		t.Error("version belongs in the path, not the request body")
	}
	mapping := raw["fieldMapping"].(map[string]any)
	if mapping["contentFields"].([]any)[0] != "content" || mapping["vectorFields"].([]any)[0] != "contentVector" {
		t.Errorf("fieldMapping = %#v", mapping)
	}
}

func TestIndexApplyCurrentAzureSearchFields(t *testing.T) {
	t.Parallel()

	var response indexResponse
	if err := json.Unmarshal([]byte(`{
		"type":"AzureSearch",
		"connectionName":"azure-ai-search",
		"indexName":"tfprobe-search-index",
		"fieldMapping":{"contentFields":["content"],"titleField":"title","vectorFields":["contentVector"]},
		"id":"azureai://accounts/x/indexes/tfprobe-idx/versions/1",
		"name":"tfprobe-idx",
		"version":"1",
		"description":"documents",
		"tags":{"team":"ai"}
	}`), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	var diagnostics diag.Diagnostics
	var model indexModel
	model.apply(context.Background(), response, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("apply diagnostics: %v", diagnostics)
	}
	if model.ID.ValueString() != "azureai://accounts/x/indexes/tfprobe-idx/versions/1" {
		t.Errorf("id = %q", model.ID.ValueString())
	}
	if model.IndexName.ValueString() != "tfprobe-search-index" || model.FieldMapping.IsNull() {
		t.Errorf("model = %+v", model)
	}
	if model.Description.ValueString() != "documents" || model.Tags.IsNull() {
		t.Errorf("mutable fields = %q, %v", model.Description.ValueString(), model.Tags)
	}
}

func TestIndexCreateRequiresCreateOnlyFields(t *testing.T) {
	t.Parallel()

	var diagnostics diag.Diagnostics
	indexModel{
		ConnectionName: types.StringNull(),
		IndexName:      types.StringNull(),
	}.validateCreate(&diagnostics)
	if !diagnostics.HasError() {
		t.Fatal("expected missing create-only fields to fail")
	}
}

func TestAssetApplyPreservesNullTags(t *testing.T) {
	t.Parallel()

	emptyTags := map[string]string{}
	dataset := datasetModel{Tags: types.MapNull(types.StringType)}
	dataset.apply(context.Background(), datasetResponse{
		Type: "uri_file",
		Tags: &emptyTags,
	}, &diag.Diagnostics{})
	if !dataset.Tags.IsNull() {
		t.Fatalf("dataset tags = %v", dataset.Tags)
	}

	index := indexModel{Tags: types.MapNull(types.StringType)}
	index.apply(context.Background(), indexResponse{
		Type: "AzureSearch",
		Tags: &emptyTags,
	}, &diag.Diagnostics{})
	if !index.Tags.IsNull() {
		t.Fatalf("index tags = %v", index.Tags)
	}
}

func TestIndexUpdateIncludesRequiredAzureSearchFields(t *testing.T) {
	t.Parallel()

	model := indexModel{
		Type:           types.StringValue("AzureSearch"),
		ConnectionName: types.StringValue("azure-ai-search"),
		IndexName:      types.StringValue("tfprobe-search-index"),
		Description:    types.StringNull(),
		Tags:           types.MapNull(types.StringType),
	}
	var diagnostics diag.Diagnostics
	encoded, err := json.Marshal(model.updateRequest(context.Background(), &diagnostics))
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if got := string(encoded); got != `{"type":"AzureSearch","connectionName":"azure-ai-search","indexName":"tfprobe-search-index","description":null,"tags":null}` {
		t.Fatalf("request = %s", got)
	}
}

func TestIndexRejectsManagedKinds(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"ManagedAzureSearch", "CosmosDBNoSqlVectorStore"} {
		t.Run(kind, func(t *testing.T) {
			var diagnostics diag.Diagnostics
			var model indexModel
			model.apply(context.Background(), indexResponse{Type: kind, Name: "index", Version: "1"}, &diagnostics)
			if !diagnostics.HasError() || !strings.Contains(diagnostics.Errors()[0].Detail(), kind) {
				t.Fatalf("diagnostics = %v", diagnostics)
			}
		})
	}
}

func TestDatasetDataSourceVersionPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		version     types.String
		wantPath    string
		response    string
		wantVersion string
	}{
		{
			name:        "explicit",
			version:     types.StringValue("v/1"),
			wantPath:    "/api/projects/default/datasets/data%2Fset%20%231/versions/v%2F1",
			response:    `{"dataUri":"https://example.test/data","type":"uri_file","isReference":true,"connectionName":"storage","id":"ds:1","name":"data/set #1","version":"v/1"}`,
			wantVersion: "v/1",
		},
		{
			name:        "latest",
			version:     types.StringNull(),
			wantPath:    "/api/projects/default/datasets",
			response:    `{"value":[{"dataUri":"https://example.test/old","type":"uri_file","name":"other","version":"9"},{"dataUri":"https://example.test/data","type":"uri_folder","isReference":false,"connectionName":"storage","id":"ds:7","name":"data/set #1","version":"7"}]}`,
			wantVersion: "7",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dataSource := NewDatasetDataSource().(*datasetDataSource)
			dataSource.client = newAgentTestClient(t, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.EscapedPath() != test.wantPath || r.URL.Query().Get("api-version") != "v1" {
					t.Errorf("request = %s %s", r.Method, r.RequestURI)
				}
				_, _ = fmt.Fprint(w, test.response)
			}))

			state, diagnostics := readAssetDataSource(t, dataSource, datasetDataSourceConfig("data/set #1", test.version))
			if diagnostics.HasError() {
				t.Fatalf("Read diagnostics: %v", diagnostics)
			}
			if state.Version.ValueString() != test.wantVersion || state.Name.ValueString() != "data/set #1" {
				t.Errorf("state = %+v", state)
			}
		})
	}
}

func TestDatasetDataSourceFollowsLatestNextLink(t *testing.T) {
	t.Parallel()

	var calls int
	dataSource := NewDatasetDataSource().(*datasetDataSource)
	dataSource.client = newAgentTestClient(t, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			_, _ = fmt.Fprint(w, `{"value":[{"dataUri":"https://example.test/other","type":"uri_file","name":"other","version":"1"}],"nextLink":"https://example.test/api/projects/default/datasets?skip=next&api-version=v1"}`)
		case 2:
			if r.URL.EscapedPath() != "/api/projects/default/datasets" || r.URL.Query().Get("skip") != "next" {
				t.Errorf("request = %s", r.RequestURI)
			}
			_, _ = fmt.Fprint(w, `{"value":[{"dataUri":"https://example.test/data","type":"uri_file","name":"target","version":"2"}]}`)
		default:
			t.Fatalf("unexpected request %d", calls)
		}
	}))

	state, diagnostics := readAssetDataSource(t, dataSource, datasetDataSourceConfig("target", types.StringNull()))
	if diagnostics.HasError() {
		t.Fatalf("Read diagnostics: %v", diagnostics)
	}
	if calls != 2 || state.Version.ValueString() != "2" {
		t.Fatalf("calls = %d, state = %+v", calls, state)
	}
}

func TestIndexDataSourceVersionPathsAndKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		version   types.String
		wantPath  string
		response  string
		wantError string
	}{
		{
			name:     "explicit",
			version:  types.StringValue("v/2"),
			wantPath: "/api/projects/default/indexes/search%2Findex/versions/v%2F2",
			response: `{"type":"AzureSearch","connectionName":"search","indexName":"documents","fieldMapping":{"contentFields":["content"]},"id":"idx:2","name":"search/index","version":"v/2"}`,
		},
		{
			name:     "latest",
			version:  types.StringNull(),
			wantPath: "/api/projects/default/indexes",
			response: `{"value":[{"type":"AzureSearch","id":"idx:3","name":"search/index","version":"3"}]}`,
		},
		{
			name:      "managed",
			version:   types.StringNull(),
			wantPath:  "/api/projects/default/indexes",
			response:  `{"value":[{"type":"ManagedAzureSearch","vectorStoreId":"vs_123","name":"search/index","version":"4"}]}`,
			wantError: "ManagedAzureSearch",
		},
		{
			name:      "cosmos",
			version:   types.StringValue("5"),
			wantPath:  "/api/projects/default/indexes/search%2Findex/versions/5",
			response:  `{"type":"CosmosDBNoSqlVectorStore","connectionName":"cosmos","databaseName":"db","containerName":"items","name":"search/index","version":"5"}`,
			wantError: "CosmosDBNoSqlVectorStore",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dataSource := NewIndexDataSource().(*indexDataSource)
			dataSource.client = newAgentTestClient(t, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.EscapedPath() != test.wantPath || r.URL.Query().Get("api-version") != "v1" {
					t.Errorf("request = %s %s", r.Method, r.RequestURI)
				}
				_, _ = fmt.Fprint(w, test.response)
			}))

			state, diagnostics := readAssetDataSource(t, dataSource, indexDataSourceConfig("search/index", test.version))
			if test.wantError != "" {
				if !diagnostics.HasError() || !strings.Contains(diagnostics.Errors()[0].Detail(), test.wantError) {
					t.Fatalf("diagnostics = %v", diagnostics)
				}
				return
			}
			if diagnostics.HasError() {
				t.Fatalf("Read diagnostics: %v", diagnostics)
			}
			if state.Type.ValueString() != "AzureSearch" || state.Name.ValueString() != "search/index" {
				t.Errorf("state = %+v", state)
			}
		})
	}
}

func TestAssetDataSourcesReportNotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dataSource datasource.DataSource
		config     any
	}{
		{name: "dataset", dataSource: NewDatasetDataSource(), config: datasetDataSourceConfig("missing", types.StringValue("1"))},
		{name: "index", dataSource: NewIndexDataSource(), config: indexDataSourceConfig("missing", types.StringValue("1"))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			switch dataSource := test.dataSource.(type) {
			case *datasetDataSource:
				dataSource.client = newAgentTestClient(t, nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "missing", http.StatusNotFound)
				}))
				_, diagnostics := readAssetDataSource(t, dataSource, test.config.(datasetModel))
				if !diagnostics.HasError() || !strings.Contains(diagnostics.Errors()[0].Summary(), "not found") {
					t.Fatalf("diagnostics = %v", diagnostics)
				}
			case *indexDataSource:
				dataSource.client = newAgentTestClient(t, nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "missing", http.StatusNotFound)
				}))
				_, diagnostics := readAssetDataSource(t, dataSource, test.config.(indexModel))
				if !diagnostics.HasError() || !strings.Contains(diagnostics.Errors()[0].Summary(), "not found") {
					t.Fatalf("diagnostics = %v", diagnostics)
				}
			}
		})
	}
}

func datasetDataSourceConfig(name string, version types.String) datasetModel {
	return datasetModel{
		Name:           types.StringValue(name),
		Version:        version,
		Type:           types.StringNull(),
		ConnectionName: types.StringNull(),
		DataURI:        types.StringNull(),
		Description:    types.StringNull(),
		Tags:           types.MapNull(types.StringType),
		ID:             types.StringNull(),
		IsReference:    types.BoolNull(),
	}
}

func indexDataSourceConfig(name string, version types.String) indexModel {
	return indexModel{
		Name:           types.StringValue(name),
		Version:        version,
		Type:           types.StringNull(),
		ConnectionName: types.StringNull(),
		IndexName:      types.StringNull(),
		FieldMapping:   types.ObjectNull(indexFieldMappingAttributeTypes),
		Description:    types.StringNull(),
		Tags:           types.MapNull(types.StringType),
		ID:             types.StringNull(),
	}
}

func readAssetDataSource[T any](t *testing.T, dataSource datasource.DataSource, config T) (T, diag.Diagnostics) {
	t.Helper()

	var schemaResponse datasource.SchemaResponse
	dataSource.Schema(context.Background(), datasource.SchemaRequest{}, &schemaResponse)
	var configValue types.Object
	diagnostics := tfsdk.ValueFrom(context.Background(), config, schemaResponse.Schema.Type(), &configValue)
	if diagnostics.HasError() {
		t.Fatalf("build config: %v", diagnostics)
	}

	raw, err := configValue.ToTerraformValue(context.Background())
	if err != nil {
		t.Fatalf("convert config: %v", err)
	}
	response := datasource.ReadResponse{State: tfsdk.State{Schema: schemaResponse.Schema}}
	dataSource.Read(context.Background(), datasource.ReadRequest{
		Config: tfsdk.Config{Raw: raw, Schema: schemaResponse.Schema},
	}, &response)

	var state T
	if !response.Diagnostics.HasError() {
		response.Diagnostics.Append(response.State.Get(context.Background(), &state)...)
	}
	return state, response.Diagnostics
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

	name, version, err = splitNameVersion("data%2Fset/v%2F1")
	if err != nil {
		t.Fatalf("unexpected escaped ID error: %v", err)
	}
	if name != "data/set" || version != "v/1" {
		t.Errorf("escaped ID got %q, %q", name, version)
	}

	for _, invalid := range []string{"tfprobe-ds", "tfprobe-ds/", "/1", ""} {
		if _, _, err := splitNameVersion(invalid); err == nil {
			t.Errorf("expected error for %q", invalid)
		}
	}
}
