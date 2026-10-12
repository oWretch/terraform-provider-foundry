package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	frameworkvalidator "github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func routingRules(t *testing.T, rules ...agentRoutingRuleModel) types.List {
	t.Helper()
	if rules == nil {
		rules = []agentRoutingRuleModel{}
	}
	value, diagnostics := types.ListValueFrom(context.Background(), types.ObjectType{AttrTypes: agentRoutingRuleAttrTypes}, rules)
	if diagnostics.HasError() {
		t.Fatalf("build rules: %v", diagnostics)
	}
	return value
}

func routingRule(version string) agentRoutingRuleModel {
	return agentRoutingRuleModel{AgentVersion: types.StringValue(version)}
}

func TestActiveAgentVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		rules []agentEndpointRule
		want  types.String
	}{
		{"latest", []agentEndpointRule{{Type: "FixedRatio", AgentVersion: "@latest", TrafficPercentage: 100}}, types.StringValue("7")},
		{"pin", []agentEndpointRule{{Type: "FixedRatio", AgentVersion: "5", TrafficPercentage: 100}}, types.StringValue("5")},
		{"split", []agentEndpointRule{{Type: "FixedRatio", AgentVersion: "5", TrafficPercentage: 80}, {Type: "FixedRatio", AgentVersion: "7", TrafficPercentage: 20}}, types.StringNull()},
		{"partial", []agentEndpointRule{{Type: "FixedRatio", AgentVersion: "5", TrafficPercentage: 80}}, types.StringNull()},
		{"unknown type", []agentEndpointRule{{Type: "FutureRule", AgentVersion: "5", TrafficPercentage: 100}}, types.StringNull()},
		{"missing version", []agentEndpointRule{{Type: "FixedRatio", TrafficPercentage: 100}}, types.StringNull()},
		{"empty", nil, types.StringNull()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			endpoint := &agentEndpoint{}
			endpoint.VersionSelector.VersionSelectionRules = test.rules
			if got := activeAgentVersion("7", endpoint); !got.Equal(test.want) {
				t.Errorf("active version = %v, want %v", got, test.want)
			}
			common := agentCommon{Version: types.StringValue("7")}
			var diagnostics diag.Diagnostics
			common.applyEndpoint(context.Background(), endpoint, &diagnostics)
			if diagnostics.HasError() || !common.ActiveVersion.Equal(test.want) {
				t.Fatalf("resource routing state = %v, %v", common.ActiveVersion, diagnostics)
			}
			wantRules := make([]attr.Value, 0, len(test.rules))
			for _, rule := range test.rules {
				wantRules = append(wantRules, types.ObjectValueMust(agentEndpointRuleAttrTypes, map[string]attr.Value{
					"type": types.StringValue(rule.Type), "agent_version": types.StringValue(rule.AgentVersion),
					"traffic_percentage": types.Int64Value(rule.TrafficPercentage),
				}))
			}
			if !common.AgentEndpoint.Attributes()["version_selection_rules"].Equal(types.ListValueMust(types.ObjectType{AttrTypes: agentEndpointRuleAttrTypes}, wantRules)) {
				t.Error("resource endpoint lost a rule or traffic percentage")
			}
		})
	}
	if !activeAgentVersion("7", nil).IsNull() {
		t.Error("absent endpoint must not imply latest is active")
	}
	endpoint := &agentEndpoint{}
	endpoint.VersionSelector.VersionSelectionRules = []agentEndpointRule{{Type: "FixedRatio", AgentVersion: "@latest", TrafficPercentage: 100}}
	if !activeAgentVersion("", endpoint).IsNull() {
		t.Error("missing latest version must remain null")
	}
}

func TestAgentDataSourceLatestWithPinnedRouting(t *testing.T) {
	t.Parallel()
	d := &agentDataSource{kind: "prompt"}
	d.client = newAgentTestClient(t, nil, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/projects/default/agents/agent" {
			t.Errorf("default lookup must read the parent agent, got %s", req.URL)
		}
		_, _ = fmt.Fprint(w, `{"name":"agent","versions":{"latest":{"version":"2","definition":{"kind":"prompt","model":"new-model"}}},"agent_endpoint":{"protocols":["responses"],"version_selector":{"version_selection_rules":[{"type":"FixedRatio","agent_version":"1","traffic_percentage":100}]}}}`)
	}))
	var diagnostics diag.Diagnostics
	var definition promptAgentDefinition
	version, agent, ok := d.read(context.Background(), "agent", types.StringNull(), &definition, &diagnostics)
	if !ok || diagnostics.HasError() {
		t.Fatalf("read: %v", diagnostics)
	}
	var state agentDataSourceCommon
	state.apply(context.Background(), version, &diagnostics)
	state.applyRouting(context.Background(), agent, &diagnostics)
	if diagnostics.HasError() || state.Version.ValueString() != "2" || definition.Model != "new-model" || state.ActiveVersion.ValueString() != "1" {
		t.Fatalf("latest definition and pinned traffic conflated: %+v, %v", state, diagnostics)
	}
}

