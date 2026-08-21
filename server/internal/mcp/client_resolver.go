// Client resolution seam for the issuer-gated OAuth surface. Every handler
// that resolves a user_session_clients row from a presented client_id —
// authorize, token, consent GET/POST — funnels through
// resolveUserSessionClient, so inbound CIMD resolution and the layers around
// it (admission control and document caching) have exactly one home instead
// of per-handler branches.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// clientIDResolveMode selects how resolveUserSessionClient treats a
// URL-shaped client_id.
type clientIDResolveMode string

const (
	// lookupClientOnly resolves strictly from the database. Used by the
	// token and consent handlers: by the time they run, the authorize leg
	// has already persisted any CIMD row, and mid-flow requests (a code
	// exchange or refresh) must keep working even if the issuer's admission
	// policy changes between legs.
	lookupClientOnly clientIDResolveMode = "lookup_only"

	// resolveClientCIMD additionally treats a URL-shaped client_id as a
	// Client ID Metadata Document reference: the row is read, the document
	// fetched and validated when that row's cache has lapsed, and the row
	// lazily upserted. Authorize-time only — the consent GET/POST re-resolve
	// the client by client_id, so the row must exist before the flow leaves
	// /authorize.
	resolveClientCIMD clientIDResolveMode = "resolve_cimd"
)

// errCIMDFetchFailed marks a transport-level document fetch failure. The
// wrapped cause may name internal network conditions (guardian SSRF denials,
// DNS errors), so handlers must log it and render a generic OAuth error
// rather than echoing it to the client.
var errCIMDFetchFailed = errors.New("cimd document fetch failed")

// An admission denial surfaces as *admission.DenialError, which carries its
// own client-facing Description. An admission denial is customer-configured
// policy rather than a rollout secret, so being explicit is safe and
// useful.

