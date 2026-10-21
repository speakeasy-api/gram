package usersessions

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// GetAttachImpact previews what attaching an issuer to an existing MCP server,
// toolset, or meta MCP server changes for the server's clients. It authorizes
// the caller as the matching update does.
func (s *Service) GetAttachImpact(ctx context.Context, payload *gen.GetAttachImpactPayload) (*gen.UserSessionIssuerAttachImpact, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	projectID := *authCtx.ProjectID
	logger := s.logger.With(attr.SlogProjectID(projectID.String()))

	candidateID, err := conv.PtrToNullUUID(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").LogWarn(ctx, logger)
	}

	target, err := s.loadAttachTarget(ctx, logger, payload, authCtx.ActiveOrganizationID, projectID)
	if err != nil {
		return nil, err
	}
	currentID := target.issuerID

	current, err := s.attachImpactIssuer(ctx, logger, currentID, authCtx.ActiveOrganizationID, projectID)
	if err != nil {
		return nil, err
	}
	candidate, err := s.attachImpactIssuer(ctx, logger, candidateID, authCtx.ActiveOrganizationID, projectID)
	if err != nil {
		return nil, err
	}

	return &gen.UserSessionIssuerAttachImpact{
		CurrentIssuer:              current,
		CandidateIssuer:            candidate,
		ReauthorizationRequired:    currentID.Valid && currentID != candidateID,
		AuthorizationServerChanges: s.advertisedAuthorizationServer(current, target.onPlatformHost) != s.advertisedAuthorizationServer(candidate, target.onPlatformHost),
	}, nil
}

// attachTarget is the server an attach impact previews.
type attachTarget struct {
	// issuerID is the issuer the server uses now.
	issuerID uuid.NullUUID
	// onPlatformHost reports whether any of the server's addresses is on the
	// platform host rather than a custom domain. Only those addresses move
	// their own authorization server to the authentication host.
	onPlatformHost bool
}

// loadAttachTarget resolves the one named server in the caller's project and
// authorizes the caller as its update does.
func (s *Service) loadAttachTarget(ctx context.Context, logger *slog.Logger, payload *gen.GetAttachImpactPayload, organizationID string, projectID uuid.UUID) (attachTarget, error) {
	none := attachTarget{issuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, onPlatformHost: false}

	named := 0
	for _, id := range []*string{payload.McpServerID, payload.ToolsetID, payload.MetaMcpServerID} {
		if id != nil {
			named++
		}
	}
	if named != 1 {
		return none, oops.E(oops.CodeBadRequest, nil, "exactly one of mcp_server_id, toolset_id, or meta_mcp_server_id must be provided").LogWarn(ctx, logger)
	}

	switch {
	case payload.McpServerID != nil:
		id, err := uuid.Parse(*payload.McpServerID)
		if err != nil {
			return none, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").LogWarn(ctx, logger)
		}
		server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: id, ProjectID: projectID})
		if err != nil {
			return none, attachTargetLoadError(ctx, logger, err, "MCP server")
		}
		// An MCP server backed by a toolset is granted through the toolset.
		grantID := server.ID
		if server.ToolsetID.Valid {
			grantID = server.ToolsetID.UUID
		}
		if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, grantID.String(), projectID.String())); err != nil {
			return none, err
		}
		endpoints, err := mcpendpointsrepo.New(s.db).ListMCPEndpointsOnLiveDomainsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsOnLiveDomainsByMCPServerIDParams{OrganizationID: organizationID, ProjectID: projectID, McpServerID: server.ID})
		if err != nil {
			return none, oops.E(oops.CodeUnexpected, err, "list MCP server endpoints").LogError(ctx, logger)
		}
		customDomainIDs := make([]uuid.NullUUID, 0, len(endpoints))
		for _, endpoint := range endpoints {
			customDomainIDs = append(customDomainIDs, endpoint.McpEndpoint.CustomDomainID)
		}
		return attachTarget{issuerID: server.UserSessionIssuerID, onPlatformHost: anyOnPlatformHost(customDomainIDs)}, nil
	case payload.ToolsetID != nil:
		id, err := uuid.Parse(*payload.ToolsetID)
		if err != nil {
			return none, oops.E(oops.CodeBadRequest, err, "invalid toolset_id").LogWarn(ctx, logger)
		}
		toolset, err := toolsetsrepo.New(s.db).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: id, ProjectID: projectID})
		if err != nil {
			return none, attachTargetLoadError(ctx, logger, err, "toolset")
		}
		if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, toolset.ID.String(), projectID.String())); err != nil {
			return none, err
		}
		return attachTarget{issuerID: toolset.UserSessionIssuerID, onPlatformHost: !toolset.CustomDomainID.Valid}, nil
	default:
		id, err := uuid.Parse(*payload.MetaMcpServerID)
		if err != nil {
			return none, oops.E(oops.CodeBadRequest, err, "invalid meta_mcp_server_id").LogWarn(ctx, logger)
		}
		if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil}); err != nil {
			return none, err
		}
		server, err := metamcprepo.New(s.db).GetMetaMCPServer(ctx, metamcprepo.GetMetaMCPServerParams{ID: id, OrganizationID: organizationID, ProjectID: projectID})
		if err != nil {
			return none, attachTargetLoadError(ctx, logger, err, "meta MCP server")
		}
		endpoints, err := mcpendpointsrepo.New(s.db).ListMCPEndpointsByMetaMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMetaMCPServerIDParams{ProjectID: projectID, MetaMcpServerID: server.ID})
		if err != nil {
			return none, oops.E(oops.CodeUnexpected, err, "list meta MCP server endpoints").LogError(ctx, logger)
		}
		customDomainIDs := make([]uuid.NullUUID, 0, len(endpoints))
		for _, endpoint := range endpoints {
			customDomainIDs = append(customDomainIDs, endpoint.CustomDomainID)
		}
		return attachTarget{issuerID: server.UserSessionIssuerID, onPlatformHost: anyOnPlatformHost(customDomainIDs)}, nil
	}
}