func TestValidateOrdinaryAgentRouting(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		rules types.List
		error bool
	}{
		{"pin", routingRules(t, routingRule("1")), false},
		{"latest", routingRules(t, routingRule("@latest")), false},
		{"split", routingRules(t, routingRule("1"), routingRule("2")), true},
		{"empty", routingRules(t), true},
		{"null", types.ListNull(types.ObjectType{AttrTypes: agentRoutingRuleAttrTypes}), true},
		{"unknown", types.ListUnknown(types.ObjectType{AttrTypes: agentRoutingRuleAttrTypes}), false},
		{"unknown version", routingRules(t, agentRoutingRuleModel{AgentVersion: types.StringUnknown()}), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var diagnostics diag.Diagnostics
			validateOrdinaryAgentRouting(test.rules, &diagnostics)
			if diagnostics.HasError() != test.error {
				t.Errorf("diagnostics = %v, want error %t", diagnostics, test.error)
			}
		})
	}
}

func TestAgentRoutingBlockSchema(t *testing.T) {
	t.Parallel()
	var response resource.SchemaResponse
	(&agentRoutingResource{}).Schema(context.Background(), resource.SchemaRequest{}, &response)
	if _, exists := response.Schema.Attributes["version_selection_rules"]; exists {
		t.Fatal("routing rules must be a block, not an attribute")
	}
	block, ok := response.Schema.Blocks["version_selection_rules"].(schema.ListNestedBlock)
	if !ok {
		t.Fatal("routing rules must use a list-nested block")
	}
	for _, name := range []string{"type", "traffic_percentage"} {
		if _, exists := block.NestedObject.Attributes[name]; exists {
			t.Fatalf("%s must not be configurable", name)
		}
	}
	for _, count := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			rules := make([]agentRoutingRuleModel, count)
			for i := range rules {
				rules[i] = routingRule(fmt.Sprint(i + 1))
			}
			var validation frameworkvalidator.ListResponse
			for _, v := range block.Validators {
				v.ValidateList(context.Background(), frameworkvalidator.ListRequest{ConfigValue: routingRules(t, rules...)}, &validation)
			}
			if validation.Diagnostics.HasError() != (count != 1) {
				t.Errorf("block count %d: %v", count, validation.Diagnostics)
			}
		})
	}
}

func TestAgentRoutingPatch(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"1", "@latest"} {
		t.Run(target, func(t *testing.T) {
			gets, patches := 0, 0
			r := &agentRoutingResource{}
			r.previewGate = previewGate{name: "agent_endpoints", feature: "AgentEndpoints"}
			r.client = newAgentTestClient(t, []string{"agent_endpoints"}, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path != "/api/projects/default/agents/test-agent" || req.URL.Query().Get("api-version") != "v1" {
					t.Errorf("URL = %s", req.URL)
				}
				if req.Header.Get("Foundry-Features") != "AgentEndpoints=V1Preview" {
					t.Errorf("preview header = %s", req.Header.Get("Foundry-Features"))
				}
				selected := "@latest"
				switch req.Method {
				case http.MethodGet:
					gets++
					if patches > 0 {
						selected = target
					}
				case http.MethodPatch:
					patches++
					selected = target
					if req.Header.Get("Content-Type") != "application/merge-patch+json" {
						t.Errorf("content type = %s", req.Header.Get("Content-Type"))
					}
					var body map[string]any
					if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
						t.Errorf("decode request: %v", err)
					}
					want := map[string]any{"agent_endpoint": map[string]any{"version_selector": map[string]any{"version_selection_rules": []any{map[string]any{"type": "FixedRatio", "agent_version": target, "traffic_percentage": float64(100)}}}}}
					if !reflect.DeepEqual(body, want) {
						t.Errorf("PATCH includes unrelated fields or wrong rules: %v", body)
					}
				default:
					t.Errorf("unexpected method %s; routing must never publish or delete a version", req.Method)
				}
				_, _ = fmt.Fprintf(w, `{"name":"test-agent","versions":{"latest":{"version":"2"}},"agent_endpoint":{"protocols":["responses"],"version_selector":{"version_selection_rules":[{"type":"FixedRatio","agent_version":%q,"traffic_percentage":100}]}}}`, selected)
			}))
			plan := agentRoutingModel{AgentName: types.StringValue("test-agent"), VersionSelectionRules: routingRules(t, routingRule(target))}
			var diagnostics diag.Diagnostics
			agent, err := r.patch(context.Background(), plan, &diagnostics)
			if err != nil || diagnostics.HasError() {
				t.Fatalf("patch: %v, %v", err, diagnostics)
			}
			plan.apply(context.Background(), agent, &diagnostics)
			wantActive := "1"
			if target == "@latest" {
				wantActive = "2"
			}
			if diagnostics.HasError() || plan.ActiveVersion.ValueString() != wantActive || !plan.VersionSelectionRules.Equal(routingRules(t, routingRule(target))) {
				t.Errorf("readback = %+v, %v", plan, diagnostics)
			}
			if gets != 2 || patches != 1 {
				t.Errorf("requests GET=%d PATCH=%d", gets, patches)
			}
		})
	}
}

