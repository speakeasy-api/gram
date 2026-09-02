package repo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpsertAIDetectionsRejectsZeroSeenAt(t *testing.T) {
	t.Parallel()
	arg := UpsertAIDetectionParams{
		OrganizationID: "org_test",
		TargetID:       "cursor",
		DeviceSerial:   "serial-1",
		UserEmail:      "member@example.com",
		Signal:         "installed",
		Category:       "harness",
		Version:        "",
		SeenAt:         time.Time{},
		UpdatedAt:      time.Now().UTC(),
	}

	err := New(nil).UpsertAIDetections(t.Context(), []UpsertAIDetectionParams{arg})
	require.ErrorContains(t, err, "seen at is required")
}

func TestUpsertAIDetectionsRejectsInvalidFiniteValues(t *testing.T) {
	t.Parallel()
	valid := UpsertAIDetectionParams{
		OrganizationID: "org_test",
		TargetID:       "cursor",
		DeviceSerial:   "serial-1",
		UserEmail:      "member@example.com",
		Signal:         "installed",
		Category:       "harness",
		Version:        "",
		SeenAt:         time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	invalidSignal := valid
	invalidSignal.Signal = "stopped"
	require.ErrorContains(t, New(nil).UpsertAIDetections(t.Context(), []UpsertAIDetectionParams{invalidSignal}), "invalid signal")

	invalidCategory := valid
	invalidCategory.Category = "other"
	require.ErrorContains(t, New(nil).UpsertAIDetections(t.Context(), []UpsertAIDetectionParams{invalidCategory}), "invalid category")
}
