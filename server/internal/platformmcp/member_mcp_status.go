package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/authz"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	MCPConnectionStateActive                  = "active"
	MCPConnectionStateNotConnected            = "not_connected"
	MCPConnectionStateReauthorizationRequired = "reauthorization_required"
	MCPConnectionStateSetupRequired           = "setup_required"
	MCPConnectionStateNotApplicable           = "not_applicable"
)

var ErrMemberMCPStatusTargetNotFound = errors.New("member MCP status target not found")

type MemberMCPConnectionReader interface {
	ListClients(ctx context.Context, projectID uuid.UUID, organizationID string, userSessionIssuerID uuid.UUID) ([]remotesessions.Client, error)
	RemoteSessionStatuses(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, userSessionIssuerID uuid.UUID) (map[uuid.UUID]remotesessions.RemoteSessionState, error)
}

type GetMyMCPStatusInput struct {
	ProjectID string `json:"project_id" jsonschema:"explicit project ID that owns the MCP server"`
	MCPID     string `json:"mcp_id" jsonschema:"exact configured MCP server ID"`
}

type GetMyMCPAccessOutput struct {
	ProjectID        string `json:"project_id"`
	MCPID            string `json:"mcp_id"`
	MCPName          string `json:"mcp_name"`
	CanRead          bool   `json:"can_read"`
	CanConnect       bool   `json:"can_connect"`
	RequiredScope    string `json:"required_scope,omitempty"`
	RequestAccessURL string `json:"request_access_url,omitempty"`
	NextAction       string `json:"next_action"`
}

type GetMyMCPConnectionStatusOutput struct {
	ProjectID        string `json:"project_id"`
	MCPID            string `json:"mcp_id"`
	MCPName          string `json:"mcp_name"`
	State            string `json:"state"`
	Reason           string `json:"reason,omitempty"`
	RequiredScope    string `json:"required_scope,omitempty"`
	RequestAccessURL string `json:"request_access_url,omitempty"`
	ConnectionURL    string `json:"connection_url,omitempty"`
	NextAction       string `json:"next_action"`
}

type memberMCPStatusTarget struct {
	projectID             uuid.UUID
	mcpID                 uuid.UUID
	name                  string
	endpointSlug          string
	canRead               bool
	canConnect            bool
	userSessionIssuerID   uuid.UUID
	remoteSessionIssuerID uuid.UUID
}

func (s *PluginsService) GetMyMCPAccess(ctx context.Context, principal Principal, input GetMyMCPStatusInput) (GetMyMCPAccessOutput, error) {
	target, err := s.memberMCPStatusTarget(ctx, principal, input)
	if err != nil {
		return GetMyMCPAccessOutput{}, err
	}
	output := GetMyMCPAccessOutput{
		ProjectID: target.projectID.String(), MCPID: target.mcpID.String(), MCPName: target.name,
		CanRead: target.canRead, CanConnect: target.canConnect,
		RequiredScope: "", RequestAccessURL: "", NextAction: "connect",
	}
	if !target.canConnect {
		output.RequiredScope = string(authz.ScopeMCPConnect)
		output.RequestAccessURL = s.requestMCPAccessURL(ctx, principal.OrganizationID, target.mcpID.String(), target.name)
		output.NextAction = memberMCPAccessNextAction(output.RequestAccessURL)
	}
	return output, nil
}

func (s *PluginsService) GetMyMCPConnectionStatus(ctx context.Context, principal Principal, input GetMyMCPStatusInput) (GetMyMCPConnectionStatusOutput, error) {
	target, err := s.memberMCPStatusTarget(ctx, principal, input)
	if err != nil {
		return GetMyMCPConnectionStatusOutput{}, err
	}
	output := GetMyMCPConnectionStatusOutput{
		ProjectID: target.projectID.String(), MCPID: target.mcpID.String(), MCPName: target.name,
		State: MCPConnectionStateNotConnected, Reason: "", RequiredScope: "", RequestAccessURL: "", ConnectionURL: "", NextAction: "connect",
	}
	if !target.canConnect {
		output.State = MCPConnectionStateNotApplicable
		output.Reason = "permission_required"
		output.RequiredScope = string(authz.ScopeMCPConnect)
		output.RequestAccessURL = s.requestMCPAccessURL(ctx, principal.OrganizationID, target.mcpID.String(), target.name)
		output.NextAction = memberMCPAccessNextAction(output.RequestAccessURL)
		return output, nil
	}
	if target.endpointSlug == "" || s.serverURL == nil {
		output.State = MCPConnectionStateSetupRequired
		output.Reason = "connection_endpoint_unavailable"
		output.NextAction = "ask_administrator"
		return output, nil
	}
	output.ConnectionURL = s.serverURL.JoinPath("mcp", target.endpointSlug).String()
	if target.remoteSessionIssuerID == uuid.Nil {
		output.State = MCPConnectionStateNotApplicable
		output.Reason = "authorization_not_required"
		output.NextAction = "use_mcp"
		return output, nil
	}
	if target.userSessionIssuerID == uuid.Nil {
		output.State = MCPConnectionStateSetupRequired
		output.Reason = "authorization_status_unavailable"
		output.NextAction = "ask_administrator"
		return output, nil
	}
	if s.remoteSessions == nil {
		return GetMyMCPConnectionStatusOutput{}, ErrUnavailable
	}

	clients, err := s.remoteSessions.ListClients(ctx, target.projectID, principal.OrganizationID, target.userSessionIssuerID)
	if err != nil {
		return GetMyMCPConnectionStatusOutput{}, fmt.Errorf("list MCP authorization clients: %w", err)
	}
	clients = memberMCPConnectionClients(clients, target.remoteSessionIssuerID)
	if len(clients) > 1 {
		output.State = MCPConnectionStateSetupRequired
		output.Reason = "multiple_authorization_clients"
		output.NextAction = "ask_administrator"
		return output, nil
	}
	if len(clients) == 0 {
		output.State = MCPConnectionStateSetupRequired
		output.Reason = "upstream_authorization_not_configured"
		output.NextAction = "ask_administrator"
		return output, nil
	}
	clientID := clients[0].ID
	statuses, err := s.remoteSessions.RemoteSessionStatuses(ctx, urn.NewUserSubject(principal.UserID), target.projectID, principal.OrganizationID, target.userSessionIssuerID)
	if err != nil {
		return GetMyMCPConnectionStatusOutput{}, fmt.Errorf("read MCP authorization status: %w", err)
	}
	status, ok := statuses[clientID]
	if !ok {
		return output, nil
	}
	if status.Status == remotesessions.RemoteSessionActive && status.ValidationStatus != remotesessions.ValidationOutcomeRejectedByMember && status.ValidationStatus != remotesessions.ValidationOutcomeInactive {
		output.State = MCPConnectionStateActive
		output.NextAction = "use_mcp"
		return output, nil
	}
	output.State = MCPConnectionStateReauthorizationRequired
	output.Reason = memberMCPReauthorizationReason(status, s.now())
	output.NextAction = "reconnect"
	return output, nil
}

