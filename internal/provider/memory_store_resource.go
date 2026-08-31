package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

const memoryStoresPreviewFeature = "MemoryStores"

// memoryStoresPreviewFeatureName is the enable_preview value users write to opt in.
const memoryStoresPreviewFeatureName = "memory_stores"

var (
	_ resource.Resource                   = &memoryStoreResource{}
	_ resource.ResourceWithConfigure      = &memoryStoreResource{}
	_ resource.ResourceWithImportState    = &memoryStoreResource{}
	_ resource.ResourceWithValidateConfig = &memoryStoreResource{}
)

func NewMemoryStoreResource() resource.Resource {
	return &memoryStoreResource{}
}

type memoryStoreResource struct {
	previewGate
}

// memoryStoreOptionsModel mirrors MemoryStoreDefaultOptions. It is part of
// the memory store definition, which the service only accepts on create and
// returns on read, so any change forces replacement.
type memoryStoreOptionsModel struct {
	UserProfileEnabled      types.Bool   `tfsdk:"user_profile_enabled"`
	UserProfileDetails      types.String `tfsdk:"user_profile_details"`
	ChatSummaryEnabled      types.Bool   `tfsdk:"chat_summary_enabled"`
	ProceduralMemoryEnabled types.Bool   `tfsdk:"procedural_memory_enabled"`
	DefaultTTLSeconds       types.Int64  `tfsdk:"default_ttl_seconds"`
}

type memoryStoreModel struct {
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Metadata       types.Map    `tfsdk:"metadata"`
	ChatModel      types.String `tfsdk:"chat_model"`
	EmbeddingModel types.String `tfsdk:"embedding_model"`
	Options        types.Object `tfsdk:"options"`
	ID             types.String `tfsdk:"id"`
	CreatedAt      types.Int64  `tfsdk:"created_at"`
	UpdatedAt      types.Int64  `tfsdk:"updated_at"`
}

var memoryStoreOptionsAttributeTypes = map[string]attr.Type{
	"user_profile_enabled":      types.BoolType,
	"user_profile_details":      types.StringType,
	"chat_summary_enabled":      types.BoolType,
	"procedural_memory_enabled": types.BoolType,
	"default_ttl_seconds":       types.Int64Type,
}

type memoryStoreDefaultOptionsRequest struct {
	UserProfileEnabled      *bool  `json:"user_profile_enabled,omitempty"`
	UserProfileDetails      string `json:"user_profile_details,omitempty"`
	ChatSummaryEnabled      *bool  `json:"chat_summary_enabled,omitempty"`
	ProceduralMemoryEnabled *bool  `json:"procedural_memory_enabled,omitempty"`
	DefaultTTLSeconds       *int64 `json:"default_ttl_seconds,omitempty"`
}

type memoryStoreDefinitionRequest struct {
	Kind           string                            `json:"kind"`
	ChatModel      string                            `json:"chat_model"`
	EmbeddingModel string                            `json:"embedding_model"`
	Options        *memoryStoreDefaultOptionsRequest `json:"options,omitempty"`
}

type memoryStoreCreateRequest struct {
	Name        string                       `json:"name"`
	Description string                       `json:"description,omitempty"`
	Metadata    map[string]string            `json:"metadata,omitempty"`
	Definition  memoryStoreDefinitionRequest `json:"definition"`
}

type memoryStoreUpdateRequest struct {
	Description string            `json:"description"`
	Metadata    map[string]string `json:"metadata"`
}

type memoryStoreDefaultOptionsResponse struct {
	UserProfileEnabled      *bool  `json:"user_profile_enabled"`
	UserProfileDetails      string `json:"user_profile_details"`
	ChatSummaryEnabled      *bool  `json:"chat_summary_enabled"`
	ProceduralMemoryEnabled *bool  `json:"procedural_memory_enabled"`
	DefaultTTLSeconds       *int64 `json:"default_ttl_seconds"`
}

type memoryStoreDefinitionResponse struct {
	Kind           string                             `json:"kind"`
	ChatModel      string                             `json:"chat_model"`
	EmbeddingModel string                             `json:"embedding_model"`
	Options        *memoryStoreDefaultOptionsResponse `json:"options"`
}

type memoryStoreResponse struct {
	Object      string                        `json:"object"`
	ID          string                        `json:"id"`
	Name        string                        `json:"name"`
	Description *string                       `json:"description"`
	Metadata    *map[string]string            `json:"metadata"`
	CreatedAt   int64                         `json:"created_at"`
	UpdatedAt   int64                         `json:"updated_at"`
	Definition  memoryStoreDefinitionResponse `json:"definition"`
}

