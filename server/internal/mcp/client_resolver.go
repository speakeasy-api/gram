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
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
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

// resolveUserSessionClient resolves the user_session_clients row behind a
// presented client_id. Error contract, in the order callers should check:
//
//   - *usersessions.OAuthError: a CIMD spec/policy rejection with a
//     client-safe code + description (resolveClientCIMD only)
//   - errCIMDFetchFailed: document fetch failure; log, render generic
//     (resolveClientCIMD only)
//   - pgx.ErrNoRows: unknown client — includes URL-shaped ids while the
//     flag is off, which deliberately fail closed as unknown even when a
//     previously-resolved row exists, so disabling the flag is a real kill
//     switch for new authorize flows
//   - anything else: infrastructure failure
func (s *Service) resolveUserSessionClient(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, clientID string, mode clientIDResolveMode) (*usersessions_repo.UserSessionClient, error) {
	queries := usersessions_repo.New(s.db)

	if mode == resolveClientCIMD && cimd.IsClientIDURL(clientID) {
		if !s.userSessionCIMDEnabled(ctx, logger, endpoint) {
			return nil, pgx.ErrNoRows
		}

		doc, err := cimd.Resolve(ctx, cimd.NewFetchClient(s.guardianPolicy), clientID)
		if err != nil {
			var oauthErr *usersessions.OAuthError
			if errors.As(err, &oauthErr) {
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

	requestedURL, err := url.Parse(requested)
	if err != nil || requestedURL.User != nil || !cimd.IsLoopbackRedirectURI(requestedURL) {
		return false
	}
	for _, registered := range client.RedirectUris {
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
// strings, covers every component in escaped form — scheme, host, path,
// query, and fragment — so a percent-encoding variant (e.g. /%63allback for
// a registered /callback) or an added fragment cannot slip through the
// variable-port exception.
func loopbackRedirectEqualIgnoringPort(a, b *url.URL) bool {
	stripPort := func(u *url.URL) string {
		c := *u
		c.Host = strings.ToLower(c.Hostname())
		return c.String()
	}
	return stripPort(a) == stripPort(b)
}
