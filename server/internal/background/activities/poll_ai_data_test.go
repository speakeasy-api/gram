package activities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
	codexapi "github.com/speakeasy-api/gram/server/internal/thirdparty/codex"
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
	require.True(t, pollRejectedByProvider(&codexapi.HTTPError{StatusCode: 401, Status: "401 Unauthorized"}))
	require.True(t, pollRejectedByProvider(&codexapi.HTTPError{StatusCode: 403, Status: "403 Forbidden"}))
	require.True(t, pollRejectedByProvider(&codexapi.HTTPError{StatusCode: 404, Status: "404 Not Found"}))
	require.False(t, pollRejectedByProvider(&codexapi.HTTPError{StatusCode: 503, Status: "503 Service Unavailable"}))
	require.False(t, pollRejectedByProvider(errors.New("network timeout")))
}

func TestPollProviderUnavailableMatchesOutages(t *testing.T) {
	t.Parallel()

	// A spent HTTP retry budget is the canonical outage signal, including
	// when the transport wrapped it in a *url.Error and a sync stage wrapped
	// that again.
	exhausted := &guardian.RetriesExhaustedError{
		Method:     "GET",
		URL:        "https://api.chatgpt.com/v1/compliance/organizations/org-x/logs",
		Attempts:   5,
		StatusCode: 500,
		Body:       `{"error":"boom"}`,
		Err:        nil,
	}
	require.True(t, pollProviderUnavailable(exhausted))
	wrapped := fmt.Errorf("sync codex cost data: %w", &aiintegrations.SyncError{
		Op: "sync codex costs",
		Stages: []aiintegrations.SyncStageError{{
			Stage: "import_cost_logs",
			Err:   fmt.Errorf("fetch codex_compliance upper bound: %w", &url.Error{Op: "Get", URL: "https://api.chatgpt.com", Err: exhausted}),
		}},
		Progress: nil,
	})
	require.True(t, pollProviderUnavailable(wrapped))

	// Direct throttling/server statuses count too; rejections and unknown
	// errors do not.
	require.True(t, pollProviderUnavailable(&codexapi.HTTPError{StatusCode: 503, Status: "503 Service Unavailable"}))
	require.True(t, pollProviderUnavailable(&anthropicapi.HTTPError{StatusCode: 429, Status: "429 Too Many Requests"}))
	require.True(t, pollProviderUnavailable(&cursorapi.HTTPError{StatusCode: 500, Status: "500 Internal Server Error"}))
	require.False(t, pollProviderUnavailable(&codexapi.HTTPError{StatusCode: 401, Status: "401 Unauthorized"}))
	require.False(t, pollProviderUnavailable(&anthropicapi.HTTPError{StatusCode: 404, Status: "404 Not Found"}))
	require.False(t, pollProviderUnavailable(errors.New("json decode failed")))
}

func TestNewPollFailureErrorMarksProviderOutagesNonRetryable(t *testing.T) {
	t.Parallel()

	configID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	cause := fmt.Errorf("sync codex cost data: %w", &guardian.RetriesExhaustedError{
		Method:     "GET",
		URL:        "https://api.chatgpt.com/v1/compliance/organizations/org-x/logs",
		Attempts:   5,
		StatusCode: 500,
		Body:       `{"error":"boom"}`,
		Err:        nil,
	})

	err := newPollFailureError(configID, aiintegrations.ProviderCodexCompliance, 1, false, true, cause)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.True(t, appErr.NonRetryable())
	require.Contains(t, appErr.Message(), "last status 500")

	var details aiUsagePollFailureDetails
	require.NoError(t, appErr.Details(&details))
	require.True(t, details.NonRetryable)
	require.True(t, details.ProviderUnavailable)
}

