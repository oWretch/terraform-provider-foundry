package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

// maxListPages bounds a cursor walk so a service that never clears has_more
// cannot spin an apply forever. At the service's maximum page size of 100 this
// covers 10,000 items, well past any plausible project.
const maxListPages = 100

// cursorPage is one page of an OpenAI-style list response. These routes page
// with limit/after and report has_more rather than returning a next link.
type cursorPage[T any] struct {
	Data    []T    `json:"data"`
	HasMore bool   `json:"has_more"`
	LastID  string `json:"last_id"`
}

// listAllPages walks a cursor-paged list route and returns every item.
//
// The service caps a page below the number of items a project can hold and sets
// has_more even for a single item, so reading only the first page would silently
// truncate the result and make the data source quietly wrong.
func listAllPages[T any](ctx context.Context, client *clients.Client, path string) ([]T, error) {
	var all []T
	after := ""

	for page := 0; page < maxListPages; page++ {
		query := url.Values{"limit": {"100"}}
		if after != "" {
			query.Set("after", after)
		}

		var decoded cursorPage[T]
		if err := client.JSON(ctx, http.MethodGet, path+"?"+query.Encode(), nil, &decoded); err != nil {
			return nil, err
		}

		all = append(all, decoded.Data...)
		// A page can report has_more with an empty last_id, which would restart
		// the walk from the beginning and never terminate.
		if !decoded.HasMore || decoded.LastID == "" || decoded.LastID == after {
			return all, nil
		}
		after = decoded.LastID
	}

	return nil, fmt.Errorf("listing %s did not finish within %d pages", path, maxListPages)
}
