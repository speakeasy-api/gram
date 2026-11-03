package assistants

import (
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestKitProductionTurnFailureRetiresUnusableRuntime(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		failure error
		retired bool
	}{
		{ErrRuntimeUnhealthy, true},
		{ErrHistoryCorrupted, true},
		{errKitMCPConfigurationUnverified, true},
		{ErrKitMCPDesiredSetUnsupported, true},
		{errKitMCPDiscoveryUnavailable, false},
	} {
		t.Run(tc.failure.Error(), func(t *testing.T) {
			t.Parallel()
			db, err := assistantsInfra.CloneTestDatabase(t, "kit_failed_turn_authority")
			require.NoError(t, err)
			project, assistant, _, thread := insertAssistantFixture(t, db)
			q := assistantrepo.New(db)
			require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: project, AssistantID: assistant, RuntimeVersion: 3}))
			var stops atomic.Int64
			logger := testenv.NewLogger(t)
			backend := testRuntimeBackend{backend: runtimeBackendGKE, runTurnErr: tc.failure, stopCalls: &stops}
			core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), db, nil, nil, backend, nil, assistanttokens.New("test-jwt-secret", db, nil, nil, nil), mustParseURLForServiceTest(t, "https://api.example"), telemetry.NewStub(logger), nil, newTestAuditLogger(), testIdentityService, newTestAuthzEngine(t, db))
			require.NoError(t, core.installKitRuntime(backend))
			admitted, err := core.AdmitPendingThreads(t.Context(), assistant)
			require.NoError(t, err)
			require.Equal(t, []uuid.UUID{thread}, admitted.ThreadIDs)
			before, err := q.GetAssistantRuntimeV2(t.Context(), assistantrepo.GetAssistantRuntimeV2Params{ProjectID: project, AssistantID: assistant})
			require.NoError(t, err)
			result, err := core.ProcessThreadEvents(t.Context(), project, thread)
			after, rerr := q.GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ProjectID: project, ID: before.ID})
			require.NoError(t, rerr)
			if tc.retired {
				require.NoError(t, err)
				require.True(t, result.RetryAdmission, "an unusable Kit runtime is replaced on readmission")
				require.Equal(t, runtimeStateFailed, after.State)
				require.True(t, after.Deleted)
				return
			}
			require.NoError(t, err)
			require.Zero(t, stops.Load())
			require.False(t, after.Deleted, "a transient failure keeps the runtime")
		})
	}
}
