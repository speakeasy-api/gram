package telemetry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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

	firstErr := logger.UpsertAIDetections(t.Context(), []AIDetection{detection})
	secondErr := logger.UpsertAIDetections(t.Context(), []AIDetection{detection})
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
	require.Error(t, logger.UpsertAIDetections(t.Context(), []AIDetection{invalidSignal}))

	invalidCategory := valid
	invalidCategory.Category = "other"
	require.Error(t, logger.UpsertAIDetections(t.Context(), []AIDetection{invalidCategory}))
}
