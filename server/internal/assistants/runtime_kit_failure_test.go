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

func TestKitProductionTurnFailureDoesNotReplaceCanonicalAuthority(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{ErrRuntimeUnhealthy, ErrHistoryCorrupted} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			db, err := assistantsInfra.CloneTestDatabase(t, "kit_failed_turn_authority")
			require.NoError(t, err)
			project, assistant, _, thread := insertAssistantFixture(t, db)
			q := assistantrepo.New(db)
			require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: project, AssistantID: assistant, RuntimeVersion: 3}))
			var stops atomic.Int64
			logger := testenv.NewLogger(t)
			backend := testRuntimeBackend{backend: runtimeBackendGKE, runTurnErr: failure, stopCalls: &stops}
			core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), db, nil, nil, backend, nil, assistanttokens.New("test-jwt-secret", db, nil, nil, nil), mustParseURLForServiceTest(t, "https://api.example"), telemetry.NewStub(logger), nil, newTestAuditLogger(), testIdentityService, newTestAuthzEngine(t, db))
			require.NoError(t, core.installKitRuntime(backend))
			admitted, err := core.AdmitPendingThreads(t.Context(), assistant)
			require.NoError(t, err)
			require.Equal(t, []uuid.UUID{thread}, admitted.ThreadIDs)
			before, err := q.GetAssistantRuntimeV2(t.Context(), assistantrepo.GetAssistantRuntimeV2Params{ProjectID: project, AssistantID: assistant})
			require.NoError(t, err)
			_, err = core.ProcessThreadEvents(t.Context(), project, thread)
			require.ErrorIs(t, err, failure)
			require.Zero(t, stops.Load())
			after, err := q.GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ProjectID: project, ID: before.ID})
			require.NoError(t, err)
			require.False(t, after.Ended)
			require.False(t, after.Deleted)
		})
	}
}
