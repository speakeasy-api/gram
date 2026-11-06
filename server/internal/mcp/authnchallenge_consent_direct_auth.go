package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

// consentDirectAuthChoicesEnforced keeps the rollout unchanged for ordinary
// servers. GitHub App credentials have no legacy transport equivalent, so their
// preview, approval choice, and inventory binding always use typed selection.
func (s *Service) consentDirectAuthChoicesEnforced(ctx context.Context, endpoint *ResolvedMcpEndpoint) (bool, error) {
	enforced, err := s.consentAuthChoicesEnforced(ctx, endpoint.OrganizationID)
	if err != nil || enforced || !endpoint.McpServerID.Valid || endpoint.ToolsetID.Valid || endpoint.MetaMcpServerID.Valid {
		return enforced, err
	}
	return s.serverRequiresGitHubApp(ctx, endpoint.OrganizationID, endpoint.McpServerID.UUID)
}

// consentDirectPreviewAuth uses the same offered choices and authorization as
// approval. A nil result means the legacy path, never an enforcement error.
func (s *Service) consentDirectPreviewAuth(ctx context.Context, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, posted string) (*authClientResolution, error) {
	enforced, err := s.consentDirectAuthChoicesEnforced(ctx, endpoint)
	if err != nil || !enforced {
		return nil, err
	}
	cards, err := s.buildRemoteSessionCards(ctx, endpoint, state, s.resolveAutoRefreshPolicy(ctx, endpoint.OrganizationID))
	if err != nil {
		return nil, fmt.Errorf("build consent connection cards: %w", err)
	}
	picks, err := s.consentDirectAuthClientPick(ctx, endpoint, state, posted, nil, cards)
	if err != nil {
		return nil, err
	}
	if len(picks) == 0 {
		res := gapResolution("")
		res.outcome = authClientNoAuth
		return &res, nil
	}
	ctx, err = s.contextForSessionSubject(ctx, endpoint, *state.Subject, "consent:"+state.ID, state.ClientID)
	if err != nil {
		return nil, fmt.Errorf("stamp consent subject context: %w", err)
	}
	res, err := s.resolveAuthClientChoiceForConsent(withMandatoryAdmission(ctx), endpoint.OrganizationID, endpoint.McpServerID.UUID, endpoint.ProjectID, picks[endpoint.McpServerID.UUID], *state.Subject)
	if err != nil {
		return nil, err
	}
	if res.outcome != authClientResolved && res.outcome != authClientLegacy {
		return nil, errConsentConfigurationChanged
	}
	return &res, nil
}

// bindDirectConsentInventory prevents one transport attempt from mixing tools
// or upstream sessions from different credentials. Nil means legacy capture;
// a pointer to the nil UUID means an enforced, explicitly no-auth capture.
func bindDirectConsentInventory(draft *consentToolInventory, auth *authClientResolution) error {
	var selected *uuid.UUID
	if auth != nil {
		id := auth.clientID
		selected = &id
	}
	if draft.DirectAuthClient != nil || draft.McpSessionID != "" || draft.Complete || len(draft.Tools) > 0 {
		if !sameConsentAuthClient(draft.DirectAuthClient, selected) {
			return errConsentConfigurationChanged
		}
	}
	draft.DirectAuthClient = selected
	return nil
}

func sameConsentAuthClient(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// validateDirectConsentInventory leaves hosted and builtin inventories alone;
// only proxied direct servers capture a typed upstream credential.
func (s *Service) validateDirectConsentInventory(ctx context.Context, endpoint *ResolvedMcpEndpoint, inventory *consentToolInventory, picks map[uuid.UUID]uuid.UUID) error {
	if inventory == nil || !endpoint.McpServerID.Valid || endpoint.ToolsetID.Valid || endpoint.MetaMcpServerID.Valid {
		return nil
	}
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: endpoint.McpServerID.UUID, ProjectID: endpoint.ProjectID})
	if err != nil {
		return fmt.Errorf("load consent backend: %w", err)
	}
	if !server.RemoteMcpServerID.Valid && !server.TunneledMcpServerID.Valid {
		return nil
	}
	enforced, err := s.consentDirectAuthChoicesEnforced(ctx, endpoint)
	if err != nil {
		return err
	}
	var selected *uuid.UUID
	if enforced {
		id := picks[endpoint.McpServerID.UUID]
		selected = &id
	}
	if !sameConsentAuthClient(inventory.DirectAuthClient, selected) {
		return errConsentConfigurationChanged
	}
	return nil
}
