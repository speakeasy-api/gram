package assistants

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcprepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/executionprofiles"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// SetMCPAuthSessionVerifier installs the same signature and revocation checks
// used to admit user-session bearers at the hosted MCP endpoint.
func (s *ServiceCore) SetMCPAuthSessionVerifier(signer *sessiontokens.Signer, revocation sessiontokens.RevocationChecker) {
	s.mcpAuthSessionSigner = signer
	s.mcpAuthSessionRevocation = revocation
}

func (s *ServiceCore) adoptMCPAuthSelection(ctx context.Context, tx pgx.Tx, claims *assistanttokens.MCPAuthFlowClaims, raw string) (uuid.UUID, error) {
	if s.mcpAuthSessionSigner == nil || s.mcpAuthSessionRevocation == nil {
		return uuid.Nil, errors.New("OAuth session verification unavailable")
	}
	version, err := uuid.Parse(claims.SelectionProfileVersionID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse selection profile version ID: %w", err)
	}
	project, err := uuid.Parse(claims.ProjectID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse OAuth project ID: %w", err)
	}
	assistant, err := uuid.Parse(claims.AssistantID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse OAuth assistant ID: %w", err)
	}
	thread, err := uuid.Parse(claims.ThreadID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse OAuth thread ID: %w", err)
	}
	u, err := url.Parse(claims.McpURL)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse OAuth MCP URL: %w", err)
	}
	slug, err := mcpSlugFromURL(u)
	if err != nil {
		return uuid.Nil, err
	}
	server, err := s.resolveOAuthDirectServer(ctx, claims.OrgID, project, u, slug)
	if err != nil {
		return uuid.Nil, err
	}
	if server == nil {
		// Positively resolved hosted/gateway endpoints do not represent one
		// direct proxied selection. Preserve the legacy continuation without
		// claiming or publishing an execution-profile repair.
		return uuid.Nil, nil
	}
	if !server.UserSessionIssuerID.Valid {
		return uuid.Nil, errors.New("MCP server has no user session issuer")
	}
	session, err := s.mcpAuthSessionSigner.ValidateExactAudienceBearer(ctx, raw, claims.McpURL, s.mcpAuthSessionRevocation)
	if errors.Is(err, jwt.ErrTokenInvalidAudience) {
		session, err = s.mcpAuthSessionSigner.ValidateBearer(ctx, raw, urn.NewUserSessionIssuer(server.UserSessionIssuerID.UUID).String(), s.mcpAuthSessionRevocation)
	}
	if errors.Is(err, jwt.ErrTokenInvalidAudience) {
		session, err = s.mcpAuthSessionSigner.ValidateExactAudienceBearer(ctx, raw, urn.NewUserSessionIssuerMCPServers(server.UserSessionIssuerID.UUID).String(), s.mcpAuthSessionRevocation)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("verify OAuth session: %w", err)
	}
	if session.Issuer() != claims.OAuthServerIssuer || claims.OAuthServerIssuer == "" {
		return uuid.Nil, errors.New("OAuth session issuer mismatch")
	}
	triggerID, err := executionprofiles.AdoptOAuthSelection(ctx, mcpAuthSelectionTx{tx}, executionprofiles.OAuthResume{
		Origin: executionprofiles.GetOAuthResumeOriginParams{VersionID: version, OrganizationID: claims.OrgID, ProjectID: project, AssistantID: assistant, ThreadID: thread},
		UserID: claims.UserID, ClientID: claims.ClientID, IssuerID: server.UserSessionIssuerID.UUID, ServerID: server.ID,
	}, session)
	if err != nil {
		return uuid.Nil, fmt.Errorf("adopt OAuth selection: %w", err)
	}
	return triggerID, nil
}

// mcpAuthSelectionTx makes profile publication a savepoint in the callback's
// event transaction. A successful repair and its exact attempt receipt must
// commit together; a failed event write must never leave a published repair.
type mcpAuthSelectionTx struct{ pgx.Tx }

func (tx mcpAuthSelectionTx) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	nested, err := tx.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin OAuth selection savepoint: %w", err)
	}
	return nested, nil
}

