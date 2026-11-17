package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const testVoiceDefinition = `{"kind":"voice","sub_protocol":"realtime","model":"gpt-realtime","model_type":"managed","audio":{"output":{"voice":"en-US-AvaNeural","voice_type":"azure-standard"}}}`

func TestVoiceAgentLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var requests atomic.Int32
	latest := `{"id":"support-voice:1","version":"1","definition":` + testVoiceDefinition + `}`
	updated := `{"id":"support-voice:2","version":"2","definition":` +
		strings.Replace(testVoiceDefinition, `"kind":"voice"`, `"kind":"voice","instructions":"Be brief.","store":true`, 1) + `}`
	r := &voiceAgentResource{client: newAgentTestClient(t, nil, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		step := requests.Add(1)
		wantMethod, wantPath := http.MethodGet, "/agents/support-voice"
		switch step {
		case 2, 6:
			wantMethod, wantPath = http.MethodPost, wantPath+"/versions"
		case 9:
			wantMethod = http.MethodDelete
		}
		checkVoiceRequest(t, req, wantMethod, wantPath)
		switch step {
		case 1, 10:
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"error":{"message":"not found"}}`)
		case 2, 6:
			var body struct {
				Definition voiceAgentDefinition `json:"definition"`
				Draft      *bool                `json:"draft"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			d := body.Definition
			if body.Draft != nil || d.Kind != "voice" || d.SubProtocol != "realtime" ||
				d.Model != "gpt-realtime" || d.ModelType != "managed" || d.Audio == nil || d.Audio.Output == nil ||
				d.Audio.Output.Voice != "en-US-AvaNeural" || d.Audio.Output.VoiceType != "azure-standard" ||
				len(d.OutputModalities) != 1 || d.OutputModalities[0] != "audio" ||
				d.Store == nil || *d.Store != (step == 6) {
				t.Errorf("unexpected publish body: %+v", body)
			}
			if step == 2 {
				if d.Instructions != nil {
					t.Error("absent instructions should be omitted")
				}
				_, _ = fmt.Fprint(w, latest)
			} else {
				if d.Instructions == nil || *d.Instructions != "Be brief." {
					t.Error("updated instructions missing")
				}
				_, _ = fmt.Fprint(w, updated)
			}
		case 3, 4, 5:
			_, _ = fmt.Fprintf(w, `{"name":"support-voice","versions":{"latest":%s}}`, latest)
		case 7, 8:
			_, _ = fmt.Fprintf(w, `{"name":"support-voice","versions":{"latest":%s}}`, updated)
		case 9:
			_, _ = fmt.Fprint(w, `{"deleted":true}`)
		default:
			t.Errorf("unexpected request %d", step)
		}
	}))}

	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	blank := tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(sr.Schema.Type().TerraformType(ctx), nil)}
	model := voiceAgentModel{
		agentCommon: agentCommon{Name: types.StringValue("support-voice"), AgentEndpoint: types.ObjectNull(agentEndpointAttrTypes)},
		voiceAgentSettings: voiceAgentSettings{
			Model: types.StringValue("gpt-realtime"), ModelType: types.StringValue("managed"),
			Voice: types.StringValue("en-US-AvaNeural"), VoiceType: types.StringValue("azure-standard"),
			Store: types.BoolValue(false),
		},
	}
	create := resource.CreateResponse{State: blank}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sr.Schema, Raw: voiceAgentTestRaw(t, model, sr.Schema.Type())}}, &create)
	if create.Diagnostics.HasError() {
		t.Fatalf("create: %v", create.Diagnostics)
	}

	imported := resource.ImportStateResponse{State: blank}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "support-voice"}, &imported)
	read := resource.ReadResponse{State: imported.State}
	r.Read(ctx, resource.ReadRequest{State: imported.State}, &read)
	if imported.Diagnostics.HasError() || read.Diagnostics.HasError() || !read.State.Raw.Equal(create.State.Raw) {
		t.Fatalf("import did not round-trip: import=%v read=%v state=%s", imported.Diagnostics, read.Diagnostics, read.State.Raw)
	}

	model.Instructions = types.StringValue("Be brief.")
	model.Store = types.BoolValue(true)
	update := resource.UpdateResponse{State: blank}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: sr.Schema, Raw: voiceAgentTestRaw(t, model, sr.Schema.Type())}}, &update)
	var state voiceAgentModel
	update.Diagnostics.Append(update.State.Get(ctx, &state)...)
	if update.Diagnostics.HasError() || !state.Store.ValueBool() || state.ID.ValueString() != "support-voice:2" ||
		!state.Instructions.Equal(model.Instructions) {
		t.Fatalf("update: %v, state=%+v", update.Diagnostics, state)
	}

	deleted := resource.DeleteResponse{State: update.State}
	r.Delete(ctx, resource.DeleteRequest{State: update.State}, &deleted)
	missing := resource.ReadResponse{State: update.State}
	r.Read(ctx, resource.ReadRequest{State: update.State}, &missing)
	if deleted.Diagnostics.HasError() || missing.Diagnostics.HasError() || !missing.State.Raw.IsNull() || requests.Load() != 10 {
		t.Fatalf("delete and refresh: %v, %v, requests=%d", deleted.Diagnostics, missing.Diagnostics, requests.Load())
	}
}