func TestAgentRoutingPatchErrors(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"preview disabled", "split", "unknown", "experiment", "missing endpoint", "service error", "readback mismatch", "readback missing"} {
		t.Run(mode, func(t *testing.T) {
			requests := 0
			previews := []string{"agent_endpoints"}
			if mode == "preview disabled" {
				previews = nil
			}
			r := &agentRoutingResource{previewGate: previewGate{name: "agent_endpoints", feature: "AgentEndpoints"}}
			r.client = newAgentTestClient(t, previews, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				requests++
				if req.Method == http.MethodPatch {
					if mode != "service error" && !strings.HasPrefix(mode, "readback") {
						t.Errorf("unsafe PATCH in %s", mode)
					}
					if mode == "service error" {
						w.WriteHeader(http.StatusBadRequest)
						_, _ = fmt.Fprint(w, `{"error":{"message":"routing denied"}}`)
					} else {
						_, _ = fmt.Fprint(w, `{}`)
					}
					return
				}
				if mode == "readback missing" && requests == 3 {
					_, _ = fmt.Fprint(w, `{"name":"agent"}`)
					return
				}
				switch mode {
				case "experiment":
					_, _ = fmt.Fprint(w, `{"agent_endpoint":{"version_selector":{"experiment_id":"experiment","salt":"salt","version_selection_rules":[{"type":"FixedRatio","agent_version":"1","traffic_percentage":80},{"type":"FixedRatio","agent_version":"2","traffic_percentage":20}]}}}`)
				case "missing endpoint":
					_, _ = fmt.Fprint(w, `{"name":"agent"}`)
				default:
					_, _ = fmt.Fprint(w, `{"agent_endpoint":{"version_selector":{"version_selection_rules":[{"type":"FixedRatio","agent_version":"1","traffic_percentage":100}]}}}`)
				}
			}))
			rules := routingRules(t, routingRule("1"))
			if strings.HasPrefix(mode, "readback") {
				rules = routingRules(t, routingRule("2"))
			}
			if mode == "split" {
				rules = routingRules(t, routingRule("1"), routingRule("2"))
			}
			if mode == "unknown" {
				rules = types.ListUnknown(types.ObjectType{AttrTypes: agentRoutingRuleAttrTypes})
			}
			var diagnostics diag.Diagnostics
			_, err := r.patch(context.Background(), agentRoutingModel{AgentName: types.StringValue("agent"), VersionSelectionRules: rules}, &diagnostics)
			if err == nil {
				t.Fatal("expected explicit routing error")
			}
			if mode == "service error" && !strings.Contains(err.Error(), "routing denied") {
				t.Errorf("service error lost: %v", err)
			}
			if (mode == "preview disabled" || mode == "split" || mode == "unknown") && requests != 0 {
				t.Errorf("invalid configuration sent %d requests", requests)
			}
		})
	}
}