func (m memoryStoreModel) createRequest(ctx context.Context, diagnostics *diag.Diagnostics) memoryStoreCreateRequest {
	request := memoryStoreCreateRequest{
		Name:        m.Name.ValueString(),
		Description: m.Description.ValueString(),
		Definition: memoryStoreDefinitionRequest{
			Kind:           "default",
			ChatModel:      m.ChatModel.ValueString(),
			EmbeddingModel: m.EmbeddingModel.ValueString(),
		},
	}
	if !m.Metadata.IsNull() {
		diagnostics.Append(m.Metadata.ElementsAs(ctx, &request.Metadata, false)...)
	}
	// options is Optional+Computed, so it is unknown rather than null when the
	// configuration omits it; unknown values cannot be read into a Go struct.
	if !m.Options.IsNull() && !m.Options.IsUnknown() {
		var options memoryStoreOptionsModel
		diagnostics.Append(m.Options.As(ctx, &options, basetypes.ObjectAsOptions{})...)
		optionsRequest := &memoryStoreDefaultOptionsRequest{
			UserProfileDetails: options.UserProfileDetails.ValueString(),
		}
		if !options.UserProfileEnabled.IsNull() {
			value := options.UserProfileEnabled.ValueBool()
			optionsRequest.UserProfileEnabled = &value
		}
		if !options.ChatSummaryEnabled.IsNull() {
			value := options.ChatSummaryEnabled.ValueBool()
			optionsRequest.ChatSummaryEnabled = &value
		}
		if !options.ProceduralMemoryEnabled.IsNull() {
			value := options.ProceduralMemoryEnabled.ValueBool()
			optionsRequest.ProceduralMemoryEnabled = &value
		}
		if !options.DefaultTTLSeconds.IsNull() {
			value := options.DefaultTTLSeconds.ValueInt64()
			optionsRequest.DefaultTTLSeconds = &value
		}
		request.Definition.Options = optionsRequest
	}
	return request
}

func (m memoryStoreModel) updateRequest(ctx context.Context, diagnostics *diag.Diagnostics) memoryStoreUpdateRequest {
	request := memoryStoreUpdateRequest{
		Description: m.Description.ValueString(),
		Metadata:    map[string]string{},
	}
	if !m.Metadata.IsNull() {
		diagnostics.Append(m.Metadata.ElementsAs(ctx, &request.Metadata, false)...)
	}
	return request
}

func (m *memoryStoreModel) apply(ctx context.Context, response memoryStoreResponse, diagnostics *diag.Diagnostics) {
	if response.Object != "memory_store" {
		diagnostics.AddError("Unexpected memory store object", fmt.Sprintf("Foundry returned object %q; expected memory_store.", response.Object))
		return
	}
	if response.Definition.Kind != "default" {
		diagnostics.AddError("Unsupported memory store kind", fmt.Sprintf("Foundry returned memory store kind %q; foundry_memory_store only supports the default kind.", response.Definition.Kind))
		return
	}

	m.ID = types.StringValue(response.ID)
	m.Name = types.StringValue(response.Name)
	if response.Description != nil {
		m.Description = applyOptionalAssetString(m.Description, *response.Description)
	}
	m.CreatedAt = types.Int64Value(response.CreatedAt)
	m.UpdatedAt = types.Int64Value(response.UpdatedAt)
	m.ChatModel = types.StringValue(response.Definition.ChatModel)
	m.EmbeddingModel = types.StringValue(response.Definition.EmbeddingModel)

	if response.Metadata != nil {
		if len(*response.Metadata) != 0 || !m.Metadata.IsNull() {
			value, diags := types.MapValueFrom(ctx, types.StringType, *response.Metadata)
			diagnostics.Append(diags...)
			m.Metadata = value
		}
	}

	if response.Definition.Options == nil {
		m.Options = types.ObjectNull(memoryStoreOptionsAttributeTypes)
	} else {
		options := response.Definition.Options
		userProfileEnabled := true
		if options.UserProfileEnabled != nil {
			userProfileEnabled = *options.UserProfileEnabled
		}
		chatSummaryEnabled := true
		if options.ChatSummaryEnabled != nil {
			chatSummaryEnabled = *options.ChatSummaryEnabled
		}
		proceduralMemoryEnabled := true
		if options.ProceduralMemoryEnabled != nil {
			proceduralMemoryEnabled = *options.ProceduralMemoryEnabled
		}
		defaultTTLSeconds := int64(0)
		if options.DefaultTTLSeconds != nil {
			defaultTTLSeconds = *options.DefaultTTLSeconds
		}
		value, diags := types.ObjectValue(memoryStoreOptionsAttributeTypes, map[string]attr.Value{
			"user_profile_enabled":      types.BoolValue(userProfileEnabled),
			"user_profile_details":      optionalString(options.UserProfileDetails),
			"chat_summary_enabled":      types.BoolValue(chatSummaryEnabled),
			"procedural_memory_enabled": types.BoolValue(proceduralMemoryEnabled),
			"default_ttl_seconds":       types.Int64Value(defaultTTLSeconds),
		})
		diagnostics.Append(diags...)
		m.Options = value
	}
}

