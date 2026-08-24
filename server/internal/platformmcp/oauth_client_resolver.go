// Client resolution seam for the Platform MCP authorization server. Every
// handler that resolves a registered client from a presented client_id —
// authorize, organization selection, connect, token — funnels through
// resolveClient, so inbound CIMD resolution and the layers around it
// (admission control and document caching) have exactly one home instead of
// per-handler branches.
//
// This mirrors internal/mcp/client_resolver.go, which does the same job for
// the hosted, issuer-gated authorization server. The two share the resolver,
// the validator, and the admission vocabulary but not their storage: hosted
// clients hang off a project's issuer, platform clients are global because
// registration happens before browser authentication and organization
// selection.

package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

// platformCIMDAdmissionMode is the admission policy this authorization
// server applies to URL-shaped client_ids. Unlike a hosted issuer, it is a
// compile-time constant: Platform MCP has one, first-party authorization
// server with no per-tenant configuration surface to hang a mode off.
//
// Open is the deliberate choice. Client identity is not the security
// boundary here — every flow still passes IDP login, organization
// selection, the live org:admin recheck in gateAndAuthorize, and explicit
// user consent — and dynamic client registration on this same server is
// already open to anyone, so admitting spec-valid documents adds no trust
// that DCR does not already grant. CIMD is in fact the stronger of the two:
// the client_id is a fetchable URL bound to an origin we can show the user,
// rather than an opaque identifier chosen anonymously.
//
// Decisions are counted under the same cimd.admission.decisions instrument
// the hosted AS uses, so tightening this to ModePresets later is a constant
// change informed by real data rather than a guess. ModePresets on this
// server denies anything outside the curated catalog: there is no
// per-issuer custom URL table to consult.
const platformCIMDAdmissionMode = admission.ModeOpen

// clientMetadataResolver is the subset of *cimd.Resolver this package uses,
// declared as an interface so tests can host documents on an httptest
// server without a guardian policy.
type clientMetadataResolver interface {
	Resolve(ctx context.Context, clientID string, cache cimd.CacheState) (*cimd.CacheResult, error)
}

// errCIMDFetchFailed marks a transport-level document fetch failure. The
// wrapped cause may name internal network conditions (guardian SSRF denials,
// DNS errors), so handlers must log it and render a generic OAuth error
// rather than echoing it to the client.
var errCIMDFetchFailed = errors.New("cimd document fetch failed")

// clientResolveMode selects how resolveClient treats a URL-shaped client_id.
type clientResolveMode string

const (
	// lookupClientOnly resolves strictly from the store. Used by every leg
	// after authorize: the row was persisted there, and a mid-flow request
	// must keep working even if the document host is briefly unreachable.
	lookupClientOnly clientResolveMode = "lookup_only"

	// resolveClientCIMD additionally treats a URL-shaped client_id as a
	// Client ID Metadata Document reference: the stored row is read for its
	// cache state, the document fetched and validated when that cache has
	// lapsed, and the row lazily upserted. Authorize-time only.
	resolveClientCIMD clientResolveMode = "resolve_cimd"
)

