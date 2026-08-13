package assets

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	// fetchURLMaxRedirects caps how many HTTP redirects a user-supplied OpenAPI
	// or image URL fetch will follow. Bounding this keeps the fetch from chasing
	// arbitrary redirect chains supplied by a hostile target.
	fetchURLMaxRedirects = 3
)

// validateFetchURL checks that rawURL is an absolute http(s) URL whose host
// isn't blocked by the guardian SSRF policy. It validates the URL string and
// resolves the host; it does not connect. Runtime enforcement still happens
// via the policy dialer on the subsequent request, including each redirect.
func validateFetchURL(ctx context.Context, policy *guardian.Policy, rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("url scheme must be http or https")
	}

	if u.Host == "" {
		return nil, fmt.Errorf("url must include a host")
	}

	if err := policy.ValidateHost(ctx, u.Hostname()); err != nil {
		return nil, fmt.Errorf("validate host: %w", err)
	}

	return u, nil
}

// outboundFetchClient returns a guardian client that follows at most
// [fetchURLMaxRedirects] hops and re-validates each redirect target against
// the SSRF policy before the next request is issued. The dialer remains the
// last line of defense: even if validation is skipped, ControlContext rejects
// connections into blocked ranges after DNS resolution.
func outboundFetchClient(policy *guardian.Policy, timeout time.Duration) *guardian.HTTPClient {
	client := policy.Client()
	client.Timeout = timeout
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= fetchURLMaxRedirects {
			return fmt.Errorf("stopped after %d redirects", fetchURLMaxRedirects)
		}
		if _, err := validateFetchURL(req.Context(), policy, req.URL.String()); err != nil {
			return fmt.Errorf("redirect url: %w", err)
		}
		return nil
	}
	return client
}

// fetchURLRequestError maps an outbound GET failure onto a client-facing
// bad-request. Blocked and unresolvable hosts are distinguished from generic
// transport faults so an operator who pointed at localhost sees "host is not
// allowed" rather than a generic fetch error; the underlying cause is not
// interpolated into the public message.
func fetchURLRequestError(err error) error {
	if errors.Is(err, guardian.ErrBlockedIP) || errors.Is(err, guardian.ErrBadHost) {
		return oops.E(oops.CodeBadRequest, err, "host is not allowed")
	}
	return oops.E(oops.CodeBadRequest, fmt.Errorf("fetch url: %w", err), "error fetching URL")
}
