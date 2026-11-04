package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ValidateRemoteLoginTypedAuthorization rechecks the exact binding and subject
// that authorized a typed login, not another client that happens to be usable.
func (s *Service) ValidateRemoteLoginTypedAuthorization(ctx context.Context, state remotesessions.RemoteLoginState) error {
	a := state.TypedAuthorization
	if a == nil || state.Subject == nil || a.Subject.IsZero() ||
		a.Subject.String() != state.Subject.String() || a.OrganizationID == "" ||
		a.OrganizationID != state.OrganizationID || a.ProjectID == uuid.Nil || a.ProjectID != state.ProjectID ||
		a.ServerID == uuid.Nil || a.AuthClientID == uuid.Nil || a.RemoteSessionClientID == uuid.Nil || a.RemoteSessionClientID != state.RemoteSessionClientID {
		return fmt.Errorf("invalid typed login authorization")
	}
	if a.Subject.Kind != urn.SessionSubjectKindUser && a.Subject.Kind != urn.SessionSubjectKindAgent {
		return fmt.Errorf("typed login requires a represented subject")
	}
	enforced, err := s.consentAuthChoicesEnforced(ctx, a.OrganizationID)
	if err != nil {
		return fmt.Errorf("load typed authorization mode: %w", err)
	}
	if !enforced {
		return fmt.Errorf("typed login authorization is no longer enforced")
	}
	if state.McpServerID.Valid && state.McpServerID.UUID != a.ServerID {
		return fmt.Errorf("typed login server changed")
	}
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: a.ServerID, OrganizationID: a.OrganizationID})
	if err != nil {
		return fmt.Errorf("load typed login server: %w", err)
	}
	if server.ProjectID != a.ProjectID {
		return fmt.Errorf("typed login project changed")
	}
	// Only fields consumed by subject admission are needed here.
	//nolint:exhaustruct // Callback subject admission does not resolve an endpoint.
	endpoint := &ResolvedMcpEndpoint{OrganizationID: a.OrganizationID, ProjectID: a.ProjectID, McpServerID: uuid.NullUUID{UUID: a.ServerID, Valid: true}, UserSessionIssuerID: state.UserSessionIssuerID}
	ctx, err = s.contextForSessionSubject(ctx, endpoint, a.Subject, "remote-login:"+state.ParentChallengeID, "")
	if err != nil {
		return fmt.Errorf("admit typed login subject: %w", err)
	}
	if s.authz == nil {
		return fmt.Errorf("typed login requires authorization engine")
	}
	authorization, err := s.authz.PrepareMandatory(ctx)
	if err != nil {
		return fmt.Errorf("load typed login permissions: %w", err)
	}
	decision, err := authorization.DecideServerAdmission(a.OrganizationID, mandatoryVisibility(server.Visibility == mcpservers.VisibilityPublic), authz.MCPCheck(authz.ScopeMCPConnect, a.ServerID.String(), a.ProjectID.String()))
	if err != nil {
		return fmt.Errorf("authorize typed login server: %w", err)
	}
	if !decision.Allowed {
		return fmt.Errorf("typed login server access is no longer authorized")
	}
	res, err := s.resolveAuthClientChoiceForConsent(ctx, a.OrganizationID, a.ServerID, a.ProjectID, a.AuthClientID, a.Subject)
	if err != nil {
		return fmt.Errorf("authorize typed login: %w", err)
	}
	if (res.outcome != authClientResolved && res.outcome != authClientLegacy) || res.clientID != a.AuthClientID ||
		res.clientType != authClientTypeRemoteSession || !res.choice.clientID.Valid || res.choice.clientID.UUID != a.RemoteSessionClientID {
		return fmt.Errorf("typed login binding is no longer authorized")
	}
	return nil
}

func (s *Service) remoteLoginTypedAuthorization(ctx context.Context, endpoint *ResolvedMcpEndpoint, challenge AuthnChallengeState, client remotesessions.Client, member chosenMetaMember) (*remotesessions.TypedAuthorization, error) {
	enforced, err := s.consentAuthChoicesEnforced(ctx, endpoint.OrganizationID)
	if err != nil || !enforced {
		return nil, err
	}
	serverID, projectID := endpoint.McpServerID, endpoint.ProjectID
	if member.serverID.Valid {
		serverID, projectID = member.serverID, member.projectID
	}
	if !serverID.Valid && (endpoint.MetaMcpServerID.Valid || endpoint.IsOrganizationGateway()) {
		// Resource ambiguity must not erase the typed authorization boundary.
		// Only proxied members offer typed registrations; hosted members retain
		// their existing issuer-bound flow.
		members, err := s.consentSubjectMembers(ctx, endpoint, challenge)
		if err != nil {
			return nil, fmt.Errorf("load typed login members: %w", err)
		}
		for _, candidate := range members {
			if candidate.backend != metaMemberBackendProxied {
				continue
			}
			a, err := s.remoteLoginTypedAuthorization(ctx, endpoint, challenge, client, chosenMetaMember{serverID: uuid.NullUUID{UUID: candidate.serverID, Valid: true}, projectID: candidate.projectID})
			if err != nil {
				return nil, err
			}
			if a != nil {
				return a, nil
			}
		}
		return nil, nil
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
		if !row.AuthClientID.Valid || row.ClientType.String != authClientTypeRemoteSession || !row.RemoteSessionClientID.Valid || row.RemoteSessionClientID.UUID != client.ID {
			continue
		}
		a := &remotesessions.TypedAuthorization{OrganizationID: endpoint.OrganizationID, ProjectID: projectID, ServerID: serverID.UUID, AuthClientID: row.AuthClientID.UUID, RemoteSessionClientID: client.ID, Subject: *challenge.Subject}
		//nolint:exhaustruct // Validate only authorization before minting the full OAuth state.
		state := remotesessions.RemoteLoginState{TypedAuthorization: a, ParentChallengeID: challenge.ID, OrganizationID: endpoint.OrganizationID, ProjectID: projectID, RemoteSessionClientID: client.ID, Subject: challenge.Subject, McpServerID: endpoint.McpServerID, UserSessionIssuerID: endpoint.UserSessionIssuerID}
		if err := s.ValidateRemoteLoginTypedAuthorization(ctx, state); err == nil {
			return a, nil
		} else {
			denied = err
		}
	}
	if denied != nil {
		return nil, fmt.Errorf("typed connection is not authorized for this subject: %w", denied)
	}
	return nil, nil
}
