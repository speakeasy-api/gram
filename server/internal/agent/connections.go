package agent

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// MemberConnectionStatuses reports a member's sign-in state for one MCP server.
type MemberConnectionStatuses interface {
	MemberConnectionStatus(ctx context.Context, principal platformmcp.Principal, grants []authz.Grant, projectID, mcpID uuid.UUID) (platformmcp.GetMyMCPConnectionStatusOutput, error)
}

// Connection states reported by getMyConnections.
const (
	connectionConnected               = "connected"
	connectionNotConnected            = "not_connected"
	connectionReauthorizationRequired = "reauthorization_required"
	connectionNotRequired             = "not_required"
	connectionSetupRequired           = "setup_required"
	connectionNoAccess                = "no_access"
	connectionUnknown                 = "unknown"
)

// connectFirstPartyPath is the route under an MCP endpoint that signs a member
// in through the browser.
const connectFirstPartyPath = "connect/first-party"

// WithConnectionStatuses sets the reader behind getMyConnections. Nil reports
// every server as unknown.
func (s *Service) WithConnectionStatuses(statuses MemberConnectionStatuses) *Service {
	s.connectionStatuses = statuses
	return s
}

// GetMyConnections reports the polling user's sign-in state for each distinct
// MCP server in the plugins delivered to them. The user is resolved the same
// way GetPlugins resolves it, so an org install key reads the vouched user's
// state: only whether each server is signed in, never a credential.
func (s *Service) GetMyConnections(ctx context.Context, payload *gen.GetMyConnectionsPayload) (*gen.GetMyConnectionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if _, ok := deviceagenttoken.EffectiveAgent(ctx); ok {
		return nil, oops.E(oops.CodeForbidden, nil, "sign-in state belongs to a person; an agent identity has none")
	}
	email, err := pollingEmail(authCtx, conv.PtrValOr(payload.Email, ""))
	if err != nil {
		return nil, err
	}

	organizationID := authCtx.ActiveOrganizationID
	principals, err := plugins.ResolveDeliveryPrincipals(ctx, s.db, organizationID, email, "")
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent plugin delivery principals").LogError(ctx, s.logger)
	}
	result := &gen.GetMyConnectionsResult{Servers: []*gen.AgentServerConnection{}}
	// Remote sessions belong to a user; an address with no membership has none.
	userID := memberUserID(principals)
	if userID == "" {
		return result, nil
	}

	pluginRows, err := s.repo.GetAgentPluginSet(ctx, repo.GetAgentPluginSetParams{
		OrganizationID: organizationID,
		PrincipalUrns:  principals,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent plugin set").LogError(ctx, s.logger)
	}
	// Only plugins the device receives, in the order it receives them.
	delivered := mv.DeliveredAgentPluginRows(pluginRows)
	pluginIDs := make([]uuid.UUID, 0, len(delivered))
	pluginOrder := make(map[uuid.UUID]int, len(delivered))
	for _, row := range delivered {
		if row.PluginID.Valid {
			pluginOrder[row.PluginID.UUID] = len(pluginIDs)
			pluginIDs = append(pluginIDs, row.PluginID.UUID)
		}
	}
	if len(pluginIDs) == 0 {
		return result, nil
	}
	serverRows, err := s.repo.ListAgentPluginServers(ctx, repo.ListAgentPluginServersParams{
		OrganizationID: organizationID,
		PluginIds:      pluginIDs,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing plugin servers").LogError(ctx, s.logger)
	}
	slices.SortStableFunc(serverRows, func(a, b repo.ListAgentPluginServersRow) int {
		return pluginOrder[a.PluginID] - pluginOrder[b.PluginID]
	})
	grants, err := authz.LoadGrants(ctx, s.db, organizationID, grantPrincipals(principals))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading member grants").LogError(ctx, s.logger)
	}

	// The device agent holds no Platform MCP connection, so the principal carries
	// none and reads no acting surface.
	principal := platformmcp.Principal{UserID: userID, OrganizationID: organizationID, ConnectionID: "", Generation: "", ClientID: "", Surface: ""}
	seen := make(map[string]bool, len(serverRows))
	for _, row := range serverRows {
		id := pluginServerID(row)
		if id == nil || seen[*id] {
			continue
		}
		seen[*id] = true
		entry, ok := s.serverConnection(ctx, principal, grants, row, *id)
		if ok {
			result.Servers = append(result.Servers, entry)
		}
	}
	return result, nil
}

// serverConnection reports one server's state. A server the member cannot
// read or connect is omitted; trouble reading one server reports it as
// unknown rather than failing the rest.
func (s *Service) serverConnection(ctx context.Context, principal platformmcp.Principal, grants []authz.Grant, row repo.ListAgentPluginServersRow, id string) (*gen.AgentServerConnection, bool) {
	entry := &gen.AgentServerConnection{ID: id, State: connectionUnknown, ConnectURL: nil, RequestAccessURL: nil}
	// A gateway's members sign in separately; its own connect page covers them.
	if !row.McpServerID.Valid || s.connectionStatuses == nil {
		return entry, true
	}
	status, err := s.connectionStatuses.MemberConnectionStatus(ctx, principal, grants, row.ProjectID, row.McpServerID.UUID)
	switch {
	case errors.Is(err, platformmcp.ErrMemberMCPStatusTargetNotFound):
		return nil, false
	case err != nil:
		s.logger.WarnContext(ctx, "mcp server sign-in state unavailable", attr.SlogError(err), attr.SlogOrganizationID(principal.OrganizationID))
		return entry, true
	}

	switch status.State {
	case platformmcp.MCPConnectionStateActive:
		entry.State = connectionConnected
	case platformmcp.MCPConnectionStateNotConnected:
		entry.State = connectionNotConnected
	case platformmcp.MCPConnectionStateReauthorizationRequired:
		entry.State = connectionReauthorizationRequired
	case platformmcp.MCPConnectionStateNotApplicable:
		entry.State = connectionNotRequired
		// Not applicable also covers a member who may not connect at all,
		// which signing in can't fix.
		if status.Reason == platformmcp.MCPConnectionReasonPermissionRequired {
			entry.State = connectionNoAccess
			entry.RequestAccessURL = conv.PtrEmpty(status.RequestAccessURL)
		}
	case platformmcp.MCPConnectionStateSetupRequired:
		entry.State = connectionSetupRequired
	}
	if (entry.State == connectionNotConnected || entry.State == connectionReauthorizationRequired) && status.ConnectionURL != "" {
		entry.ConnectURL = new(strings.TrimRight(status.ConnectionURL, "/") + "/" + connectFirstPartyPath)
	}
	return entry, true
}

// memberUserID returns the user id among resolved delivery principals, or ""
// when the email belongs to no member of the organization.
func memberUserID(principals []string) string {
	for _, raw := range principals {
		principal, err := urn.ParsePrincipal(raw)
		if err == nil && principal.Type == urn.PrincipalTypeUser && principal.ID != urn.AllUsersPrincipalID {
			return principal.ID
		}
	}
	return ""
}
