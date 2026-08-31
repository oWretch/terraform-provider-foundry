package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

// ponytail: bound malformed pagination; raise this only if a project can exceed 2,000 latest assets.
const maxAssetListPages = 100

type assetPage[T any] struct {
	Value    []T    `json:"value"`
	NextLink string `json:"nextLink"`
}

func assetVersionPath(kind, name, version string) string {
	return kind + "/" + url.PathEscape(name) + "/versions/" + url.PathEscape(version)
}

func assetNamePath(kind, name string) string {
	return kind + "/" + url.PathEscape(name)
}

func applyOptionalAssetString(current types.String, value string) types.String {
	if value == "" && !current.IsNull() {
		return types.StringValue("")
	}
	return optionalString(value)
}

func splitNameVersion(id string) (string, string, error) {
	name, version, found := strings.Cut(id, "/")
	if !found || name == "" || version == "" {
		return "", "", fmt.Errorf("expected import ID in the form URL-escaped-name/URL-escaped-version, got %q", id)
	}

	name, err := url.PathUnescape(name)
	if err != nil {
		return "", "", fmt.Errorf("unescape imported asset name: %w", err)
	}
	version, err = url.PathUnescape(version)
	if err != nil {
		return "", "", fmt.Errorf("unescape imported asset version: %w", err)
	}
	if name == "" || version == "" {
		return "", "", fmt.Errorf("expected import ID in the form URL-escaped-name/URL-escaped-version, got %q", id)
	}
	return name, version, nil
}

func readLatestAsset[T any](ctx context.Context, client *clients.Client, kind, name string, itemName func(T) string) (T, bool, error) {
	var zero T
	path := kind

	for pageNumber := 0; pageNumber < maxAssetListPages; pageNumber++ {
		var page assetPage[T]
		if err := client.JSON(ctx, http.MethodGet, path, nil, &page); err != nil {
			return zero, false, err
		}
		for _, item := range page.Value {
			if itemName(item) == name {
				return item, true, nil
			}
		}
		if page.NextLink == "" {
			return zero, false, nil
		}

		next, err := assetNextLinkPath(page.NextLink, kind)
		if err != nil {
			return zero, false, err
		}
		path = next
	}

	return zero, false, fmt.Errorf("listing latest %s did not finish within %d pages", kind, maxAssetListPages)
}

func assetNextLinkPath(nextLink, kind string) (string, error) {
	next, err := url.Parse(nextLink)
	if err != nil {
		return "", fmt.Errorf("parse %s nextLink: %w", kind, err)
	}

	marker := "/" + kind
	index := strings.LastIndex(next.EscapedPath(), marker)
	if index < 0 && next.IsAbs() {
		return "", fmt.Errorf("%s nextLink does not contain the collection path", kind)
	}
	path := strings.TrimLeft(next.EscapedPath(), "/")
	if index >= 0 {
		path = strings.TrimLeft(next.EscapedPath()[index:], "/")
	}
	if next.RawQuery != "" {
		path += "?" + next.RawQuery
	}
	return path, nil
}
