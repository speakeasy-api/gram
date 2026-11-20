package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// errTypedLoginNotOffered refuses a login through a registration the server
// offers through no typed auth client.
var errTypedLoginNotOffered = fmt.Errorf("%w: server does not offer this connection", remotesessions.ErrTypedAuthorizationDenied)

// ValidateRemoteLoginTypedAuthorization rechecks the exact server and subject
// that authorized a placed login. For a typed login it also checks the exact
// auth client, not another client that happens to be usable.
func (s *Service) ValidateRemoteLoginTypedAuthorization(ctx context.Context, state remotesessions.RemoteLoginState) error {
	if state.HostedPlacement != nil {
		return s.validateHostedLoginSubject(ctx, state)
	}
	a := state.TypedAuthorization
	if a == nil || state.Subject == nil || a.Subject.IsZero() ||
		a.Subject.String() != state.Subject.String() || a.OrganizationID == "" ||
		a.OrganizationID != state.OrganizationID || a.ProjectID == uuid.Nil || a.ProjectID != state.ProjectID ||
		a.ServerID == uuid.Nil || a.AuthClientID == uuid.Nil || a.RemoteSessionClientID == uuid.Nil || a.RemoteSessionClientID != state.RemoteSessionClientID {
		return fmt.Errorf("%w: invalid typed login authorization", remotesessions.ErrTypedAuthorizationInvalid)
	}
	if a.Subject.Kind != urn.SessionSubjectKindUser && a.Subject.Kind != urn.SessionSubjectKindAgent {
		return fmt.Errorf("%w: typed login requires a represented subject", remotesessions.ErrTypedAuthorizationInvalid)
	}
	if (state.McpServerID.Valid && state.McpServerID.UUID != a.ServerID) ||
		(state.ResourceMcpServerID.Valid && state.ResourceMcpServerID.UUID != a.ServerID) {
		return fmt.Errorf("%w: typed login server changed", remotesessions.ErrTypedAuthorizationChanged)
	}
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: a.ServerID, OrganizationID: a.OrganizationID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: typed login server no longer exists", remotesessions.ErrTypedAuthorizationChanged)
		}
		return fmt.Errorf("load typed login server: %w", err)
	}
	if server.ProjectID != a.ProjectID {
		return fmt.Errorf("%w: typed login project changed", remotesessions.ErrTypedAuthorizationChanged)
	}
	if server.Visibility != mcpservers.VisibilityPublic && server.Visibility != mcpservers.VisibilityPrivate {
		return fmt.Errorf("%w: typed login server is no longer available", remotesessions.ErrTypedAuthorizationChanged)
	}
	ctx, err = s.authorizeRemoteLoginServer(ctx, state, server, a.Subject, "")
	if err != nil {
		return err
	}
	res, err := s.resolveAuthClientChoiceForConsent(withTypedConsentAuth(ctx), a.OrganizationID, a.ServerID, a.ProjectID, a.AuthClientID, a.Subject)
	if err != nil {
		return fmt.Errorf("authorize typed login: %w", err)
	}
	if (res.outcome != authClientResolved && res.outcome != authClientLegacy) || res.clientID != a.AuthClientID ||
		(res.clientType != authClientTypeRemoteSession && res.clientType != authClientTypeEMA) || !res.choice.clientID.Valid || res.choice.clientID.UUID != a.RemoteSessionClientID {
		return fmt.Errorf("%w: typed login binding is no longer authorized", remotesessions.ErrTypedAuthorizationChanged)
	}
	return nil
}

