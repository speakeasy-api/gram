package mcpapproval

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ReviewShadowMCPPolicyURLEdit names every standing decision a policy
// URL-list edit would contradict, so the write path can demand explicit
// confirmation. Counterpart of ReconcileStandingDecisionsForPolicy: when a
// policy becomes blocking the replay runs and decisions win; when an
// already-blocking policy's list is edited this review runs and the edit
// wins after confirmation.
//
// A conflict is a membership change, not a membership state: only toggles
// the edit itself performs are flagged, never pre-existing drift.
func (s *Service) ReviewShadowMCPPolicyURLEdit(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID uuid.UUID,
	policyID uuid.UUID,
	disposition string,
	desiredAllowedURLs []string,
	desiredBlockedURLs []string,
) (shadowmcp.StandingDecisionReview, error) {
	review := shadowmcp.StandingDecisionReview{Conflicts: nil, StandingURLs: nil}

	// The same lock decision-time enforcement and the policy replay take, so
	// a concurrent decision never interleaves into a silent contradiction.
	if err := repo.New(tx).LockProjectEnforcementState(ctx, projectID.String()); err != nil {
		return review, fmt.Errorf("lock project enforcement state for url edit review: %w", err)
	}

	standing, err := repo.New(tx).ListStandingServerDecisionsForProject(ctx, projectID)
	if err != nil {
		return review, fmt.Errorf("list standing decisions for url edit review: %w", err)
	}
	if len(standing) == 0 {
		return review, nil
	}

	// Membership changes are judged against the policy's current grant set
	// for the list the disposition uses: bypass grants carry a block_all
	// policy's allow list, block grants an allow_all policy's block list.
	scope := authz.ScopeRiskPolicyBypass
	desired := desiredAllowedURLs
	if disposition == shadowmcp.DispositionAllowAll {
		scope = authz.ScopeRiskPolicyBlock
		desired = desiredBlockedURLs
	}
	existing, err := policyURLGrantSet(ctx, tx, organizationID, scope, policyID.String())
	if err != nil {
		return review, err
	}

	// A nil list means this save does not edit the list (an audience-only
	// refresh); membership is unchanged and nothing can conflict.
	desiredSet := existing
	if desired != nil {
		desiredSet = make(map[string]struct{}, len(desired))
		for _, serverURL := range desired {
			desiredSet[serverURL] = struct{}{}
		}
	}

	for _, row := range standing {
		review.StandingURLs = append(review.StandingURLs, row.TargetKey)

		_, has := existing[row.TargetKey]
		_, wants := desiredSet[row.TargetKey]
		if has == wants {
			continue
		}

		// On an allow list (block_all), removing revokes access and adding
		// grants it; on a block list (allow_all) the directions invert.
		removingAllow := has && !wants
		if disposition == shadowmcp.DispositionAllowAll {
			removingAllow = !removingAllow
		}
		contradicted := removingAllow && row.Decision == decisionApproved ||
			!removingAllow && row.Decision == decisionDenied
		if !contradicted {
			continue
		}

		review.Conflicts = append(review.Conflicts, shadowmcp.StandingDecisionConflict{
			RequestID: row.ID,
			TargetKey: row.TargetKey,
			TargetRaw: row.TargetRaw,
			Decision:  row.Decision,
		})
	}

	slices.SortFunc(review.Conflicts, func(a, b shadowmcp.StandingDecisionConflict) int {
		return strings.Compare(a.TargetRaw, b.TargetRaw)
	})
	slices.Sort(review.StandingURLs)

	return review, nil
}

// SupersedeShadowMCPDecisions transitions each conflicted request to
// superseded — actor-attributed, audit-logged, decision history intact — in
// the same transaction as the policy edit that displaces the decisions.
func (s *Service) SupersedeShadowMCPDecisions(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID uuid.UUID,
	conflicts []shadowmcp.StandingDecisionConflict,
	actor urn.Principal,
	actorDisplayName *string,
) error {
	queries := repo.New(tx)
	for _, conflict := range conflicts {
		if err := queries.SetApprovalRequestStatus(ctx, repo.SetApprovalRequestStatusParams{
			ID:        conflict.RequestID,
			ProjectID: projectID,
			Status:    statusSuperseded,
		}); err != nil {
			return fmt.Errorf("supersede approval request %s: %w", conflict.RequestID, err)
		}

		if err := s.audit.LogMCPApprovalRequestSupersede(ctx, tx, audit.LogMCPApprovalRequestSupersedeEvent{
			OrganizationID:   organizationID,
			ProjectID:        projectID,
			Actor:            actor,
			ActorDisplayName: actorDisplayName,
			ActorSlug:        nil,
			RequestURN:       urn.NewMCPApprovalRequest(conflict.RequestID),
			Decision:         conflict.Decision,
			TargetRaw:        conflict.TargetRaw,
		}); err != nil {
			return fmt.Errorf("audit superseded approval request %s: %w", conflict.RequestID, err)
		}
	}

	return nil
}

// policyURLGrantSet reads the canonical server URLs a policy holds URL
// grants for under one scope, re-canonicalizing legacy selector spellings.
func policyURLGrantSet(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	scope authz.Scope,
	policyID string,
) (map[string]struct{}, error) {
	grants, err := authz.ListGrantsForResource(ctx, tx, authz.Resource{
		OrganizationID: organizationID,
		Scope:          scope,
		ResourceID:     policyID,
	})
	if err != nil {
		return nil, fmt.Errorf("list policy url grants for edit review: %w", err)
	}

	urls := make(map[string]struct{}, len(grants))
	for _, grant := range grants {
		serverURL := grant.Selector[authz.SelectorKeyServerURL]
		if serverURL == "" {
			continue
		}
		if inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(serverURL); ok {
			serverURL = inventoryURL.CanonicalURL
		}
		urls[serverURL] = struct{}{}
	}
	return urls, nil
}
