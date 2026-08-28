package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

// versionedParent is the shape shared by the Skills and Toolboxes preview
// APIs: a named container that tracks a default_version pointer over an
// immutable chain of versions. Both foundry_skill and foundry_toolbox are the
// single owner of this relationship: Update publishes a new version (or, for
// toolboxes, both publishes a version and repoints the default), never
// mutates a version in place.
//
// parentPath returns the collection path, e.g. "skills" or "toolboxes".
// versionsPath returns "<parentPath>/<name>/versions".
// versionPath returns "<parentPath>/<name>/versions/<version>".
func parentPath(kind, name string) string {
	return kind + "/" + name
}

func versionsPath(kind, name string) string {
	return kind + "/" + name + "/versions"
}

func versionPath(kind, name, version string) string {
	return kind + "/" + name + "/versions/" + version
}

// deleteParent deletes a skill or toolbox parent by name, treating an
// already-absent parent as success.
func deleteParent(ctx context.Context, gate previewGate, kind, name string) error {
	client := gate.client
	ctx, err := gate.previewContext(ctx)
	if err != nil {
		return err
	}
	err = client.JSON(ctx, http.MethodDelete, parentPath(kind, name), nil, nil)
	if clients.IsNotFound(err) {
		return nil
	}
	return err
}

// defaultVersionRequest is the body shared by the skill and toolbox
// "promote a version to default" calls.
type defaultVersionRequest struct {
	DefaultVersion string `json:"default_version"`
}

// promoteDefaultVersion updates the parent's default_version pointer to
// version, using method and path conventions that differ slightly between
// skills (POST .../{name}) and toolboxes (PATCH .../{name}).
func promoteDefaultVersion(ctx context.Context, gate previewGate, kind, name, version, method string) error {
	client := gate.client
	ctx, err := gate.previewContext(ctx)
	if err != nil {
		return err
	}
	return client.JSON(ctx, method, parentPath(kind, name), defaultVersionRequest{DefaultVersion: version}, nil)
}

// rawJSONOrEmpty converts a nullable/unknown Terraform string holding a JSON
// document into a json.RawMessage, or nil when the attribute isn't set, so
// omitempty on the request struct drops it entirely rather than sending "".
func rawJSONOrEmpty(value types.String) []byte {
	if value.IsNull() || value.IsUnknown() || value.ValueString() == "" {
		return nil
	}
	return []byte(value.ValueString())
}

// stringFromRawJSON renders a json.RawMessage back into a Terraform string
// attribute, keeping it null when the service omitted the field.
func stringFromRawJSON(value json.RawMessage) types.String {
	if len(value) == 0 {
		return types.StringNull()
	}
	return types.StringValue(string(value))
}

// decodeJSON decodes an HTTP response body into out, used by multipart
// upload paths that build the request manually instead of via client.JSON.
func decodeJSON(body io.Reader, out any) error {
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return fmt.Errorf("decode response body: %w", err)
	}
	return nil
}
