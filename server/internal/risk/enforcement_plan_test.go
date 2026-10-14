package risk_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const planTestSecret = "export GITHUB_TOKEN=ghp_R2D2C3POLuk3Skywalker1234567890ab"

func TestScanner_InferenceNeverEvaluatesFlagPolicies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	policyID := uuid.New()
	_, err := riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:             policyID,
		ProjectID:      *auth.ProjectID,
		OrganizationID: auth.ActiveOrganizationID,
		Name:           "flag secrets and injections",
		Sources:        []string{"gitleaks", "prompt_injection"},
		Enabled:        true,
		Action:         "flag",
		AudienceType:   "everyone",
	})
	require.NoError(t, err)
	grantRiskPolicyToAllUsers(t, ti, ctx, auth.ActiveOrganizationID, policyID)

	var classifications atomic.Int32
	classifier := promptinjection.Classifier(func(context.Context, promptinjection.Request) ([]promptinjection.Result, error) {
		classifications.Add(1)
		return []promptinjection.Result{{Label: promptinjection.LabelInjection, Completed: true}}, nil
	})
	scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, promptinjection.NewScanner(testenv.NewLogger(t), classifier), nil, &feature.InMemory{}, testCELEngine(t), metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))
	require.NoError(t, err)

	outcome, err := scanner.ScanForInferenceEnforcement(risk.WithEnforcementPlanMemo(ctx), realtimeScanRequest(auth.ActiveOrganizationID, *auth.ProjectID, auth.UserID, planTestSecret, message.User, ""))
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Nil(t, outcome.Result, "flag policies are left to background analysis")
	require.True(t, outcome.Complete)
	require.Zero(t, classifications.Load(), "no flag policy source runs before the verdict")
}

func TestScanner_EnforcementPlanMemoSnapshotsPoliciesForTheRequest(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	scanner := newScannerWithDispatcher(t, ti, nil, &feature.InMemory{}, nil)
	req := realtimeScanRequest(auth.ActiveOrganizationID, *auth.ProjectID, auth.UserID, planTestSecret, message.User, "")

	requestCtx := risk.WithEnforcementPlanMemo(ctx)
	outcome, err := scanner.ScanForInferenceEnforcement(requestCtx, req)
	require.NoError(t, err)
	require.Nil(t, outcome.Result)
	require.True(t, outcome.Complete)

	insertRealtimeBlockPolicy(t, ti, ctx, "block secrets", []string{"gitleaks"}, nil)

	outcome, err = scanner.ScanForInferenceEnforcement(requestCtx, req)
	require.NoError(t, err)
	require.Nil(t, outcome.Result, "scans of one request share the plan resolved by its first scan")

	outcome, err = scanner.ScanForInferenceEnforcement(risk.WithEnforcementPlanMemo(ctx), req)
	require.NoError(t, err)
	require.NotNil(t, outcome.Result, "the next request resolves the current policies")
	require.Equal(t, "block", outcome.Result.Action)
	require.Equal(t, "gitleaks", outcome.Result.Source)

	result, err := scanner.ScanForEnforcement(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result, "a scan without a memo resolves its own plan")
}

func TestScanner_EnforcementPlanMemoConcurrentScans(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	insertRealtimeBlockPolicy(t, ti, ctx, "block secrets", []string{"gitleaks"}, nil)
	scanner := newScannerWithDispatcher(t, ti, nil, &feature.InMemory{}, nil)

	requestCtx := risk.WithEnforcementPlanMemo(ctx)
	const scans = 16
	outcomes := make([]*risk.InferenceScanOutcome, scans)
	errs := make([]error, scans)
	var wg sync.WaitGroup
	for i := range scans {
		text := "ordinary content"
		if i%2 == 0 {
			text = planTestSecret
		}
		wg.Go(func() {
			outcomes[i], errs[i] = scanner.ScanForInferenceEnforcement(requestCtx, realtimeScanRequest(auth.ActiveOrganizationID, *auth.ProjectID, auth.UserID, text, message.User, ""))
		})
	}
	wg.Wait()

	for i := range scans {
		require.NoError(t, errs[i])
		require.NotNil(t, outcomes[i])
		require.True(t, outcomes[i].Complete)
		if i%2 == 0 {
			require.NotNil(t, outcomes[i].Result)
			require.Equal(t, "gitleaks", outcomes[i].Result.Source)
		} else {
			require.Nil(t, outcomes[i].Result)
		}
	}
}

func TestScanner_EnforcementPlanMemoKeysByPrincipal(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	policyID := uuid.New()
	_, err := riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:             policyID,
		ProjectID:      *auth.ProjectID,
		OrganizationID: auth.ActiveOrganizationID,
		Name:           "targeted secrets",
		Sources:        []string{"gitleaks"},
		Enabled:        true,
		Action:         "block",
		AudienceType:   "targeted",
	})
	require.NoError(t, err)
	scanner := newScannerWithDispatcher(t, ti, nil, &feature.InMemory{}, nil)

	requestCtx := risk.WithEnforcementPlanMemo(ctx)
	outcome, err := scanner.ScanForInferenceEnforcement(requestCtx, realtimeScanRequest(auth.ActiveOrganizationID, *auth.ProjectID, auth.UserID, planTestSecret, message.User, ""))
	require.NoError(t, err)
	require.Nil(t, outcome.Result, "a targeted policy without a grant does not apply")

	grantRiskPolicyToAllUsers(t, ti, ctx, auth.ActiveOrganizationID, policyID)
	outcome, err = scanner.ScanForInferenceEnforcement(requestCtx, realtimeScanRequest(auth.ActiveOrganizationID, *auth.ProjectID, "", planTestSecret, message.User, ""))
	require.NoError(t, err)
	require.NotNil(t, outcome.Result, "another principal resolves its own plan")
}
