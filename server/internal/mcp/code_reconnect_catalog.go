package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
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
	if _, err := s.loadConnectionCatalog(ctx, endpoint, grant.Subject, policy.Catalog, time.Now()); err != nil {
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
	if !ok || member.ServerID != binding.ServerID || member.Auth == nil || !member.Auth.RegistrationID.Valid || member.Auth.RegistrationID.UUID != grant.RemoteSessionClientID {
		return fmt.Errorf("could not discover tools with the reconnected credential; request a new connection link")
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
	conditional, ok := s.connectionCatalogCache.(cache.CompareAndSwapCache)
	if !ok {
		return fmt.Errorf("catalogue store does not support reconnect")
	}
	for range 3 {
		catalog, err := s.loadConnectionCatalog(ctx, endpoint, grant.Subject, policy.Catalog, time.Now())
		if err != nil {
			return err
		}
		expected, err := json.Marshal(catalog)
		if err != nil {
			return fmt.Errorf("encode current catalogue: %w", err)
		}
		if err := mergeReconnectedCatalog(catalog, binding.Server, member, tools, outcomes, policy.Catalog.AllTools); err != nil {
			return err
		}
		replacement, err := json.Marshal(catalog)
		if err != nil {
			return fmt.Errorf("encode reconnected catalogue: %w", err)
		}
		if len(replacement) > connectionCatalogMaxBytes {
			return errConnectionCatalogTooLarge
		}
		ttl := time.Until(catalog.ExpiresAt)
		if ttl <= 0 {
			return errConnectionCatalogExpired
		}
		swapped, err := conditional.CompareAndSwap(ctx, catalog.CacheKey(), expected, replacement, ttl)
		if err != nil {
			return fmt.Errorf("update reconnected catalogue: %w", err)
		}
		if swapped {
			// A connected card alone must not close this page before its catalogue
			// update has succeeded (including pages offering several credentials).
			for range 3 {
				current, err := s.authnChallengeCache.Get(ctx, state.CacheKey())
				if err != nil {
					return fmt.Errorf("load completed reconnect: %w", err)
				}
				if current.CodeReconnect == nil || current.CodeReconnect.CatalogID != binding.CatalogID {
					return fmt.Errorf("reconnect challenge changed")
				}
				updated := current
				completed := *current.CodeReconnect
				completed.CatalogComplete = true
				updated.CodeReconnect = &completed
				done, err := s.authnChallengeCache.CompareAndSwap(ctx, current, updated)
				if err != nil {
					return fmt.Errorf("complete reconnect challenge: %w", err)
				}
				if done {
					return nil
				}
			}
			return fmt.Errorf("reconnect challenge changed while completing consent")
		}
	}
	return fmt.Errorf("catalogue changed while reconnecting; request a new connection link")
}

// mergeReconnectedCatalog preserves other members and existing tool selections.
// A previously unapproved member gains its discovered tools through this consent.
func mergeReconnectedCatalog(catalog *connectionCatalog, slug string, member connectionCatalogMember, tools []consentInventoryTool, outcomes []consentMemberOutcome, allTools bool) error {
	_, alreadyApproved := catalog.Members[slug]
	selected := map[string]bool{}
	kept := make([]*toolListEntry, 0, len(catalog.Tools)+len(tools))
	for _, tool := range catalog.Tools {
		source, _, err := metamcp.SplitQualifiedName(tool.Name)
		if err != nil {
			return fmt.Errorf("parse catalogue tool name: %w", err)
		}
		if source == slug {
			selected[tool.Name] = true
		} else {
			kept = append(kept, tool)
		}
	}
	for _, tool := range tools {
		source, _, err := metamcp.SplitQualifiedName(tool.Name)
		if err != nil || source != slug || tool.Definition == nil || tool.Definition.Name != tool.Name || consentEvidenceHash(tool.Definition) != tool.DefinitionHash {
			return fmt.Errorf("incomplete reconnect tool inventory")
		}
		if allTools || !alreadyApproved || len(selected) == 0 || selected[tool.Name] {
			kept = append(kept, tool.Definition)
		}
	}
	slices.SortFunc(kept, func(a, b *toolListEntry) int { return strings.Compare(a.Name, b.Name) })
	catalog.Tools = kept
	if catalog.Members == nil {
		catalog.Members = map[string]connectionCatalogMember{}
	}
	catalog.Members[slug] = member
	catalog.MemberOutcomes = slices.DeleteFunc(catalog.MemberOutcomes, func(outcome consentMemberOutcome) bool { return outcome.Slug == slug })
	for _, outcome := range outcomes {
		if outcome.Slug == slug {
			catalog.MemberOutcomes = append(catalog.MemberOutcomes, outcome)
		}
	}
	return nil
}
