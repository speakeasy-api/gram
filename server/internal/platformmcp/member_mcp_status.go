package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	MCPConnectionStateActive                  = "active"
	MCPConnectionStateNotConnected            = "not_connected"
	MCPConnectionStateReauthorizationRequired = "reauthorization_required"
	MCPConnectionStateSetupRequired           = "setup_required"
	MCPConnectionStateNotApplicable           = "not_applicable"

	// MCPConnectionReasonIdentityChaining marks an active connection served by
	// a usable identity chaining credential rather than an interactive one.
	MCPConnectionReasonIdentityChaining = "identity_chaining"
)

var ErrMemberMCPStatusTargetNotFound = errors.New("member MCP status target not found")

type MemberMCPConnectionReader interface {
	ListClients(ctx context.Context, projectID uuid.UUID, organizationID string, userSessionIssuerID uuid.UUID) ([]remotesessions.Client, error)
	RemoteSessionStatuses(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, userSessionIssuerID uuid.UUID) (map[uuid.UUID]remotesessions.RemoteSessionState, error)
	ListAuthClientRegistrations(ctx context.Context, projectID uuid.UUID, organizationID string, ids []uuid.UUID) ([]remotesessions.Client, error)
	AuthClientRemoteSessionStatuses(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, clientIDs []uuid.UUID) (map[uuid.UUID]remotesessions.RemoteSessionState, error)
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
	remoteMCPServerID     uuid.NullUUID
	tunneledMCPServerID   uuid.NullUUID
	// associatedClients is a proxied server's associated upstream clients;
	// nil, for a hosted server, matches clients by the server's derived
	// authorization server instead.
	associatedClients map[uuid.UUID]bool
}

