// Package interceptors adapts the outgoing upstream authorize request to a
// provider's non-standard requirements before the user is redirected to it.
//
// Each interceptor is a per-provider rule: it Matches an issuer and, on match,
// Applies a mutation to the authorize query params. Selection is per-call
// because the issuer is only known at request time (mirroring the
// backend-selection Match in externalmcp). The per-type / Name() /
// injected-slice shape follows the remotemcp/proxy interceptors — there is no
// package-global registry; the set is assembled and injected at construction
// time (see remotesessions.NewChallengeManager).
package interceptors

import (
	"context"
	"net/url"
)

// AuthorizeInterceptor adapts an upstream authorize request to a provider's
// non-standard requirements. Selected per-issuer via Match (mirrors the
// backend-selection Match in externalmcp); on match, ModifyAuthorize mutates the
// authorize query params in place. Holds its own logger, like the
// remotemcp/proxy interceptors.
type AuthorizeInterceptor interface {
	// Name is a stable id for log correlation / tracing.
	Name() string
	// Match reports whether this interceptor applies to the issuer. It
	// receives the full issuer URL so an implementation can key on any
	// property (host, scheme, path, …), not just the host.
	Match(issuerURL string) bool
	// ModifyAuthorize mutates the authorize query in place. Called only on Match.
	ModifyAuthorize(ctx context.Context, q url.Values)
}
