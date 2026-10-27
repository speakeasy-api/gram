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
	"github.com/speakeasy-api/gram/server/internal/hiddenmcpservers"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
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

// ConnectionsRate bounds getMyConnections reads per member. A device reads at
// most every few seconds while a sign-in is in flight; the limit stops a leaked
// org key from hammering one member's state. It is keyed per member, not per
// key, because a fleet shares one org key.
var ConnectionsRate = ratelimit.PerMinute(60)

// WithConnectionsLimiter sets the per-member limit on getMyConnections. Nil
// leaves it unlimited.
func (s *Service) WithConnectionsLimiter(limiter *ratelimit.Limiter) *Service {
	s.connectionsLimiter = limiter
	return s
}

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
	member, err := s.resolveMember(ctx, payload.Email, "sign-in state")
	if err != nil {
		return nil, err
	}
	result := &gen.GetMyConnectionsResult{Servers: []*gen.AgentServerConnection{}}
	// Remote sessions belong to a user; an address with no membership has none.
	if member.userID == "" {
		return result, nil
	}
	serverRows, err := s.memberServers(ctx, member)
	if err != nil {
		return nil, err
	}
	if len(serverRows) == 0 {
		return result, nil
	}
	grants, err := authz.LoadGrants(ctx, s.db, member.organizationID, grantPrincipals(member.principals))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading member grants").LogError(ctx, s.logger)
	}
	// Hidden is a display preference: if it can't be read, report every
	// server as shown rather than fail the sign-in state.
	hidden, err := hiddenmcpservers.HiddenServerIDs(ctx, s.db, member.organizationID, member.userID)
	if err != nil {
		s.logger.WarnContext(ctx, "hidden mcp servers unavailable", attr.SlogError(err), attr.SlogOrganizationID(member.organizationID))
		hidden = nil
	}

	// The device agent holds no Platform MCP connection, so the principal carries
	// none and reads no acting surface.
	principal := platformmcp.Principal{UserID: member.userID, OrganizationID: member.organizationID, ConnectionID: "", Generation: "", ClientID: "", Surface: ""}
	for _, row := range serverRows {
		entry, ok := s.serverConnection(ctx, principal, grants, row, *pluginServerID(row))
		if !ok {
			continue
		}
		if row.McpServerID.Valid && hidden[row.McpServerID.UUID] {
			entry.Hidden = new(true)
		}
		result.Servers = append(result.Servers, entry)
	}
	return result, nil
}

// HideMcpServer hides an MCP server for the enrolled user. It writes the same
// preference as the gateway consent page, so hiding in either place hides in
// both. Only a server in the plugins delivered to the user can be hidden.
func (s *Service) HideMcpServer(ctx context.Context, payload *gen.HideMcpServerPayload) error {
	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid mcp server id")
	}
	member, err := s.resolveMember(ctx, payload.Email, "hidden servers")
	if err != nil {
		return err
	}
	if member.userID == "" {
		return oops.E(oops.CodeNotFound, nil, "mcp server not found")
	}
	serverRows, err := s.memberServers(ctx, member)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(serverRows, func(row repo.ListAgentPluginServersRow) bool {
		return row.McpServerID.Valid && row.McpServerID.UUID == serverID
	}) {
		return oops.E(oops.CodeNotFound, nil, "mcp server not found")
	}
	if err := hiddenmcpservers.Hide(ctx, s.db, member.organizationID, member.userID, serverID); err != nil {
		if errors.Is(err, hiddenmcpservers.ErrServerNotFound) {
			return oops.E(oops.CodeNotFound, err, "mcp server not found")
		}
		return oops.E(oops.CodeUnexpected, err, "error hiding mcp server").LogError(ctx, s.logger)
	}
	return nil
}

// UnhideMcpServer shows a hidden MCP server again for the enrolled user. It
// needs no plugin check: a user may always clear their own preference.
func (s *Service) UnhideMcpServer(ctx context.Context, payload *gen.UnhideMcpServerPayload) error {
	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid mcp server id")
	}
	member, err := s.resolveMember(ctx, payload.Email, "hidden servers")
	if err != nil {
		return err
	}
	if member.userID == "" {
		return nil
	}
	if err := hiddenmcpservers.Unhide(ctx, s.db, member.organizationID, member.userID, serverID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error unhiding mcp server").LogError(ctx, s.logger)
	}
	return nil
}