// signInRequired reports whether the server authenticates upstream through a
// client the member connects.
func (t memberMCPStatusTarget) signInRequired() bool {
	if t.associatedClients != nil {
		return len(t.associatedClients) > 0
	}
	return t.remoteSessionIssuerID != uuid.Nil
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
	output.ConnectionURL = platformBaseURL(ctx, s.serverURL).JoinPath("mcp", target.endpointSlug).String()
	if target.associatedClients != nil && len(target.associatedClients) == 0 && target.remoteSessionIssuerID != uuid.Nil {
		// A proxied server with no associated client that still derives an
		// authorization server signs in through a client nobody configured for
		// it, which the runtime refuses until an administrator does.
		output.State = MCPConnectionStateSetupRequired
		output.Reason = "upstream_authorization_not_configured"
		output.NextAction = "ask_administrator"
		return output, nil
	}
	if !target.signInRequired() {
		// Identity chaining serves a server only through a client the
		// caller's sign-in chooses, so one without a client needs none.
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

	// Where the auth client resolver serves the organization, a proxied
	// server's registrations and the member's grants are read as the runtime
	// reads them, without the sign-in link.
	resolver := target.associatedClients != nil && feature.AuthClientResolver(ctx, s.authClientFlags, principal.OrganizationID) == feature.AuthClientResolverEnforced
	clients, err := s.memberMCPConnectionClients(ctx, principal, target, resolver)
	if err != nil {
		return GetMyMCPConnectionStatusOutput{}, err
	}
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
	if clients[0].CredentialOwner == remotesessions.CredentialOwnerSelf {
		// The MCP server presents its own upstream credential for every
		// caller, so the member has nothing to connect.
		output.State = MCPConnectionStateNotApplicable
		output.Reason = "upstream_credential_managed"
		output.NextAction = "use_mcp"
		return output, nil
	}
	clientID := clients[0].ID
	subject := urn.NewUserSubject(principal.UserID)
	var statuses map[uuid.UUID]remotesessions.RemoteSessionState
	if resolver {
		statuses, err = s.remoteSessions.AuthClientRemoteSessionStatuses(ctx, subject, target.projectID, principal.OrganizationID, []uuid.UUID{clientID})
	} else {
		statuses, err = s.remoteSessions.RemoteSessionStatuses(ctx, subject, target.projectID, principal.OrganizationID, target.userSessionIssuerID)
	}
	if err != nil {
		return GetMyMCPConnectionStatusOutput{}, fmt.Errorf("read MCP authorization status: %w", err)
	}
	status, ok := statuses[clientID]
	if ok && status.Status == remotesessions.RemoteSessionActive {
		// The runtime forwards an active interactive token even when validation rejected it, and never chains around it.
		if status.ValidationStatus != remotesessions.ValidationOutcomeRejectedByMember && status.ValidationStatus != remotesessions.ValidationOutcomeInactive {
			output.State = MCPConnectionStateActive
			output.NextAction = "use_mcp"
			return output, nil
		}
		output.State = MCPConnectionStateReauthorizationRequired
		output.Reason = memberMCPReauthorizationReason(status, s.now())
		output.NextAction = "reconnect"
		return output, nil
	}
	if ok {
		output.State = MCPConnectionStateReauthorizationRequired
		output.Reason = memberMCPReauthorizationReason(status, s.now())
		output.NextAction = "reconnect"
	}
	if _, err := s.memberMCPChainedStatus(ctx, principal, target, clients[0], &output); err != nil {
		return GetMyMCPConnectionStatusOutput{}, err
	}
	return output, nil
}

// memberMCPChainedStatus applies identity chaining to output when it serves
// the target's upstream: active on a usable chained credential, otherwise the
// first call through the server obtains access and Connect stays a fallback.
func (s *PluginsService) memberMCPChainedStatus(ctx context.Context, principal Principal, target memberMCPStatusTarget, chosen remotesessions.Client, output *GetMyMCPConnectionStatusOutput) (bool, error) {
	served, usable, err := s.memberMCPIdentityChaining(ctx, principal, target, chosen)
	switch {
	case err != nil:
		return false, err
	case usable:
		output.State = MCPConnectionStateActive
		output.Reason = MCPConnectionReasonIdentityChaining
	case served:
		output.Reason = ReadinessEvidenceIdentityChainingConfigured
	default:
		return false, nil
	}
	output.NextAction = "use_mcp"
	return true, nil
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
	associated, err := s.serverAssociatedClients(ctx, principal.OrganizationID, server)
	if err != nil {
		return memberMCPStatusTarget{}, err
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
		remoteMCPServerID:     server.RemoteMcpServerID,
		tunneledMCPServerID:   server.TunneledMcpServerID,
		associatedClients:     associated,
	}, nil
}

// memberMCPIdentityChaining reports whether identity chaining is configured
// to serve the target's upstream for the caller, and whether a stored chained
// credential is usable now. It resolves the upstream as the MCP runtime does
// and never acquires a token, through the one client the caller's sign-in
// chooses for it.
func (s *PluginsService) memberMCPIdentityChaining(ctx context.Context, principal Principal, target memberMCPStatusTarget, chosen remotesessions.Client) (bool, bool, error) {
	if s.identityChaining == nil {
		return false, false, nil
	}
	var upstream string
	tunneled := false
	switch {
	case target.remoteMCPServerID.Valid:
		remote, err := remotemcprepo.New(s.db).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{ID: target.remoteMCPServerID.UUID, ProjectID: target.projectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return false, false, nil
		}
		if err != nil {
			return false, false, fmt.Errorf("load member MCP remote upstream: %w", err)
		}
		upstream = remote.Url
	case target.tunneledMCPServerID.Valid:
		server, err := tunneledmcprepo.New(s.db).GetServerByID(ctx, tunneledmcprepo.GetServerByIDParams{ID: target.tunneledMCPServerID.UUID, ProjectID: target.projectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return false, false, nil
		}
		if err != nil {
			return false, false, fmt.Errorf("load member MCP tunneled upstream: %w", err)
		}
		upstream = server.ResourceIdentifier.String
		tunneled = true
	default:
		return false, false, nil
	}
	tunnelIssuer := uuid.NullUUID{UUID: target.remoteSessionIssuerID, Valid: target.remoteSessionIssuerID != uuid.Nil}
	if tunneled {
		// A tunneled upstream routes, and chains, by the chosen client's own
		// issuer.
		tunnelIssuer = uuid.NullUUID{UUID: chosen.RemoteSessionIssuerID, Valid: true}
	}
	req, ok := identitychaining.NewRequest(principal.OrganizationID, target.projectID, target.userSessionIssuerID, principal.UserID, upstream, tunneled, tunnelIssuer)
	if !ok {
		return false, false, nil
	}
	req.RemoteSessionClientID = uuid.NullUUID{UUID: chosen.ID, Valid: true}
	if _, served := s.identityChaining.Serves(ctx, req); !served {
		return false, false, nil
	}
	return true, s.identityChaining.HasUsableCredential(ctx, req), nil
}

func memberMCPAccessNextAction(requestAccessURL string) string {
	if requestAccessURL == "" {
		return "ask_administrator"
	}
	return "request_access"
}

// memberMCPConnectionClients returns the clients that can serve the target:
// those linked to its sign-in or, for the auth client resolver, its reachable
// associated registrations.
func (s *PluginsService) memberMCPConnectionClients(ctx context.Context, principal Principal, target memberMCPStatusTarget, resolver bool) ([]remotesessions.Client, error) {
	if resolver {
		ids := make([]uuid.UUID, 0, len(target.associatedClients))
		for id := range target.associatedClients {
			ids = append(ids, id)
		}
		slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		clients, err := s.remoteSessions.ListAuthClientRegistrations(ctx, target.projectID, principal.OrganizationID, ids)
		if err != nil {
			return nil, fmt.Errorf("list MCP auth client registrations: %w", err)
		}
		return clients, nil
	}
	clients, err := s.remoteSessions.ListClients(ctx, target.projectID, principal.OrganizationID, target.userSessionIssuerID)
	if err != nil {
		return nil, fmt.Errorf("list MCP authorization clients: %w", err)
	}
	return matchTargetClients(clients, target), nil
}

func matchTargetClients(clients []remotesessions.Client, target memberMCPStatusTarget) []remotesessions.Client {
	matched := make([]remotesessions.Client, 0, 1)
	for _, client := range clients {
		if target.associatedClients != nil && target.associatedClients[client.ID] ||
			target.associatedClients == nil && client.RemoteSessionIssuerID == target.remoteSessionIssuerID {
			matched = append(matched, client)
		}
	}
	return matched
}

// serverAssociatedClients returns the live clients associated with a proxied
// server; nil for a hosted server, which keeps the derived match as the
// runtime does.
func (s *PluginsService) serverAssociatedClients(ctx context.Context, organizationID string, server mcpserversrepo.McpServer) (map[uuid.UUID]bool, error) {
	if !server.RemoteMcpServerID.Valid && !server.TunneledMcpServerID.Valid {
		return nil, nil
	}
	rows, err := upstreamclientsrepo.New(s.db).ListServerAssociatedClients(ctx, upstreamclientsrepo.ListServerAssociatedClientsParams{
		McpServerIds:   []uuid.UUID{server.ID},
		OrganizationID: organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("list server associated clients: %w", err)
	}
	associated := make(map[uuid.UUID]bool, len(rows))
	for _, row := range rows {
		associated[row.RemoteSessionClientID] = true
	}
	return associated, nil
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
