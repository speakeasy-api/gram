package assets

import (
	"errors"
	"fmt"
	"net/http"
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
		if _, err := policy.ValidateHTTPSURL(req.Context(), req.URL.String()); err != nil {
			return fmt.Errorf("redirect url: %w", err)
		}
		return nil
	}
	return client
}

// fetchURLRequestError maps an outbound GET failure onto a client-facing
// bad-request. A blocked IP is distinguished from generic transport faults so
// an operator who pointed at localhost sees "host is not allowed" rather than
// a generic fetch error. Unresolvable hosts fall through to the generic
// message: [guardian.ErrBadHost] is a DNS/address failure, not an SSRF deny.
// The underlying cause is not interpolated into the public message.
func fetchURLRequestError(err error) error {
	if errors.Is(err, guardian.ErrBlockedIP) {
		return oops.E(oops.CodeBadRequest, err, "host is not allowed")
	}
	return oops.E(oops.CodeBadRequest, fmt.Errorf("fetch url: %w", err), "error fetching URL")
}
