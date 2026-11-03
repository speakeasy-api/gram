package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func (s *Service) reconnectPolicy(ctx context.Context, q *usersessionsrepo.Queries, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) (*toolfilter.SessionPolicy, error) {
	b := state.CodeReconnect
	raw, err := q.GetReconnectSessionPolicy(ctx, usersessionsrepo.GetReconnectSessionPolicyParams{CredentialID: b.CredentialID, UserSessionIssuerID: state.UserSessionIssuerID, SubjectUrn: *state.Subject, ClientID: b.ClientID, OrganizationID: endpoint.OrganizationID, ProjectID: uuid.NullUUID{UUID: endpoint.ProjectID, Valid: endpoint.ProjectID != uuid.Nil}})
	if err != nil {
		return nil, fmt.Errorf("reconnect connection is no longer active: %w", err)
	}
	policy, err := toolfilter.ParseSessionPolicy(raw)
	if err != nil {
		return nil, fmt.Errorf("parse reconnect policy: %w", err)
	}
	if policy == nil || policy.Catalog == nil || !endpointAcceptsToolSelectionResource(endpoint, policy.Resource) {
		return nil, fmt.Errorf("reconnect catalogue no longer matches the connection")
	}
	if policy.Catalog.ID != b.CatalogID {
		catalog, err := s.loadConnectionCatalog(contextvalues.SetOAuthClientID(ctx, b.ClientID), endpoint, *state.Subject, policy.Catalog, time.Now())
		if err != nil {
			return nil, err
		}
		if state.ID == "" || catalog.ApprovalChallengeID != state.ID {
			return nil, fmt.Errorf("reconnect catalogue belongs to another approval")
		}
	} else if policy.Catalog.Mode != toolfilter.CatalogModeCached {
		return nil, fmt.Errorf("reconnect catalogue is not frozen")
	}
	return policy, nil
}

// OAuth completion opens review; only the subsequent approval changes tools.
func (s *Service) completeCodeReconnect(r *http.Request, grant remotesessions.RemoteGrant) error {
	state, err := s.authnChallengeCache.Get(r.Context(), "authnChallenge:"+grant.ParentChallengeID)
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load reconnect challenge: %w", err)
	}
	if !state.CodeReconnect.catalogPending() {
		return nil
	}
	if err := validateCodeReconnectBinding(state); err != nil {
		return err
	}
	if state.Subject == nil || *state.Subject != grant.Subject || state.UserSessionIssuerID != grant.UserSessionIssuerID {
		return fmt.Errorf("reconnect grant does not belong to this connection")
	}
	endpoint, err := s.loadResolvedMcpEndpointByRef(r.Context(), state.Endpoint)
	if err != nil {
		return err
	}
	if err := endpoint.ValidateGlobalChallenge(r.Context(), s.db, state.Endpoint, state.UserSessionIssuerID); err != nil {
		return err
	}
	policy, err := s.reconnectPolicy(r.Context(), usersessionsrepo.New(s.db), endpoint, state)
	if err != nil {
		return err
	}
	ctx := contextvalues.SetOAuthClientID(r.Context(), state.CodeReconnect.ClientID)
	if _, err := s.loadConnectionCatalog(ctx, endpoint, grant.Subject, policy.Catalog, time.Now()); err != nil {
		return err
	}
	binding := *state.CodeReconnect
	binding.ReviewReady = true
	return s.updateCodeReconnect(r.Context(), state, binding)
}

