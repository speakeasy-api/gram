package aiintegrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
)

func TestNewSyncErrorAccumulatesAllStageFailures(t *testing.T) {
	t.Parallel()

	discoverErr := errors.New("list activities: 503 Service Unavailable")
	writeErr := errors.New("write chat messages: connection reset")

	err := newSyncError("sync anthropic compliance", ComplianceSyncProgress{
		FirstSync:           false,
		ActivityPages:       4,
		ChatActivities:      312,
		ChatsImported:       57,
		MessagePagesFetched: 210,
		MessagePagesWritten: 208,
		CursorReached:       "activity_9",
		CursorPersisted:     "activity_5",
	},
		SyncStageError{Stage: "discover_activities", Err: discoverErr},
		SyncStageError{Stage: "import_chats", Err: nil},
		SyncStageError{Stage: "write_messages", Err: writeErr},
	)

	var syncErr *SyncError
	require.ErrorAs(t, err, &syncErr)
	require.Len(t, syncErr.Stages, 2)
	require.Equal(t, "discover_activities", syncErr.Stages[0].Stage)
	require.Equal(t, "write_messages", syncErr.Stages[1].Stage)

	msg := err.Error()
	require.Contains(t, msg, "[discover_activities] list activities: 503 Service Unavailable")
	require.Contains(t, msg, "[write_messages] write chat messages: connection reset")
	require.Contains(t, msg, "activity_pages=4")
	require.Contains(t, msg, "chats_imported=57")
	require.Contains(t, msg, `cursor_reached="activity_9"`)
	require.Contains(t, msg, `cursor_persisted="activity_5"`)

	require.ErrorIs(t, err, discoverErr)
	require.ErrorIs(t, err, writeErr)
}

func TestNewSyncErrorDropsCancellationNoiseWhenRealErrorExists(t *testing.T) {
	t.Parallel()

	realErr := errors.New("fetch usage events page: boom")

	err := newSyncError("sync cursor usage", CursorUsageSyncProgress{
		WindowStart: time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC),
		WindowEnd:   time.Date(2026, 7, 9, 11, 0, 0, 0, time.UTC),
		UsagePages:  2,
		UsageEvents: 1500,
	},
		SyncStageError{Stage: "fetch_usage_events", Err: realErr},
		SyncStageError{Stage: "write_telemetry", Err: fmt.Errorf("insert logs: %w", context.Canceled)},
	)

	var syncErr *SyncError
	require.ErrorAs(t, err, &syncErr)
	require.Len(t, syncErr.Stages, 1)
	require.Equal(t, "fetch_usage_events", syncErr.Stages[0].Stage)
	require.NotContains(t, err.Error(), "write_telemetry")
}

func TestNewSyncErrorKeepsCancellationWhenItIsTheOnlyError(t *testing.T) {
	t.Parallel()

	err := newSyncError("sync cursor usage", CursorUsageSyncProgress{
		WindowStart: time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC),
		WindowEnd:   time.Date(2026, 7, 9, 11, 0, 0, 0, time.UTC),
		UsagePages:  0,
		UsageEvents: 0,
	},
		SyncStageError{Stage: "fetch_usage_events", Err: context.Canceled},
		SyncStageError{Stage: "write_telemetry", Err: nil},
	)

	var syncErr *SyncError
	require.ErrorAs(t, err, &syncErr)
	require.Len(t, syncErr.Stages, 1)
	require.ErrorIs(t, err, context.Canceled)
}

func TestSyncErrorExposesTypedCausesThroughUnwrap(t *testing.T) {
	t.Parallel()

	httpErr := &anthropicapi.HTTPError{StatusCode: 401, Status: "401 Unauthorized"}

	err := newSyncError("sync anthropic compliance", ComplianceSyncProgress{
		FirstSync:           true,
		ActivityPages:       0,
		ChatActivities:      0,
		ChatsImported:       0,
		MessagePagesFetched: 0,
		MessagePagesWritten: 0,
		CursorReached:       "",
		CursorPersisted:     "",
	},
		SyncStageError{Stage: "discover_activities", Err: fmt.Errorf("list anthropic compliance activities: %w", httpErr)},
	)

	var unwrapped *anthropicapi.HTTPError
	require.ErrorAs(t, err, &unwrapped)
	require.Equal(t, 401, unwrapped.StatusCode)
}

func TestComplianceSyncProgressMarshalsToJSON(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(ComplianceSyncProgress{
		FirstSync:           true,
		ActivityPages:       1,
		ChatActivities:      2,
		ChatsImported:       3,
		MessagePagesFetched: 4,
		MessagePagesWritten: 5,
		CursorReached:       "activity_1",
		CursorPersisted:     "activity_0",
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
		"first_sync": true,
		"activity_pages": 1,
		"chat_activities": 2,
		"chats_imported": 3,
		"message_pages_fetched": 4,
		"message_pages_written": 5,
		"cursor_reached": "activity_1",
		"cursor_persisted": "activity_0"
	}`, string(raw))
}