func TestVoiceAgentRefusesLossyUpdates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, old, new, want string
	}{
		{"top-level", `"kind":"voice"`, `"kind":"voice","future_setting":true`, `unknown field "future_setting"`},
		{"nested", `"voice_type":"azure-standard"`, `"voice_type":"azure-standard","speed":1.2`, `unknown field "speed"`},
		{"wrong kind", `"kind":"voice"`, `"kind":"prompt"`, `service returned "prompt"`},
		{"wrong protocol", `"sub_protocol":"realtime"`, `"sub_protocol":"live"`, `unsupported sub_protocol`},
		{"wrong modalities", `"model_type":"managed"`, `"model_type":"managed","output_modalities":["audio","avatar"]`, `only the audio output modality`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newAgentTestClient(t, nil, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				checkVoiceRequest(t, req, http.MethodGet, "/agents/support-voice")
				_, _ = fmt.Fprintf(w, `{"name":"support-voice","versions":{"latest":{"definition":%s}}}`,
					strings.Replace(testVoiceDefinition, tc.old, tc.new, 1))
			}))
			var d diag.Diagnostics
			if validateAgentBeforeUpdate(context.Background(), client, "support-voice", &managedVoiceAgentDefinition{}, &d) ||
				!d.HasError() || !strings.Contains(d[0].Detail(), tc.want) {
				t.Fatalf("wanted %s, got %v", tc.want, d)
			}
		})
	}
}

func TestVoiceAgentDataSourceReadsPinnedVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := NewVoiceAgentDataSource().(*agentDataSource)
	d.client = newAgentTestClient(t, nil, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/versions/1") {
			checkVoiceRequest(t, req, http.MethodGet, "/agents/support-voice/versions/1")
			// Fields not managed by the resource do not prevent lookup.
			_, _ = fmt.Fprintf(w, `{"id":"support-voice:1","version":"1","definition":%s}`,
				strings.Replace(testVoiceDefinition, `"voice_type":"azure-standard"`, `"voice_type":"azure-standard","speed":1.2`, 1))
			return
		}
		checkVoiceRequest(t, req, http.MethodGet, "/agents/support-voice")
		_, _ = fmt.Fprint(w, `{"name":"support-voice","versions":{"latest":{"version":"2"}},"agent_endpoint":{"version_selector":{"version_selection_rules":[{"type":"FixedRatio","agent_version":"@latest","traffic_percentage":100}]}}}`)
	}))
	var sr datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &sr)
	model := voiceAgentDataSourceModel{agentDataSourceCommon: nullAgentDataSourceCommon("support-voice")}
	model.Version = types.StringValue("1")
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: sr.Schema}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: voiceAgentTestRaw(t, model, sr.Schema.Type())}}, &resp)
	var state voiceAgentDataSourceModel
	resp.Diagnostics.Append(resp.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || state.SubProtocol.ValueString() != "realtime" ||
		state.Voice.ValueString() != "en-US-AvaNeural" || state.ID.ValueString() != "support-voice:1" ||
		state.ActiveVersion.ValueString() != "2" {
		t.Fatalf("read: %v, state=%+v", resp.Diagnostics, state)
	}
}

func checkVoiceRequest(t *testing.T, req *http.Request, method, path string) {
	t.Helper()
	if req.Method != method || req.URL.Path != "/api/projects/default"+path ||
		req.URL.Query().Get("api-version") != "v1" || req.Header.Get("Foundry-Features") != "" {
		t.Errorf("unexpected voice request: %s %s (Foundry-Features=%q)", req.Method, req.URL, req.Header.Get("Foundry-Features"))
	}
}

func voiceAgentTestRaw(t *testing.T, model any, typ attr.Type) tftypes.Value {
	t.Helper()
	var value types.Object
	if d := tfsdk.ValueFrom(context.Background(), model, typ, &value); d.HasError() {
		t.Fatal(d)
	}
	raw, err := value.ToTerraformValue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
