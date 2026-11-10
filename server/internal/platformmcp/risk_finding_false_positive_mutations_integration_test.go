package platformmcp

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
)

type recordingFalsePositiveStore struct {
	mu         sync.Mutex
	states     map[uuid.UUID]chrepo.FalsePositiveState
	suppressed []uuid.UUID
	reversed   []uuid.UUID
}

func (s *recordingFalsePositiveStore) add(ids ...uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.states == nil {
		s.states = make(map[uuid.UUID]chrepo.FalsePositiveState)
	}
	for _, id := range ids {
		s.states[id] = chrepo.FalsePositiveState{ID: id, Dismissed: false, RuleSuppressed: false}
	}
}

func (s *recordingFalsePositiveStore) ListFalsePositiveStates(_ context.Context, _, _ string, ids []uuid.UUID) ([]chrepo.FalsePositiveState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	states := make([]chrepo.FalsePositiveState, 0, len(ids))
	for _, id := range ids {
		if state, ok := s.states[id]; ok {
			states = append(states, state)
		}
	}
	return states, nil
}

func (s *recordingFalsePositiveStore) AppendFalsePositiveSuppression(_ context.Context, _, _, _, _, _ string, ids []uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.suppressed = append(s.suppressed, ids...)
	for _, id := range ids {
		s.states[id] = chrepo.FalsePositiveState{ID: id, Dismissed: true, RuleSuppressed: false}
	}
	return nil
}

func (s *recordingFalsePositiveStore) AppendFalsePositiveReversal(_ context.Context, _, _, _ string, ids []uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reversed = append(s.reversed, ids...)
	for _, id := range ids {
		s.states[id] = chrepo.FalsePositiveState{ID: id, Dismissed: false, RuleSuppressed: false}
	}
	return nil
}

