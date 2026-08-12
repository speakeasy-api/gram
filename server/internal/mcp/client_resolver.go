// Client resolution seam for the issuer-gated OAuth surface. Every handler
// that resolves a user_session_clients row from a presented client_id —
// authorize, token, consent GET/POST — funnels through
// resolveUserSessionClient, so inbound CIMD resolution (and its future
// layers: document caching, per-origin rate limits, admission control) has
// exactly one home instead of per-handler branches.

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
	"github.com/speakeasy-api/gram/server/internal/feature"
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
	// exchange or refresh) must keep working even if the CIMD flag flips
	// off between legs.
	lookupClientOnly clientIDResolveMode = "lookup_only"

	// resolveClientCIMD additionally treats a URL-shaped client_id as a
	// Client ID Metadata Document reference when the issuer organization's
	// gram-user-session-cimd flag is on: the document is fetched and
	// validated on every call (no caching) and the row lazily
	// upserted. Authorize-time only — the consent GET/POST re-resolve the
	// client by client_id, so the row must exist before the flow leaves
	// /authorize.
	resolveClientCIMD clientIDResolveMode = "resolve_cimd"
)

// errCIMDFetchFailed marks a transport-level document fetch failure. The
// wrapped cause may name internal network conditions (guardian SSRF denials,
// DNS errors), so handlers must log it and render a generic OAuth error
// rather than echoing it to the client.
var errCIMDFetchFailed = errors.New("cimd document fetch failed")

// errCIMDDisabled marks a URL-shaped client_id rejected because the issuer
// organization's gram-user-session-cimd flag is off. Handlers must render it
// exactly like an unknown client (invalid_client, "unknown client_id") so
// the wire response does not leak per-organization flag state; the dedicated
// sentinel exists so the flag-off path is loggable and is not conflated
// with pgx.ErrNoRows from a lookup that actually ran.
var errCIMDDisabled = errors.New("cimd disabled for organization")

// An admission denial surfaces as *admission.DenialError, which carries its
// own client-facing Description. Unlike errCIMDDisabled — whose wire
// response is deliberately generic so it cannot be used to probe
// per-organization flag state — an admission denial is customer-configured
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
//   - errCIMDDisabled: URL-shaped client_id while the flag is off; render
//     as unknown client (resolveClientCIMD only). Deliberately fails closed
//     even when a previously-resolved row exists, so disabling the flag is
//     a real off switch for new authorize flows
//   - pgx.ErrNoRows: unknown client
//   - anything else: infrastructure failure
func (s *Service) resolveUserSessionClient(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, clientID string, mode clientIDResolveMode) (*usersessions_repo.UserSessionClient, error) {
	queries := usersessions_repo.New(s.db)

	if mode == resolveClientCIMD && cimd.IsClientIDURL(clientID) {
		if !s.userSessionCIMDEnabled(ctx, logger, endpoint) {
			return nil, errCIMDDisabled
		}

		// Admission runs before the resolver, so a denied client_id costs a
		// map lookup — no outbound request, no fetch timeout, and (once
		// AIS-215 lands) no rate-limiter token. It also runs before the
		// length cap inside the resolver, which is why admission applies its
		// own bound before a catalog miss can reach the database.
		if err := s.admitCIMDClient(ctx, logger, endpoint, clientID); err != nil {
			return nil, err
		}

		doc, err := s.cimdResolver.Resolve(ctx, clientID)
		if err != nil {
			if _, ok := errors.AsType[*oauthwire.Error](err); ok {
				return nil, fmt.Errorf("resolve cimd client: %w", err)
			}
			return nil, fmt.Errorf("%w: %w", errCIMDFetchFailed, err)
		}

		row, err := queries.UpsertUserSessionClientFromCIMD(ctx, usersessions_repo.UpsertUserSessionClientFromCIMDParams{
			UserSessionIssuerID: endpoint.UserSessionIssuerID,
			ClientID:            clientID,
			ClientName:          doc.ClientName,
			RedirectUris:        doc.RedirectURIs,
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

// userSessionCIMDEnabled evaluates the inbound-CIMD rollout flag for the
// endpoint's organization. distinctID is the organization ID with no groups
// (matching the flag's PostHog targeting). Successful evaluations are
// remembered per organization so a flag-provider outage degrades to the
// last known state instead of failing closed — this runs on unauthenticated
// endpoints (/authorize and the well-known metadata), where fail-closed
// would turn a PostHog outage into an OAuth login outage for every
// CIMD-enabled organization. Fresh evaluations always win, so flag flips
// propagate immediately; the map holds one bool per organization that
// touches the OAuth surface, so growth is bounded by tenant count.
func (s *Service) userSessionCIMDEnabled(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint) bool {
	enabled, err := s.features.IsFlagEnabled(ctx, feature.FlagUserSessionCIMD, endpoint.OrganizationID, nil)
	if err != nil {
		s.cimdOrgFlagMu.RLock()
		lastKnown, ok := s.cimdOrgFlagLastKnown[endpoint.OrganizationID]
		s.cimdOrgFlagMu.RUnlock()
		logger.WarnContext(ctx, "evaluate user session cimd flag", attr.SlogError(err))
		return ok && lastKnown
	}

	s.cimdOrgFlagMu.Lock()
	s.cimdOrgFlagLastKnown[endpoint.OrganizationID] = enabled
	s.cimdOrgFlagMu.Unlock()
	return enabled
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
