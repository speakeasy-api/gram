// ResolvedMcpEndpoint is the backend-neutral shape consumed by the
// issuer-gated OAuth handlers. The same OAuth machinery — DCR, authorize,
// idp_callback, consent, token, revoke, well-known — runs against a
// ResolvedMcpEndpoint regardless of which backend (toolsets or mcp_servers)
// produced it.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	mcpendpoints_repo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projects_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ResolvedMcpEndpoint carries everything the issuer-gated OAuth handlers
// need after resolving an addressed MCP endpoint. Callers construct one
// only after confirming the underlying endpoint is issuer-gated (the
// user_session_issuer_id column is Valid).
type ResolvedMcpEndpoint struct {
	// AudienceURN is the JWT audience string used by ValidateBearer and
	// Mint. /mcp uses urn.NewToolset(toolset.ID).String(); /x/mcp uses
	// urn.NewUserSessionIssuer(issuerID).String().
	AudienceURN string

	// CustomDomainID, when valid, scopes the endpoint to a custom domain.
	CustomDomainID uuid.NullUUID

	// IsPublic mirrors toolsets.mcp_is_public — controls
	// HandleAuthorize's anonymous-vs-IDP path selection.
	IsPublic bool

	// McpServerID is populated when the endpoint resolves through an
	// mcp_endpoints → mcp_servers pair. Zero (Valid=false) for the
	// toolset-keyed resolution. Used for telemetry / log attribution.
	McpServerID uuid.NullUUID

	// OrganizationID is the org that owns the project.
	OrganizationID string

	// ProjectID owns the endpoint and scopes downstream queries.
	ProjectID uuid.UUID

	// RouteBase is "mcp" or "x/mcp" — drives URL construction in
	// WriteAuthenticateChallenge, the issuer URL emitted by /token, the
	// consent form action, and the redirect from idp_callback.
	RouteBase string

	// Slug is the public-facing endpoint slug (mcp_slug or
	// mcp_endpoints.slug).
	Slug string

	// ToolsetID is populated when the endpoint resolves through the
	// toolsets path. Zero (Valid=false) for mcp_endpoint-keyed
	// resolutions. Used for telemetry / log attribution.
	ToolsetID uuid.NullUUID

	// UserSessionIssuerID is the user_session_issuer the endpoint is
	// gated on.
	UserSessionIssuerID uuid.UUID
}

// AuthorizationServerURLs is the set of URLs the endpoint advertises in
// its RFC 8414 authorization-server metadata document. Issuer is the
// endpoint root (also the value used for the `iss` claim of minted JWTs);
// the other four are the per-handler endpoints under it.
type AuthorizationServerURLs struct {
	Issuer    string
	Authorize string
	Token     string
	Register  string
	Revoke    string
}

// AuthorizationServerURLs builds every OAuth metadata URL the endpoint
// advertises in one call, so HandleGetAuthorizationServer doesn't have
// to thread five JoinPath errors through the response builder.
func (e *ResolvedMcpEndpoint) AuthorizationServerURLs(baseURL string) (AuthorizationServerURLs, error) {
	root, err := e.RootURL(baseURL)
	if err != nil {
		return AuthorizationServerURLs{}, err
	}
	urls := AuthorizationServerURLs{Issuer: root, Authorize: "", Token: "", Register: "", Revoke: ""}
	for _, p := range []struct {
		target *string
		suffix string
	}{
		{&urls.Authorize, "authorize"},
		{&urls.Token, "token"},
		{&urls.Register, "register"},
		{&urls.Revoke, "revoke"},
	} {
		u, jerr := url.JoinPath(root, p.suffix)
		if jerr != nil {
			return AuthorizationServerURLs{}, fmt.Errorf("build %s URL: %w", p.suffix, jerr)
		}
		*p.target = u
	}
	return urls, nil
}

