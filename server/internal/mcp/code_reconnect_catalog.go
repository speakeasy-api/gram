package mcp

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// completeCodeReconnect updates only the requested connection and member after
// a successful OAuth callback. Verification and consent-page GETs never grant
// tools. The next execution reloads this catalogue; in-flight runs keep theirs.
func (s *Service) completeCodeReconnect(r *http.Request, grant remotesessions.RemoteGrant) error {
	ctx := r.Context()
	state, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+grant.ParentChallengeID)
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load reconnect challenge: %w", err)
	}
	binding := state.CodeReconnect
	if !binding.catalogPending() {
		return nil
	}
	if err := validateCodeReconnectBinding(state); err != nil {
		return err
	}
	if state.Subject == nil || *state.Subject != grant.Subject || state.UserSessionIssuerID != grant.UserSessionIssuerID {
		return fmt.Errorf("reconnect grant does not belong to this connection")
	}
	endpoint, err := s.loadResolvedMcpEndpointByRef(ctx, state.Endpoint)
	if err != nil {
		return err
	}
	if err := endpoint.ValidateGlobalChallenge(ctx, s.db, state.Endpoint, state.UserSessionIssuerID); err != nil {
		return err
	}
	rawPolicy, err := usersessionsrepo.New(s.db).GetReconnectSessionPolicy(ctx, usersessionsrepo.GetReconnectSessionPolicyParams{
		CredentialID: binding.CredentialID, UserSessionIssuerID: state.UserSessionIssuerID, SubjectUrn: grant.Subject, ClientID: binding.ClientID,
	})
	if err != nil {
		return fmt.Errorf("reconnect connection is no longer active: %w", err)
	}
	policy, err := toolfilter.ParseSessionPolicy(rawPolicy)
	if err != nil {
		return fmt.Errorf("parse reconnect policy: %w", err)
	}
	if policy == nil || policy.Catalog == nil || policy.Catalog.ID != binding.CatalogID || policy.Catalog.Mode != toolfilter.CatalogModeCached || !endpointAcceptsToolSelectionResource(endpoint, policy.Resource) {
		return fmt.Errorf("reconnect catalogue no longer matches the connection")
	}
	ctx = contextvalues.SetOAuthClientID(ctx, binding.ClientID)
	catalog, err := s.loadConnectionCatalog(ctx, endpoint, grant.Subject, policy.Catalog, time.Now())
	if err != nil {
		return err
	}
	r = r.WithContext(ctx)
	// Resolve by server ID as well as slug. Two members may share a display name.
	members, err := s.consentSubjectMembers(ctx, endpoint, state)
	if err != nil {
		return err
	}
	cards, err := s.buildRemoteSessionCards(ctx, endpoint, state, s.resolveAutoRefreshPolicy(ctx, endpoint.OrganizationID))
	if err != nil {
		return err
	}
	cards = slices.DeleteFunc(cards, func(card remoteSessionCard) bool { return card.ClientID != grant.RemoteSessionClientID.String() })
	if len(cards) != 1 {
		return fmt.Errorf("reconnected client is no longer offered by the server")
	}
	// Select the auth client whose registration actually completed OAuth;
	// remembered choices or another ready shared credential cannot substitute.
	offered, err := upstreamclientsrepo.New(s.db).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: endpoint.OrganizationID, McpServerIds: []uuid.UUID{binding.ServerID}})
	if err != nil {
		return fmt.Errorf("load reconnected auth client: %w", err)
	}
	selected := uuid.Nil
	for _, option := range offered {
		if option.RemoteSessionClientID.Valid && option.RemoteSessionClientID.UUID == grant.RemoteSessionClientID && option.AuthClientID.Valid {
			if selected != uuid.Nil {
				return fmt.Errorf("reconnected registration has ambiguous auth clients")
			}
			selected = option.AuthClientID.UUID
		}
	}
	if selected != uuid.Nil {
		cloned := *r.URL
		query := cloned.Query()
		query.Set(gatewayAuthClientParam+binding.Server, selected.String())
		cloned.RawQuery = query.Encode()
		r = r.Clone(ctx)
		r.URL = &cloned
	}
	view := gatewayServersView(members, cards)
	tools, approved, outcomes, err := s.enumerateGatewayConsentInventory(r, endpoint, state, view)
	if err != nil {
		return err
	}
	member, ok := approved[binding.Server]
	if !ok || len(approved) != 1 || member.ServerID != binding.ServerID || member.Auth == nil || !member.Auth.RegistrationID.Valid || member.Auth.RegistrationID.UUID != grant.RemoteSessionClientID {
		return fmt.Errorf("could not discover tools with the reconnected credential; request a new connection link")
	}
	// Use exactly the same completed inventory and freeze validation as initial
	// consent, including current configuration, credential and definition checks.
	var inventory consentToolInventory
	inventory.StateID = state.ID
	inventory.Resource = endpointToolSelectionResource(endpoint)
	inventory.MemberBindings = approved
	inventory.MemberOutcomes = outcomes
	inventory, err = mergeConsentInventoryPage(inventory, "", tools, "")
	if err != nil {
		return err
	}
	consent := state
	consent.ClientID = binding.ClientID
	frozen, err := s.freezeConnectionCatalog(ctx, endpoint, consent, &inventory, nil, toolfilter.CatalogModeCached, map[uuid.UUID]uuid.UUID{member.ServerID: grant.RemoteSessionClientID}, catalog.ExpiresAt)
	if err != nil {
		return err
	}
	// Discovery binds the selected auth client and complete definitions. Persist
	// just that selection, never the other servers reachable from this issuer.
	count, err := usersessionsrepo.New(s.db).ReconnectUpstreamClientSelection(ctx, usersessionsrepo.ReconnectUpstreamClientSelectionParams{
		CredentialID: binding.CredentialID, McpServerID: member.ServerID, OrganizationID: endpoint.OrganizationID,
		RemoteSessionClientID: grant.RemoteSessionClientID, AuthClientID: uuid.NullUUID{UUID: member.Auth.ClientID, Valid: member.Auth.ClientID != uuid.Nil}, UserSessionIssuerID: endpoint.UserSessionIssuerID,
	})
	if err != nil {
		return fmt.Errorf("record reconnected credential: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("reconnected credential is no longer offered by the server")
	}
	if err := s.replaceConnectionCatalogMember(ctx, endpoint, grant.Subject, policy.Catalog, binding.Server, frozen); err != nil {
		return err
	}
	return s.completeCatalogConsent(ctx, state)
}
