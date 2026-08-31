package usersessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const (
	// mintAccessTokenLifetime mirrors mcp.authnchallenge_token.accessTokenLifetime
	// so dashboard-minted JWTs and /token-minted JWTs have the same wall-clock
	// validity. Hardcoded short — OAuth 2.1 best practice — because the dashboard
	// doesn't have a refresh-token surface; the dashboard re-mints by calling
	// this method again with a fresh dashboard session.
	mintAccessTokenLifetime = 1 * time.Hour

	// dashboardMintRefreshTokenHashPrefix marks user_sessions rows minted by the
	// dashboard instead of a DCR-registered OAuth client.
	dashboardMintRefreshTokenHashPrefix = "dashboard-mint"
)

// mintTarget is the issuer-gated audience the JWT is bound to, resolved from
// either a toolset (/mcp) or a remote MCP server (/x/mcp) before the shared
// mint+persist tail runs.
type mintTarget struct {
	issuerID uuid.UUID
	audience string
	// resourceID is the toolset / mcp_server id the audience derives from; the
	// mcp:connect RBAC check runs against it.
	resourceID string
	issuerURL  string
	logAttr    slog.Attr
}

// MintUserSession issues a user-session JWT against an issuer-gated audience —
// either a toolset (/mcp) or a remote MCP server (/x/mcp) — on behalf of the
// authenticated dashboard user. Exactly one of toolset_id / mcp_server_id must
// be set. The resulting JWT has the same shape as the one /token would emit
// after a real OAuth dance, so the runtime gateway validates it through the
// existing validateUserSessionToken path with no special-casing.
//
// Auth posture: dashboard session only (see design.go, which scopes the method
// to security.Session). API-key callers are rejected at the security scheme
// layer. CSRF risk is bounded by the org-pinned CORS policy: a cross-origin
// caller could trigger the mint (cookie auto-attached) but cannot read the
// response body, so the resulting JWT cannot be exfiltrated.
//
// Persists a user_sessions row with user_session_client_id = NULL — the minted
// JWT has no DCR-registered OAuth client behind it. Its refresh token hash uses
// dashboardMintRefreshTokenHashPrefix as the source sentinel. The row is
// otherwise identical to a /token-issued session so userSessions.list,
// userSessions.revoke, and the runtime revocation cache all work unchanged.
func (s *Service) MintUserSession(ctx context.Context, payload *gen.MintUserSessionPayload) (*gen.MintUserSessionResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID == "" || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if s.signer == nil || s.serverURL == "" {
		return nil, oops.E(oops.CodeUnexpected, nil, "user-session signer not configured").LogError(ctx, s.logger)
	}

	hasToolset := payload.ToolsetID != nil && *payload.ToolsetID != ""
	hasServer := payload.McpServerID != nil && *payload.McpServerID != ""
	hasMeta := payload.MetaMcpServerID != nil && *payload.MetaMcpServerID != ""
	targets := 0
	for _, set := range []bool{hasToolset, hasServer, hasMeta} {
		if set {
			targets++
		}
	}
	if targets != 1 {
		return nil, oops.E(oops.CodeBadRequest, nil, "exactly one of toolset_id, mcp_server_id, or meta_mcp_server_id must be provided").LogError(ctx, s.logger)
	}

	var target *mintTarget
	var err error
	switch {
	case hasToolset:
		target, err = s.resolveToolsetMintTarget(ctx, *payload.ToolsetID, *authCtx.ProjectID)
	case hasServer:
		target, err = s.resolveServerMintTarget(ctx, *payload.McpServerID, *authCtx.ProjectID)
	default:
		target, err = s.resolveMetaServerMintTarget(ctx, *payload.MetaMcpServerID, *authCtx.ProjectID)
	}
	if err != nil {
		return nil, err
	}

	// Authorization mirrors the runtime gate: minting a bearer grants runtime
	// access, so the endpoint requires the same mcp:connect permission.
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, target.resourceID, authCtx.ProjectID.String())); err != nil {
		return nil, fmt.Errorf("authorize MCP session mint: %w", mcpaccess.ServerPermissionDenied(err, ""))
	}

	issuer, err := repo.New(s.db).GetUserSessionIssuerByID(ctx, repo.GetUserSessionIssuerByIDParams{
		ID:        target.issuerID,
		ProjectID: *authCtx.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "user_session_issuer not found").LogError(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load user_session_issuer").LogError(ctx, s.logger)
	}
	if !issuer.SessionDuration.Valid || issuer.SessionDuration.Months != 0 || issuer.SessionDuration.Days != 0 {
		return nil, oops.E(oops.CodeUnexpected, nil, "issuer session_duration is unset or carries Months/Days; only Microseconds intervals are supported").LogError(ctx, s.logger)
	}
	refreshLifetime := time.Duration(issuer.SessionDuration.Microseconds) * time.Microsecond
	if refreshLifetime <= 0 {
		return nil, oops.E(oops.CodeUnexpected, nil, "issuer session_duration is non-positive").LogError(ctx, s.logger)
	}

	subject := urn.NewUserSubject(authCtx.UserID)
	access, jti, err := s.signer.Mint(MintParams{
		Subject:   subject,
		Audience:  target.audience,
		Issuer:    target.issuerURL,
		Lifetime:  mintAccessTokenLifetime,
		ExpiresAt: nil,
		// No DCR-registered client — this mint bypasses the OAuth dance, so the
		// session is attributed to our own surface rather than left unlabelled.
		ClientID: FirstPartyClientID,
		JTI:      "",
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "mint session jwt").LogError(ctx, s.logger)
	}

	now := time.Now()
	if _, err := repo.New(s.db).CreateUserSession(ctx, repo.CreateUserSessionParams{
		UserSessionIssuerID: target.issuerID,
		// No DCR-registered client — this mint bypasses the OAuth dance.
		UserSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SubjectUrn:          subject,
		Jti:                 jti,
		// No refresh token issued: the dashboard re-mints by calling this method
		// again. Store a sentinel that satisfies the NOT NULL + unique constraint
		// without colliding with any real sha256 hash; the column will be migrated
		// to nullable separately.
		RefreshTokenHash: fmt.Sprintf("%s:%s", dashboardMintRefreshTokenHashPrefix, jti),
		ExpiresAt:        pgtype.Timestamptz{Time: now.Add(mintAccessTokenLifetime), InfinityModifier: 0, Valid: true},
		RefreshExpiresAt: pgtype.Timestamptz{Time: now.Add(refreshLifetime), InfinityModifier: 0, Valid: true},
		ToolSelection:    nil,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "persist user session").LogError(ctx, s.logger)
	}

	// Spread the attrs (incl. the per-target audience attr) so loggercheck
	// doesn't misread the dynamic slog.Attr field as a stray key.
	logAttrs := []any{
		attr.SlogProjectID(authCtx.ProjectID.String()),
		attr.SlogUserID(authCtx.UserID),
		target.logAttr,
	}
	s.logger.InfoContext(ctx, "minted user session via dashboard", logAttrs...)

	return &gen.MintUserSessionResult{
		AccessToken: access,
		ExpiresIn:   int(mintAccessTokenLifetime.Seconds()),
	}, nil
}