func TestShareablePollErrorClassifiesProviderOutage(t *testing.T) {
	t.Parallel()

	cause := fmt.Errorf("sync codex cost data: %w", &guardian.RetriesExhaustedError{
		Method:     "GET",
		URL:        "https://api.chatgpt.com/v1/compliance/organizations/org-x/logs",
		Attempts:   5,
		StatusCode: 500,
		Body:       `{"error":"internal secret detail"}`,
		Err:        nil,
	})

	err := shareablePollError(aiintegrations.ScheduleCodexCompliance, cause)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
	require.Contains(t, err.Error(), "temporarily unavailable (HTTP 500)")
	require.NotContains(t, err.Error(), "internal secret detail")

	// Exhaustion without a response reports unreachability instead.
	noResponse := &guardian.RetriesExhaustedError{
		Method:     "",
		URL:        "",
		Attempts:   5,
		StatusCode: 0,
		Body:       "",
		Err:        errors.New("dial tcp: connection refused"),
	}
	err = shareablePollError(aiintegrations.ScheduleCodexCompliance, noResponse)
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
	require.Contains(t, err.Error(), "unreachable")
	require.NotContains(t, err.Error(), "dial tcp")
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
	wrapped := fmt.Errorf("sync anthropic compliance data: %w", syncErr)

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
	cause := fmt.Errorf("sync anthropic compliance data: %w", syncErr)

	err := newPollFailureError(configID, aiintegrations.ProviderAnthropicCompliance, 5, false, false, cause)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, ErrTypeAIUsagePollFailed, appErr.Type())
	require.False(t, appErr.NonRetryable())
	require.Contains(t, appErr.Message(), "provider=anthropic_compliance")
	require.Contains(t, appErr.Message(), fmt.Sprintf("attempt=5/%d", PollUsageMaxAttempts))

	require.Contains(t, appErr.Message(), "[discover_activities] list anthropic compliance activities: 503 Service Unavailable")
	require.Contains(t, appErr.Message(), "(progress:")

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

func TestShareablePollErrorPreservesInteriorShareableBoundaries(t *testing.T) {
	t.Parallel()

	// A schedule/provider mismatch keeps its invalid-configuration code and
	// message instead of the generic schedule fallback.
	mismatch := oops.E(oops.CodeInvalid, nil, "cursor schedule cannot run for provider %s", aiintegrations.ProviderAnthropicCompliance)
	err := shareablePollError(aiintegrations.ScheduleCursor, mismatch)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeInvalid, shareable.Code)
	require.Contains(t, err.Error(), "cursor schedule cannot run for provider")

	// The telemetry insert boundary's safe message is persisted; the raw
	// ClickHouse cause behind it is not.
	insertErr := fmt.Errorf("sync codex cost data: %w",
		oops.E(oops.CodeUnexpected, errors.New("clickhouse: connection refused"), "insert codex cost telemetry logs"))
	err = shareablePollError(aiintegrations.ScheduleCodexCompliance, insertErr)
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnexpected, shareable.Code)
	require.Equal(t, "insert codex cost telemetry logs", err.Error())
	require.NotContains(t, err.Error(), "clickhouse")
}

func TestNewPollFailureErrorKeepsStageContextAroundShareableStageErrors(t *testing.T) {
	t.Parallel()

	syncErr := &aiintegrations.SyncError{
		Op: "sync codex costs",
		Stages: []aiintegrations.SyncStageError{{
			Stage: "import_cost_logs",
			Err:   oops.E(oops.CodeUnexpected, errors.New("connection refused"), "insert codex cost telemetry logs"),
		}},
		Progress: aiintegrations.CodexCostSyncProgress{
			WindowStart:       time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
			LogPages:          2,
			LogFiles:          3,
			CostEvents:        40,
			CostEventsWritten: 0,
			CostEventsDeduped: 0,
			WatermarkReached:  time.Date(2026, 7, 16, 6, 0, 0, 0, time.UTC),
		},
	}
	cause := fmt.Errorf("sync codex cost data: %w", syncErr)

	err := newPollFailureError(
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		aiintegrations.ProviderCodexCompliance,
		5,
		false,
		false,
		cause,
	)

	// The stage label and progress summary survive verbatim, and the
	// shareable stage error's hidden cause arrives in the trailer.
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Contains(t, appErr.Message(), "[import_cost_logs] insert codex cost telemetry logs")
	require.Contains(t, appErr.Message(), "(progress:")
	require.Contains(t, appErr.Message(), "insert codex cost telemetry logs: connection refused")
}

