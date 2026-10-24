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
	if c.selector.credentialKindOrDefault() != upstreamclients.CredentialKindUserSession {
		return false
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
		return false
	}
	service := c.selector.service
	queries := upstreamclientsrepo.New(service.db)
	added, err := queries.ListGatewayMembersAddedAfterIssuance(ctx, upstreamclientsrepo.ListGatewayMembersAddedAfterIssuanceParams{
		OrganizationID:  c.selector.organizationID,
		MetaMcpServerID: c.metaServerID,
		McpServerIds:    missing,
		CredentialID:    c.selector.credentialID,
		SubjectUrn:      c.subject.String(),
	})
	if err != nil {
		logger.ErrorContext(ctx, "list gateway members added after issuance", attr.SlogError(err))
		return false
	}
	recorded := false
	for _, serverID := range added {
		var member metaMember
		for _, candidate := range members {
			if candidate.serverID == serverID {
				member = candidate
				break
			}
		}
		choice, decided, err := service.newMemberChoice(ctx, c.selector.organizationID, c.selector.userSessionIssuerID, c.subject, member)
		if err != nil {
			logger.ErrorContext(ctx, "decide new gateway member choice", attr.SlogMcpServerID(serverID.String()), attr.SlogError(err))
			continue
		}
		if !decided {
			continue
		}
		if _, err := queries.RecordAuthClientChoice(ctx, upstreamclientsrepo.RecordAuthClientChoiceParams{
			CredentialKind: upstreamclients.CredentialKindUserSession,
			CredentialID:   c.selector.credentialID,
			Source:         upstreamclients.SelectionSourceNewMember,
			OrganizationID: c.selector.organizationID,
			AuthClientID:   choice,
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

// newMemberChoice decides a new member's choice when nobody has to pick: no
// upstream authentication when the member needs none, or the one client the
// subject may use that is ready without any action, a personal connection
// counting once it is live. With several such clients, or one needing a
// connection or an identity-provider check, the person chooses by
// reconnecting, so nothing is decided. authClientID is invalid for no-auth.
func (s *Service) newMemberChoice(ctx context.Context, organizationID string, userSessionIssuerID uuid.UUID, subject urn.SessionSubject, member metaMember) (authClientID uuid.NullUUID, decided bool, err error) {
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	rows, err := upstreamclientsrepo.New(s.db).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: organizationID, McpServerIds: []uuid.UUID{member.serverID}})
	if err != nil {
		return none, false, fmt.Errorf("list member auth clients: %w", err)
	}
	if len(rows) == 0 {
		return none, false, nil
	}
	requires := rows[0].RequiresUpstreamAuth
	var offered []upstreamclientsrepo.ListServerConsentAuthClientsRow
	for _, row := range rows {
		if row.AuthClientID.Valid {
			offered = append(offered, row)
		}
	}
	switch {
	case requires.Valid && !requires.Bool, len(offered) == 0 && !requires.Valid:
		return none, true, nil
	case len(offered) == 0:
		return none, false, nil
	}

	represented := subject.Kind == urn.SessionSubjectKindUser || subject.Kind == urn.SessionSubjectKindAgent
	var ready []uuid.UUID
	for _, row := range offered {
		clientType := row.ClientType.String
		personal := clientType == authz.AuthClientTypeRemoteSession || clientType == authz.AuthClientTypeEMA
		if personal && !represented {
			continue
		}
		res, err := s.resolveAuthClientChoiceForConsent(ctx, organizationID, member.serverID, member.projectID, row.AuthClientID.UUID, subject)
		if err != nil {
			return none, false, err
		}
		switch {
		case res.outcome == authClientNoAccess, res.outcome == authClientGap:
			continue
		case clientType == authz.AuthClientTypeEMA:
			// Whether the identity provider can sign the person in is only
			// known by trying, so it is the person's choice to make.
			return none, false, nil
		case clientType == authz.AuthClientTypeRemoteSession:
			reconnect, err := s.remoteChallengeMgr.AuthClientNeedsReconnect(ctx, remotesessions.AuthClientTokenRequest{
				ProjectID: member.projectID, OrganizationID: organizationID, UserSessionIssuerID: userSessionIssuerID,
				Subject: subject, RegistrationID: row.RemoteSessionClientID.UUID, SkipUnusable: false,
			})
			if err != nil {
				return none, false, fmt.Errorf("check member connection: %w", err)
			}
			if reconnect {
				return none, false, nil
			}
		}
		ready = append(ready, row.AuthClientID.UUID)
	}
	if len(ready) != 1 {
		return none, false, nil
	}
	return uuid.NullUUID{UUID: ready[0], Valid: true}, true, nil
}
