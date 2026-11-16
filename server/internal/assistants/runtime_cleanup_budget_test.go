package assistants

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestReapStoppedAssistantRuntimesSharesRetirementBudget(t *testing.T) {
	t.Parallel()
	for _, batchSize := range []int32{1, 2} {
		for _, failed := range []bool{false, true} {
			t.Run(fmt.Sprintf("batch_%d_failed_%t", batchSize, failed), func(t *testing.T) {
				t.Parallel()
				db, err := assistantsInfra.CloneTestDatabase(t, "cleanup_budget")
				require.NoError(t, err)
				old := time.Now().UTC().Add(-48 * time.Hour)
				projectID, assistantID, threadID := insertReapableProject(t, db, "retirement-budget")
				insertReapableRuntimeRow(t, db, projectID, assistantID, threadID, "switch_retiring", "retiring-machine", old)
				for i := range 2 {
					projectID, assistantID, threadID := insertReapableProject(t, db, fmt.Sprintf("stopped-budget-%d", i))
					insertReapableRuntimeRow(t, db, projectID, assistantID, threadID, runtimeStateStopped, fmt.Sprintf("stopped-machine-%d", i), old)
				}
				stops, reaps := &atomic.Int64{}, &atomic.Int64{}
				backend := testRuntimeBackend{backend: runtimeBackendGKE, stopCalls: stops, reapMachineCalls: reaps}
				if failed {
					backend.stopErr = errors.New("retirement stop failed")
				}
				core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), db, nil, nil, backend, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger(), testIdentityService, newTestAuthzEngine(t, db))
				result, err := core.ReapStoppedAssistantRuntimes(t.Context(), ReapStoppedAssistantRuntimesParams{StoppedTTL: 24 * time.Hour, BatchSize: batchSize})
				require.NoError(t, err)
				require.EqualValues(t, 1, stops.Load())
				require.EqualValues(t, batchSize-1, reaps.Load())
				require.EqualValues(t, batchSize, stops.Load()+reaps.Load())
				require.EqualValues(t, batchSize-1, result.Reaped)
				if failed {
					require.Equal(t, 1, result.Errors)
				} else {
					require.Zero(t, result.Errors)
				}
			})
		}
	}
}
