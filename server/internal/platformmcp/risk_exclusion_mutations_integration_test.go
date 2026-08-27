package platformmcp

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type recordingRiskExclusionReconciler struct {
	mu    sync.Mutex
	calls []uuid.UUID
}

func (r *recordingRiskExclusionReconciler) Reconcile(_ context.Context, _, exclusionID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, exclusionID)
	return nil
}

func (r *recordingRiskExclusionReconciler) called(exclusionID uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.calls, exclusionID)
}

func TestRiskExclusionMutationHandlersCreateUpdateReplayAndRedact(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_risk_exclusion_mutations")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	principal.ClientID = "test-client"
	principal.Surface = SurfacePlatformMCP
	ctx = ContextWithPrincipal(ctx, principal)

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPRiskMutations, principal.OrganizationID, true)
	controls, err := NewRiskMutationControls(conn, flags, NewPostgresOrganizationSlugResolver(conn), testOperationBudget(), "risk-exclusion-test-key")
	require.NoError(t, err)
	reconciler := &recordingRiskExclusionReconciler{}
	exclusions := risk.NewExclusionMutationCore(testenv.NewLogger(t), conn, audit.NewLogger(), reconciler, "risk-exclusion-test-key")
	handlers, err := NewRiskMutationHandlers(conn, controls, risk.NewPolicyMutationCore(conn, audit.NewLogger(), nil, noopRiskPolicySignaler{}, nil), exclusions)
	require.NoError(t, err)
	require.NotNil(t, handlers.CreateExclusion)
	require.NotNil(t, handlers.UpdateExclusion)

	secret := "sensitive-value"
	createInput := map[string]any{
		"project_slug": project.Slug, "match_type": "exact", "match_value": secret,
		"enabled": true, "rule_id_filter": "secret.aws_secret_access_key", "source_filter": "gitleaks",
		"idempotency_key": "create-exclusion-key",
	}
	_, created, err := handlers.CreateExclusion(ctx, nil, createInput)
	require.NoError(t, err)
	require.False(t, created.Receipt.Replayed)
	require.Equal(t, "created", created.ResultCategory)
	require.NotEmpty(t, created.Version)

	exclusionID, err := uuid.Parse(created.Exclusion.ID)
	require.NoError(t, err)
	stored, err := riskrepo.New(conn).GetRiskExclusion(ctx, riskrepo.GetRiskExclusionParams{ID: exclusionID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, secret, stored.MatchValue)
	require.Equal(t, "secret.aws_secret_access_key", stored.RuleIDFilter.String)
	require.Equal(t, "gitleaks", stored.SourceFilter.String)

	createAudit, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionRiskExclusionCreate)
	require.NoError(t, err)
	require.Equal(t, exclusionID.String(), createAudit.SubjectID)
	require.Equal(t, principal.UserID, createAudit.ActorID)
	require.NotContains(t, string(createAudit.Metadata), secret)

	_, replayedCreate, err := handlers.CreateExclusion(ctx, nil, createInput)
	require.NoError(t, err)
	require.True(t, replayedCreate.Receipt.Replayed)
	require.Equal(t, created.Receipt.ID, replayedCreate.Receipt.ID)

	matchingInput := cloneRiskMutationInput(createInput)
	matchingInput["idempotency_key"] = "create-exclusion-convergence-key"
	_, matched, err := handlers.CreateExclusion(ctx, nil, matchingInput)
	require.NoError(t, err)
	require.True(t, matched.MatchedExisting)
	require.Equal(t, exclusionID.String(), matched.Exclusion.ID)

	changedCreate := cloneRiskMutationInput(createInput)
	changedCreate["match_value"] = "different"
	_, _, err = handlers.CreateExclusion(ctx, nil, changedCreate)
	requireRiskMutationRefusal(t, err, "conflict")

	reads, err := newRiskReadService(conn, "risk-exclusion-test-key")
	require.NoError(t, err)
	listed, err := reads.ListExclusions(ctx, principal, ListRiskExclusionsInput{ProjectSlug: project.Slug})
	require.NoError(t, err)
	require.Len(t, listed.Exclusions, 1)
	require.Equal(t, created.Version, listed.Exclusions[0].Version)
	require.Empty(t, listed.Exclusions[0].MatchValue)
	require.NotEmpty(t, listed.Exclusions[0].MatchFingerprint)

	updateInput := map[string]any{
		"project_slug": project.Slug, "exclusion_id": exclusionID.String(), "enabled": false,
		"expected_version": listed.Exclusions[0].Version, "idempotency_key": "update-exclusion-key",
	}
	_, updated, err := handlers.UpdateExclusion(ctx, nil, updateInput)
	require.NoError(t, err)
	require.False(t, updated.Exclusion.Enabled)
	require.NotEqual(t, created.Version, updated.Version)

	stored, err = riskrepo.New(conn).GetRiskExclusion(ctx, riskrepo.GetRiskExclusionParams{ID: exclusionID, ProjectID: project.ID})
	require.NoError(t, err)
	require.False(t, stored.Enabled)
	require.Equal(t, secret, stored.MatchValue, "enabled-only update must preserve the definition")

	updateAudit, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionRiskExclusionUpdate)
	require.NoError(t, err)
	require.NotContains(t, string(updateAudit.BeforeSnapshot), secret)
	require.NotContains(t, string(updateAudit.AfterSnapshot), secret)

	_, replayedUpdate, err := handlers.UpdateExclusion(ctx, nil, updateInput)
	require.NoError(t, err)
	require.True(t, replayedUpdate.Receipt.Replayed)
	require.Equal(t, updated.Version, replayedUpdate.Version)

	stale := cloneRiskMutationInput(updateInput)
	stale["idempotency_key"] = "stale-exclusion-key"
	_, _, err = handlers.UpdateExclusion(ctx, nil, stale)
	requireRiskMutationRefusal(t, err, "conflict")

	_, _, err = handlers.CreateExclusion(ctx, nil, map[string]any{
		"project_slug": project.Slug, "match_type": "regex", "match_value": ".*", "enabled": true, "idempotency_key": "regex-key",
	})
	requireRiskMutationRefusal(t, err, "invalid_request")
	_, _, err = handlers.UpdateExclusion(ctx, nil, map[string]any{
		"project_slug": project.Slug, "exclusion_id": exclusionID.String(), "enabled": true,
		"expected_version": updated.Version, "idempotency_key": "definition-edit-key", "match_value": "forbidden",
	})
	requireRiskMutationRefusal(t, err, "invalid_request")

	receipt, err := platformrepo.New(conn).GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID, UserID: conv.ToPGText(principal.UserID), SubjectUrn: userSubjectURN(principal.UserID),
		ProjectID: project.ID, Operation: operationCreateRiskExclusion, IdempotencyKey: "create-exclusion-key",
	})
	require.NoError(t, err)
	require.NotContains(t, string(receipt.ResultPayload), secret)
	var safeResult CreateRiskExclusionReceiptResult
	require.NoError(t, json.Unmarshal(receipt.ResultPayload, &safeResult))
	require.Equal(t, exclusionID.String(), safeResult.Exclusion.ID)

	require.Eventually(t, func() bool { return reconciler.called(exclusionID) }, time.Second, 10*time.Millisecond)
}

var _ risk.RiskExclusionReconciler = (*recordingRiskExclusionReconciler)(nil)
