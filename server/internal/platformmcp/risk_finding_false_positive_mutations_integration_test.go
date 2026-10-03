package platformmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

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
	controls := NewRiskMutationControls(conn, flags, NewPostgresOrganizationSlugResolver(conn), testOperationBudget(), "risk-finding-test-key")
	handlers := NewRiskMutationHandlers(conn, controls, newTestRiskPolicyCore(t, conn, flags), risk.NewExclusionMutationCore(testenv.NewLogger(t), conn, audit.NewLogger(), &recordingRiskExclusionReconciler{}, "risk-exclusion-test-key"), risk.NewFalsePositiveCore(audit.NewLogger()), testRiskPolicyCatalog(t))
	require.NotNil(t, handlers.MarkFindingsFalsePositive)
	require.NotNil(t, handlers.UnmarkFindingsFalsePositive)

	first, second, third := seedRiskFinding(t, ctx, conn, principal.OrganizationID, project.ID), seedRiskFinding(t, ctx, conn, principal.OrganizationID, project.ID), seedRiskFinding(t, ctx, conn, principal.OrganizationID, project.ID)
	unknown := uuid.New()
	dismissBefore, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionRiskResultDismiss)
	require.NoError(t, err)
	restoreBefore, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionRiskResultRestore)
	require.NoError(t, err)

	// An unconfirmed call changes nothing and is refused before any work.
	_, _, err = handlers.MarkFindingsFalsePositive(ctx, nil, map[string]any{
		"project_slug": project.Slug, "finding_ids": []any{first.String()}, "confirmed": false, "idempotency_key": "unconfirmed",
	})
	requireRiskMutationRefusal(t, err, "confirmation_required")
	requireFindingDismissed(t, ctx, conn, project.ID, first, false)

	markInput := map[string]any{
		"project_slug": project.Slug, "finding_ids": []any{first.String(), unknown.String(), second.String(), first.String()},
		"reason": "reviewed with the owning team", "confirmed": true, "idempotency_key": "mark-key",
	}
	_, marked, err := handlers.MarkFindingsFalsePositive(ctx, nil, markInput)
	require.NoError(t, err)
	require.False(t, marked.Receipt.Replayed)
	require.Equal(t, riskReceiptProject(project), marked.Project)
	require.Equal(t, []string{first.String(), second.String()}, marked.ChangedFindingIDs)
	require.Empty(t, marked.AlreadyInRequestedStateFindingIDs)
	require.Equal(t, []string{unknown.String()}, marked.NotFoundFindingIDs)
	require.Equal(t, riskFindingResultCategoryDismissed, marked.ResultCategory)
	requireFindingDismissed(t, ctx, conn, project.ID, first, true)
	requireFindingDismissed(t, ctx, conn, project.ID, second, true)
	requireFindingDismissed(t, ctx, conn, project.ID, third, false)

	dismissAfter, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionRiskResultDismiss)
	require.NoError(t, err)
	require.Equal(t, dismissBefore+2, dismissAfter, "one dismiss audit entry per changed finding")
	dismissAudit, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionRiskResultDismiss)
	require.NoError(t, err)
	require.Equal(t, principal.UserID, dismissAudit.ActorID)
	require.Contains(t, []string{first.String(), second.String()}, dismissAudit.SubjectID)

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
		"project_slug": project.Slug, "finding_ids": []any{unknown.String(), first.String(), second.String()}, "confirmed": true, "idempotency_key": "unmark-key",
	})
	require.NoError(t, err)
	require.Equal(t, []string{first.String(), second.String()}, restored.ChangedFindingIDs)
	require.Equal(t, []string{unknown.String()}, restored.NotFoundFindingIDs)
	require.Equal(t, riskFindingResultCategoryRestored, restored.ResultCategory)
	requireFindingDismissed(t, ctx, conn, project.ID, first, false)
	requireFindingDismissed(t, ctx, conn, project.ID, second, false)
	requireFindingDismissed(t, ctx, conn, project.ID, third, true)

	restoreAfter, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionRiskResultRestore)
	require.NoError(t, err)
	require.Equal(t, restoreBefore+2, restoreAfter)
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

// seedRiskFinding inserts one active gitleaks finding anchored to a fresh chat
// message and returns its risk_results id, the identifier the tools accept.
func seedRiskFinding(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	policyID := uuid.New()
	_, err := riskrepo.New(conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID: policyID, ProjectID: projectID, OrganizationID: organizationID, Name: "Finding fixture " + policyID.String()[:8], Sources: []string{"gitleaks"},
		AnalyzerConfig: []byte(`{}`), DisabledRules: nil, Enabled: true, Action: "flag", AudienceType: "everyone",
		ShadowMcpDisposition: pgtype.Text{}, UserMessage: pgtype.Text{},
	})
	require.NoError(t, err)

	chatID, err := chatrepo.New(conn).UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID: uuid.New(), ProjectID: projectID, OrganizationID: organizationID, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	messageID, err := testrepo.New(conn).InsertChatMessage(ctx, testrepo.InsertChatMessageParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Role: "user", Content: "fixture message carrying an example token",
	})
	require.NoError(t, err)

	findingID := uuid.New()
	match := "EXAMPLE_TOKEN_VALUE"
	_, err = riskrepo.New(conn).InsertRiskResults(ctx, []riskrepo.InsertRiskResultsParams{{
		ID: findingID, ProjectID: projectID, OrganizationID: organizationID, RiskPolicyID: policyID, RiskPolicyVersion: 1,
		ChatMessageID: uuid.NullUUID{UUID: messageID, Valid: true}, Source: "gitleaks", Found: true,
		RuleID: pgtype.Text{String: "generic-api-key", Valid: true}, Description: pgtype.Text{String: "Generic API key", Valid: true},
		Match: pgtype.Text{String: match, Valid: true}, StartPos: pgtype.Int4{Int32: 0, Valid: true}, EndPos: pgtype.Int4{Int32: int32(len(match)), Valid: true},
		Confidence: pgtype.Float8{Float64: 1, Valid: true},
	}})
	require.NoError(t, err)
	return findingID
}

func requireFindingDismissed(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, findingID uuid.UUID, dismissed bool) {
	t.Helper()
	rows, err := riskrepo.New(conn).GetRiskResultsByIDs(ctx, riskrepo.GetRiskResultsByIDsParams{ProjectID: projectID, Ids: []uuid.UUID{findingID}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, dismissed, rows[0].FalsePositiveAt.Valid)
}
