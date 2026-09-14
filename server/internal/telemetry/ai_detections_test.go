package telemetry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestUpsertAIDetectionsRejectsZeroSeenAtDeterministically(t *testing.T) {
	t.Parallel()
	logger := NewStub(testenv.NewLogger(t))
	detection := AIDetection{
		OrganizationID: "org_test",
		TargetID:       "cursor",
		DeviceSerial:   "serial-1",
		UserEmail:      "member@example.com",
		Signal:         "installed",
		Category:       "harness",
		Version:        "",
		SeenAt:         time.Time{},
	}

	_, firstErr := logger.UpsertAIDetections(t.Context(), []AIDetection{detection})
	_, secondErr := logger.UpsertAIDetections(t.Context(), []AIDetection{detection})
	require.Error(t, firstErr)
	require.Error(t, secondErr)
	require.EqualError(t, firstErr, secondErr.Error())

	var shareableErr *oops.ShareableError
	require.ErrorAs(t, firstErr, &shareableErr)
	require.Equal(t, oops.CodeUnexpected, shareableErr.Code)
}

func TestUpsertAIDetectionsRejectsInvalidSignalAndCategory(t *testing.T) {
	t.Parallel()
	logger := NewStub(testenv.NewLogger(t))
	valid := AIDetection{
		OrganizationID: "org_test",
		TargetID:       "cursor",
		DeviceSerial:   "serial-1",
		UserEmail:      "member@example.com",
		Signal:         "installed",
		Category:       "harness",
		Version:        "",
		SeenAt:         time.Now().UTC(),
	}

	invalidSignal := valid
	invalidSignal.Signal = "stopped"
	_, signalErr := logger.UpsertAIDetections(t.Context(), []AIDetection{invalidSignal})
	require.Error(t, signalErr)

	invalidCategory := valid
	invalidCategory.Category = "other"
	_, categoryErr := logger.UpsertAIDetections(t.Context(), []AIDetection{invalidCategory})
	require.Error(t, categoryErr)
}

// TestUpsertAIDetectionsAcceptsEveryKnownCategory pins this write path's
// category vocabulary to aitargets.KnownCategories, the list the scan-report
// ingest validates against and the catalog overwrites a reported category
// with. They drifted once: `assistant` reached the registry and the API
// contract while both detection validators still accepted only harness and
// local_model, so every scan naming an assistant-category target failed the
// whole batch — taking the harness rows reported alongside it down too.
func TestUpsertAIDetectionsAcceptsEveryKnownCategory(t *testing.T) {
	t.Parallel()
	logger := NewStub(testenv.NewLogger(t))
	for _, category := range aitargets.KnownCategories() {
		require.Containsf(t, aiDetectionCategories, string(category),
			"category %q is in the ingest contract but rejected here", category)

		detection := AIDetection{
			OrganizationID: "org_test",
			TargetID:       "cursor",
			DeviceSerial:   "serial-1",
			UserEmail:      "member@example.com",
			Signal:         "installed",
			Category:       string(category),
			Version:        "",
			SeenAt:         time.Now().UTC(),
		}
		_, err := logger.UpsertAIDetections(t.Context(), []AIDetection{detection})
		require.NoErrorf(t, err, "category %q must survive validation", category)
	}
}