func (s *PluginsService) memberMCPStatusTarget(ctx context.Context, principal Principal, input GetMyMCPStatusInput) (memberMCPStatusTarget, error) {
	if !s.valid() || s.authorization == nil {
		return memberMCPStatusTarget{}, ErrUnavailable
	}
	projectID, err := uuid.Parse(strings.TrimSpace(input.ProjectID))
	if err != nil {
		return memberMCPStatusTarget{}, ErrMemberMCPStatusTargetNotFound
	}
	mcpID, err := uuid.Parse(strings.TrimSpace(input.MCPID))
	if err != nil {
		return memberMCPStatusTarget{}, ErrMemberMCPStatusTargetNotFound
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return memberMCPStatusTarget{}, err
	}
	row, err := platformrepo.New(s.db).GetPlatformMCPInstallTarget(ctx, platformrepo.GetPlatformMCPInstallTargetParams{
		OrganizationID: principal.OrganizationID, McpServerID: mcpID, ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return memberMCPStatusTarget{}, ErrMemberMCPStatusTargetNotFound
	}
	if err != nil {
		return memberMCPStatusTarget{}, fmt.Errorf("get member MCP status target: %w", err)
	}
	grants, ok := authz.GrantsFromContext(ctx)
	if !ok {
		return memberMCPStatusTarget{}, ErrUnavailable
	}
	canRead, err := authz.GrantsAuthorize(grants, authz.MCPCheck(authz.ScopeMCPRead, mcpID.String(), projectID.String()))
	if err != nil {
		return memberMCPStatusTarget{}, fmt.Errorf("evaluate MCP read access: %w", err)
	}
	canConnect, err := authz.GrantsAuthorize(grants, authz.MCPCheck(authz.ScopeMCPConnect, mcpID.String(), projectID.String()))
	if err != nil {
		return memberMCPStatusTarget{}, fmt.Errorf("evaluate MCP connection access: %w", err)
	}
	if !canRead && !canConnect {
		return memberMCPStatusTarget{}, ErrMemberMCPStatusTargetNotFound
	}
	server, err := mcpserversrepo.New(s.db).GetMCPServerByLiveProjectForOrganization(ctx, mcpserversrepo.GetMCPServerByLiveProjectForOrganizationParams{
		OrganizationID: principal.OrganizationID, ID: mcpID, ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return memberMCPStatusTarget{}, ErrMemberMCPStatusTargetNotFound
	}
	if err != nil {
		return memberMCPStatusTarget{}, fmt.Errorf("load member MCP authorization target: %w", err)
	}
	name := row.Name.String
	if name == "" {
		name = row.Slug.String
	}
	if name == "" {
		name = mcpID.String()
	}
	return memberMCPStatusTarget{
		projectID: projectID, mcpID: mcpID, name: name, endpointSlug: row.EndpointSlug,
		canRead: canRead, canConnect: canConnect,
		userSessionIssuerID:   server.UserSessionIssuerID.UUID,
		remoteSessionIssuerID: server.RemoteSessionIssuerID.UUID,
	}, nil
}

func memberMCPAccessNextAction(requestAccessURL string) string {
	if requestAccessURL == "" {
		return "ask_administrator"
	}
	return "request_access"
}

func memberMCPConnectionClients(clients []remotesessions.Client, issuerID uuid.UUID) []remotesessions.Client {
	matched := make([]remotesessions.Client, 0, 1)
	for _, client := range clients {
		if client.RemoteSessionIssuerID == issuerID {
			matched = append(matched, client)
		}
	}
	return matched
}

func memberMCPReauthorizationReason(status remotesessions.RemoteSessionState, now time.Time) string {
	if status.AuthorizationExpiresAt != nil && !now.Before(*status.AuthorizationExpiresAt) {
		return "authorization_expired"
	}
	if status.RefreshExpiresAt != nil && !now.Before(*status.RefreshExpiresAt) {
		return "refresh_expired"
	}
	if status.ValidationStatus == remotesessions.ValidationOutcomeRejectedByMember || status.ValidationStatus == remotesessions.ValidationOutcomeInactive {
		return "authorization_rejected"
	}
	return "access_expired"
}