func (s *Service) approveCodeReconnect(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, inventory *consentToolInventory, selection *toolfilter.SessionSelection, mode toolfilter.CatalogMode) error {
	ctx := contextvalues.SetOAuthClientID(r.Context(), state.CodeReconnect.ClientID)
	if inventory == nil || !state.CodeReconnect.reviewReady() {
		return fmt.Errorf("reload the gateway tool review before approving")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin catalogue approval: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := usersessionsrepo.New(tx)
	policy, err := s.reconnectPolicy(ctx, q, endpoint, state)
	if err != nil {
		return err
	}
	if policy.Catalog.ID != state.CodeReconnect.CatalogID {
		// This challenge already committed; retry only cache invalidation and completion.
		jtis, err := commitCodeReconnectPolicy(ctx, tx, endpoint, state, policy, policy.Catalog.ID)
		if err != nil {
			return err
		}
		return s.finishCodeReconnectApproval(w, r, endpoint, state, jtis)
	}
	previous, err := s.loadConnectionCatalog(ctx, endpoint, *state.Subject, policy.Catalog, time.Now())
	if err != nil {
		return err
	}
	cards, err := s.buildRemoteSessionCards(ctx, endpoint, state, s.resolveAutoRefreshPolicy(ctx, endpoint.OrganizationID))
	if err != nil {
		return err
	}
	picks, err := s.consentUpstreamClientPicks(ctx, endpoint, state, cards)
	if err != nil {
		return err
	}
	consent := state
	consent.ClientID = state.CodeReconnect.ClientID
	frozen, err := s.freezeConnectionCatalog(ctx, endpoint, consent, inventory, selection, mode, picks, previous.ExpiresAt)
	if err != nil {
		return err
	}
	frozen.ApprovalChallengeID = state.ID
	replacement, err := frozen.marshal()
	if err != nil {
		return err
	}
	b := state.CodeReconnect
	if err := q.ClearReconnectUpstreamChoices(ctx, usersessionsrepo.ClearReconnectUpstreamChoicesParams{CredentialID: b.CredentialID, OrganizationID: endpoint.OrganizationID, McpServerIds: reconnectMemberIDs(previous, frozen)}); err != nil {
		return fmt.Errorf("replace upstream choices: %w", err)
	}
	var servers, clients, authClients []uuid.UUID
	for _, member := range frozen.Members {
		if member.Auth != nil {
			servers = append(servers, member.ServerID)
			clients = append(clients, member.Auth.RegistrationID.UUID)
			authClients = append(authClients, member.Auth.ClientID)
		}
	}
	if _, err := q.RecordUpstreamClientSelections(ctx, usersessionsrepo.RecordUpstreamClientSelectionsParams{UserSessionIssuerID: endpoint.UserSessionIssuerID, CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: b.CredentialID, Source: upstreamclients.SelectionSourceIssuance, PickedMcpServerIds: servers, PickedClientIds: clients, PickedAuthClientIds: authClients}); err != nil {
		return fmt.Errorf("record upstream choices: %w", err)
	}
	policy.Selection = selection
	policy.Catalog.ID = frozen.ID
	policy.Catalog.AllTools = selection == nil
	policy.Catalog.Mode = mode
	conditional, ok := s.connectionCatalogCache.(cache.ConditionalCache)
	if !ok {
		return fmt.Errorf("catalogue store does not support reconnect")
	}
	ttl := time.Until(previous.ExpiresAt)
	if ttl <= 0 {
		return errConnectionCatalogExpired
	}
	// Publish an immutable snapshot before committing its reference. A failed or
	// ambiguous commit leaves the old policy and catalogue consistent; an
	// unreferenced snapshot expires naturally at the original deadline.
	stored, err := conditional.SetIfAbsent(ctx, frozen.CacheKey(), replacement, ttl)
	if err != nil {
		return fmt.Errorf("replace catalogue: %w", err)
	}
	if !stored {
		return fmt.Errorf("catalogue publication conflict; reload the review before approving")
	}
	jtis, err := commitCodeReconnectPolicy(ctx, tx, endpoint, state, policy, b.CatalogID)
	if err != nil {
		return err
	}
	return s.finishCodeReconnectApproval(w, r, endpoint, state, jtis)
}

func commitCodeReconnectPolicy(ctx context.Context, tx pgx.Tx, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, policy *toolfilter.SessionPolicy, expectedCatalogID uuid.UUID) ([]string, error) {
	b := state.CodeReconnect
	rawPolicy, err := policy.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("encode approved policy: %w", err)
	}
	jtis, err := usersessionsrepo.New(tx).UpdateReconnectSessionPolicy(ctx, usersessionsrepo.UpdateReconnectSessionPolicyParams{CredentialID: b.CredentialID, UserSessionIssuerID: endpoint.UserSessionIssuerID, OrganizationID: endpoint.OrganizationID, ProjectID: uuid.NullUUID{UUID: endpoint.ProjectID, Valid: endpoint.ProjectID != uuid.Nil}, SubjectUrn: *state.Subject, ToolSelection: rawPolicy, ClientID: b.ClientID, CatalogID: expectedCatalogID.String()})
	if err != nil {
		return nil, fmt.Errorf("update approved session policy: %w", err)
	}
	if len(jtis) == 0 {
		return nil, fmt.Errorf("reconnect connection is no longer active")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit catalogue approval: %w", err)
	}
	return jtis, nil
}

func (s *Service) finishCodeReconnectApproval(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, jtis []string) error {
	ctx := r.Context()
	b := state.CodeReconnect
	for _, jti := range jtis {
		if err := s.sessionPolicyCache.DeleteByKey(ctx, sessionPolicyCacheKey(endpoint.UserSessionIssuerID.String(), jti)); err != nil {
			return fmt.Errorf("invalidate session policy: %w", err)
		}
	}
	completed := *b
	completed.CatalogComplete = true
	if err := s.updateCodeReconnect(ctx, state, completed); err != nil {
		return err
	}
	s.evictConsentToolInventory(ctx, state.ID)
	http.Redirect(w, r, endpoint.consentPath()+"?state="+state.ID, http.StatusSeeOther)
	return nil
}

// Include removed members so their stale choices cannot survive approval.
func reconnectMemberIDs(catalogs ...*connectionCatalog) []uuid.UUID {
	var ids []uuid.UUID
	for _, catalog := range catalogs {
		for _, member := range catalog.Members {
			if !slices.Contains(ids, member.ServerID) {
				ids = append(ids, member.ServerID)
			}
		}
	}
	return ids
}
