package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"

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

// jsonSemanticallyEqual reports whether two JSON documents encode the same
// value, ignoring whitespace and object key order, so a service response that
// only differs in formatting is not treated as a change.
func jsonSemanticallyEqual(a, b []byte) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}

// decodeJSON decodes an HTTP response body into out, used by multipart
// upload paths that build the request manually instead of via client.JSON.
func decodeJSON(body io.Reader, out any) error {
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return fmt.Errorf("decode response body: %w", err)
	}
	return nil
}
