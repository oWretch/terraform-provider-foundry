package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// operationPollInterval is the delay between status checks when the service
// does not send Retry-After. The model registry answers in a few seconds, so a
// short interval keeps an apply responsive without hammering the service.
const operationPollInterval = 3 * time.Second

// maxOperationPollInterval caps a Retry-After the service might send, so a
// single large value cannot stall an apply past the point of usefulness.
const maxOperationPollInterval = 30 * time.Second

// WaitForOperation polls a long-running operation until it finishes, then
// decodes the result into out.
//
// The location comes from the service rather than from the caller, and the
// model registry reports it on its own host rather than on the project
// endpoint, so it cannot go through NewRequest. The host is checked against the
// cloud's operation host before the request is sent, because the client's
// credential is attached to every request the client makes and must not reach
// an unrelated host if a location is ever malformed or attacker-influenced.
//
// Polling stops when ctx is cancelled, so a practitioner interrupting an apply
// is not left waiting on the service.
func (c *Client) WaitForOperation(ctx context.Context, location string, out any) error {
	target, err := c.operationURL(location)
	if err != nil {
		return err
	}

	for {
		done, retryAfter, err := c.pollOperation(ctx, target, out)
		if err != nil {
			return err
		}
		if done {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for operation %s: %w", target, ctx.Err())
		case <-time.After(retryAfter):
		}
	}
}

// operationURL validates a service-supplied operation location before the
// client's credential is attached to a request for it.
func (c *Client) operationURL(location string) (string, error) {
	target, err := url.Parse(location)
	if err != nil {
		return "", fmt.Errorf("parse operation location: %w", err)
	}
	if target.Scheme != "https" {
		return "", fmt.Errorf("operation location must use https, got %q", target.Scheme)
	}

	host := target.Hostname()
	if host != c.endpoint.Hostname() && !hostMatches(host, c.operationHostSuffix) {
		return "", fmt.Errorf("operation location host %q is neither the project endpoint nor %q", host, c.operationHostSuffix)
	}
	return target.String(), nil
}

// hostMatches reports whether host is the suffix itself or a subdomain of it,
// so that a lookalike such as "notapi.azureml.ms.example.com" is rejected.
func hostMatches(host, suffix string) bool {
	if suffix == "" {
		return false
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	suffix = strings.ToLower(suffix)
	return host == suffix || strings.HasSuffix(host, "."+suffix)
}

// pollOperation performs one status check. A 200 carries the finished resource;
// a 202 means the operation is still running.
func (c *Client) pollOperation(ctx context.Context, target string, out any) (bool, time.Duration, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false, 0, fmt.Errorf("create operation request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", c.userAgent)

	response, err := c.Do(request)
	if err != nil {
		return false, 0, err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusAccepted {
		return false, retryAfter(response), nil
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return true, 0, nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return false, 0, fmt.Errorf("decode operation result: %w", err)
	}
	return true, 0, nil
}

// retryAfter honours a Retry-After header expressed in seconds, falling back to
// the default interval when it is absent or unusable.
func retryAfter(response *http.Response) time.Duration {
	seconds, err := strconv.Atoi(response.Header.Get("Retry-After"))
	if err != nil || seconds <= 0 {
		return operationPollInterval
	}
	return min(time.Duration(seconds)*time.Second, maxOperationPollInterval)
}
