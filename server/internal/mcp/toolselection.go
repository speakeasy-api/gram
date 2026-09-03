// Serve-path loading of the consent-screen tool selection. The session JWT
// stays standard-claims-only, so the policy lives on the user_sessions row
// and is pulled per request through a short-TTL Redis cache keyed by
// (issuer, jti). Failures reject the request (fail closed) — a policy store
// outage must never widen a restrictive session to all tools.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// sessionToolSelectionTTL bounds staleness of the cached policy. The jti in
// the key rotates on every refresh-token slide, so a rotated session never
// reads a predecessor's entry; the TTL only bounds how long a same-jti
// revision (there are none today — rows are written once) could lag.
const sessionToolSelectionTTL = 5 * time.Minute

// errToolSelectionResourceMismatch marks a restrictive selection presented
// against an endpoint other than the one it was consented on. Session tokens
// are issuer-scoped and portable across endpoints sharing the issuer, so the
// caller must 401 into reauth rather than reinterpret the policy.
var errToolSelectionResourceMismatch = errors.New("session tool selection is bound to a different endpoint")

// errToolSelectionLoad marks a post-validation policy-store failure. The
// bearer token itself is valid; the request still fails closed, but logs must
// distinguish this operational or stored-data failure from bad credentials.
var errToolSelectionLoad = errors.New("load session tool selection")

// sessionToolSelectionEntry is the cached (issuer, jti) -> raw policy row.
type sessionToolSelectionEntry struct {
	IssuerID string `json:"issuer_id"`
	JTI      string `json:"jti"`
	// Selection is the raw tool_selection document; empty means NULL (all
	// tools). Kept raw so the cache round-trip cannot normalize away a parse
	// failure — parsing (and its fail-closed error) happens on every read.
	Selection []byte `json:"selection,omitempty"`
}

func sessionToolSelectionCacheKey(issuerID, jti string) string {
	return "sessionToolSelection:" + issuerID + ":" + jti
}

// CacheKey implements cache.CacheableObject.
func (e sessionToolSelectionEntry) CacheKey() string {
	return sessionToolSelectionCacheKey(e.IssuerID, e.JTI)
}

// AdditionalCacheKeys implements cache.CacheableObject.
func (e sessionToolSelectionEntry) AdditionalCacheKeys() []string { return []string{} }

// TTL implements cache.CacheableObject.
func (e sessionToolSelectionEntry) TTL() time.Duration { return sessionToolSelectionTTL }

// endpointToolSelectionResource is the server-authored identity a consent
// selection binds to: the fronting mcp_server when there is one, else the
// toolset. Empty when the endpoint has neither, as with meta-MCP.
func endpointToolSelectionResource(endpoint *ResolvedMcpEndpoint) string {
	switch {
	case endpoint.McpServerID.Valid:
		return "mcp_server:" + endpoint.McpServerID.UUID.String()
	case endpoint.ToolsetID.Valid:
		return "toolset:" + endpoint.ToolsetID.UUID.String()
	default:
		return ""
	}
}

// endpointAcceptsToolSelectionResource reports whether a stored consent
// selection belongs to this endpoint. A toolset-backed wrapper additionally
// accepts the legacy "toolset:<id>" form: selections consented while the
// server was still resolved through toolsets.mcp_slug were stored against the
// toolset resource and must keep authorizing the same server for one
// access-token lifetime after the backfill (AIS-633; removed with the legacy
// audience acceptance by AIS-646).
func endpointAcceptsToolSelectionResource(endpoint *ResolvedMcpEndpoint, resource string) bool {
	if resource == endpointToolSelectionResource(endpoint) {
		return true
	}
	return endpoint.McpServerID.Valid && endpoint.ToolsetID.Valid && resource == "toolset:"+endpoint.ToolsetID.UUID.String()
}

// loadSessionToolSelection resolves the tool policy for a validated session
// jti. Returns nil for an all-tools session. Any error means the request
// must be rejected: missing row (a live jti always has one — refresh
// rotation revokes the old jti before soft-deleting its row), database
// failure, or a malformed stored policy.
func (s *Service) loadSessionToolSelection(ctx context.Context, endpoint *ResolvedMcpEndpoint, jti string) (*toolfilter.SessionSelection, error) {
	issuerID := endpoint.UserSessionIssuerID.String()

	var raw []byte
	if cached, err := s.toolSelectionCache.Get(ctx, sessionToolSelectionCacheKey(issuerID, jti)); err == nil {
		raw = cached.Selection
	} else {
		row, derr := usersessions_repo.New(s.db).GetUserSessionToolSelectionByJTI(ctx, usersessions_repo.GetUserSessionToolSelectionByJTIParams{
			UserSessionIssuerID: endpoint.UserSessionIssuerID,
			Jti:                 jti,
		})
		if derr != nil {
			if errors.Is(derr, pgx.ErrNoRows) {
				return nil, fmt.Errorf("user session row not found for jti")
			}
			return nil, fmt.Errorf("load session tool selection: %w", derr)
		}
		raw = row.ToolSelection
		if cerr := s.toolSelectionCache.Store(ctx, sessionToolSelectionEntry{
			IssuerID:  issuerID,
			JTI:       jti,
			Selection: raw,
		}); cerr != nil {
			s.logger.WarnContext(ctx, "cache session tool selection", attr.SlogError(cerr))
		}
	}

	sel, err := toolfilter.ParseSessionSelection(raw)
	if err != nil {
		return nil, fmt.Errorf("parse stored tool selection: %w", err)
	}
	return sel, nil
}