func TestAgentRoutingReadImportAndDelete(t *testing.T) {
	t.Parallel()
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			requests := 0
			r := &agentRoutingResource{previewGate: previewGate{name: "agent_endpoints", feature: "AgentEndpoints"}}
			r.client = newAgentTestClient(t, []string{"agent_endpoints"}, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				requests++
				if req.Method != http.MethodGet {
					t.Errorf("unexpected destructive method %s", req.Method)
				}
				if missing {
					w.WriteHeader(http.StatusNotFound)
					_, _ = fmt.Fprint(w, `{"error":{"message":"missing"}}`)
					return
				}
				_, _ = fmt.Fprint(w, `{"name":"agent","versions":{"latest":{"version":"2"}},"agent_endpoint":{"version_selector":{"version_selection_rules":[{"type":"FixedRatio","agent_version":"1","traffic_percentage":100}]}}}`)
			}))
			var schemaResponse resource.SchemaResponse
			r.Schema(context.Background(), resource.SchemaRequest{}, &schemaResponse)
			imported := resource.ImportStateResponse{State: tfsdk.State{
				Schema: schemaResponse.Schema,
				Raw:    tftypes.NewValue(schemaResponse.Schema.Type().TerraformType(context.Background()), nil),
			}}
			r.ImportState(context.Background(), resource.ImportStateRequest{ID: "agent"}, &imported)
			if imported.Diagnostics.HasError() {
				t.Fatalf("import: %v", imported.Diagnostics)
			}
			response := resource.ReadResponse{State: imported.State}
			r.Read(context.Background(), resource.ReadRequest{State: imported.State}, &response)
			if response.Diagnostics.HasError() {
				t.Fatalf("read: %v", response.Diagnostics)
			}
			if missing {
				if !response.State.Raw.IsNull() {
					t.Error("missing parent must remove routing from state")
				}
				return
			}
			var state agentRoutingModel
			response.Diagnostics.Append(response.State.Get(context.Background(), &state)...)
			if response.Diagnostics.HasError() || state.ActiveVersion.ValueString() != "1" ||
				!state.VersionSelectionRules.Equal(routingRules(t, routingRule("1"))) {
				t.Fatalf("pinned rule was not preserved: %+v, %v", state, response.Diagnostics)
			}
			deleted := resource.DeleteResponse{State: response.State}
			r.Delete(context.Background(), resource.DeleteRequest{State: response.State}, &deleted)
			if deleted.Diagnostics.HasError() || !deleted.State.Raw.IsNull() || requests != 1 {
				t.Errorf("destroy must only forget state: requests=%d diagnostics=%v", requests, deleted.Diagnostics)
			}
		})
	}
}

func TestAgentRoutingRejectsUnrepresentableRemoteRules(t *testing.T) {
	t.Parallel()
	for _, rules := range [][]agentEndpointRule{
		{{Type: "FixedRatio", AgentVersion: "1", TrafficPercentage: 80}, {Type: "FixedRatio", AgentVersion: "2", TrafficPercentage: 20}},
		{{Type: "FixedRatio", AgentVersion: "1", TrafficPercentage: 99}},
		{{Type: "FutureRule", AgentVersion: "1", TrafficPercentage: 100}},
		nil,
	} {
		t.Run(fmt.Sprint(rules), func(t *testing.T) {
			agent := agentResponse{AgentEndpoint: &agentEndpoint{}}
			agent.AgentEndpoint.VersionSelector.VersionSelectionRules = rules
			model := agentRoutingModel{AgentName: types.StringValue("agent")}
			var diagnostics diag.Diagnostics
			model.apply(context.Background(), agent, &diagnostics)
			if !diagnostics.HasError() {
				t.Fatal("remote rules must not be silently flattened to an implicit 100% target")
			}
		})
	}
}