// resolveClient resolves the registered client behind a presented client_id.
// Error contract, in the order callers should check:
//
//   - *admission.DenialError: policy refused this client_id before any fetch
//     ran; render invalid_client with the error's own Description
//     (resolveClientCIMD only)
//   - *oauthwire.Error: a CIMD spec/policy rejection with a client-safe code
//     and description (resolveClientCIMD only)
//   - errCIMDFetchFailed: document fetch failure; log it, render generic
//     (resolveClientCIMD only)
//   - platformoauth.ErrNotFound / ErrRevoked: unknown or revoked client
//   - anything else: infrastructure failure
func (s *OAuthHTTP) resolveClient(ctx context.Context, clientID string, mode clientResolveMode) (platformoauth.Client, error) {
	if mode != resolveClientCIMD || s.cimd == nil || !cimd.IsClientIDURL(clientID) {
		client, err := s.store.GetClient(ctx, clientID)
		if err != nil {
			return platformoauth.Client{}, fmt.Errorf("lookup platform client: %w", err)
		}
		return client, nil
	}

	// Admission runs before the resolver, so a denied client_id costs a map
	// lookup — no outbound request, no fetch timeout.
	if err := s.admitCIMDClient(ctx, clientID); err != nil {
		return platformoauth.Client{}, err
	}

	// The persisted row doubles as the document cache, so it is read before
	// the fetch rather than only written after one. A miss is the ordinary
	// first-contact case, not an error. Only a CIMD row is a cache: a
	// secret-bearing registration sharing this client_id must still force a
	// fetch, and the upsert's guard will refuse to rewrite it anyway.
	var cache cimd.CacheState
	cached, err := s.store.GetClient(ctx, clientID)
	switch {
	case err == nil:
		if cached.IsCIMD() && cached.CacheExpiresAt != nil {
			cache = cimd.CacheState{ExpiresAt: *cached.CacheExpiresAt, ETag: cached.ETag}
		}
	case errors.Is(err, platformoauth.ErrNotFound):
	default:
		return platformoauth.Client{}, fmt.Errorf("lookup cimd platform client: %w", err)
	}

	result, err := s.cimd.Resolve(ctx, clientID, cache)
	if err != nil {
		// Fail closed on every refresh failure, leaving the cached row as it
		// was: -02 §5.1 says a fetch failure SHOULD abort the authorization
		// request, so an expired document is never served stale to keep a
		// flow alive. A spec rejection is already client-safe; anything else
		// may name internal network conditions.
		if _, ok := errors.AsType[*oauthwire.Error](err); ok {
			return platformoauth.Client{}, fmt.Errorf("resolve cimd client: %w", err)
		}
		return platformoauth.Client{}, fmt.Errorf("%w: %w", errCIMDFetchFailed, err)
	}

	switch result.Outcome {
	case cimd.CacheOutcomeCached:
		return cached, nil
	case cimd.CacheOutcomeNotModified:
		client, err := s.store.TouchClientCIMDCache(ctx, platformoauth.TouchCIMDCacheInput{ClientID: clientID, CacheTTL: result.TTL, ETag: result.ETag})
		if err != nil {
			// No such updatable row means it stopped being a CIMD row
			// between the read and the write (revoked, or replaced by a
			// secret-bearing registration); the caller renders that as an
			// unknown client rather than serving what the 304 was about.
			return platformoauth.Client{}, fmt.Errorf("refresh cimd client cache: %w", err)
		}
		return client, nil
	case cimd.CacheOutcomeRefreshed:
		client, err := s.store.UpsertClientFromCIMD(ctx, platformoauth.UpsertCIMDClientInput{
			ClientID:     clientID,
			Name:         result.Document.ClientName,
			RedirectURIs: result.Document.RedirectURIs,
			CacheTTL:     result.TTL,
			ETag:         result.ETag,
		})
		if err != nil {
			return platformoauth.Client{}, fmt.Errorf("upsert cimd client: %w", err)
		}
		return client, nil
	default:
		return platformoauth.Client{}, fmt.Errorf("unknown cimd resolve outcome %q", result.Outcome)
	}
}

// admitCIMDClient enforces platformCIMDAdmissionMode against a presented
// URL-shaped client_id, before any document is fetched. Returns nil when the
// client is admitted and a *admission.DenialError when policy denies it.
func (s *OAuthHTTP) admitCIMDClient(ctx context.Context, clientID string) error {
	decision := admission.Evaluate(platformCIMDAdmissionMode, clientID)
	if decision.Outcome == admission.OutcomeCheckCustom {
		// The branch that asks the caller to consult an issuer's own URL
		// rows. Platform MCP has no such table — it is a single first-party
		// server, not a per-tenant one — so a catalog miss is final.
		decision = admission.Decision{Outcome: admission.OutcomeDeny, Admit: "", Denial: admission.DenialNotListed}
	}

	if decision.Outcome == admission.OutcomeAdmit {
		s.cimdAdmission.RecordAdmitted(ctx, platformCIMDAdmissionMode, decision.Admit)
		return nil
	}

	// Recorded before the enforcement check, so a reporting mode produces
	// the same counter series an enforcing one would.
	s.cimdAdmission.RecordDenied(ctx, platformCIMDAdmissionMode, decision.Denial)

	// The presented client_id goes in the log and never in a metric
	// dimension: it is attacker-chosen and unbounded on this surface.
	logAttrs := []any{
		attr.SlogOAuthClientID(truncateClientIDForLog(clientID)),
		attr.SlogCIMDAdmissionMode(platformCIMDAdmissionMode),
		attr.SlogCIMDAdmissionOutcome(decision.Denial),
	}
	if !platformCIMDAdmissionMode.Enforces() {
		s.logger.InfoContext(ctx, "cimd admission would deny", logAttrs...)
		return nil
	}
	s.logger.InfoContext(ctx, "cimd admission denied", logAttrs...)
	return &admission.DenialError{Mode: platformCIMDAdmissionMode, Reason: decision.Denial}
}