// resolveUserSessionClient resolves the user_session_clients row behind a
// presented client_id. Error contract, in the order callers should check:
//
//   - *admission.DenialError: the issuer's admission policy refused this
//     client_id before any fetch ran; render invalid_client with the
//     error's own Description (resolveClientCIMD only)
//   - *oauthwire.Error: a CIMD spec/policy rejection with a
//     client-safe code + description (resolveClientCIMD only)
//   - errCIMDFetchFailed: document fetch failure; log, render generic
//     (resolveClientCIMD only)
//   - pgx.ErrNoRows: unknown client
//   - anything else: infrastructure failure
func (s *Service) resolveUserSessionClient(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, clientID string, mode clientIDResolveMode) (*usersessions_repo.UserSessionClient, error) {
	queries := usersessions_repo.New(s.db)

	if mode == resolveClientCIMD && cimd.IsClientIDURL(clientID) {
		// Admission runs before the resolver, so a denied client_id costs a
		// map lookup — no outbound request, no fetch timeout. It also runs
		// before the length cap inside the resolver, which is why admission
		// applies its own bound before a catalog miss can reach the database.
		if err := s.admitCIMDClient(ctx, logger, endpoint, clientID); err != nil {
			return nil, err
		}

		// The persisted row doubles as the document cache, so it is read
		// before the fetch rather than only written after one. A miss is
		// the ordinary first-contact case, not an error.
		var cachedRow *usersessions_repo.UserSessionClient
		existing, err := queries.GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
			UserSessionIssuerID: endpoint.UserSessionIssuerID,
			ClientID:            clientID,
		})
		switch {
		case err == nil:
			cachedRow = &existing
		case errors.Is(err, pgx.ErrNoRows):
		default:
			return nil, fmt.Errorf("lookup cimd user session client: %w", err)
		}

		// Only a CIMD-resolved row is a cache. A secret-bearing DCR row
		// that happens to share this client_id must still force a fetch:
		// its metadata never came from a document, and the upsert's guard
		// will refuse to rewrite it anyway.
		//
		// A row with no expiry still contributes its validator. The two
		// columns are independent: a host may serve an ETag with no
		// freshness directives at all, and clearing only the expiry is a
		// request to revalidate now, not to discard what revalidation
		// needs. Discarding both is what a purge is for.
		var cache cimd.CacheState
		if cachedRow != nil && cachedRow.ClientIDMetadataUri.Valid {
			cache = cimd.CacheState{
				ExpiresAt: cachedRow.ClientIDMetadataCacheExpiresAt.Time,
				ETag:      cachedRow.ClientIDMetadataEtag.String,
			}
		}

		result, err := s.cimdResolver.Resolve(ctx, clientID, cache)
		if err != nil {
			// Fail closed on every refresh failure, leaving the cached row
			// as it was: -02 §5.1 says a fetch failure SHOULD abort the
			// authorization request, so an expired document is never served
			// stale to keep a flow alive.
			if _, ok := errors.AsType[*oauthwire.Error](err); ok {
				return nil, fmt.Errorf("resolve cimd client: %w", err)
			}
			return nil, fmt.Errorf("%w: %w", errCIMDFetchFailed, err)
		}

		if result.Outcome != cimd.CacheOutcomeRefreshed && cachedRow == nil {
			// Unreachable: the resolver reports a cache outcome only for
			// cache state this function passed in, and it passes none
			// without a row. Checked rather than dereferenced because the
			// alternative is a panic on an unauthenticated endpoint.
			return nil, fmt.Errorf("cimd resolver reported %q outcome with no cached client", result.Outcome)
		}

		switch result.Outcome {
		case cimd.CacheOutcomeCached:
			return cachedRow, nil
		case cimd.CacheOutcomeNotModified:
			row, err := queries.UpdateUserSessionClientCIMDCache(ctx, usersessions_repo.UpdateUserSessionClientCIMDCacheParams{
				ID:                   cachedRow.ID,
				CacheTtlSeconds:      result.TTL.Seconds(),
				ClientIDMetadataEtag: conv.ToPGTextEmpty(result.ETag),
			})
			if err != nil {
				// No-rows means the row stopped being an updatable CIMD row
				// between the read and the write (revoked, or replaced by a
				// secret-bearing registration); treat it as unknown rather
				// than serving the row the 304 was about.
				if errors.Is(err, pgx.ErrNoRows) {
					return nil, pgx.ErrNoRows
				}
				return nil, fmt.Errorf("update cimd user session client cache: %w", err)
			}
			return &row, nil
		case cimd.CacheOutcomeRefreshed:
			row, err := queries.UpsertUserSessionClientFromCIMD(ctx, usersessions_repo.UpsertUserSessionClientFromCIMDParams{
				UserSessionIssuerID:  endpoint.UserSessionIssuerID,
				ClientID:             clientID,
				ClientName:           result.Document.ClientName,
				RedirectUris:         result.Document.RedirectURIs,
				CacheTtlSeconds:      result.TTL.Seconds(),
				ClientIDMetadataEtag: conv.ToPGTextEmpty(result.ETag),
			})
			if err != nil {
				// No-rows here means the DO UPDATE guard refused to rewrite a
				// secret-bearing row sharing this client_id; surface it as an
				// unknown client rather than a 500.
				if errors.Is(err, pgx.ErrNoRows) {
					return nil, pgx.ErrNoRows
				}
				return nil, fmt.Errorf("upsert cimd user session client: %w", err)
			}
			return &row, nil
		default:
			return nil, fmt.Errorf("unknown cimd resolve outcome %q", result.Outcome)
		}
	}

	row, err := queries.GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: endpoint.UserSessionIssuerID,
		ClientID:            clientID,
	})
	if err != nil {
		return nil, fmt.Errorf("lookup user session client: %w", err)
	}
	return &row, nil
}

