// Choices for gateway members added after a credential was issued. Consent
// considered every member present at issuance; a member added since has no
// choice yet, and gets one the first time it is used only when nothing is
// left to choose. Anything else needs the person to reconnect.

package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// recordNewMemberChoices records a choice for each proxied member that has
// none and joined the gateway after the credential was issued, when exactly
// one choice needs nobody to pick. It reports whether it recorded any. A
// failure is logged and leaves those members without a choice, which reads
// as needing a reconnection.
func (c *memberChoices) recordNewMemberChoices(ctx context.Context, logger *slog.Logger, members []metaMember, resolved map[uuid.UUID]authClientResolution) bool {
	queries := upstreamclientsrepo.New(c.selector.service.db)
	recorded := false
	for serverID, decision := range c.decideNewMemberChoices(ctx, logger, members, resolved) {
		if _, err := queries.RecordAuthClientChoice(ctx, upstreamclientsrepo.RecordAuthClientChoiceParams{
			CredentialKind: upstreamclients.CredentialKindUserSession,
			CredentialID:   c.selector.credentialID,
			Source:         upstreamclients.SelectionSourceNewMember,
			OrganizationID: c.selector.organizationID,
			AuthClientID:   decision.authClientID,
			McpServerID:    serverID,
		}); err != nil {
			logger.ErrorContext(ctx, "record new gateway member choice", attr.SlogMcpServerID(serverID.String()), attr.SlogError(err))
			continue
		}
		// A concurrent request recording first is as good as recording here,
		// so the choices are read again either way.
		recorded = true
	}
	return recorded
}

// previewNewMemberChoices resolves what recording would decide, writing nothing.
func (c *memberChoices) previewNewMemberChoices(ctx context.Context, logger *slog.Logger, members []metaMember, resolved map[uuid.UUID]authClientResolution) {
	service := c.selector.service
	for serverID, decision := range c.decideNewMemberChoices(ctx, logger, members, resolved) {
		if decision.authClientID.Valid {
			resolved[serverID] = decision.resolution
			continue
		}
		projectID := uuid.Nil
		for _, member := range members {
			if member.serverID == serverID {
				projectID = member.projectID
				break
			}
		}
		res, err := service.resolveAuthClientForConsent(ctx, c.selector.organizationID, serverID, projectID, decision.authClientID, c.subject)
		if err != nil {
			logger.ErrorContext(ctx, "preview new gateway member choice", attr.SlogMcpServerID(serverID.String()), attr.SlogError(err))
			continue
		}
		resolved[serverID] = res
	}
}

// newMemberDecision is a new member's decided client and, when it names one, its resolution.
type newMemberDecision struct {
	authClientID uuid.NullUUID
	resolution   authClientResolution
}