// resolveOAuthDirectServer follows the serving address lookup: published
// endpoints take precedence over legacy toolset aliases. A management slug is
// never an address. Nil means a verified non-direct endpoint, not a lookup miss.
func (s *ServiceCore) resolveOAuthDirectServer(ctx context.Context, org string, projectID uuid.UUID, u *url.URL, slug string) (*mcprepo.McpServer, error) {
	project, err := projectsrepo.New(s.db).GetProjectByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get OAuth project: %w", err)
	}
	if project.OrganizationID != org {
		return nil, errors.New("OAuth endpoint organization mismatch")
	}
	var domainID uuid.NullUUID
	domain, err := customdomainsrepo.New(s.db).GetCustomDomainByDomain(ctx, u.Hostname())
	if err == nil {
		if domain.OrganizationID != org || !domain.Verified || !domain.Activated {
			return nil, errors.New("OAuth MCP domain is unavailable")
		}
		domainID = uuid.NullUUID{UUID: domain.ID, Valid: true}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("get OAuth custom domain: %w", err)
	}
	endpoint, err := mcpendpointsrepo.New(s.db).GetMCPEndpointByCustomDomainAndSlug(ctx, mcpendpointsrepo.GetMCPEndpointByCustomDomainAndSlugParams{Slug: slug, CustomDomainID: domainID})
	var server mcprepo.McpServer
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Only a genuine address miss permits the same legacy fallback used by
		// the serving path. Never fall back after a disabled or foreign endpoint.
		toolset, _, err := legacyalias.Find(ctx, s.logger, s.db, slug, domainID, false)
		if err != nil {
			return nil, fmt.Errorf("resolve OAuth legacy alias: %w", err)
		}
		if toolset.ProjectID != projectID {
			return nil, errors.New("OAuth alias project mismatch")
		}
		server, err = mcprepo.New(s.db).GetMCPServerByToolsetID(ctx, mcprepo.GetMCPServerByToolsetIDParams{ToolsetID: toolset.ID, ProjectID: projectID})
		if err != nil {
			return nil, fmt.Errorf("get OAuth legacy MCP server: %w", err)
		}
	case err != nil:
		return nil, fmt.Errorf("get OAuth MCP endpoint: %w", err)
	default:
		if endpoint.ProjectID != projectID {
			return nil, errors.New("OAuth endpoint project mismatch")
		}
		if endpoint.MetaMcpServerID.Valid {
			gateway, err := metamcprepo.New(s.db).GetMetaMCPServerByIDAndProjectID(ctx, metamcprepo.GetMetaMCPServerByIDAndProjectIDParams{ID: endpoint.MetaMcpServerID.UUID, ProjectID: projectID})
			if err != nil {
				return nil, fmt.Errorf("get OAuth gateway: %w", err)
			}
			mode, err := networkaccess.Effective(gateway.NetworkAccessMode)
			if err != nil || gateway.OrganizationID != org || gateway.Visibility == "disabled" || !gateway.UserSessionIssuerID.Valid || !mode.Allows(networkaccess.SurfacePublic) {
				return nil, errors.New("OAuth gateway is unavailable")
			}
			return nil, nil
		}
		server, err = mcprepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcprepo.GetMCPServerByIDAndProjectIDParams{ID: endpoint.McpServerID.UUID, ProjectID: projectID})
		if err != nil {
			return nil, fmt.Errorf("get OAuth MCP server: %w", err)
		}
	}
	mode, err := networkaccess.Effective(server.NetworkAccessMode)
	if err != nil || (server.Visibility != "private" && server.Visibility != "public") || !server.UserSessionIssuerID.Valid || !mode.Allows(networkaccess.SurfacePublic) || (server.TunneledMcpServerID.Valid && server.Visibility == "public") {
		return nil, errors.New("OAuth MCP server is unavailable")
	}
	if !server.RemoteMcpServerID.Valid && !server.TunneledMcpServerID.Valid {
		return nil, nil
	}
	return &server, nil
}