// anyOnPlatformHost reports whether any of a server's endpoints, by their
// custom domains, is on the platform host. A server without endpoints counts as
// on the platform host, so its preview errs toward warning.
func anyOnPlatformHost(customDomainIDs []uuid.NullUUID) bool {
	if len(customDomainIDs) == 0 {
		return true
	}
	for _, id := range customDomainIDs {
		if !id.Valid {
			return true
		}
	}
	return false
}

func attachTargetLoadError(ctx context.Context, logger *slog.Logger, err error, kind string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, err, "%s not found", kind).LogWarn(ctx, logger)
	}
	return oops.E(oops.CodeUnexpected, err, "get %s", kind).LogError(ctx, logger)
}

// attachImpactIssuer loads an issuer visible to the project, the project's own
// or its organization's. It returns nil for no issuer.
func (s *Service) attachImpactIssuer(ctx context.Context, logger *slog.Logger, id uuid.NullUUID, organizationID string, projectID uuid.UUID) (*types.UserSessionIssuer, error) {
	if !id.Valid {
		return nil, nil
	}
	row, err := repo.New(s.db).GetUserSessionIssuerByID(ctx, repo.GetUserSessionIssuerByIDParams{
		ID:             id.UUID,
		ProjectID:      projectID,
		OrganizationID: organizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
	}
	return s.issuerView(ctx, row), nil
}

// advertisedAuthorizationServer names the authorization server an MCP server
// attached to issuer advertises to clients: none without an issuer, the
// issuer's shared authorization server when it is served, and otherwise the
// server's own. On the platform host that is announced on the authentication
// host or the server URL by the issuer's setting; a custom domain keeps it on
// the domain either way.
func (s *Service) advertisedAuthorizationServer(issuer *types.UserSessionIssuer, onPlatformHost bool) string {
	switch {
	case issuer == nil:
		return ""
	case issuer.IssuerURL != nil:
		return *issuer.IssuerURL
	case !onPlatformHost:
		return string(authserver.ModeEndpoint)
	default:
		return s.sharedHosts.PerServerOrigin(issuer.UseAuthenticationHost)
	}
}