// resolveToolsetMintTarget binds a toolset-addressed mint to the toolset's
// wrapper when a live one exists (issuer-URN audience, wrapper RBAC), else to
// the legacy toolset binding — matching what the serving path validates.
func (s *Service) resolveToolsetMintTarget(ctx context.Context, toolsetIDStr string, projectID uuid.UUID) (*mintTarget, error) {
	toolsetID, err := uuid.Parse(toolsetIDStr)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid toolset_id").LogError(ctx, s.logger)
	}

	toolset, err := toolsetsrepo.New(s.db).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
		ID:        toolsetID,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "toolset not found").LogError(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load toolset").LogError(ctx, s.logger)
	}

	wrapper, err := mcpserversrepo.New(s.db).GetMCPServerByToolsetID(ctx, mcpserversrepo.GetMCPServerByToolsetIDParams{
		ToolsetID: toolset.ID,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No wrapper: legacy toolset binding below.
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load mcp server for toolset").LogError(ctx, s.logger)
	case wrapper.Visibility == visibility.Disabled:
		// The runtime refuses disabled wrappers and serves the legacy route,
		// so a wrapper-bound token would be rejected everywhere.
	default:
		return s.serverMintTarget(ctx, &wrapper, projectID)
	}

	if !toolset.UserSessionIssuerID.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "toolset is not issuer-gated; minting a user-session JWT is only meaningful for issuer-gated toolsets").LogError(ctx, s.logger)
	}
	if toolset.McpSlug.String == "" {
		return nil, oops.E(oops.CodeInvariantViolation, nil, "issuer-gated toolset has no mcp slug").LogError(ctx, s.logger)
	}

	issuerURL, err := url.JoinPath(s.serverURL, "mcp", toolset.McpSlug.String)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build issuer URL").LogError(ctx, s.logger)
	}

	return &mintTarget{
		issuerID:   toolset.UserSessionIssuerID.UUID,
		audience:   urn.NewToolset(toolset.ID).String(),
		issuerURL:  issuerURL,
		resourceID: toolset.ID.String(),
		logAttr:    attr.SlogToolsetID(toolset.ID.String()),
	}, nil
}

