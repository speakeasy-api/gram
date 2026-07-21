package activities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/oops"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
	cursorapi "github.com/speakeasy-api/gram/server/internal/thirdparty/cursor"
)

func TestPollAIDataInputRoundTripsThroughTemporal(t *testing.T) {
	t.Parallel()

	syncID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	input := syncID.String()
	echo := func(_ context.Context, decoded string) (string, error) {
		return decoded, nil
	}

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivityWithOptions(echo, activity.RegisterOptions{Name: "EchoPollAIDataInput"})

	value, err := env.ExecuteActivity("EchoPollAIDataInput", input)
	require.NoError(t, err)

	var actual string
	require.NoError(t, value.Get(&actual))
	require.Equal(t, syncID.String(), actual)
}

func TestPollRejectedByProviderMatchesPermanentProviderFailures(t *testing.T) {
	t.Parallel()

	require.True(t, pollRejectedByProvider(&cursorapi.HTTPError{StatusCode: 401, Status: "401 Unauthorized"}))
	require.False(t, pollRejectedByProvider(&cursorapi.HTTPError{StatusCode: 404, Status: "404 Not Found"}))
	require.False(t, pollRejectedByProvider(&cursorapi.HTTPError{StatusCode: 503, Status: "503 Service Unavailable"}))
	require.True(t, pollRejectedByProvider(&anthropicapi.HTTPError{StatusCode: 401, Status: "401 Unauthorized"}))
	require.True(t, pollRejectedByProvider(&anthropicapi.HTTPError{StatusCode: 403, Status: "403 Forbidden"}))
	require.True(t, pollRejectedByProvider(&anthropicapi.HTTPError{StatusCode: 404, Status: "404 Not Found"}))
	require.False(t, pollRejectedByProvider(&anthropicapi.HTTPError{StatusCode: 503, Status: "503 Service Unavailable"}))
	require.False(t, pollRejectedByProvider(errors.New("network timeout")))
}

func TestPollRejectedByProviderSeesThroughWrappedSyncErrors(t *testing.T) {
	t.Parallel()

	httpErr := &anthropicapi.HTTPError{StatusCode: 401, Status: "401 Unauthorized"}
	syncErr := &aiintegrations.SyncError{
		Op: "sync anthropic compliance",
		Stages: []aiintegrations.SyncStageError{{
			Stage: "discover_activities",
			Err:   fmt.Errorf("list anthropic compliance activities: %w", httpErr),
		}},
		Progress: aiintegrations.ComplianceSyncProgress{
			FirstSync:           true,
			ActivityPages:       0,
			ChatActivities:      0,
			ChatsImported:       0,
			MessagePagesFetched: 0,
			MessagePagesWritten: 0,
			CursorReached:       "",
			CursorPersisted:     "",
		},
	}
	wrapped := oops.E(oops.CodeUnauthorized, syncErr, "anthropic compliance rejected the configured api key")

	require.True(t, pollRejectedByProvider(wrapped))
}

func TestNewPollFailureErrorCarriesStageAndProgressDetails(t *testing.T) {
	t.Parallel()

	configID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	discoverErr := errors.New("list anthropic compliance activities: 503 Service Unavailable")
	syncErr := &aiintegrations.SyncError{
		Op: "sync anthropic compliance",
		Stages: []aiintegrations.SyncStageError{{
			Stage: "discover_activities",
			Err:   discoverErr,
		}},
		Progress: aiintegrations.ComplianceSyncProgress{
			FirstSync:           false,
			ActivityPages:       4,
			ChatActivities:      312,
			ChatsImported:       57,
			MessagePagesFetched: 210,
			MessagePagesWritten: 208,
			CursorReached:       "activity_9",
			CursorPersisted:     "activity_5",
		},
	}
	cause := oops.E(oops.CodeUnexpected, syncErr, "sync anthropic compliance data")

	err := newPollFailureError(configID, aiintegrations.ProviderAnthropicCompliance, 5, false, cause)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, ErrTypeAIUsagePollFailed, appErr.Type())
	require.False(t, appErr.NonRetryable())
	require.Contains(t, appErr.Message(), "provider=anthropic_compliance")
	require.Contains(t, appErr.Message(), fmt.Sprintf("attempt=5/%d", PollUsageMaxAttempts))

	require.True(t, appErr.HasDetails())
	var details aiUsagePollFailureDetails
	require.NoError(t, appErr.Details(&details))
	require.Equal(t, configID.String(), details.ConfigID)
	require.Equal(t, aiintegrations.ProviderAnthropicCompliance, details.Provider)
	require.Equal(t, int32(5), details.Attempt)
	require.Len(t, details.Stages, 1)
	require.Equal(t, "discover_activities", details.Stages[0].Stage)
	require.Contains(t, details.Stages[0].Error, "503 Service Unavailable")

	// The original causes stay reachable for errors.Is/errors.As callers.
	require.ErrorIs(t, err, discoverErr)
}

func TestNewPollFailureErrorMarksAuthFailuresNonRetryable(t *testing.T) {
	t.Parallel()

	configID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	cause := oops.E(oops.CodeUnauthorized, &cursorapi.HTTPError{StatusCode: 401, Status: "401 Unauthorized"}, "cursor rejected the configured api key")

	err := newPollFailureError(configID, aiintegrations.ProviderCursor, 1, true, cause)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.True(t, appErr.NonRetryable())

	var details aiUsagePollFailureDetails
	require.NoError(t, appErr.Details(&details))
	require.True(t, details.NonRetryable)
	require.Empty(t, details.Stages)
	require.Nil(t, details.Progress)
}

func TestAIUsagePollFailureDetailsMarshalToJSON(t *testing.T) {
	t.Parallel()

	// Details cross the Temporal payload boundary as JSON; make sure the
	// progress interface field serializes into an inspectable object.
	raw, err := json.Marshal(aiUsagePollFailureDetails{
		ConfigID:     "33333333-3333-3333-3333-333333333333",
		Provider:     aiintegrations.ProviderCursor,
		Attempt:      3,
		MaxAttempts:  PollUsageMaxAttempts,
		NonRetryable: false,
		Stages:       []stageFailureDetail{{Stage: "fetch_usage_events", Error: "boom"}},
		Progress: aiintegrations.CursorUsageSyncProgress{
			WindowStart: time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC),
			WindowEnd:   time.Date(2026, 7, 9, 11, 0, 0, 0, time.UTC),
			UsagePages:  2,
			UsageEvents: 1500,
		},
	})
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	progress, ok := decoded["progress"].(map[string]any)
	require.True(t, ok)
	require.InDelta(t, 1500, progress["usage_events"], 0)
	stages, ok := decoded["stages"].([]any)
	require.True(t, ok)
	require.Len(t, stages, 1)
}