// admitCIMDClient enforces the issuer's CIMD admission policy against a
// presented URL-shaped client_id, before any document is fetched. Returns
// nil when the client is admitted, a *admission.DenialError when policy
// denies it, and a wrapped error when the custom-URL lookup itself fails.
//
// The catalog is consulted first and in memory, so the common case — a
// preset client on a presets-mode issuer — never touches the database. Only
// a catalog miss pays for a lookup.
func (s *Service) admitCIMDClient(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, clientID string) error {
	mode, recognized := admission.ResolveMode(endpoint.CIMDAdmissionModeRaw.String, endpoint.CIMDAdmissionModeRaw.Valid)
	if !recognized {
		// A value outside the enum is a data error, never an implicit
		// allow. ResolveMode has already folded it to the closed mode; this
		// logs it at error level because it means something wrote a mode
		// the application does not understand.
		logger.ErrorContext(ctx, "unrecognized cimd admission mode stored on issuer, failing closed",
			attr.SlogCIMDAdmissionMode(endpoint.CIMDAdmissionModeRaw.String),
		)
	}

	decision := admission.Evaluate(mode, clientID)
	if decision.Outcome == admission.OutcomeCheckCustom {
		// The only branch admission cannot decide alone: whether this issuer
		// carries the URL as one of its own entries.
		admitted, err := usersessions_repo.New(s.db).IssuerAdmitsCimdClientURI(ctx, usersessions_repo.IssuerAdmitsCimdClientURIParams{
			UserSessionIssuerID: endpoint.UserSessionIssuerID,
			ClientIDMetadataUri: clientID,
		})
		if err != nil {
			return fmt.Errorf("lookup issuer cimd client: %w", err)
		}
		if admitted {
			decision = admission.Decision{Outcome: admission.OutcomeAdmit, Admit: admission.AdmitCustom, Denial: ""}
		} else {
			decision = admission.Decision{Outcome: admission.OutcomeDeny, Admit: "", Denial: admission.DenialNotListed}
		}
	}

	if decision.Outcome == admission.OutcomeAdmit {
		s.cimdAdmissionMetrics.RecordAdmitted(ctx, mode, decision.Admit)
		return nil
	}

	// Recorded before the enforcement check, so a reporting-mode issuer
	// produces the same counter series a presets-mode one would. Comparing
	// the two across the mode dimension is how the rollout decides whether
	// enforcement is safe.
	s.cimdAdmissionMetrics.RecordDenied(ctx, mode, decision.Denial)

	// The presented client_id goes in the log and never in a metric
	// dimension: this is the URL an operator needs when diagnosing a preset
	// miss, and it is exactly the value too unbounded to be a metric label.
	logAttrs := []any{
		attr.SlogOAuthClientID(truncateClientIDForLog(clientID)),
		attr.SlogCIMDAdmissionMode(mode),
		attr.SlogCIMDAdmissionOutcome(decision.Denial),
	}

	if !mode.Enforces() {
		// Reporting mode: the decision was measured, not applied. A distinct
		// message keeps "denied" honest in the logs, since nothing was.
		logger.InfoContext(ctx, "cimd admission would deny", logAttrs...)
		return nil
	}

	logger.InfoContext(ctx, "cimd admission denied", logAttrs...)
	return &admission.DenialError{Mode: mode, Reason: decision.Denial}
}

// truncateClientIDForLog bounds a presented client_id for logging. The value
// is attacker-chosen on the unauthenticated OAuth surface and is logged at
// points that run before any length validation — a rejected authorize
// request, an admission denial in disabled mode — so an oversized client_id
// could otherwise inflate every line it appears on.
func truncateClientIDForLog(clientID string) string {
	if len(clientID) <= admission.MaxClientIDLength {
		return clientID
	}
	return clientID[:admission.MaxClientIDLength] + "…(truncated)"
}

// redirectURIMatches reports whether the request's redirect_uri matches an
// entry registered on the client row. The rule is exact string matching (RFC
// 9700 §4.1.3) with exactly one exception, applied to CIMD-resolved rows
// only: when both the registered and requested URIs are RFC 8252 §7.3
// loopback redirects, the port is ignored. RFC 8252 requires the AS to allow
// variable loopback ports for native apps — Claude Code binds an OS-assigned
// ephemeral port per invocation — and RFC 9700 preserves that carve-out. The
// port is the ONLY component allowed to vary: every other component must
// match in escaped form, and neither side may carry userinfo — otherwise an
// attacker-crafted authorize URL could inject extra query parameters, an
// encoding-variant path, or browser-sent Basic credentials into the
// legitimate client's local callback. DCR rows keep byte-exact matching.
func redirectURIMatches(client *usersessions_repo.UserSessionClient, requested string) bool {
	if slices.Contains(client.RedirectUris, requested) {
		return true
	}
	if !client.ClientIDMetadataUri.Valid {
		return false
	}

	// Fragments disqualify the exception on either side: RFC 6749 §3.1.2
	// forbids fragments in redirect URIs, and url.Parse cannot distinguish
	// an absent fragment from an explicit empty one ("...#") — URL.String()
	// drops the latter, which would let a malformed registered URI match a
	// fragment-less request. A raw-string check is the only reliable guard.
	if strings.Contains(requested, "#") {
		return false
	}
	requestedURL, err := url.Parse(requested)
	if err != nil || requestedURL.User != nil || !cimd.IsLoopbackRedirectURI(requestedURL) {
		return false
	}
	for _, registered := range client.RedirectUris {
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
// Callers must have rejected fragments on both sides already: URL.String()
// cannot represent an explicit empty fragment.
func loopbackRedirectEqualIgnoringPort(a, b *url.URL) bool {
	stripPort := func(u *url.URL) string {
		c := *u
		c.Host = strings.ToLower(c.Hostname())
		return c.String()
	}
	return stripPort(a) == stripPort(b)
}