// ConsentURL is the URL the user agent is redirected to after the
// authorization request has been minted and (for private endpoints) the
// IDP has stamped a subject onto the cached challenge state. Shape:
// `<baseURL>/<RouteBase>/<Slug>/connect?state=<stateID>`.
func (e *ResolvedMcpEndpoint) ConsentURL(baseURL, stateID string) (string, error) {
	consentURL, err := url.JoinPath(baseURL, e.RouteBase, e.Slug, "connect")
	if err != nil {
		return "", fmt.Errorf("join consent path: %w", err)
	}
	u, err := url.Parse(consentURL)
	if err != nil {
		return "", fmt.Errorf("parse consent URL: %w", err)
	}
	q := u.Query()
	q.Set("state", stateID)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// EndpointRef materialises the cached-state reference for re-resolving
// this endpoint later (e.g. from HandleIDPCallback / HandleConsent after
// a Redis round-trip). The reference captures only what's needed to
// re-resolve — not the resolved state itself — so re-entry on a subsequent
// handler picks up mutations to the underlying row. baseURL is the
// public base URL the challenge is being minted under (the caller's
// BaseURLForRequest); it's snapshotted into the ref so handlers that
// resume the challenge from a global URL (HandleIDPCallback) can
// rebuild the consent redirect without re-deriving the origin.
func (e *ResolvedMcpEndpoint) EndpointRef(baseURL string) EndpointRef {
	return EndpointRef{
		BaseURL:        baseURL,
		RouteBase:      e.RouteBase,
		McpSlug:        e.Slug,
		CustomDomainID: e.CustomDomainID,
		McpServerID:    e.McpServerID,
	}
}

// IDPCallbackURL is the route-base-scoped callback the Speakeasy IDP
// redirects back to after authenticating a user on the private-endpoint
// path. Shape: `<baseURL>/<RouteBase>/idp_callback`. The endpoint slug
// is intentionally absent — the callback dispatches on the `state`
// query parameter to recover the originating challenge.
func (e *ResolvedMcpEndpoint) IDPCallbackURL(baseURL string) (string, error) {
	u, err := url.JoinPath(baseURL, e.RouteBase, "idp_callback")
	if err != nil {
		return "", fmt.Errorf("build IDP callback URL: %w", err)
	}
	return u, nil
}

// LogWith returns a child slog.Logger stamped with the project id plus
// whichever backend-specific row id (toolset or mcp_server) populated
// this endpoint, so the seven OAuth handlers attribute their log lines
// to the same identifiers regardless of which resolver produced the
// endpoint.
func (e *ResolvedMcpEndpoint) LogWith(logger *slog.Logger) *slog.Logger {
	args := []any{attr.SlogProjectID(e.ProjectID.String())}
	if e.ToolsetID.Valid {
		args = append(args, attr.SlogToolsetID(e.ToolsetID.UUID.String()))
	}
	if e.McpServerID.Valid {
		args = append(args, attr.SlogMcpServerID(e.McpServerID.UUID.String()))
	}
	return logger.With(args...)
}

// ProtectedResourceURL builds the RFC 9728 protected-resource metadata
// URL — `<baseURL>/.well-known/oauth-protected-resource/<RouteBase>/<Slug>`.
// Used by WriteAuthenticateChallenge for the resource_metadata parameter
// and as the `resource` field inside the protected-resource metadata
// response itself; the two must match for spec-compliant clients to
// find the AS.
func (e *ResolvedMcpEndpoint) ProtectedResourceURL(baseURL string) (string, error) {
	u, err := url.JoinPath(baseURL, ".well-known", "oauth-protected-resource", e.RouteBase, e.Slug)
	if err != nil {
		return "", fmt.Errorf("build protected-resource URL: %w", err)
	}
	return u, nil
}

// RootURL is the endpoint's public root — `<baseURL>/<RouteBase>/<Slug>`.
// This is the value spec-compliant OAuth clients construct from the
// `issuer` claim of an access token, and the base every other OAuth
// metadata URL on the endpoint hangs off of.
func (e *ResolvedMcpEndpoint) RootURL(baseURL string) (string, error) {
	u, err := url.JoinPath(baseURL, e.RouteBase, e.Slug)
	if err != nil {
		return "", fmt.Errorf("build endpoint root URL: %w", err)
	}
	return u, nil
}

// ValidateRef asserts the cached EndpointRef stored on an
// in-flight AuthnChallengeState still describes this resolved endpoint,
// guarding against the state-confusion attack where a challenge minted
// against endpoint A is resumed against endpoint B's URL. Centralised
// here so a future model with multiple addresses per endpoint can
// expand the check to "the stored ref is in the endpoint's address
// set" without churning callers.
func (e *ResolvedMcpEndpoint) ValidateRef(ref EndpointRef) error {
	if e.Slug != ref.McpSlug {
		return errToolsetEndpointMismatch
	}
	if e.CustomDomainID != ref.CustomDomainID {
		return errToolsetEndpointMismatch
	}
	return nil
}

// NewResolvedMcpEndpointFromMcpServer materialises a ResolvedMcpEndpoint
// from a resolved (mcp_endpoint, mcp_server) pair plus the owning
// project's organisation id. Caller is responsible for first checking
// mcpServer.UserSessionIssuerID.Valid; organizationID comes from a
// separate projects lookup since mcp_servers doesn't carry the org id
// directly. AudienceURN is bound to the issuer URN rather than a
// backend-specific id so /x/mcp tokens stay portable between
// toolset-backed and remote-backed servers under the same issuer.
func NewResolvedMcpEndpointFromMcpServer(
	mcpEndpoint *mcpendpoints_repo.McpEndpoint,
	mcpServer *mcpservers_repo.McpServer,
	organizationID string,
) *ResolvedMcpEndpoint {
	return &ResolvedMcpEndpoint{
		AudienceURN:         urn.NewUserSessionIssuer(mcpServer.UserSessionIssuerID.UUID).String(),
		CustomDomainID:      mcpEndpoint.CustomDomainID,
		IsPublic:            mcpServer.Visibility == mcpservers.VisibilityPublic,
		McpServerID:         uuid.NullUUID{UUID: mcpServer.ID, Valid: true},
		OrganizationID:      organizationID,
		ProjectID:           mcpEndpoint.ProjectID,
		RouteBase:           "x/mcp",
		Slug:                mcpEndpoint.Slug,
		ToolsetID:           mcpServer.ToolsetID,
		UserSessionIssuerID: mcpServer.UserSessionIssuerID.UUID,
	}
}

// newResolvedMcpEndpointFromToolset materialises a ResolvedMcpEndpoint
// from a resolved toolsets row. Caller is responsible for first checking
// toolset.UserSessionIssuerID.Valid. routeBase is the URL surface the
// request arrived under ("mcp" or "x/mcp") — passed explicitly because a
// toolset-backed endpoint can be addressed from either /mcp/{slug} or
// /x/mcp/{slug} and the WWW-Authenticate URL, OAuth issuer URL, and
// consent form action all need to match the caller's surface.
func newResolvedMcpEndpointFromToolset(toolset *toolsets_repo.Toolset, routeBase string) *ResolvedMcpEndpoint {
	return &ResolvedMcpEndpoint{
		AudienceURN:         urn.NewToolset(toolset.ID).String(),
		CustomDomainID:      toolset.CustomDomainID,
		IsPublic:            toolset.McpIsPublic,
		McpServerID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:      toolset.OrganizationID,
		ProjectID:           toolset.ProjectID,
		RouteBase:           routeBase,
		Slug:                conv.PtrValOr(conv.FromPGText[string](toolset.McpSlug), ""),
		ToolsetID:           uuid.NullUUID{UUID: toolset.ID, Valid: true},
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
	}
}

// loadResolvedMcpEndpointByRef resolves the cached EndpointRef stored
// on an in-flight AuthnChallengeState back to a fresh
// ResolvedMcpEndpoint and verifies its issuer FK is still live.
// Dispatches on the ref's McpServerID — when valid, resolves through the
// /x/mcp mcp_endpoints → mcp_servers path; otherwise resolves through
// the legacy /mcp toolsets path. Returns CodeNotFound when the
// underlying row is missing or no longer issuer-gated. Used by
// HandleIDPCallback (mounted under both route surfaces) to resume an
// in-flight challenge against the addressing path it was minted under.
func (s *Service) loadResolvedMcpEndpointByRef(ctx context.Context, ref EndpointRef) (*ResolvedMcpEndpoint, error) {
	endpoint, err := s.buildResolvedMcpEndpointByRef(ctx, ref)
	if err != nil {
		return nil, err
	}
	if err := s.RequireUserSessionIssuer(ctx, endpoint); err != nil {
		return nil, err
	}
	return endpoint, nil
}

func (s *Service) buildResolvedMcpEndpointByRef(ctx context.Context, ref EndpointRef) (*ResolvedMcpEndpoint, error) {
	if ref.McpServerID.Valid {
		mcpEndpoint, err := mcpendpoints_repo.New(s.db).GetMCPEndpointByCustomDomainAndSlug(ctx, mcpendpoints_repo.GetMCPEndpointByCustomDomainAndSlugParams{
			Slug:           ref.McpSlug,
			CustomDomainID: ref.CustomDomainID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found")
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "load mcp endpoint").Log(ctx, s.logger)
		}
		mcpServer, err := mcpservers_repo.New(s.db).GetMCPServerByID(ctx, mcpservers_repo.GetMCPServerByIDParams{
			ID:        mcpEndpoint.McpServerID,
			ProjectID: mcpEndpoint.ProjectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found")
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "load mcp server").Log(ctx, s.logger)
		}
		if !mcpServer.UserSessionIssuerID.Valid {
			return nil, oops.E(oops.CodeNotFound, nil, "not found")
		}
		// Guard against an mcp_endpoint that has been re-pointed mid-flow
		// at a different mcp_server: the cached challenge belongs to the
		// original server, not the one the endpoint currently resolves to.
		if mcpServer.ID != ref.McpServerID.UUID {
			return nil, errToolsetEndpointMismatch
		}
		project, err := projects_repo.New(s.db).GetProjectByID(ctx, mcpEndpoint.ProjectID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.E(oops.CodeNotFound, err, "project not found")
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "load project").Log(ctx, s.logger)
		}
		return NewResolvedMcpEndpointFromMcpServer(&mcpEndpoint, &mcpServer, project.OrganizationID), nil
	}

	toolset, err := s.loadToolset(ctx, ref.McpSlug, ref.CustomDomainID, true)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return nil, oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load mcp server").Log(ctx, s.logger)
	}
	if !toolset.UserSessionIssuerID.Valid {
		return nil, oops.E(oops.CodeNotFound, nil, "not found")
	}
	// Honour the surface the challenge was minted under so the resumed
	// endpoint's URLs match the original mint. Empty ref.RouteBase falls
	// back to "mcp" for states cached before EndpointRef.RouteBase existed.
	routeBase := ref.RouteBase
	if routeBase == "" {
		routeBase = "mcp"
	}
	return newResolvedMcpEndpointFromToolset(toolset, routeBase), nil
}

