package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	frameworkvalidator "github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

func newAgentTestClient(t *testing.T, previews []string, handler http.Handler) *clients.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := clients.New(clients.Config{
		AccountName:     "test",
		ProjectName:     "default",
		Environment:     clients.EnvironmentPublic,
		Auth:            clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
		PreviewFeatures: previews,
		Transport:       rewriteHostTransport{target: server.Listener.Addr().String()},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	return client
}

func TestPromptAgentDefinitionRoundTrip(t *testing.T) {
	t.Parallel()

	model := promptAgentModel{
		Model:        types.StringValue("gpt-4.1"),
		Instructions: types.StringValue("Be terse."),
	}
	encoded, err := json.Marshal(model.definition())
	if err != nil {
		t.Fatalf("marshal definition: %v", err)
	}

	var applied promptAgentModel
	var definition promptAgentDefinition
	if err := json.Unmarshal(encoded, &definition); err != nil {
		t.Fatalf("unmarshal definition: %v", err)
	}
	var diagnostics diag.Diagnostics
	applied.apply(context.Background(), agentVersion{ID: "a:1", Version: "1"}, nil, definition, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("apply diagnostics: %v", diagnostics)
	}

	if definition.Kind != "prompt" {
		t.Errorf("kind = %q, want prompt", definition.Kind)
	}
	if hasJSONValue(definition.Harness) {
		t.Error("default prompt definition must omit the harness")
	}
	if applied.Model.ValueString() != "gpt-4.1" {
		t.Errorf("model = %q", applied.Model.ValueString())
	}
	if applied.Instructions.ValueString() != "Be terse." {
		t.Errorf("instructions = %q", applied.Instructions.ValueString())
	}
	// An absent description must stay null so Terraform does not report a permanent diff.
	if !applied.Description.IsNull() {
		t.Error("empty description should be null")
	}
}

func TestPromptCopilotLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var posts atomic.Int32
	const definition = `{"kind":"prompt","model":"reasoning-model","harness":{"type":"microsoft_copilot_preview"},"text":{},"top_p":1}`
	versionJSON := func(version int) string {
		return fmt.Sprintf(`{"id":"agent:%d","name":"agent","version":"%d","definition":%s}`, version, version, definition)
	}
	agentJSON := func(version int) string {
		return fmt.Sprintf(`{"name":"agent","versions":{"latest":%s}}`, versionJSON(version))
	}
	client := newAgentTestClient(t, []string{"copilot_harness", "agent_endpoints"}, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		header := req.Header.Get("Foundry-Features")
		switch req.Method {
		case http.MethodPost:
			if header != "MicrosoftCopilot=V1Preview" || req.URL.Query().Get("api-version") != "v1" {
				t.Errorf("publish header = %q, URL = %s", header, req.URL)
			}
			var body struct {
				Definition struct {
					Harness map[string]string `json:"harness"`
					Kind    string            `json:"kind"`
				} `json:"definition"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil ||
				body.Definition.Kind != "prompt" || len(body.Definition.Harness) != 1 ||
				body.Definition.Harness["type"] != copilotHarnessType {
				t.Errorf("unexpected publish body %+v: %v", body, err)
			}
			switch count := posts.Add(1); {
			case count == 1 && strings.HasSuffix(req.URL.Path, "/agents"):
				_, _ = fmt.Fprint(w, agentJSON(1))
			case count == 2 && strings.HasSuffix(req.URL.Path, "/agents/agent/versions"):
				_, _ = fmt.Fprint(w, versionJSON(2))
			default:
				t.Errorf("unexpected publish route %s", req.URL)
			}
		case http.MethodGet:
			if header != "" {
				t.Errorf("GET sent preview header %q", header)
			}
			if strings.HasSuffix(req.URL.Path, "/agents/agent/versions/1") {
				_, _ = fmt.Fprint(w, versionJSON(1))
			} else {
				_, _ = fmt.Fprint(w, agentJSON(int(posts.Load())))
			}
		default:
			t.Errorf("unexpected request %s %s", req.Method, req.URL)
		}
	}))
	r := &promptAgentResource{client: client}
	var rs resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &rs)
	model := promptAgentModel{
		agentCommon: agentCommon{Name: types.StringValue("agent"), AgentEndpoint: types.ObjectNull(agentEndpointAttrTypes)},
		Model:       types.StringValue("reasoning-model"),
		HarnessType: types.StringValue(copilotHarnessType),
	}
	plan := func(model promptAgentModel) tfsdk.Plan {
		value := tfsdk.Plan{Schema: rs.Schema}
		if d := value.Set(ctx, model); d.HasError() {
			t.Fatalf("plan: %v", d)
		}
		return value
	}
	created := resource.CreateResponse{State: tfsdk.State{Schema: rs.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: plan(model)}, &created)
	if created.Diagnostics.HasError() || posts.Load() != 1 {
		t.Fatalf("create: %v, POSTs: %d", created.Diagnostics, posts.Load())
	}

	imported := resource.ImportStateResponse{State: tfsdk.State{Schema: rs.Schema,
		Raw: tftypes.NewValue(rs.Schema.Type().TerraformType(ctx), nil)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "agent"}, &imported)
	read := resource.ReadResponse{State: imported.State}
	r.Read(ctx, resource.ReadRequest{State: imported.State}, &read)
	var state promptAgentModel
	read.Diagnostics.Append(read.State.Get(ctx, &state)...)
	if imported.Diagnostics.HasError() || read.Diagnostics.HasError() || !state.HarnessType.Equal(model.HarnessType) {
		t.Fatalf("import/read: %v, %v, harness: %v", imported.Diagnostics, read.Diagnostics, state.HarnessType)
	}

	update := resource.UpdateResponse{State: read.State}
	r.Update(ctx, resource.UpdateRequest{Plan: plan(model)}, &update)
	if update.Diagnostics.HasError() || posts.Load() != 2 {
		t.Fatalf("update: %v, POSTs: %d", update.Diagnostics, posts.Load())
	}
	withoutPreview := &promptAgentResource{client: newAgentTestClient(t, []string{"draft_agents"}, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		t.Errorf("preview-disabled create sent %s", req.Method)
		w.WriteHeader(http.StatusBadRequest)
	}))}
	gated := resource.CreateResponse{State: tfsdk.State{Schema: rs.Schema}}
	withoutPreview.Create(ctx, resource.CreateRequest{Plan: plan(model)}, &gated)
	if !gated.Diagnostics.HasError() {
		t.Fatal("create did not enforce the Copilot opt-in")
	}

	model.HarnessType = types.StringNull()
	var modified resource.ModifyPlanResponse
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{State: read.State, Plan: plan(model)}, &modified)
	if !modified.Diagnostics.HasError() {
		t.Fatal("plan accepted a same-name harness change")
	}
	blocked := resource.UpdateResponse{State: read.State}
	r.Update(ctx, resource.UpdateRequest{Plan: plan(model)}, &blocked)
	if !blocked.Diagnostics.HasError() || posts.Load() != 2 {
		t.Fatalf("same-name harness change: %v, POSTs: %d", blocked.Diagnostics, posts.Load())
	}
	model.Name = types.StringValue("new-agent")
	modified = resource.ModifyPlanResponse{}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{State: read.State, Plan: plan(model)}, &modified)
	if modified.Diagnostics.HasError() {
		t.Fatalf("new name: %v", modified.Diagnostics)
	}

	d := &agentDataSource{kind: "prompt"}
	var ds datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &ds)
	config := promptAgentDataSourceModel{agentDataSourceCommon: nullAgentDataSourceCommon("agent")}
	for _, selected := range []types.String{types.StringNull(), types.StringValue("1")} {
		config.Version = selected
		values := tfsdk.State{Schema: ds.Schema}
		if diags := values.Set(ctx, config); diags.HasError() {
			t.Fatalf("data source config: %v", diags)
		}
		request := datasource.ReadRequest{Config: tfsdk.Config{Raw: values.Raw, Schema: ds.Schema}}
		d.client = client
		response := datasource.ReadResponse{State: tfsdk.State{Schema: ds.Schema}}
		d.Read(ctx, request, &response)
		response.Diagnostics.Append(response.State.Get(ctx, &config)...)
		if response.Diagnostics.HasError() || config.HarnessType.ValueString() != copilotHarnessType ||
			(!selected.IsNull() && config.Version.ValueString() != selected.ValueString()) {
			t.Fatalf("data source: %v, state: %+v", response.Diagnostics, config)
		}
		d.client = newAgentTestClient(t, nil, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if strings.HasSuffix(req.URL.Path, "/versions/1") {
				_, _ = fmt.Fprint(w, versionJSON(1))
			} else {
				_, _ = fmt.Fprint(w, agentJSON(2))
			}
		}))
		response = datasource.ReadResponse{State: tfsdk.State{Schema: ds.Schema}}
		d.Read(ctx, request, &response)
		if !response.Diagnostics.HasError() {
			t.Fatal("data source read did not enforce the Copilot opt-in")
		}
	}
}

func TestHostedAgentDefinitionRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	variables, diags := types.MapValueFrom(ctx, types.StringType, map[string]string{"LOG_LEVEL": "debug"})
	if diags.HasError() {
		t.Fatalf("build map: %v", diags)
	}

	model := hostedAgentModel{
		Image:                types.StringValue("registry/repo:1"),
		RegistryConnectionID: types.StringValue("registry-connection"),
		CPU:                  types.StringValue("1"),
		Memory:               types.StringValue("2Gi"),
		EnvironmentVariables: variables,
		ProtocolVersions: []protocolVersionModel{
			{Protocol: types.StringValue("responses"), Version: types.StringValue("1")},
		},
		RAIConfig: types.ObjectValueMust(raiConfigAttrTypes, map[string]attr.Value{
			"rai_policy_name": types.StringValue("strict"),
		}),
	}

	var diagnostics diag.Diagnostics
	encoded, err := json.Marshal(model.definition(ctx, &diagnostics))
	if err != nil {
		t.Fatalf("marshal definition: %v", err)
	}
	if diagnostics.HasError() {
		t.Fatalf("definition diagnostics: %v", diagnostics)
	}

	var definition hostedAgentDefinition
	if err := json.Unmarshal(encoded, &definition); err != nil {
		t.Fatalf("unmarshal definition: %v", err)
	}

	var applied hostedAgentModel
	applied.apply(ctx, agentVersion{ID: "a:1", Version: "1"}, nil, definition, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("apply diagnostics: %v", diagnostics)
	}

	if definition.Kind != "hosted" {
		t.Errorf("kind = %q, want hosted", definition.Kind)
	}
	if applied.Memory.ValueString() != "2Gi" {
		t.Errorf("memory = %q", applied.Memory.ValueString())
	}
	if applied.Image.ValueString() != "registry/repo:1" {
		t.Errorf("image = %q", applied.Image.ValueString())
	}
	if applied.RegistryConnectionID.ValueString() != "registry-connection" {
		t.Errorf("registry_connection_id = %q", applied.RegistryConnectionID.ValueString())
	}
	if applied.RAIConfig.Attributes()["rai_policy_name"].(types.String).ValueString() != "strict" {
		t.Errorf("rai_config = %+v", applied.RAIConfig)
	}
	if len(applied.ProtocolVersions) != 1 || applied.ProtocolVersions[0].Protocol.ValueString() != "responses" {
		t.Errorf("protocol versions = %+v", applied.ProtocolVersions)
	}
	if applied.EnvironmentVariables.IsNull() {
		t.Error("environment variables should be preserved")
	}
}

func TestHostedAgentEmptyEnvironmentIsNull(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var diagnostics diag.Diagnostics
	var applied hostedAgentModel
	applied.apply(ctx, agentVersion{}, nil, hostedAgentDefinition{
		Kind:                   "hosted",
		ContainerConfiguration: &containerConfiguration{Image: "registry/repo:1"},
	}, &diagnostics)

	if !applied.EnvironmentVariables.IsNull() {
		t.Error("absent environment variables should be null")
	}
}

func TestHostedAgentRequestUsesCurrentWireFields(t *testing.T) {
	t.Parallel()

	client := newAgentTestClient(t, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/projects/default/agents" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Foundry-Features"); got != "" {
			t.Errorf("Foundry-Features = %q, want empty for GA hosted agents", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		definition := body["definition"].(map[string]any)
		container := definition["container_configuration"].(map[string]any)
		if container["image"] != "registry/repo:1" || container["registry_connection_id"] != "registry-connection" {
			t.Errorf("container_configuration = %#v", container)
		}
		if _, ok := definition["image"]; ok {
			t.Error("obsolete top-level image field was sent")
		}
		if _, ok := definition["container_protocol_versions"]; ok {
			t.Error("obsolete container_protocol_versions field was sent")
		}
		if _, ok := definition["protocol_versions"]; !ok {
			t.Error("protocol_versions field was not sent")
		}
		if definition["rai_config"].(map[string]any)["rai_policy_name"] != "strict" {
			t.Errorf("rai_config = %#v", definition["rai_config"])
		}
		_, _ = fmt.Fprint(w, `{
			"object":"agent",
			"id":"agent-id",
			"name":"hosted-agent",
			"instance_identity":{"principal_id":"principal","client_id":"client"},
			"versions":{"latest":{
				"object":"agent.version",
				"id":"hosted-agent:1",
				"version":"1",
				"created_at":123,
				"status":"active",
				"definition":{
					"kind":"hosted",
					"cpu":"1",
					"memory":"2Gi",
					"container_configuration":{"image":"registry/repo:1","registry_connection_id":"registry-connection"},
					"protocol_versions":[{"protocol":"responses","version":"v1"}],
					"rai_config":{"rai_policy_name":"strict"}
				}
			}}
		}`)
	}))

	definition := hostedAgentDefinition{
		Kind:                   "hosted",
		CPU:                    "1",
		Memory:                 "2Gi",
		ContainerConfiguration: &containerConfiguration{Image: "registry/repo:1", RegistryConnectionID: "registry-connection"},
		ProtocolVersions:       []protocolVersion{{Protocol: "responses", Version: "v1"}},
		RAIConfig:              &raiConfig{PolicyName: "strict"},
	}
	version, _, err := createAgent(context.Background(), client, "hosted-agent", "", definition, false)
	if err != nil {
		t.Fatalf("createAgent: %v", err)
	}
	if version.Name != "hosted-agent" || version.Identity == nil || version.Identity.PrincipalID != "principal" {
		t.Errorf("top-level AgentObject fields were not preserved: %+v", version)
	}
	if version.CreatedAt != 123 || version.Status != "active" {
		t.Errorf("latest AgentVersionObject fields = %+v", version)
	}
}

func TestAgentNamesAreValidatedAndPathEscaped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		wantError bool
	}{
		{name: "a"},
		{name: "agent-1"},
		{name: "-agent", wantError: true},
		{name: "agent-", wantError: true},
		{name: "agent_name", wantError: true},
		{name: strings.Repeat("a", 64), wantError: true},
	}
	for _, test := range tests {
		var diagnostics diag.Diagnostics
		for _, item := range agentNameValidators {
			var response frameworkvalidator.StringResponse
			item.ValidateString(context.Background(), frameworkvalidator.StringRequest{
				ConfigValue: types.StringValue(test.name),
			}, &response)
			diagnostics.Append(response.Diagnostics...)
		}
		if diagnostics.HasError() != test.wantError {
			t.Errorf("name %q HasError() = %v, want %v", test.name, diagnostics.HasError(), test.wantError)
		}
	}

	requests := 0
	client := newAgentTestClient(t, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if !strings.Contains(r.RequestURI, "/agents/a%2Fb?") {
			if !strings.Contains(r.RequestURI, "/agents/a%2Fb/versions?") {
				t.Errorf("RequestURI = %q, want escaped name segment", r.RequestURI)
			}
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = fmt.Fprint(w, `{"name":"a/b","versions":{"latest":{"id":"a/b:1","version":"1","definition":{"kind":"prompt","model":"gpt-4.1"}}}}`)
		case http.MethodPost:
			_, _ = fmt.Fprint(w, `{"id":"a/b:2","name":"a/b","version":"2","definition":{"kind":"prompt","model":"gpt-4.1"}}`)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	if _, _, err := readAgent(context.Background(), client, "a/b"); err != nil {
		t.Fatalf("readAgent: %v", err)
	}
	updated, err := updateAgent(context.Background(), client, "a/b", "", promptAgentDefinition{Kind: "prompt", Model: "gpt-4.1"}, false)
	if err != nil {
		t.Fatalf("updateAgent: %v", err)
	}
	if updated.ID != "a/b:2" || updated.Version != "2" {
		t.Errorf("AgentVersionObject response = %+v", updated)
	}
	if err := deleteAgent(context.Background(), client, "a/b"); err != nil {
		t.Fatalf("deleteAgent: %v", err)
	}
	if requests != 3 {
		t.Errorf("requests = %d, want 3", requests)
	}
}

func TestManagedAgentDefinitionRejectsUnsupportedFieldsAndKindMismatch(t *testing.T) {
	t.Parallel()

	one := 1.0
	tests := []struct {
		name       string
		version    agentVersion
		definition supportedAgentDefinition
		wantError  bool
	}{
		{
			name:       "prompt defaults are harmless",
			version:    agentVersion{Definition: json.RawMessage(`{"kind":"prompt","model":"gpt-4.1","temperature":1,"top_p":1,"text":{}}`)},
			definition: &promptAgentDefinition{Temperature: &one},
		},
		{
			name:       "prompt tools",
			version:    agentVersion{Definition: json.RawMessage(`{"kind":"prompt","model":"gpt-4.1","tools":[{"type":"code_interpreter"}]}`)},
			definition: &promptAgentDefinition{},
			wantError:  true,
		},
		{
			name:       "prompt harness",
			version:    agentVersion{Definition: json.RawMessage(`{"kind":"prompt","model":"gpt-4.1","harness":{"type":"github_copilot"}}`)},
			definition: &promptAgentDefinition{},
			wantError:  true,
		},
		{
			name:       "copilot harness with extra settings",
			version:    agentVersion{Definition: json.RawMessage(`{"kind":"prompt","model":"model","harness":{"type":"microsoft_copilot_preview","future_option":false}}`)},
			definition: &promptAgentDefinition{},
			wantError:  true,
		},
		{
			name:       "copilot definition with unknown settings",
			version:    agentVersion{Definition: json.RawMessage(`{"kind":"prompt","model":"model","harness":{"type":"microsoft_copilot_preview"},"future_option":false}`)},
			definition: &promptAgentDefinition{},
			wantError:  true,
		},
		{
			name:       "copilot tools are not managed",
			version:    agentVersion{Definition: json.RawMessage(`{"kind":"prompt","model":"model","harness":{"type":"microsoft_copilot_preview"},"tools":[{"type":"microsoft_copilot_toolset_preview"}]}`)},
			definition: &promptAgentDefinition{},
			wantError:  true,
		},
		{
			name:       "prompt skills",
			version:    agentVersion{Definition: json.RawMessage(`{"kind":"prompt","model":"gpt-4.1","skills":[{"name":"review"}]}`)},
			definition: &promptAgentDefinition{},
			wantError:  true,
		},
		{
			name:       "hosted code",
			version:    agentVersion{Definition: json.RawMessage(`{"kind":"hosted","cpu":"1","memory":"2Gi","code_configuration":{"runtime":"python_3_12"}}`)},
			definition: &hostedAgentDefinition{},
			wantError:  true,
		},
		{
			name:       "kind mismatch",
			version:    agentVersion{Definition: json.RawMessage(`{"kind":"external","otel_agent_id":"agent"}`)},
			definition: &promptAgentDefinition{},
			wantError:  true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var diagnostics diag.Diagnostics
			ok := decodeManagedDefinition(test.version, test.definition, &diagnostics)
			if ok == test.wantError || diagnostics.HasError() != test.wantError {
				t.Errorf("ok = %v, diagnostics = %v, wantError = %v", ok, diagnostics, test.wantError)
			}
		})
	}
}

func TestManagedAgentDefinitionRejectsMetadata(t *testing.T) {
	t.Parallel()

	version := agentVersion{
		Metadata:   map[string]string{"owner": "platform"},
		Definition: json.RawMessage(`{"kind":"prompt","model":"gpt-4.1"}`),
	}
	var diagnostics diag.Diagnostics
	if decodeManagedDefinition(version, &promptAgentDefinition{}, &diagnostics) || !diagnostics.HasError() {
		t.Errorf("version metadata was accepted: %+v", version)
	}
}

func TestManagedDefinitionAcceptsServiceBlueprintFields(t *testing.T) {
	t.Parallel()

	response := agentResponse{
		Blueprint:          json.RawMessage(`{"principal_id":"principal"}`),
		BlueprintReference: json.RawMessage(`{"name":"blueprint"}`),
	}
	response.Versions.Latest.Definition = json.RawMessage(`{"kind":"prompt","model":"gpt-4.1"}`)

	version := response.latestVersion()
	var diagnostics diag.Diagnostics
	if !decodeManagedDefinition(version, &promptAgentDefinition{}, &diagnostics) || diagnostics.HasError() {
		t.Errorf("service-managed blueprint fields were rejected: %v", diagnostics)
	}
}

func TestValidateAgentBeforeUpdateRejectsLossyCurrentVersion(t *testing.T) {
	t.Parallel()

	client := newAgentTestClient(t, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %s, want GET", r.Method)
		}
		_, _ = fmt.Fprint(w, `{
			"name":"prompt-agent",
			"versions":{"latest":{
				"id":"prompt-agent:1",
				"version":"1",
				"metadata":{"owner":"platform"},
				"definition":{"kind":"prompt","model":"gpt-4.1"}
			}}
		}`)
	}))

	var diagnostics diag.Diagnostics
	if validateAgentBeforeUpdate(context.Background(), client, "prompt-agent", &promptAgentDefinition{}, &diagnostics) || !diagnostics.HasError() {
		t.Errorf("lossy current version was accepted: %v", diagnostics)
	}
}

func TestAgentDataSourceSchemas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind       string
		dataSource *agentDataSource
		fields     []string
	}{
		{kind: "prompt", dataSource: NewPromptAgentDataSource().(*agentDataSource), fields: []string{"model", "instructions"}},
		{kind: "hosted", dataSource: NewHostedAgentDataSource().(*agentDataSource), fields: []string{"image", "registry_connection_id", "cpu", "memory", "environment_variables", "protocol_versions", "rai_config"}},
		{kind: "external", dataSource: NewExternalAgentDataSource().(*agentDataSource), fields: []string{"otel_agent_id", "rai_config"}},
	}

	for _, test := range tests {
		var metadata datasource.MetadataResponse
		test.dataSource.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "foundry"}, &metadata)
		if metadata.TypeName != "foundry_"+test.kind+"_agent" {
			t.Errorf("%s TypeName = %q", test.kind, metadata.TypeName)
		}
		var response datasource.SchemaResponse
		test.dataSource.Schema(context.Background(), datasource.SchemaRequest{}, &response)
		for _, field := range append([]string{"name", "id", "version", "description", "metadata", "status", "created_at"}, test.fields...) {
			if _, ok := response.Schema.Attributes[field]; !ok {
				t.Errorf("%s schema missing %q", test.kind, field)
			}
		}

		if _, ok := response.Schema.Attributes["endpoint"]; ok {
			t.Errorf("%s schema contains obsolete endpoint", test.kind)
		}
	}

	var resourceSchema resource.SchemaResponse
	(&hostedAgentResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resourceSchema)
	if _, ok := resourceSchema.Schema.Attributes["protocol_versions"]; !ok {
		t.Error("hosted resource schema missing protocol_versions")
	}
	if _, ok := resourceSchema.Schema.Attributes["container_protocol_versions"]; ok {
		t.Error("hosted resource schema contains obsolete container_protocol_versions")
	}
}

func TestAgentDataSourcesRejectKindMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind       string
		previews   []string
		dataSource *agentDataSource
		definition supportedAgentDefinition
	}{
		{kind: "prompt", dataSource: NewPromptAgentDataSource().(*agentDataSource), definition: &promptAgentDefinition{}},
		{kind: "hosted", dataSource: NewHostedAgentDataSource().(*agentDataSource), definition: &hostedAgentDefinition{}},
		{kind: "external", previews: []string{"external_agents"}, dataSource: NewExternalAgentDataSource().(*agentDataSource), definition: &externalAgentDefinition{}},
	}
	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			test.dataSource.client = newAgentTestClient(t, test.previews, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, `{"name":"agent","versions":{"latest":{"id":"agent:1","version":"1","definition":{"kind":"workflow"}}}}`)
			}))
			if test.kind == "external" {
				test.dataSource.feature = "ExternalAgents"
				test.dataSource.name = "external_agents"
			}
			var diagnostics diag.Diagnostics
			if _, _, ok := test.dataSource.read(context.Background(), "agent", types.StringNull(), test.definition, &diagnostics); ok || !diagnostics.HasError() {
				t.Errorf("read accepted mismatched kind: %v", diagnostics)
			}
		})
	}
}

func TestPromptAgentDataSourceRead(t *testing.T) {
	t.Parallel()

	client := newAgentTestClient(t, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.Contains(r.RequestURI, "/agents/prompt-agent?api-version=v1") {
			t.Errorf("request = %s %s", r.Method, r.RequestURI)
		}
		_, _ = fmt.Fprint(w, `{
			"name":"prompt-agent",
			"versions":{"latest":{
				"id":"prompt-agent:2",
				"version":"2",
				"description":"current",
				"created_at":456,
				"status":"active",
				"metadata":{"team":"ai"},
				"definition":{
					"kind":"prompt",
					"model":"gpt-4.1",
					"instructions":"Be terse.",
					"temperature":0.2,
					"tools":[{"type":"code_interpreter"}],
					"rai_config":{"rai_policy_name":"strict"}
				}
			}}
		}`)
	}))
	dataSource := NewPromptAgentDataSource().(*agentDataSource)
	dataSource.client = client

	var schemaResponse datasource.SchemaResponse
	dataSource.Schema(context.Background(), datasource.SchemaRequest{}, &schemaResponse)
	configModel := promptAgentDataSourceModel{
		agentDataSourceCommon: nullAgentDataSourceCommon("prompt-agent"),
		Model:                 types.StringNull(),
		Instructions:          types.StringNull(),
	}
	var configValue types.Object
	diagnostics := tfsdk.ValueFrom(context.Background(), configModel, schemaResponse.Schema.Type(), &configValue)
	if diagnostics.HasError() {
		t.Fatalf("build config: %v", diagnostics)
	}
	raw, err := configValue.ToTerraformValue(context.Background())
	if err != nil {
		t.Fatalf("convert config: %v", err)
	}
	response := datasource.ReadResponse{
		State: tfsdk.State{Schema: schemaResponse.Schema},
	}
	dataSource.Read(context.Background(), datasource.ReadRequest{
		Config: tfsdk.Config{Raw: raw, Schema: schemaResponse.Schema},
	}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("Read diagnostics: %v", response.Diagnostics)
	}
	var state promptAgentDataSourceModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &state)...)
	if response.Diagnostics.HasError() {
		t.Fatalf("State.Get diagnostics: %v", response.Diagnostics)
	}
	if state.Model.ValueString() != "gpt-4.1" || state.Instructions.ValueString() != "Be terse." {
		t.Errorf("definition state = %+v", state)
	}
	if state.ID.ValueString() != "prompt-agent:2" || state.CreatedAt.ValueInt64() != 456 || state.Metadata.IsNull() {
		t.Errorf("common state = %+v", state.agentDataSourceCommon)
	}
}

func nullAgentDataSourceCommon(name string) agentDataSourceCommon {
	return agentDataSourceCommon{
		Name:          types.StringValue(name),
		ID:            types.StringNull(),
		Version:       types.StringNull(),
		ActiveVersion: types.StringNull(),
		AgentEndpoint: types.ObjectNull(agentEndpointAttrTypes),
		Description:   types.StringNull(),
		Metadata:      types.MapNull(types.StringType),
		Status:        types.StringNull(),
		CreatedAt:     types.Int64Null(),
		AgentGUID:     types.StringNull(),
		PrincipalID:   types.StringNull(),
		ClientID:      types.StringNull(),
	}
}
