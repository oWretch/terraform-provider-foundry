package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ datasource.DataSource                   = &memoryStoreDataSource{}
	_ datasource.DataSourceWithConfigure      = &memoryStoreDataSource{}
	_ datasource.DataSourceWithValidateConfig = &memoryStoreDataSource{}
)

func NewMemoryStoreDataSource() datasource.DataSource {
	return &memoryStoreDataSource{}
}

type memoryStoreDataSource struct {
	previewGate
}

type memoryStoreDataSourceModel struct {
	Name           types.String `tfsdk:"name"`
	Object         types.String `tfsdk:"object"`
	ID             types.String `tfsdk:"id"`
	Description    types.String `tfsdk:"description"`
	Metadata       types.Map    `tfsdk:"metadata"`
	CreatedAt      types.Int64  `tfsdk:"created_at"`
	UpdatedAt      types.Int64  `tfsdk:"updated_at"`
	Kind           types.String `tfsdk:"kind"`
	ChatModel      types.String `tfsdk:"chat_model"`
	EmbeddingModel types.String `tfsdk:"embedding_model"`
	Options        types.Object `tfsdk:"options"`
}

func (d *memoryStoreDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_memory_store"
}

func (d *memoryStoreDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "~> **Preview:** This data source requires `enable_preview = [\"memory_stores\"]` on the provider. Preview features may change or be removed in any provider release without following semantic versioning.\n\n" +
			"Looks up the current memory store object by name.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the memory store to look up.",
			},
			"object": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Object discriminator, always `memory_store`.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned memory store identifier.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the memory store.",
			},
			"metadata": schema.MapAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Key-value metadata attached to the memory store.",
			},
			"created_at": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Unix timestamp when the memory store was created.",
			},
			"updated_at": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Unix timestamp when the memory store was last updated.",
			},
			"kind": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Memory store definition kind. This data source currently accepts `default`.",
			},
			"chat_model": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Chat completion model deployment used for memory processing.",
			},
			"embedding_model": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Embedding model deployment used for memory processing.",
			},
			"options": schema.SingleNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Options for the default memory store implementation.",
				Attributes: map[string]schema.Attribute{
					"user_profile_enabled":      schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether user profile extraction is enabled."},
					"user_profile_details":      schema.StringAttribute{Computed: true, MarkdownDescription: "User profile categories to extract."},
					"chat_summary_enabled":      schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether chat summary extraction is enabled."},
					"procedural_memory_enabled": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether procedural memory extraction is enabled."},
					"default_ttl_seconds":       schema.Int64Attribute{Computed: true, MarkdownDescription: "Default memory time-to-live in seconds."},
				},
			},
		},
	}
}

func (d *memoryStoreDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client := clientFromProviderData(req.ProviderData, &resp.Diagnostics)
	d.previewGate = previewGate{client: client, feature: memoryStoresPreviewFeature, name: memoryStoresPreviewFeatureName}
}

func (d *memoryStoreDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	d.ValidateDataSourceConfig(ctx, req, resp)
}

func (d *memoryStoreDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config memoryStoreDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := d.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read memory store", err.Error())
		return
	}

	var response memoryStoreResponse
	err = d.client.JSON(ctx, http.MethodGet, assetNamePath("memory_stores", config.Name.ValueString()), nil, &response)
	if clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Memory store not found", "No memory store matched the given name.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read memory store", err.Error())
		return
	}
	model := memoryStoreModel{
		Description: types.StringNull(),
		Metadata:    types.MapNull(types.StringType),
		Options:     types.ObjectNull(memoryStoreOptionsAttributeTypes),
	}
	model.apply(ctx, response, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	config.Object = types.StringValue(response.Object)
	config.ID = model.ID
	config.Name = model.Name
	config.Description = model.Description
	config.Metadata = model.Metadata
	config.CreatedAt = model.CreatedAt
	config.UpdatedAt = model.UpdatedAt
	config.Kind = types.StringValue(response.Definition.Kind)
	config.ChatModel = model.ChatModel
	config.EmbeddingModel = model.EmbeddingModel
	config.Options = model.Options
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