func (s *Service) authorizeRemoteLoginServer(ctx context.Context, state remotesessions.RemoteLoginState, server mcpserversrepo.McpServer, subject urn.SessionSubject, oauthClientID string) (context.Context, error) {
	// Only fields consumed by subject admission are needed here.
	//nolint:exhaustruct_v5 // Callback subject admission does not resolve an endpoint.
	endpoint := &ResolvedMcpEndpoint{OrganizationID: state.OrganizationID, ProjectID: server.ProjectID, McpServerID: uuid.NullUUID{UUID: server.ID, Valid: true}, UserSessionIssuerID: state.UserSessionIssuerID}
	ctx, err := s.contextForSessionSubject(ctx, endpoint, subject, "remote-login:"+state.ParentChallengeID, oauthClientID)
	if err != nil {
		return ctx, fmt.Errorf("admit remote login subject: %w", err)
	}
	if s.authz == nil {
		return ctx, fmt.Errorf("remote login requires authorization engine")
	}
	authorization, err := s.authz.PrepareMandatory(ctx)
	if err != nil {
		return ctx, fmt.Errorf("load remote login permissions: %w", err)
	}
	decision, err := authorization.DecideServerAdmission(state.OrganizationID, mandatoryVisibility(server.Visibility == mcpservers.VisibilityPublic), authz.MCPCheck(authz.ScopeMCPConnect, server.ID.String(), server.ProjectID.String()))
	if err != nil {
		return ctx, fmt.Errorf("authorize remote login server: %w", err)
	}
	if !decision.Allowed {
		return ctx, fmt.Errorf("%w: remote login server access is no longer authorized", remotesessions.ErrTypedAuthorizationDenied)
	}
	return ctx, nil
}

// validateHostedLoginSubject checks the exact live gateway member or direct
// server against the same subject admission as consent. The challenge manager
// separately checks and locks the registration placement for the grant write.
func (s *Service) validateHostedLoginSubject(ctx context.Context, state remotesessions.RemoteLoginState) error {
	a := state.HostedPlacement
	if state.Subject == nil || state.Subject.IsZero() {
		return remotesessions.ErrTypedAuthorizationInvalid
	}
	if state.MetaMcpServerID.Valid || state.RouteBase == mcproute.OrganizationGateway {
		//nolint:exhaustruct_v5 // Only member resolution and admission fields are needed.
		endpoint := &ResolvedMcpEndpoint{OrganizationID: a.OrganizationID, ProjectID: a.GatewayProjectID, UserSessionIssuerID: state.UserSessionIssuerID, RouteBase: state.RouteBase, MetaMcpServerID: state.MetaMcpServerID}
		//nolint:exhaustruct_v5 // The challenge is only used to stamp the represented subject.
		members, err := s.consentSubjectMembers(ctx, endpoint, AuthnChallengeState{ID: state.ParentChallengeID, Subject: state.Subject, ClientID: a.OAuthClientID})
		if err != nil {
			return err
		}
		for _, member := range members {
			if member.backend == metaMemberBackendHosted && member.serverID == a.ServerID && member.projectID == a.ProjectID {
				return nil
			}
		}
		return remotesessions.ErrTypedAuthorizationChanged
	}
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: a.ServerID, OrganizationID: a.OrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return remotesessions.ErrTypedAuthorizationChanged
	}
	if err != nil {
		return fmt.Errorf("load hosted login server: %w", err)
	}
	if server.ProjectID != a.ProjectID || !server.ToolsetID.Valid || (server.Visibility != mcpservers.VisibilityPublic && server.Visibility != mcpservers.VisibilityPrivate) {
		return remotesessions.ErrTypedAuthorizationChanged
	}
	_, err = s.authorizeRemoteLoginServer(ctx, state, server, *state.Subject, a.OAuthClientID)
	return err
}

// typedLoginResource reads the target of the exact authorized server rather
// than inferring a destination from the registration's other attachments.
func (s *Service) typedLoginResource(ctx context.Context, authorization *remotesessions.TypedAuthorization) (string, error) {
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: authorization.ServerID, OrganizationID: authorization.OrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", remotesessions.ErrTypedAuthorizationChanged
	}
	if err != nil {
		return "", fmt.Errorf("load typed login resource server: %w", err)
	}
	if server.ProjectID != authorization.ProjectID || (!server.RemoteMcpServerID.Valid && !server.TunneledMcpServerID.Valid) {
		return "", remotesessions.ErrTypedAuthorizationChanged
	}
	return s.resolveUpstreamResource(ctx, s.logger, authorization.ProjectID, &server)
}

