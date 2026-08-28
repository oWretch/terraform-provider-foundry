package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// evaluationsPreviewFeature is the Foundry-Features header value gating every
// resource and data source in the evaluations family: evaluator versions,
// evaluation taxonomies, evaluations, and evaluation rules.
const evaluationsPreviewFeature = "Evaluations"

// evaluationsPreviewFeatureName is the human-readable name used in
// enable_preview and in diagnostics.
const evaluationsPreviewFeatureName = "evaluations"

// evaluationsPreviewNote is prefixed to every evaluations resource and data
// source MarkdownDescription.
const evaluationsPreviewNote = "~> **Preview:** This resource requires `enable_preview = [\"evaluations\"]` on the provider. Preview features may change their inputs, outputs, or behavior in any provider release without following semantic versioning.\n\n"

const evaluationsPreviewNoteDataSource = "~> **Preview:** This data source requires `enable_preview = [\"evaluations\"]` on the provider. Preview features may change their inputs, outputs, or behavior in any provider release without following semantic versioning.\n\n"

// evaluatorMetric mirrors EvaluatorMetric. Metrics are always returned by the
// service (never authored beyond an empty object per metric name), so the
// model only needs to read them back for computed attributes.
type evaluatorMetric struct {
	Type               string   `json:"type,omitempty"`
	DesirableDirection *string  `json:"desirable_direction,omitempty"`
	MinValue           *float64 `json:"min_value,omitempty"`
	MaxValue           *float64 `json:"max_value,omitempty"`
	IsPrimary          bool     `json:"is_primary,omitempty"`
}

// stringListOrNull returns a null list when values is empty, keeping optional
// list-shaped attributes free of a permanent diff against an absent field.
func stringListOrNull(values []string) types.List {
	if len(values) == 0 {
		return types.ListNull(types.StringType)
	}
	elements := make([]attr.Value, len(values))
	for i, v := range values {
		elements[i] = types.StringValue(v)
	}
	list, _ := types.ListValue(types.StringType, elements)
	return list
}