func TestAgentRoutingCreateAndUpdateState(t *testing.T) {
	t.Parallel()
	target := "@latest"
	patches := 0
	r := &agentRoutingResource{previewGate: previewGate{name: "agent_endpoints", feature: "AgentEndpoints"}}
	r.client = newAgentTestClient(t, []string{"agent_endpoints"}, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPatch {
			patches++
			var agent agentResponse
			if err := json.NewDecoder(req.Body).Decode(&agent); err != nil {
				t.Errorf("decode PATCH: %v", err)
			}
			target = agent.AgentEndpoint.VersionSelector.VersionSelectionRules[0].AgentVersion
		}
		_, _ = fmt.Fprintf(w, `{"name":"agent","versions":{"latest":{"version":"2"}},"agent_endpoint":{"version_selector":{"version_selection_rules":[{"type":"FixedRatio","agent_version":%q,"traffic_percentage":100}]}}}`, target)
	}))
	var schemaResponse resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schemaResponse)
	for _, version := range []string{"1", "@latest"} {
		model := agentRoutingModel{
			ID: types.StringUnknown(), AgentName: types.StringValue("agent"),
			VersionSelectionRules: routingRules(t, routingRule(version)), ActiveVersion: types.StringUnknown(),
		}
		var value types.Object
		diagnostics := tfsdk.ValueFrom(context.Background(), model, schemaResponse.Schema.Type(), &value)
		if diagnostics.HasError() {
			t.Fatalf("plan: %v", diagnostics)
		}
		raw, err := value.ToTerraformValue(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		plan := tfsdk.Plan{Raw: raw, Schema: schemaResponse.Schema}
		var state tfsdk.State
		if version == "1" {
			resp := resource.CreateResponse{State: tfsdk.State{Schema: schemaResponse.Schema}}
			r.Create(context.Background(), resource.CreateRequest{Plan: plan}, &resp)
			diagnostics.Append(resp.Diagnostics...)
			state = resp.State
		} else {
			resp := resource.UpdateResponse{State: tfsdk.State{Schema: schemaResponse.Schema}}
			r.Update(context.Background(), resource.UpdateRequest{Plan: plan}, &resp)
			diagnostics.Append(resp.Diagnostics...)
			state = resp.State
		}
		if diagnostics.HasError() {
			t.Fatalf("apply: %v", diagnostics)
		}
		var result agentRoutingModel
		diagnostics.Append(state.Get(context.Background(), &result)...)
		wantActive := version
		if version == "@latest" {
			wantActive = "2"
		}
		if diagnostics.HasError() || result.ID.ValueString() != "agent" ||
			result.ActiveVersion.ValueString() != wantActive || !result.VersionSelectionRules.Equal(model.VersionSelectionRules) {
			t.Fatalf("applied state: %+v, %v", result, diagnostics)
		}
	}
	if patches != 2 {
		t.Errorf("PATCH count = %d, want 2", patches)
	}
}

func TestAgentDataSourceVersionSelection(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"prompt", "hosted", "external"} {
		for _, selected := range []types.String{types.StringNull(), types.StringValue("1")} {
			t.Run(kind+"/"+selected.String(), func(t *testing.T) {
				requests := 0
				d := &agentDataSource{kind: kind, previewGate: previewGate{name: "external_agents", feature: "ExternalAgents"}}
				d.client = newAgentTestClient(t, []string{"external_agents"}, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					requests++
					if req.Method != http.MethodGet || req.URL.Query().Get("api-version") != "v1" {
						t.Errorf("request = %s %s", req.Method, req.URL)
					}
					definition := fmt.Sprintf(`{"kind":%q,"model":"gpt-4.1-mini","container_configuration":{"image":"image"},"cpu":"1","memory":"2Gi","otel_agent_id":"otel"}`, kind)
					switch req.URL.EscapedPath() {
					case "/api/projects/default/agents/agent%2Fname":
						_, _ = fmt.Fprintf(w, `{"name":"agent/name","versions":{"latest":{"id":"agent/name:2","version":"2","description":"latest only","metadata":{"latest":"only"},"definition":%s}},"agent_endpoint":{"protocols":["responses"],"version_selector":{"version_selection_rules":[{"type":"FixedRatio","agent_version":"@latest","traffic_percentage":100}]}}}`, definition)
					case "/api/projects/default/agents/agent%2Fname/versions/1":
						_, _ = fmt.Fprintf(w, `{"id":"agent/name:1","version":"1","definition":%s}`, definition)
					default:
						t.Errorf("unexpected path %s", req.URL.EscapedPath())
					}
					if kind == "external" && req.Header.Get("Foundry-Features") != "ExternalAgents=V1Preview" {
						t.Error("specific version lookup lost external preview gate")
					}
				}))
				var definition supportedAgentDefinition
				switch kind {
				case "prompt":
					definition = &promptAgentDefinition{}
				case "hosted":
					definition = &hostedAgentDefinition{}
				case "external":
					definition = &externalAgentDefinition{}
				}
				var diagnostics diag.Diagnostics
				version, agent, ok := d.read(context.Background(), "agent/name", selected, definition, &diagnostics)
				if !ok || diagnostics.HasError() {
					t.Fatalf("read: %v", diagnostics)
				}
				wantVersion, wantRequests := "2", 1
				if !selected.IsNull() {
					wantVersion, wantRequests = "1", 2
					if version.Description != "" || version.Metadata != nil {
						t.Error("specific version inherited fields from latest")
					}
				}
				if version.Version != wantVersion || requests != wantRequests || version.Name != "agent/name" {
					t.Errorf("version = %+v, requests = %d", version, requests)
				}
				var state agentDataSourceCommon
				state.applyRouting(context.Background(), agent, &diagnostics)
				if diagnostics.HasError() || state.ActiveVersion.ValueString() != "2" {
					t.Errorf("@latest must resolve against agent latest, not selected version: %+v, %v", state, diagnostics)
				}
			})
		}
	}
}