func TestRiskFindingFalsePositiveHandlersPartitionReplayAndAudit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_risk_finding_false_positive")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	principal.ClientID = "test-client"
	principal.Surface = SurfacePlatformMCP
	ctx = ContextWithPrincipal(ctx, principal)

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPRiskMutations, principal.OrganizationID, true)
	findingsStore := &recordingFalsePositiveStore{}
	controls, err := NewRiskMutationControls(conn, flags, NewPostgresOrganizationSlugResolver(conn), testOperationBudget(), "risk-finding-test-key")
	require.NoError(t, err)
	handlers, err := NewRiskMutationHandlers(conn, controls, risk.NewPolicyMutationCore(conn, audit.NewLogger(), nil, noopRiskPolicySignaler{}, nil), nil, risk.NewFalsePositiveCore(audit.NewLogger(), findingsStore))
	require.NoError(t, err)
	require.NotNil(t, handlers.MarkFindingsFalsePositive)
	require.NotNil(t, handlers.UnmarkFindingsFalsePositive)
	require.Nil(t, handlers.CreateExclusion, "a nil exclusion core leaves exclusion writes stubbed")

	first, second, third := uuid.New(), uuid.New(), uuid.New()
	clickHouseOnly, unknown := uuid.New(), uuid.New()
	findingsStore.add(first, second, third, clickHouseOnly)
	dismissBefore, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionRiskResultDismiss)
	require.NoError(t, err)
	restoreBefore, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionRiskResultRestore)
	require.NoError(t, err)

	// An unconfirmed call changes nothing and is refused before any work.
	_, _, err = handlers.MarkFindingsFalsePositive(ctx, nil, map[string]any{
		"project_slug": project.Slug, "finding_ids": []any{first.String()}, "confirmed": false, "idempotency_key": "unconfirmed",
	})
	requireRiskMutationRefusal(t, err, "confirmation_required")
	requireFindingDismissed(t, findingsStore, first, false)

	markInput := map[string]any{
		"project_slug": project.Slug, "finding_ids": []any{first.String(), unknown.String(), clickHouseOnly.String(), second.String(), first.String()},
		"reason": "reviewed with the owning team", "confirmed": true, "idempotency_key": "mark-key",
	}
	_, marked, err := handlers.MarkFindingsFalsePositive(ctx, nil, markInput)
	require.NoError(t, err)
	require.False(t, marked.Receipt.Replayed)
	require.Equal(t, riskReceiptProject(project), marked.Project)
	require.Equal(t, []string{first.String(), clickHouseOnly.String(), second.String()}, marked.ChangedFindingIDs)
	require.Empty(t, marked.AlreadyInRequestedStateFindingIDs)
	require.Equal(t, []string{unknown.String()}, marked.NotFoundFindingIDs)
	require.Equal(t, riskFindingResultCategoryDismissed, marked.ResultCategory)
	requireFindingDismissed(t, findingsStore, first, true)
	requireFindingDismissed(t, findingsStore, second, true)
	requireFindingDismissed(t, findingsStore, third, false)
	require.ElementsMatch(t, []uuid.UUID{first, clickHouseOnly, second}, findingsStore.suppressed)

	dismissAfter, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionRiskResultDismiss)
	require.NoError(t, err)
	require.Equal(t, dismissBefore+3, dismissAfter, "one dismiss audit entry per changed ClickHouse finding")
	dismissAudit, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionRiskResultDismiss)
	require.NoError(t, err)
	require.Equal(t, principal.UserID, dismissAudit.ActorID)
	require.Contains(t, []string{first.String(), clickHouseOnly.String(), second.String()}, dismissAudit.SubjectID)

	// Replaying the same key returns the stored receipt without re-auditing.
	_, replayed, err := handlers.MarkFindingsFalsePositive(ctx, nil, markInput)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	require.Equal(t, marked.Receipt.ID, replayed.Receipt.ID)
	require.Equal(t, marked.RiskFindingFalsePositiveReceipt, replayed.RiskFindingFalsePositiveReceipt)
	dismissAfterReplay, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionRiskResultDismiss)
	require.NoError(t, err)
	require.Equal(t, dismissAfter, dismissAfterReplay)

	// A fresh key over already-dismissed findings reports them as unchanged.
	_, again, err := handlers.MarkFindingsFalsePositive(ctx, nil, map[string]any{
		"project_slug": project.Slug, "finding_ids": []any{first.String(), third.String()}, "confirmed": true, "idempotency_key": "mark-again-key",
	})
	require.NoError(t, err)
	require.False(t, again.Receipt.Replayed)
	require.Equal(t, []string{third.String()}, again.ChangedFindingIDs)
	require.Equal(t, []string{first.String()}, again.AlreadyInRequestedStateFindingIDs)
	require.Empty(t, again.NotFoundFindingIDs)

	_, _, err = handlers.UnmarkFindingsFalsePositive(ctx, nil, map[string]any{
		"project_slug": project.Slug, "finding_ids": []any{first.String()}, "confirmed": false, "idempotency_key": "unmark-unconfirmed",
	})
	requireRiskMutationRefusal(t, err, "confirmation_required")

	_, restored, err := handlers.UnmarkFindingsFalsePositive(ctx, nil, map[string]any{
		"project_slug": project.Slug, "finding_ids": []any{unknown.String(), first.String(), clickHouseOnly.String(), second.String()}, "confirmed": true, "idempotency_key": "unmark-key",
	})
	require.NoError(t, err)
	require.Equal(t, []string{first.String(), clickHouseOnly.String(), second.String()}, restored.ChangedFindingIDs)
	require.Equal(t, []string{unknown.String()}, restored.NotFoundFindingIDs)
	require.Equal(t, riskFindingResultCategoryRestored, restored.ResultCategory)
	requireFindingDismissed(t, findingsStore, first, false)
	requireFindingDismissed(t, findingsStore, second, false)
	requireFindingDismissed(t, findingsStore, third, true)

	restoreAfter, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionRiskResultRestore)
	require.NoError(t, err)
	require.Equal(t, restoreBefore+3, restoreAfter)
	restoreAudit, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionRiskResultRestore)
	require.NoError(t, err)
	require.Equal(t, principal.UserID, restoreAudit.ActorID)

	_, unchanged, err := handlers.UnmarkFindingsFalsePositive(ctx, nil, map[string]any{
		"project_slug": project.Slug, "finding_ids": []any{first.String()}, "confirmed": true, "idempotency_key": "unmark-noop-key",
	})
	require.NoError(t, err)
	require.Empty(t, unchanged.ChangedFindingIDs)
	require.Equal(t, []string{first.String()}, unchanged.AlreadyInRequestedStateFindingIDs)
	require.Equal(t, riskFindingResultCategoryNoChange, unchanged.ResultCategory)
	restoreAfterNoop, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionRiskResultRestore)
	require.NoError(t, err)
	require.Equal(t, restoreAfter, restoreAfterNoop, "a no-op restore audits nothing")

	// The same key with a different finding set is a conflict, not a replay.
	conflicting := cloneRiskMutationInput(markInput)
	conflicting["finding_ids"] = []any{third.String()}
	_, _, err = handlers.MarkFindingsFalsePositive(ctx, nil, conflicting)
	requireRiskMutationRefusal(t, err, "conflict")

	_, _, err = handlers.MarkFindingsFalsePositive(ctx, nil, map[string]any{
		"project_slug": "missing-project", "finding_ids": []any{first.String()}, "confirmed": true, "idempotency_key": "missing-project-key",
	})
	requireRiskMutationRefusal(t, err, "not_found")
}

func requireFindingDismissed(t *testing.T, store *recordingFalsePositiveStore, findingID uuid.UUID, dismissed bool) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	state, ok := store.states[findingID]
	require.True(t, ok)
	require.Equal(t, dismissed, state.Dismissed)
}