func (s *Service) remoteLoginTypedAuthorization(ctx context.Context, endpoint *ResolvedMcpEndpoint, challenge AuthnChallengeState, client remotesessions.Client, member chosenMetaMember) (*remotesessions.TypedAuthorization, error) {
	serverID, projectID := endpoint.McpServerID, endpoint.ProjectID
	if member.serverID.Valid {
		serverID, projectID = member.serverID, member.projectID
	}
	if !serverID.Valid && (endpoint.MetaMcpServerID.Valid || endpoint.IsOrganizationGateway()) {
		// Without a resource-qualified member, require exactly one authorized
		// typed member rather than choosing arbitrary server provenance.
		// Hosted members are authorized separately by their exact placement.
		members, err := s.consentSubjectMembers(ctx, endpoint, challenge)
		if err != nil {
			return nil, fmt.Errorf("load typed login members: %w", err)
		}
		var selected *remotesessions.TypedAuthorization
		var denied error
		for _, candidate := range members {
			if candidate.backend != metaMemberBackendProxied {
				continue
			}
			a, err := s.remoteLoginTypedAuthorization(ctx, endpoint, challenge, client, chosenMetaMember{serverID: uuid.NullUUID{UUID: candidate.serverID, Valid: true}, projectID: candidate.projectID})
			if errors.Is(err, errTypedLoginNotOffered) {
				continue
			}
			if err != nil {
				if !errors.Is(err, remotesessions.ErrTypedAuthorizationDenied) {
					return nil, err
				}
				denied = err
				continue
			}
			if a != nil {
				if selected != nil {
					return nil, fmt.Errorf("%w: connection must identify one server", remotesessions.ErrTypedAuthorizationConfiguration)
				}
				selected = a
			}
		}
		if selected != nil {
			return selected, nil
		}
		if denied == nil {
			return nil, errTypedLoginNotOffered
		}
		return nil, denied
	}
	if !serverID.Valid || endpoint.ToolsetID.Valid {
		return nil, nil
	}
	rows, err := upstreamclientsrepo.New(s.db).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: endpoint.OrganizationID, McpServerIds: []uuid.UUID{serverID.UUID}})
	if err != nil {
		return nil, fmt.Errorf("load typed login bindings: %w", err)
	}
	var denied error
	for _, row := range rows {
		if !row.AuthClientID.Valid || (row.ClientType.String != authClientTypeRemoteSession && row.ClientType.String != authClientTypeEMA) || !row.RemoteSessionClientID.Valid || row.RemoteSessionClientID.UUID != client.ID {
			continue
		}
		a := &remotesessions.TypedAuthorization{OrganizationID: endpoint.OrganizationID, ProjectID: projectID, ServerID: serverID.UUID, AuthClientID: row.AuthClientID.UUID, RemoteSessionClientID: client.ID, Subject: *challenge.Subject}
		//nolint:exhaustruct_v5 // Validate only authorization before minting the full OAuth state.
		state := remotesessions.RemoteLoginState{TypedAuthorization: a, ParentChallengeID: challenge.ID, OrganizationID: endpoint.OrganizationID, ProjectID: projectID, RemoteSessionClientID: client.ID, Subject: challenge.Subject, McpServerID: endpoint.McpServerID, UserSessionIssuerID: endpoint.UserSessionIssuerID}
		if err := s.ValidateRemoteLoginTypedAuthorization(ctx, state); err == nil {
			return a, nil
		} else {
			if !errors.Is(err, remotesessions.ErrTypedAuthorizationDenied) {
				return nil, err
			}
			denied = err
		}
	}
	if denied != nil {
		return nil, fmt.Errorf("typed connection is not authorized for this subject: %w", denied)
	}
	return nil, errTypedLoginNotOffered
}