// decideNewMemberChoices is the choice for each proxied member with none that
// joined after the credential was issued, where nobody has to pick.
func (c *memberChoices) decideNewMemberChoices(ctx context.Context, logger *slog.Logger, members []metaMember, resolved map[uuid.UUID]authClientResolution) map[uuid.UUID]newMemberDecision {
	if c.selector.credentialKindOrDefault() != upstreamclients.CredentialKindUserSession {
		return nil
	}
	var missing []uuid.UUID
	for _, member := range members {
		if member.backend != metaMemberBackendProxied {
			continue
		}
		if _, ok := resolved[member.serverID]; !ok {
			missing = append(missing, member.serverID)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	service := c.selector.service
	added, err := upstreamclientsrepo.New(service.db).ListGatewayMembersAddedAfterIssuance(ctx, upstreamclientsrepo.ListGatewayMembersAddedAfterIssuanceParams{
		OrganizationID:  c.selector.organizationID,
		MetaMcpServerID: c.metaServerID,
		McpServerIds:    missing,
		CredentialID:    c.selector.credentialID,
		SubjectUrn:      c.subject.String(),
	})
	if err != nil {
		logger.ErrorContext(ctx, "list gateway members added after issuance", attr.SlogError(err))
		return nil
	}
	decided := make(map[uuid.UUID]newMemberDecision, len(added))
	for _, serverID := range added {
		var member metaMember
		for _, candidate := range members {
			if candidate.serverID == serverID {
				member = candidate
				break
			}
		}
		choice, resolution, ok, err := service.newMemberChoice(ctx, c.selector.organizationID, c.selector.userSessionIssuerID, c.subject, member)
		if err != nil {
			logger.ErrorContext(ctx, "decide new gateway member choice", attr.SlogMcpServerID(serverID.String()), attr.SlogError(err))
			continue
		}
		if ok {
			decided[serverID] = newMemberDecision{authClientID: choice, resolution: resolution}
		}
	}
	return decided
}

// newMemberChoice decides a new member's choice when nobody has to pick: no
// upstream authentication when the member needs none, or the one client the
// subject may use that is ready without any action, a personal connection
// counting once it is live. With several such clients, or one needing a
// connection or an identity-provider check, the person chooses by
// reconnecting, so nothing is decided. authClientID is invalid for no-auth,
// and resolution is set only when it names a client.
func (s *Service) newMemberChoice(ctx context.Context, organizationID string, userSessionIssuerID uuid.UUID, subject urn.SessionSubject, member metaMember) (authClientID uuid.NullUUID, resolution authClientResolution, decided bool, err error) {
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	unresolved := gapResolution("")
	rows, err := upstreamclientsrepo.New(s.db).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: organizationID, McpServerIds: []uuid.UUID{member.serverID}})
	if err != nil {
		return none, unresolved, false, fmt.Errorf("list member auth clients: %w", err)
	}
	if len(rows) == 0 {
		return none, unresolved, false, nil
	}
	var offered []upstreamclientsrepo.ListServerConsentAuthClientsRow
	for _, row := range rows {
		if row.AuthClientID.Valid {
			offered = append(offered, row)
		}
	}
	if len(offered) == 0 {
		// A server offering no enabled client needs no upstream
		// authentication, unless it has upstream credentials no auth client
		// records yet; then nothing is decided.
		return none, unresolved, !rows[0].UpstreamAuthPending, nil
	}

	represented := subject.Kind == urn.SessionSubjectKindUser || subject.Kind == urn.SessionSubjectKindAgent
	var ready []uuid.UUID
	var readyResolution authClientResolution
	for _, row := range offered {
		clientType := row.ClientType.String
		personal := clientType == authz.AuthClientTypeRemoteSession || clientType == authz.AuthClientTypeEMA
		if personal && !represented {
			continue
		}
		res, err := s.resolveAuthClientChoiceForConsent(ctx, organizationID, member.serverID, member.projectID, row.AuthClientID.UUID, subject)
		if err != nil {
			return none, unresolved, false, err
		}
		switch {
		case res.outcome == authClientNoAccess, res.outcome == authClientGap:
			continue
		case clientType == authz.AuthClientTypeEMA:
			// Whether the identity provider can sign the person in is only
			// known by trying, so it is the person's choice to make.
			return none, unresolved, false, nil
		case clientType == authz.AuthClientTypeRemoteSession:
			reconnect, err := s.remoteChallengeMgr.AuthClientNeedsReconnect(ctx, remotesessions.AuthClientTokenRequest{
				ProjectID: member.projectID, OrganizationID: organizationID, UserSessionIssuerID: userSessionIssuerID,
				Subject: subject, RegistrationID: row.RemoteSessionClientID.UUID, SkipUnusable: false,
			})
			if err != nil {
				return none, unresolved, false, fmt.Errorf("check member connection: %w", err)
			}
			if reconnect {
				return none, unresolved, false, nil
			}
		}
		ready = append(ready, row.AuthClientID.UUID)
		readyResolution = res
	}
	if len(ready) != 1 {
		return none, unresolved, false, nil
	}
	return uuid.NullUUID{UUID: ready[0], Valid: true}, readyResolution, true, nil
}