// resolveServerMintTarget binds the JWT to an mcp_server's user_session_issuer
// audience (urn.NewUserSessionIssuer) — any issuer-gated backend, hosted
// (toolset-backed) servers included. The runtime validates bearer audience
// against the issuer URN (see NewResolvedMcpEndpointFromMcpServer).
func (s *Service) resolveServerMintTarget(ctx context.Context, serverIDStr string, projectID uuid.UUID) (*mintTarget, error) {
	serverID, err := uuid.Parse(serverIDStr)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").LogError(ctx, s.logger)
	}

	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load mcp server").LogError(ctx, s.logger)
	}

	return s.serverMintTarget(ctx, &server, projectID)
}

// serverMintTarget is the shared mint binding for an already-loaded
// mcp_servers row, used by both addressing arms (mcp_server_id directly, and
// toolset_id resolving to its wrapper).
func (s *Service) serverMintTarget(ctx context.Context, server *mcpserversrepo.McpServer, projectID uuid.UUID) (*mintTarget, error) {
	if !server.UserSessionIssuerID.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "mcp server is not issuer-gated; minting a user-session JWT is only meaningful for issuer-gated servers").LogError(ctx, s.logger)
	}

	issuerURL, err := s.serverMintIssuerURL(ctx, server, projectID)
	if err != nil {
		return nil, err
	}

	return &mintTarget{
		issuerID:   server.UserSessionIssuerID.UUID,
		audience:   urn.NewUserSessionIssuer(server.UserSessionIssuerID.UUID).String(),
		issuerURL:  issuerURL,
		resourceID: server.ID.String(),
		logAttr:    attr.SlogMcpServerID(server.ID.String()),
	}, nil
}

// serverMintIssuerURL builds the descriptive iss claim from the server's
// primary endpoint; servers with no endpoint keep the legacy /x/mcp shape.
func (s *Service) serverMintIssuerURL(ctx context.Context, server *mcpserversrepo.McpServer, projectID uuid.UUID) (string, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return "", oops.C(oops.CodeUnauthorized)
	}

	issuerURL, err := mcpendpoints.PrimaryEndpointURL(ctx, s.db, authCtx.ActiveOrganizationID, projectID, server.ID, s.serverURL)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "resolve mint issuer URL").LogError(ctx, s.logger)
	}
	if issuerURL != "" {
		return issuerURL, nil
	}

	if server.Slug.String == "" {
		return "", oops.E(oops.CodeInvariantViolation, nil, "issuer-gated mcp server has no addressable endpoint or slug").LogError(ctx, s.logger)
	}
	issuerURL, err = url.JoinPath(s.serverURL, "x", "mcp", server.Slug.String)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "build issuer URL").LogError(ctx, s.logger)
	}
	return issuerURL, nil
}

// resolveMetaServerMintTarget binds the JWT to a meta MCP server's
// user_session_issuer audience, matching what
// NewResolvedMcpEndpointFromMetaMcpServer expects on the wire. A gateway has
// no slug of its own, so the issuer claim names its platform endpoint — the
// address a client would actually connect to. The mcp:connect check runs
// against the gateway id, so a caller whose grant is restricted to specific
// resources cannot mint for it.
func (s *Service) resolveMetaServerMintTarget(ctx context.Context, metaServerIDStr string, projectID uuid.UUID) (*mintTarget, error) {
	metaServerID, err := uuid.Parse(metaServerIDStr)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid meta_mcp_server_id").LogError(ctx, s.logger)
	}

	metaServer, err := metamcprepo.New(s.db).GetMetaMCPServerByIDAndProjectID(ctx, metamcprepo.GetMetaMCPServerByIDAndProjectIDParams{
		ID:        metaServerID,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "meta mcp server not found").LogError(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load meta mcp server").LogError(ctx, s.logger)
	}

	if !metaServer.UserSessionIssuerID.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "meta mcp server is not issuer-gated; minting a user-session JWT is only meaningful for issuer-gated servers").LogError(ctx, s.logger)
	}

	endpoints, err := mcpendpointsrepo.New(s.db).ListMCPEndpointsByMetaMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMetaMCPServerIDParams{
		ProjectID:       projectID,
		MetaMcpServerID: metaServerID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list meta mcp server endpoints").LogError(ctx, s.logger)
	}
	slug := ""
	for _, endpoint := range endpoints {
		// Prefer the platform address; a custom-domain endpoint only serves
		// under its own host, which this claim cannot name.
		if !endpoint.CustomDomainID.Valid {
			slug = endpoint.Slug
			break
		}
	}
	if slug == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "meta mcp server has no hosted address to mint against").LogError(ctx, s.logger)
	}

	issuerURL, err := url.JoinPath(s.serverURL, "mcp", slug)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build issuer URL").LogError(ctx, s.logger)
	}

	return &mintTarget{
		issuerID:   metaServer.UserSessionIssuerID.UUID,
		audience:   urn.NewUserSessionIssuer(metaServer.UserSessionIssuerID.UUID).String(),
		issuerURL:  issuerURL,
		resourceID: metaServer.ID.String(),
		logAttr:    attr.SlogMetaMcpServerID(metaServer.ID.String()),
	}, nil
}