func TestAgentDataSourceVersionErrors(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"unknown", "empty", "missing", "wrong version", "kind mismatch", "escaped"} {
		t.Run(mode, func(t *testing.T) {
			requests := 0
			d := &agentDataSource{kind: "prompt"}
			d.client = newAgentTestClient(t, nil, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				requests++
				if requests == 1 {
					_, _ = fmt.Fprint(w, `{"name":"agent","versions":{"latest":{"version":"2"}}}`)
					return
				}
				switch mode {
				case "missing":
					w.WriteHeader(http.StatusNotFound)
					_, _ = fmt.Fprint(w, `{"error":{"message":"version missing"}}`)
				case "wrong version":
					_, _ = fmt.Fprint(w, `{"version":"2","definition":{"kind":"prompt","model":"model"}}`)
				case "kind mismatch":
					_, _ = fmt.Fprint(w, `{"version":"1","definition":{"kind":"hosted"}}`)
				case "escaped":
					if req.URL.EscapedPath() != "/api/projects/default/agents/agent/versions/v%2F1%20%232" {
						t.Errorf("unescaped version path: %s", req.URL)
					}
					_, _ = fmt.Fprint(w, `{"version":"v/1 #2","definition":{"kind":"prompt","model":"model"}}`)
				}
			}))
			selected := types.StringValue("1")
			switch mode {
			case "unknown":
				selected = types.StringUnknown()
			case "empty":
				selected = types.StringValue("")
			case "escaped":
				selected = types.StringValue("v/1 #2")
			}
			var diagnostics diag.Diagnostics
			_, _, ok := d.read(context.Background(), "agent", selected, &promptAgentDefinition{}, &diagnostics)
			if mode == "escaped" {
				if !ok || diagnostics.HasError() {
					t.Fatalf("escaped lookup: %v", diagnostics)
				}
			} else if ok || !diagnostics.HasError() {
				t.Fatalf("invalid lookup returned success: %v", diagnostics)
			}
			if (mode == "empty" || mode == "unknown") && requests != 0 {
				t.Error("invalid version fell back to latest")
			}
		})
	}
}

func TestPromptAgentDataSourceSelectedVersionState(t *testing.T) {
	t.Parallel()
	d := &agentDataSource{kind: "prompt"}
	d.client = newAgentTestClient(t, nil, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/versions/1") {
			_, _ = fmt.Fprint(w, `{"name":"agent","id":"agent:1","version":"1","definition":{"kind":"prompt","model":"old-model","instructions":"old"}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"name":"agent","versions":{"latest":{"version":"2"}},"agent_endpoint":{"protocols":["responses"],"version_selector":{"version_selection_rules":[{"type":"FixedRatio","agent_version":"1","traffic_percentage":100}]}}}`)
	}))
	var schemaResponse datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &schemaResponse)
	configModel := promptAgentDataSourceModel{agentDataSourceCommon: nullAgentDataSourceCommon("agent"), Model: types.StringNull(), Instructions: types.StringNull()}
	configModel.Version = types.StringValue("1")
	var configValue types.Object
	diagnostics := tfsdk.ValueFrom(context.Background(), configModel, schemaResponse.Schema.Type(), &configValue)
	if diagnostics.HasError() {
		t.Fatalf("config: %v", diagnostics)
	}
	raw, err := configValue.ToTerraformValue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	response := datasource.ReadResponse{State: tfsdk.State{Schema: schemaResponse.Schema}}
	d.Read(context.Background(), datasource.ReadRequest{Config: tfsdk.Config{Raw: raw, Schema: schemaResponse.Schema}}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("read: %v", response.Diagnostics)
	}
	var state promptAgentDataSourceModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &state)...)
	if response.Diagnostics.HasError() || state.Version.ValueString() != "1" || state.Model.ValueString() != "old-model" ||
		state.ActiveVersion.ValueString() != "1" || state.AgentEndpoint.IsNull() {
		t.Errorf("selected state = %+v, %v", state, response.Diagnostics)
	}
}