// agentMember is the person a device agent call acts for.
type agentMember struct {
	organizationID string
	// userID is empty when the email belongs to no member of the organization.
	userID     string
	principals []string
}

// resolveMember resolves the user the way GetPlugins does (the per-user key's
// owner, or the email an org install key vouches) and charges the member's
// budget. Agent identities are refused; what names the personal data they
// have none of.
func (s *Service) resolveMember(ctx context.Context, vouchedEmail *string, what string) (agentMember, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return agentMember{}, oops.C(oops.CodeUnauthorized)
	}
	if _, ok := deviceagenttoken.EffectiveAgent(ctx); ok {
		return agentMember{}, oops.E(oops.CodeForbidden, nil, "%s belongs to a person; an agent identity has none", what)
	}
	email, err := pollingEmail(authCtx, conv.PtrValOr(vouchedEmail, ""))
	if err != nil {
		return agentMember{}, err
	}

	organizationID := authCtx.ActiveOrganizationID
	principals, err := plugins.ResolveDeliveryPrincipals(ctx, s.db, organizationID, email, "")
	if err != nil {
		return agentMember{}, oops.E(oops.CodeUnexpected, err, "error resolving agent plugin delivery principals").LogError(ctx, s.logger)
	}
	member := agentMember{organizationID: organizationID, userID: memberUserID(principals), principals: principals}
	if member.userID == "" {
		return member, nil
	}
	// Charged per member after the membership check, so addresses outside
	// the organization never create limiter state.
	if err := s.allowConnectionsRead(ctx, organizationID, member.userID); err != nil {
		return agentMember{}, err
	}
	return member, nil
}

// memberServers lists the distinct servers in the plugins delivered to the
// member, in the order the device receives them.
func (s *Service) memberServers(ctx context.Context, member agentMember) ([]repo.ListAgentPluginServersRow, error) {
	pluginRows, err := s.repo.GetAgentPluginSet(ctx, repo.GetAgentPluginSetParams{
		OrganizationID: member.organizationID,
		PrincipalUrns:  member.principals,
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
		return nil, nil
	}
	serverRows, err := s.repo.ListAgentPluginServers(ctx, repo.ListAgentPluginServersParams{
		OrganizationID: member.organizationID,
		PluginIds:      pluginIDs,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing plugin servers").LogError(ctx, s.logger)
	}
	slices.SortStableFunc(serverRows, func(a, b repo.ListAgentPluginServersRow) int {
		return pluginOrder[a.PluginID] - pluginOrder[b.PluginID]
	})
	seen := make(map[string]bool, len(serverRows))
	distinct := make([]repo.ListAgentPluginServersRow, 0, len(serverRows))
	for _, row := range serverRows {
		id := pluginServerID(row)
		if id == nil || seen[*id] {
			continue
		}
		seen[*id] = true
		distinct = append(distinct, row)
	}
	return distinct, nil
}

// allowConnectionsRead charges one read to the member's budget. A limiter
// store outage fails open: it isn't a throttle.
func (s *Service) allowConnectionsRead(ctx context.Context, organizationID, userID string) error {
	if s.connectionsLimiter == nil {
		return nil
	}
	res, err := s.connectionsLimiter.Allow(ctx, organizationID+":"+userID)
	switch {
	case err != nil:
		s.logger.WarnContext(ctx, "getMyConnections rate limit unavailable", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return nil
	case !res.Allowed:
		return oops.E(oops.CodeRateLimitExceeded, nil, "too many sign-in state reads; retry shortly")
	}
	return nil
}

// serverConnection reports one server's state. A server the member cannot
// read or connect is omitted; trouble reading one server reports it as
// unknown rather than failing the rest.
func (s *Service) serverConnection(ctx context.Context, principal platformmcp.Principal, grants []authz.Grant, row repo.ListAgentPluginServersRow, id string) (*gen.AgentServerConnection, bool) {
	entry := &gen.AgentServerConnection{ID: id, State: connectionUnknown, ConnectURL: nil, RequestAccessURL: nil, Hidden: nil}
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
