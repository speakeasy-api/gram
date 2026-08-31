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
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpoints_repo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcp_repo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	metamcp_visibility "github.com/speakeasy-api/gram/server/internal/metamcp/visibility"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projects_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/clientauth"
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

	// CIMDAdmissionModeRaw is the issuer's stored
	// client_id_metadata_admission_mode, carried verbatim so that
	// admission.ResolveMode stays the single place deciding what it means:
	// NULL resolves to "presets", and a value outside the enum fails closed
	// to "disabled".
	//
	// Raw rather than a resolved admission.Mode so that resolution stays in
	// one place. Carrying a resolved Mode would mean resolving at every
	// stamping site, which is exactly what admission.ResolveMode exists to
	// prevent.
	//
	// The cost is that an unstamped endpoint reads as NULL and therefore as
	// "presets" — the default, not a denial. Callers must ensure this is
	// populated before enforcement; RequireUserSessionIssuer is the one
	// place that does so.
	CIMDAdmissionModeRaw pgtype.Text

	// CustomDomainID, when valid, scopes the endpoint to a custom domain.
	CustomDomainID uuid.NullUUID

	// IsPublic mirrors toolsets.mcp_is_public — controls
	// HandleAuthorize's anonymous-vs-IDP path selection.
	IsPublic bool

	// McpServerID is populated when the endpoint resolves through an
	// mcp_endpoints → mcp_servers pair. Zero (Valid=false) for the
	// toolset-keyed resolution. Used for telemetry / log attribution.
	McpServerID uuid.NullUUID

	// MetaMcpServerID is populated when the endpoint resolves through an
	// mcp_endpoints → meta_mcp_servers pair. Zero (Valid=false) for every
	// other resolution. Used for telemetry / log attribution and for
	// dispatching cached-challenge resumption back to the meta path.
	MetaMcpServerID uuid.NullUUID

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

	// UpstreamResource is the RFC 8707 resource indicator for the
	// endpoint's upstream — the remote backend URL for remote-backed
	// servers, empty otherwise.
	UpstreamResource string

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