// loadResolvedMcpEndpointByToolsetSlug resolves an mcp_slug to a
// ResolvedMcpEndpoint via the toolsets path and verifies its issuer FK
// is still live. Returns CodeNotFound when either no toolset matches
// the slug or the toolset is not issuer-gated. Used by the six
// /mcp/{slug} OAuth handlers (Register, Authorize, Consent, Token,
// Revoke); the well-known handlers keep their own toolset load so they
// can fall through to the legacy non-issuer-gated metadata response
// when user_session_issuer_id is unset.
func (s *Service) loadResolvedMcpEndpointByToolsetSlug(ctx context.Context, mcpSlug string) (*ResolvedMcpEndpoint, error) {
	var customDomainID uuid.NullUUID
	if domainCtx := customdomains.FromContext(ctx); domainCtx != nil {
		customDomainID = uuid.NullUUID{UUID: domainCtx.DomainID, Valid: true}
	}
	toolset, err := s.loadToolset(ctx, mcpSlug, customDomainID, false)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return nil, oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}
	if !toolset.UserSessionIssuerID.Valid {
		return nil, oops.E(oops.CodeNotFound, nil, "not found")
	}
	endpoint := newResolvedMcpEndpointFromToolset(toolset, "mcp")
	if err := s.RequireUserSessionIssuer(ctx, endpoint); err != nil {
		return nil, err
	}
	return endpoint, nil
}
