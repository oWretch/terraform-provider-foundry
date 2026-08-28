package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

// previewGate is embedded by resources and data sources that require a
// provider enable_preview opt-in. It implements ValidateConfig so
// terraform validate/plan reject use of the feature before any API call,
// instead of the service returning an opaque error at apply time.
type previewGate struct {
	client  *clients.Client
	feature string // Foundry-Features header token, e.g. "Evaluations"
	name    string // enable_preview value users write, e.g. "evaluations"
}

// previewContext returns a context carrying this gate's Foundry-Features
// header, erroring if the feature was not opted into via enable_preview.
func (g previewGate) previewContext(ctx context.Context) (context.Context, error) {
	return g.client.WithPreviewFeature(ctx, g.name, g.feature)
}

func (g previewGate) validate(diagnostics interface {
	AddError(summary, detail string)
}) {
	// client is nil until Configure runs; an unconfigured `terraform validate`
	// skips this check and the same error surfaces at plan time instead.
	if g.client == nil {
		return
	}
	if !g.client.PreviewEnabled(g.name) {
		diagnostics.AddError(
			"Preview feature not enabled",
			`This resource requires the "`+g.name+`" preview feature. Add it to the provider's enable_preview attribute to use it.`,
		)
	}
}

func (g previewGate) ValidateResourceConfig(_ context.Context, _ resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	g.validate(&resp.Diagnostics)
}

func (g previewGate) ValidateDataSourceConfig(_ context.Context, _ datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	g.validate(&resp.Diagnostics)
}
