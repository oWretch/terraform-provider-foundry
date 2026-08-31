package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ datasource.DataSource              = &filesDataSource{}
	_ datasource.DataSourceWithConfigure = &filesDataSource{}
)

func NewFilesDataSource() datasource.DataSource {
	return &filesDataSource{}
}

type filesDataSource struct {
	client *clients.Client
}

type filesDataSourceModel struct {
	Purpose types.String    `tfsdk:"purpose"`
	Files   []fileListModel `tfsdk:"files"`
}

type fileListModel struct {
	ID        types.String `tfsdk:"id"`
	Filename  types.String `tfsdk:"filename"`
	Purpose   types.String `tfsdk:"purpose"`
	Bytes     types.Int64  `tfsdk:"bytes"`
	CreatedAt types.Int64  `tfsdk:"created_at"`
	Status    types.String `tfsdk:"status"`
}

type fileListItem struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	Purpose   string `json:"purpose"`
	Bytes     int64  `json:"bytes"`
	CreatedAt int64  `json:"created_at"`
	Status    string `json:"status"`
}

func (d *filesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_files"
}

func (d *filesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the files uploaded to the project. Files are identified by a service-assigned ID rather than by name, so this data source is the way to find the ID of a file uploaded outside this configuration. Filenames are not unique; filter on `purpose` and match on `filename` in the configuration if a specific file is needed.",
		Attributes: map[string]schema.Attribute{
			"purpose": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Return only files uploaded with this purpose, such as `assistants` or `evals`. Omit to return every file.",
			},
			"files": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Files available to the project, oldest first.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":         schema.StringAttribute{Computed: true, MarkdownDescription: "Service-assigned file identifier, used by `foundry_vector_store_file` and by tools that reference a file."},
						"filename":   schema.StringAttribute{Computed: true, MarkdownDescription: "Name the service recorded for the file. Not unique."},
						"purpose":    schema.StringAttribute{Computed: true, MarkdownDescription: "Purpose the file was uploaded with."},
						"bytes":      schema.Int64Attribute{Computed: true, MarkdownDescription: "Size of the file in bytes."},
						"created_at": schema.Int64Attribute{Computed: true, MarkdownDescription: "Unix timestamp when the file was uploaded."},
						"status":     schema.StringAttribute{Computed: true, MarkdownDescription: "Processing status of the file."},
					},
				},
			},
		},
	}
}

func (d *filesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *filesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config filesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	items, err := listAllPages[fileListItem](ctx, d.client, "openai/v1/files")
	if err != nil {
		resp.Diagnostics.AddError("Unable to list files", err.Error())
		return
	}

	// The list route accepts a purpose filter, but filtering here keeps the
	// paging walk to a single code path and the result is identical.
	purpose := config.Purpose.ValueString()
	config.Files = make([]fileListModel, 0, len(items))
	for _, item := range items {
		if purpose != "" && item.Purpose != purpose {
			continue
		}
		config.Files = append(config.Files, fileListModel{
			ID:        types.StringValue(item.ID),
			Filename:  types.StringValue(item.Filename),
			Purpose:   types.StringValue(item.Purpose),
			Bytes:     types.Int64Value(item.Bytes),
			CreatedAt: types.Int64Value(item.CreatedAt),
			Status:    types.StringValue(item.Status),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