// clientAssertionAudiences is the pair of aud values a client assertion may
// name when authenticating at the given endpoint: the issuer identifier, or
// that endpoint's own URL.
//
// Derived from the same values the RFC 8414 document advertises, so what a
// client can read from metadata and what an assertion may name are one
// value. Only the addressed endpoint's URL is accepted, so an assertion
// minted for the revocation endpoint does not authenticate a token request or
// the reverse.
func (u AuthorizationServerURLs) clientAssertionAudiences(at clientAssertionEndpoint) clientauth.Audiences {
	endpoint := ""
	switch at {
	case clientAssertionAtToken:
		endpoint = u.Token
	case clientAssertionAtRevoke:
		endpoint = u.Revoke
	}
	return clientauth.Audiences{
		Issuer:   u.Issuer,
		Endpoint: endpoint,
	}
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
	isPublic := e.IsPublic
	toolsetID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if !e.McpServerID.Valid && !e.MetaMcpServerID.Valid {
		toolsetID = e.ToolsetID
	}
	return EndpointRef{
		BaseURL:         baseURL,
		RouteBase:       e.RouteBase,
		McpSlug:         e.Slug,
		CustomDomainID:  e.CustomDomainID,
		McpServerID:     e.McpServerID,
		MetaMcpServerID: e.MetaMcpServerID,
		ToolsetID:       toolsetID,
		IsPublic:        &isPublic,
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
	if e.MetaMcpServerID.Valid {
		args = append(args, attr.SlogMetaMcpServerID(e.MetaMcpServerID.UUID.String()))
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
func (e *ResolvedMcpEndpoint) ValidateChallenge(ref EndpointRef, issuerID uuid.UUID) error {
	if e.UserSessionIssuerID != issuerID {
		return errToolsetEndpointMismatch
	}
	return e.ValidateRef(ref)
}

func (e *ResolvedMcpEndpoint) ValidateGrant(ref EndpointRef, issuerID uuid.UUID, baseURL string) error {
	if err := e.ValidateChallenge(ref, issuerID); err != nil {
		return err
	}
	if ref.BaseURL != "" && ref.BaseURL != baseURL {
		return errToolsetEndpointMismatch
	}
	return nil
}

func (e *ResolvedMcpEndpoint) ValidateRef(ref EndpointRef) error {
	if e.Slug != ref.McpSlug {
		return errToolsetEndpointMismatch
	}
	if e.CustomDomainID != ref.CustomDomainID {
		return errToolsetEndpointMismatch
	}
	if ref.IsPublic != nil && e.IsPublic != *ref.IsPublic {
		return errToolsetEndpointMismatch
	}
	// Modern states pin their primary backend identity. Server/meta-backed
	// endpoints are identified by those rows even when a server delegates to a
	// toolset; only direct-toolset endpoints pin ToolsetID. States carrying no
	// backend ID retain TTL-bounded compatibility with pre-field cached values.
	switch {
	case ref.McpServerID.Valid || ref.MetaMcpServerID.Valid:
		if e.McpServerID != ref.McpServerID || e.MetaMcpServerID != ref.MetaMcpServerID {
			return errToolsetEndpointMismatch
		}
	case ref.ToolsetID.Valid:
		if e.McpServerID.Valid || e.MetaMcpServerID.Valid || e.ToolsetID != ref.ToolsetID {
			return errToolsetEndpointMismatch
		}
	}
	// The route surface is part of the endpoint's identity: the same slug can
	// resolve on both /mcp and /x/mcp, and the RFC 9207 `iss` on every
	// authorization response is built from the resolved endpoint's RouteBase.
	// Resuming a challenge on the other surface would emit an issuer that
	// differs from the one the client recorded at mint time, which an
	// iss-validating client rejects as a mix-up. Empty ref.RouteBase is
	// treated as "mcp" for states minted before EndpointRef.RouteBase existed.
	if e.RouteBase != conv.Default(ref.RouteBase, "mcp") {
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
// backend-specific id so tokens stay portable between toolset-backed and
// remote-backed servers under the same issuer. routeBase is the URL surface
// the request arrived under ("mcp" or "x/mcp") — always taken from the
// inbound request or the cached ref, never assumed.
func NewResolvedMcpEndpointFromMcpServer(
	mcpEndpoint *mcpendpoints_repo.McpEndpoint,
	mcpServer *mcpservers_repo.McpServer,
	organizationID string,
	routeBase string,
) *ResolvedMcpEndpoint {
	return &ResolvedMcpEndpoint{
		AudienceURN: urn.NewUserSessionIssuer(mcpServer.UserSessionIssuerID.UUID).String(),
		// Stamped by RequireUserSessionIssuer, which every path runs next.
		CIMDAdmissionModeRaw: pgtype.Text{String: "", Valid: false},
		CustomDomainID:       mcpEndpoint.CustomDomainID,
		IsPublic:             mcpServer.Visibility == mcpservers.VisibilityPublic,
		McpServerID:          uuid.NullUUID{UUID: mcpServer.ID, Valid: true},
		MetaMcpServerID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:       organizationID,
		ProjectID:            mcpEndpoint.ProjectID,
		RouteBase:            routeBase,
		Slug:                 mcpEndpoint.Slug,
		ToolsetID:            mcpServer.ToolsetID,
		UpstreamResource:     "",
		UserSessionIssuerID:  mcpServer.UserSessionIssuerID.UUID,
	}
}

// connectResourceID is the mcp:connect resource id: the wrapper's id when one
// fronts the endpoint, else the toolset id.
func (e *ResolvedMcpEndpoint) connectResourceID() uuid.UUID {
	if e.McpServerID.Valid {
		return e.McpServerID.UUID
	}
	return e.ToolsetID.UUID
}

// legacyToolsetAudienceURN is the pre-migration toolset-URN audience a
// toolset-backed wrapper's bearers may still carry; ok is false for every
// other endpoint shape (AIS-633; acceptance is deleted by AIS-646).
func (e *ResolvedMcpEndpoint) legacyToolsetAudienceURN() (string, bool) {
	if !e.McpServerID.Valid || !e.ToolsetID.Valid {
		return "", false
	}
	return urn.NewToolset(e.ToolsetID.UUID).String(), true
}

// NewResolvedMcpEndpointFromMetaMcpServer materialises a ResolvedMcpEndpoint
// from a resolved (mcp_endpoint, meta_mcp_server) pair plus the owning
// project's organisation id. Caller is responsible for first checking
// metaServer.UserSessionIssuerID.Valid. AudienceURN is bound to the issuer URN,
// matching the generic-server constructor, so tokens stay portable between
// backends under one issuer. IsPublic is always false: a gateway's visibility
// vocabulary has no anonymous state, and one with no issuer is already refused
// by RequireUserSessionIssuer.
func NewResolvedMcpEndpointFromMetaMcpServer(
	mcpEndpoint *mcpendpoints_repo.McpEndpoint,
	metaServer *metamcp_repo.MetaMcpServer,
	organizationID string,
	routeBase string,
) *ResolvedMcpEndpoint {
	return &ResolvedMcpEndpoint{
		AudienceURN: urn.NewUserSessionIssuer(metaServer.UserSessionIssuerID.UUID).String(),
		// Stamped by RequireUserSessionIssuer, which every path runs next.
		CIMDAdmissionModeRaw: pgtype.Text{String: "", Valid: false},
		CustomDomainID:       mcpEndpoint.CustomDomainID,
		IsPublic:             false,
		McpServerID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		MetaMcpServerID:      uuid.NullUUID{UUID: metaServer.ID, Valid: true},
		OrganizationID:       organizationID,
		ProjectID:            mcpEndpoint.ProjectID,
		RouteBase:            routeBase,
		Slug:                 mcpEndpoint.Slug,
		ToolsetID:            uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UpstreamResource:     "",
		UserSessionIssuerID:  metaServer.UserSessionIssuerID.UUID,
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
		AudienceURN: urn.NewToolset(toolset.ID).String(),
		// Stamped by RequireUserSessionIssuer, which every path runs next.
		CIMDAdmissionModeRaw: pgtype.Text{String: "", Valid: false},
		CustomDomainID:       toolset.CustomDomainID,
		IsPublic:             toolset.McpIsPublic,
		McpServerID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		MetaMcpServerID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:       toolset.OrganizationID,
		ProjectID:            toolset.ProjectID,
		RouteBase:            routeBase,
		Slug:                 conv.PtrValOr(conv.FromPGText[string](toolset.McpSlug), ""),
		ToolsetID:            uuid.NullUUID{UUID: toolset.ID, Valid: true},
		UpstreamResource:     "",
		UserSessionIssuerID:  toolset.UserSessionIssuerID.UUID,
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
	if ref.MetaMcpServerID.Valid {
		return s.buildResolvedMetaMcpEndpointByRef(ctx, ref)
	}
	if ref.McpServerID.Valid {
		mcpEndpoint, err := mcpendpoints_repo.New(s.db).GetMCPEndpointByCustomDomainAndSlug(ctx, mcpendpoints_repo.GetMCPEndpointByCustomDomainAndSlugParams{
			Slug:           ref.McpSlug,
			CustomDomainID: ref.CustomDomainID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found")
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "load mcp endpoint").LogError(ctx, s.logger)
		}
		// A challenge minted against a generic-server endpoint cannot resume
		// on an endpoint that has since been re-pointed at a meta backend.
		if !mcpEndpoint.McpServerID.Valid {
			return nil, oops.E(oops.CodeNotFound, nil, "mcp server not found")
		}
		mcpServer, err := mcpservers_repo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
			ID:        mcpEndpoint.McpServerID.UUID,
			ProjectID: mcpEndpoint.ProjectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found")
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "load mcp server").LogError(ctx, s.logger)
		}
		// A tunnel flipped to public visibility mid-OAuth-flow has no OAuth
		// surface: reject the cached-ref resumption (e.g. /mcp/idp_callback)
		// so a visibility change closes in-flight flows.
		if mcpServer.Visibility == mcpservers.VisibilityDisabled || !mcpServer.UserSessionIssuerID.Valid || isTunneledPublic(&mcpServer) {
			return nil, oops.E(oops.CodeNotFound, nil, "not found")
		}
		mode, err := networkaccess.Effective(mcpServer.NetworkAccessMode)
		if err != nil || !mode.Allows(networkaccess.SurfacePublic) {
			return nil, oops.E(oops.CodeNotFound, mcpendpoints.ErrPolicyDenied, "not found")
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
			return nil, oops.E(oops.CodeUnexpected, err, "load project").LogError(ctx, s.logger)
		}
		// Refs cached before EndpointRef.RouteBase existed were only ever
		// minted on the /x/mcp surface for server-keyed endpoints.
		endpoint := NewResolvedMcpEndpointFromMcpServer(&mcpEndpoint, &mcpServer, project.OrganizationID, conv.Default(ref.RouteBase, "x/mcp"))
		upstreamResource, err := s.resolveUpstreamResource(ctx, s.logger, mcpEndpoint.ProjectID, &mcpServer)
		if err != nil {
			return nil, err
		}
		endpoint.UpstreamResource = upstreamResource
		return endpoint, nil
	}

	toolset, err := s.loadToolset(ctx, ref.McpSlug, ref.CustomDomainID, true)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return nil, oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load mcp server").LogError(ctx, s.logger)
	}
	s.metrics.RecordToolsetSlugFallback(ctx, mcpmetrics.LegacyFallbackChallengeResume)
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
// ResolvedMcpEndpoint via the legacy toolsets path and verifies its
// issuer FK is still live. Returns CodeNotFound when either no toolset
// matches the slug or the toolset is not issuer-gated. routeBase ("mcp"
// or "x/mcp") is the surface the request arrived under and propagates
// into the resolved endpoint's URL building. Used as the fallback leaf
// of LoadResolvedMcpEndpointBySlug for slugs with no mcp_endpoint →
// mcp_server row yet (issuer-gated toolset-backed servers predating the
// toolsets → mcp_servers migration).
func (s *Service) loadResolvedMcpEndpointByToolsetSlug(ctx context.Context, mcpSlug, routeBase string) (*ResolvedMcpEndpoint, error) {
	var customDomainID uuid.NullUUID
	if domainCtx := customdomains.FromContext(ctx); domainCtx != nil {
		customDomainID = uuid.NullUUID{UUID: domainCtx.DomainID, Valid: true}
	}
	toolset, err := s.loadToolset(ctx, mcpSlug, customDomainID, false)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return nil, oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load MCP server").LogError(ctx, s.logger)
	}
	s.metrics.RecordToolsetSlugFallback(ctx, mcpmetrics.LegacyFallbackOAuth)
	if !toolset.UserSessionIssuerID.Valid {
		return nil, oops.E(oops.CodeNotFound, nil, "not found")
	}
	endpoint := newResolvedMcpEndpointFromToolset(toolset, routeBase)
	if err := s.RequireUserSessionIssuer(ctx, endpoint); err != nil {
		return nil, err
	}
	return endpoint, nil
}

// buildResolvedMetaMcpEndpointByRef resolves a cached EndpointRef minted
// against a meta-MCP-backed endpoint. Mirrors the generic-server branch of
// buildResolvedMcpEndpointByRef: the slug is re-resolved fresh so mutations
// to the underlying rows are honored, and a ref whose endpoint has been
// re-pointed at a different backend fails closed.
func (s *Service) buildResolvedMetaMcpEndpointByRef(ctx context.Context, ref EndpointRef) (*ResolvedMcpEndpoint, error) {
	mcpEndpoint, err := mcpendpoints_repo.New(s.db).GetMCPEndpointByCustomDomainAndSlug(ctx, mcpendpoints_repo.GetMCPEndpointByCustomDomainAndSlugParams{
		Slug:           ref.McpSlug,
		CustomDomainID: ref.CustomDomainID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load mcp endpoint").LogError(ctx, s.logger)
	}
	if !mcpEndpoint.MetaMcpServerID.Valid || mcpEndpoint.MetaMcpServerID.UUID != ref.MetaMcpServerID.UUID {
		// The cached challenge belongs to the original meta server, not
		// whatever the endpoint currently resolves to.
		return nil, errToolsetEndpointMismatch
	}
	metaServer, err := metamcp_repo.New(s.db).GetMetaMCPServerByIDAndProjectID(ctx, metamcp_repo.GetMetaMCPServerByIDAndProjectIDParams{
		ID:        mcpEndpoint.MetaMcpServerID.UUID,
		ProjectID: mcpEndpoint.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "meta mcp server not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load meta mcp server").LogError(ctx, s.logger)
	}
	if !metaServer.UserSessionIssuerID.Valid {
		// An issuer detached mid-flow closes in-flight challenges.
		return nil, oops.E(oops.CodeNotFound, nil, "not found")
	}
	if metaServer.Visibility == metamcp_visibility.Disabled {
		// A gateway disabled mid-flow closes in-flight challenges, matching
		// the generic-server branch's visibility check.
		return nil, oops.E(oops.CodeNotFound, nil, "not found")
	}
	mode, err := networkaccess.Effective(metaServer.NetworkAccessMode)
	if err != nil || !mode.Allows(networkaccess.SurfacePublic) {
		return nil, oops.E(oops.CodeNotFound, mcpendpoints.ErrPolicyDenied, "not found")
	}

	routeBase := ref.RouteBase
	if routeBase == "" {
		routeBase = "mcp"
	}
	// The denormalized org id is authoritative — the composite FK on
	// meta_mcp_servers pins (organization_id, project_id) to the projects
	// row, and BuildResolvedMcpEndpointForMetaServer already relies on it.
	return NewResolvedMcpEndpointFromMetaMcpServer(&mcpEndpoint, &metaServer, metaServer.OrganizationID, routeBase), nil
}