func TestNewPollFailureErrorExpandsShareableCauses(t *testing.T) {
	t.Parallel()

	cause := fmt.Errorf(
		"sync codex cost data: %w",
		oops.E(oops.CodeUnexpected, errors.New("download failed"), "import cost logs"),
	)

	err := newPollFailureError(
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		aiintegrations.ProviderCodexCompliance,
		5,
		false,
		false,
		cause,
	)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Contains(t, appErr.Message(), "sync codex cost data: import cost logs")
	require.Contains(t, appErr.Message(), "import cost logs: download failed")
}

func TestNewPollFailureErrorMarksAuthFailuresNonRetryable(t *testing.T) {
	t.Parallel()

	configID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	cause := fmt.Errorf("fetch cursor usage window: %w", &cursorapi.HTTPError{StatusCode: 401, Status: "401 Unauthorized"})

	err := newPollFailureError(configID, aiintegrations.ProviderCursor, 1, true, false, cause)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.True(t, appErr.NonRetryable())

	var details aiUsagePollFailureDetails
	require.NoError(t, appErr.Details(&details))
	require.True(t, details.NonRetryable)
	require.Empty(t, details.Stages)
	require.Nil(t, details.Progress)
}

func TestCustomerPollErrorClassifiesCodexContentFailuresWithoutPayload(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"event_id":"event_1","payload":{"hour":"invalid","identity":{"email":"user@example.com"}}}`)
	testCases := []struct {
		kind        aiintegrations.CodexCostContentErrorKind
		want        string
		withPayload bool
	}{
		{kind: aiintegrations.CodexCostContentSHA256Mismatch, want: "failed sha256 verification", withPayload: false},
		{kind: aiintegrations.CodexCostContentInvalidJSON, want: "decode codex compliance cost log", withPayload: true},
		{kind: aiintegrations.CodexCostContentMissingEventID, want: "missing event_id", withPayload: true},
		{kind: aiintegrations.CodexCostContentInvalidTimestamp, want: "parse codex compliance cost timestamp", withPayload: true},
	}

	for _, testCase := range testCases {
		contentPayload := payload
		if !testCase.withPayload {
			contentPayload = nil
		}
		internalErr := fmt.Errorf("sync codex cost data: %w", &aiintegrations.CodexCostContentError{
			Kind:    testCase.kind,
			LogID:   "eclf_bad",
			Payload: contentPayload,
			Cause:   errors.New("private parser context"),
		})

		shareableErr := shareablePollError(aiintegrations.ScheduleCodexCompliance, internalErr)

		var shareable *oops.ShareableError
		require.ErrorAs(t, shareableErr, &shareable, testCase.kind)
		require.Equal(t, oops.CodeUnexpected, shareable.Code, testCase.kind)
		require.Contains(t, shareableErr.Error(), "eclf_bad", testCase.kind)
		require.Contains(t, shareableErr.Error(), testCase.want, testCase.kind)
		require.NotContains(t, shareableErr.Error(), "user@example.com", testCase.kind)
		require.NotContains(t, shareableErr.Error(), "private parser context", testCase.kind)

		temporalErr := newPollFailureError(
			uuid.MustParse("33333333-3333-3333-3333-333333333333"),
			aiintegrations.ProviderCodexCompliance,
			5,
			false,
			false,
			internalErr,
		)
		var appErr *temporal.ApplicationError
		require.ErrorAs(t, temporalErr, &appErr, testCase.kind)
		if testCase.withPayload {
			require.Contains(t, appErr.Message(), "user@example.com", testCase.kind)
			require.Contains(t, appErr.Message(), `\"hour\":\"invalid\"`, testCase.kind)
		} else {
			require.NotContains(t, appErr.Message(), "user@example.com", testCase.kind)
			require.NotContains(t, appErr.Message(), "payload=", testCase.kind)
		}
		require.Contains(t, appErr.Message(), "private parser context", testCase.kind)
	}
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