// truncateClientIDForLog bounds a presented client_id for logging. The value
// is attacker-chosen on the unauthenticated OAuth surface and is logged at
// points that run before any length validation, so an oversized client_id
// could otherwise inflate every line it appears on.
func truncateClientIDForLog(clientID string) string {
	if len(clientID) <= admission.MaxClientIDLength {
		return clientID
	}
	return clientID[:admission.MaxClientIDLength] + "…(truncated)"
}

// clientRedirectAllowed reports whether a request's redirect_uri matches one
// registered on the client. The rule is exact string matching (RFC 9700
// §4.1.3) with exactly one exception, applied to CIMD-resolved clients only:
// when both the registered and requested URIs are RFC 8252 §7.3 loopback
// redirects, the port is ignored. RFC 8252 requires the AS to allow variable
// loopback ports for native apps — Claude Code binds an OS-assigned
// ephemeral port per invocation — and RFC 9700 preserves that carve-out. The
// port is the ONLY component allowed to vary: every other component must
// match in escaped form, and neither side may carry userinfo. Dynamically
// registered clients keep byte-exact matching.
func clientRedirectAllowed(client platformoauth.Client, requested string) bool {
	if slices.Contains(client.RedirectURIs, requested) {
		return true
	}
	if !client.IsCIMD() {
		return false
	}

	// Fragments disqualify the exception on either side: RFC 6749 §3.1.2
	// forbids fragments in redirect URIs, and url.Parse cannot distinguish
	// an absent fragment from an explicit empty one ("...#") — URL.String()
	// drops the latter, which would let a malformed registered URI match a
	// fragment-less request.
	if strings.Contains(requested, "#") {
		return false
	}
	requestedURL, err := url.Parse(requested)
	if err != nil || requestedURL.User != nil || !cimd.IsLoopbackRedirectURI(requestedURL) {
		return false
	}
	for _, registered := range client.RedirectURIs {
		if strings.Contains(registered, "#") {
			continue
		}
		registeredURL, err := url.Parse(registered)
		if err != nil || registeredURL.User != nil || !cimd.IsLoopbackRedirectURI(registeredURL) {
			continue
		}
		if loopbackRedirectEqualIgnoringPort(registeredURL, requestedURL) {
			return true
		}
	}
	return false
}

// loopbackRedirectEqualIgnoringPort reports whether two parsed loopback
// redirect URIs are identical except for the port. Rebuilding each URI with
// the port stripped and the host lowercased, then comparing the resulting
// strings, covers every remaining component in escaped form — scheme, host,
// path, and query — so a percent-encoding variant (e.g. /%63allback for a
// registered /callback) cannot slip through the variable-port exception.
// Callers must have rejected fragments on both sides already.
func loopbackRedirectEqualIgnoringPort(a, b *url.URL) bool {
	stripPort := func(u *url.URL) string {
		c := *u
		c.Host = strings.ToLower(c.Hostname())
		return c.String()
	}
	return stripPort(a) == stripPort(b)
}

// clientIDOrigin is the host a CIMD client's metadata document was fetched
// from, for display on the consent page. Empty for a dynamically registered
// client, whose identifier says nothing about who it belongs to.
func clientIDOrigin(client platformoauth.Client) string {
	if !client.IsCIMD() {
		return ""
	}
	parsed, err := url.Parse(client.MetadataURI)
	if err != nil {
		return ""
	}
	return parsed.Host
}
