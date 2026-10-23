package agent

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// pluginServers lists the MCP servers inside each delivered plugin, keyed by
// plugin id, for the menu bar's expandable plugin rows. A server carries a
// dashboard link only when the polling identity holds mcp:read on it. The map
// is non-nil on success, so plugins missing from it have no servers.
//
// The grants are loaded for the polling identity explicitly rather than read
// from the request: the poll authenticates with an API key, which the engine
// does not evaluate grants for, and an org install key's owner is an admin
// rather than the enrolled user.
func (s *Service) pluginServers(ctx context.Context, organizationID string, principals []string, rows []repo.GetAgentPluginSetRow) (map[uuid.UUID][]*gen.AgentPluginServer, error) {
	pluginIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if row.PluginID.Valid {
			pluginIDs = append(pluginIDs, row.PluginID.UUID)
		}
	}
	servers := make(map[uuid.UUID][]*gen.AgentPluginServer)
	if len(pluginIDs) == 0 {
		return servers, nil
	}

	serverRows, err := s.repo.ListAgentPluginServers(ctx, repo.ListAgentPluginServersParams{
		OrganizationID: organizationID,
		PluginIds:      pluginIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("list plugin servers: %w", err)
	}
	if len(serverRows) == 0 {
		return servers, nil
	}

	// An address that is not an org member has no RBAC principals, and so no
	// grants; LoadGrants rejects an empty principal set.
	var grants []authz.Grant
	if rbacPrincipals := grantPrincipals(principals); len(rbacPrincipals) > 0 {
		grants, err = authz.LoadGrants(ctx, s.db, organizationID, rbacPrincipals)
		if err != nil {
			return nil, fmt.Errorf("load plugin server grants: %w", err)
		}
	}

	for _, row := range serverRows {
		dashboardURL, err := s.pluginServerDashboardURL(grants, row)
		if err != nil {
			return nil, err
		}
		servers[row.PluginID] = append(servers[row.PluginID], &gen.AgentPluginServer{
			ID:           pluginServerID(row),
			Name:         row.DisplayName,
			DashboardURL: dashboardURL,
		})
	}
	return servers, nil
}

// pluginServerDashboardURL returns the server's dashboard page when grants
// allow mcp:read on it, and nil otherwise. Each backend uses the same check
// and route its own dashboard page does.
func (s *Service) pluginServerDashboardURL(grants []authz.Grant, row repo.ListAgentPluginServersRow) (*string, error) {
	projectID := row.ProjectID.String()

	var (
		check authz.Check
		route []string
	)
	switch {
	case row.McpServerID.Valid:
		resourceID := row.McpServerID.UUID.String()
		if row.McpServerToolsetID.Valid {
			resourceID = row.McpServerToolsetID.UUID.String()
		}
		check = authz.MCPCheck(authz.ScopeMCPRead, resourceID, projectID)
		slug := row.McpServerSlug.String
		if !row.McpServerSlug.Valid || slug == "" {
			slug = row.McpServerID.UUID.String()
		}
		route = []string{"x", slug}
	case row.GatewayID.Valid:
		check = authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: projectID, Dimensions: nil}
		route = []string{"gateway", row.GatewayID.UUID.String()}
	default:
		return nil, nil
	}

	allowed, err := authz.GrantsAuthorize(grants, check)
	if err != nil {
		return nil, fmt.Errorf("authorize plugin server dashboard link: %w", err)
	}
	// The agent drops links that aren't https, so an http dashboard (local
	// development) sends none rather than one the agent would discard.
	site := s.orgHosts.SiteURL(row.OrganizationDefaultHost)
	if !allowed || site == nil || site.Scheme != "https" || site.Host == "" {
		return nil, nil
	}

	segments := append([]string{row.OrganizationSlug, "projects", row.ProjectSlug, "mcp"}, route...)
	return new(site.JoinPath(segments...).String()), nil
}

// grantPrincipals keeps the plugin delivery principals that can hold RBAC
// grants. Email, wildcard, and directory audience principals only select
// plugins; grants are never written to them.
func grantPrincipals(principals []string) []urn.Principal {
	out := make([]urn.Principal, 0, len(principals))
	for _, raw := range principals {
		principal, err := urn.ParsePrincipal(raw)
		if err != nil {
			continue
		}
		switch principal.Type {
		case urn.PrincipalTypeUser, urn.PrincipalTypeRole, urn.PrincipalTypeAgent:
			out = append(out, principal)
		default:
		}
	}
	return out
}

// pluginServerID is the server's id, shared by every plugin that contains it:
// the MCP server's, or the gateway's when the membership is a gateway.
func pluginServerID(row repo.ListAgentPluginServersRow) *string {
	switch {
	case row.McpServerID.Valid:
		return new(row.McpServerID.UUID.String())
	case row.GatewayID.Valid:
		return new(row.GatewayID.UUID.String())
	default:
		return nil
	}
}
