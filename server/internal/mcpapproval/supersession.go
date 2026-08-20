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

// ReviewShadowMCPPolicyURLEdit implements the risk service's conflict-check
// seam: before a policy URL-list save touches grants, it names every standing
// decision the save would contradict, so the write path can demand explicit
// confirmation instead of contradicting a decision record silently.
//
// It is the counterpart of ReconcileStandingDecisionsForPolicy, and the two
// split the policy write paths between them: when a policy becomes blocking
// the replay runs and decisions win (a freshly typed list has never seen the
// standing record); when an already-blocking policy's list is edited this
// review runs and the edit wins — but only after the caller confirms
// superseding the decisions it contradicts.
//
// A conflict is a membership change, not a membership state: only toggles the
// edit itself performs are flagged. A denied server whose allow grant already
// drifted in before this feature reads as drift on the inventory, and a save
// that does not touch it is not asked to answer for it.
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

	// The same lock decision-time enforcement and the policy replay take: a
	// decision recorded concurrently with this edit either commits first and
	// is seen by this check, or waits and re-derives its grants after the
	// edit commits — never interleaves into a silent contradiction.
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

		// Which transition contradicts the decision depends on what the list
		// means. On an allow list (block_all), removing an approved server
		// revokes its access and adding a denied one grants it. On a block
		// list (allow_all), the directions invert: removing a denied server
		// unblocks it and adding an approved one blocks it.
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
// superseded — actor-attributed and audit-logged, with the decision history
// and its rationale left intact. Runs in the same transaction as the policy
// edit that displaces the decisions, so the record and the grant state it
// explains commit atomically.
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

// policyURLGrantSet reads the canonical server URLs a policy currently holds
// URL grants for under one scope. Legacy selectors may carry pre-canonical
// URL forms, so each value re-canonicalizes before joining the set — the same
// tolerance the grant revoker applies.
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