func (m memoryStoreModel) path() string {
	return assetNamePath("memory_stores", m.Name.ValueString())
}

func (r *memoryStoreResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_memory_store"
}

func (r *memoryStoreResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "~> **Preview:** This resource requires `enable_preview = [\"memory_stores\"]` on the provider. Preview features may change or be removed in any provider release without following semantic versioning.\n\n" +
			"Manages a memory store, which extracts and stores user profile, chat summary, and procedural memories from agent conversations for later retrieval.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the memory store. Changing this forces a new memory store to be created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Human-readable description of the memory store.",
			},
			"metadata": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Key-value metadata attached to the memory store.",
			},
			"chat_model": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name or identifier of the chat completion model deployment used for memory processing. Changing this forces a new memory store to be created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"embedding_model": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name or identifier of the embedding model deployment used for memory processing. Changing this forces a new memory store to be created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"options": schema.SingleNestedAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Options for the default memory store implementation. Changing this forces a new memory store to be created, because the service only accepts these options at creation time.",
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.RequiresReplace(),
					// Without this the service-populated defaults go unknown on
					// every plan, and RequiresReplace then forces a replacement
					// forever when the configuration omits options.
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"user_profile_enabled": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(true),
						MarkdownDescription: "Whether to enable user profile extraction and storage. Defaults to `true`.",
					},
					"user_profile_details": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Specific categories or types of user profile information to extract and store.",
						Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
					},
					"chat_summary_enabled": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(true),
						MarkdownDescription: "Whether to enable chat summary extraction and storage. Defaults to `true`.",
					},
					"procedural_memory_enabled": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(true),
						MarkdownDescription: "Whether to enable procedural memory extraction and storage. Defaults to `true`.",
					},
					"default_ttl_seconds": schema.Int64Attribute{
						Optional:            true,
						Computed:            true,
						Default:             int64default.StaticInt64(0),
						MarkdownDescription: "Default time-to-live for memories in seconds. A value of `0` means memories do not expire. Defaults to `0`.",
					},
				},
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned memory store identifier.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Unix timestamp when the memory store was created.",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Unix timestamp when the memory store was last updated.",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *memoryStoreResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client := clientFromProviderData(req.ProviderData, &resp.Diagnostics)
	r.previewGate = previewGate{client: client, feature: memoryStoresPreviewFeature, name: memoryStoresPreviewFeatureName}
}

func (r *memoryStoreResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan memoryStoreModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create memory store", err.Error())
		return
	}

	request := plan.createRequest(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var response memoryStoreResponse
	if err := r.client.JSON(ctx, http.MethodPost, "memory_stores", request, &response); err != nil {
		resp.Diagnostics.AddError("Unable to create memory store", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *memoryStoreResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state memoryStoreModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read memory store", err.Error())
		return
	}

	var response memoryStoreResponse
	err = r.client.JSON(ctx, http.MethodGet, state.path(), nil, &response)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read memory store", err.Error())
		return
	}

	state.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *memoryStoreResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan memoryStoreModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update memory store", err.Error())
		return
	}

	request := plan.updateRequest(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var response memoryStoreResponse
	if err := r.client.JSON(ctx, http.MethodPost, plan.path(), request, &response); err != nil {
		resp.Diagnostics.AddError("Unable to update memory store", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *memoryStoreResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state memoryStoreModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete memory store", err.Error())
		return
	}

	err = r.client.JSON(ctx, http.MethodDelete, state.path(), nil, nil)
	if err != nil && !clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete memory store", err.Error())
	}
}

func (r *memoryStoreResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

// ValidateConfig delegates to previewGate. Go's embedding does not promote
// previewGate.ValidateResourceConfig to satisfy
// resource.ResourceWithValidateConfig on *memoryStoreResource automatically
// in all toolchains, so this thin wrapper makes the interface assertion
// explicit and stable.
func (r *memoryStoreResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	r.ValidateResourceConfig(ctx, req, resp)
}
