package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/stretchr/testify/require"

	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	typesafe "github.com/speakeasy-api/gram/server/internal/thirdparty/typesafedecisions"
)

const validVerdictJSON = `{"directive_kind":"none","target":"none","operational":false,"rationale":"No directive."}`

// scriptedCompletion returns its responses in order and records the models it
// was asked for.
type scriptedCompletion struct {
	openrouter.CompletionClient
	responses []*openrouter.CompletionResponse
	models    []string
}

func (s *scriptedCompletion) GetCompletion(_ context.Context, req openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	s.models = append(s.models, req.Model)
	next := s.responses[0]
	s.responses = s.responses[1:]
	return next, nil
}

func refusal() *openrouter.CompletionResponse {
	return &openrouter.CompletionResponse{FinishReason: new(finishReasonContentFilter)}
}

func answer(content string) *openrouter.CompletionResponse {
	text := or.CreateChatAssistantMessageContentStr(content)
	msg := or.CreateChatMessagesAssistant(or.ChatAssistantMessage{Role: or.ChatAssistantMessageRoleAssistant, Content: optionalnullable.From(&text)})
	return &openrouter.CompletionResponse{Message: &msg, FinishReason: new("stop")}
}

func newObservedCompletion(client openrouter.CompletionClient, refusalFallback bool) (*observedCompletion, *bool) {
	refused := false
	return &observedCompletion{
		CompletionClient: client, observation: &decisionObservation{}, calls: new(0), refusals: new(0),
		fallbacks: new(0), refused: &refused, refusalFallback: refusalFallback,
	}, &refused
}

func TestObservedCompletionAsksAgainAfterRefusalAndMalformedVerdict(t *testing.T) {
	t.Parallel()

	client := &scriptedCompletion{responses: []*openrouter.CompletionResponse{refusal(), answer(`{"directive_kind":"none"}`), answer(validVerdictJSON)}}
	observed, refused := newObservedCompletion(client, true)
	result, err := observed.GetCompletion(t.Context(), openrouter.CompletionRequest{Model: piopenrouter.ConfirmationModel})
	require.NoError(t, err)
	require.True(t, hasVerdict(result))
	require.False(t, *refused)
	require.Equal(t, 3, *observed.calls)
	require.Equal(t, 1, *observed.refusals)
	require.Len(t, observed.observation.Calls, 3)
}

func TestObservedCompletionStopsAfterMaxVerdictAttempts(t *testing.T) {
	t.Parallel()

	client := &scriptedCompletion{responses: []*openrouter.CompletionResponse{refusal(), refusal(), refusal(), answer(validVerdictJSON)}}
	observed, refused := newObservedCompletion(client, true)
	result, err := observed.GetCompletion(t.Context(), openrouter.CompletionRequest{Model: piopenrouter.ConfirmationModel})
	require.NoError(t, err)
	require.True(t, isRefusal(result))
	require.True(t, *refused, "the report scores a confirmation refused three times as a refusal")
	require.Equal(t, maxVerdictAttempts, *observed.calls)
}

func TestObservedCompletionSkipsDisabledRefusalFallback(t *testing.T) {
	t.Parallel()

	client := &scriptedCompletion{responses: nil}
	observed, _ := newObservedCompletion(client, false)
	result, err := observed.GetCompletion(t.Context(), openrouter.CompletionRequest{Model: piopenrouter.RefusalFallbackModel})
	require.NoError(t, err)
	require.True(t, isRefusal(result))
	require.Empty(t, client.models, "a disabled fallback makes no provider call")
	require.Zero(t, *observed.calls)
}

func TestTransientCallFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "success", err: nil, want: false},
		{name: "openrouter throttled", err: fmt.Errorf("observe: %w", &openrouter.HTTPError{StatusCode: http.StatusTooManyRequests, Err: openrouter.ErrRateLimited}), want: true},
		{name: "openrouter server error", err: &openrouter.HTTPError{StatusCode: http.StatusBadGateway, Err: nil}, want: true},
		{name: "openrouter bad request", err: &openrouter.HTTPError{StatusCode: http.StatusBadRequest, Err: openrouter.ErrBadRequest}, want: false},
		{name: "openrouter out of credits", err: &openrouter.HTTPError{StatusCode: http.StatusPaymentRequired, Err: openrouter.ErrInsufficientCredits}, want: false},
		{name: "jev throttled", err: &typesafe.StatusError{StatusCode: http.StatusTooManyRequests}, want: true},
		{name: "jev server error", err: &typesafe.StatusError{StatusCode: http.StatusServiceUnavailable}, want: true},
		{name: "jev unauthorized", err: &typesafe.StatusError{StatusCode: http.StatusUnauthorized}, want: false},
		{name: "jev context overflow", err: typesafe.ErrContextLengthExceeded, want: false},
		{name: "deadline", err: fmt.Errorf("request typesafe evaluation: %w", context.DeadlineExceeded), want: true},
		{name: "transport", err: errors.New("connection reset by peer"), want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, transientCallFailure(callObservation{Err: tc.err}))
		})
	}
}

func TestCascadeCaseRetryReporting(t *testing.T) {
	t.Parallel()
	transient := &typesafe.StatusError{StatusCode: http.StatusTooManyRequests}
	permanent := &typesafe.StatusError{StatusCode: http.StatusUnauthorized}
	for _, tc := range []struct {
		name                   string
		failures               []error
		stopWait               bool
		wantAttempts           int
		wantLatency            time.Duration
		wantInitialUnavailable bool
		wantFinalUnavailable   bool
	}{
		{name: "immediate success", failures: []error{nil}, wantAttempts: 1, wantLatency: time.Second},
		{name: "recovers", failures: []error{transient, nil}, wantAttempts: 2, wantLatency: 7 * time.Second, wantInitialUnavailable: true},
		{name: "exhausts retries", failures: []error{transient, transient, transient, transient}, wantAttempts: 4, wantLatency: 39 * time.Second, wantInitialUnavailable: true, wantFinalUnavailable: true},
		{name: "permanent failure", failures: []error{permanent}, wantAttempts: 1, wantLatency: time.Second, wantInitialUnavailable: true, wantFinalUnavailable: true},
		{name: "canceled retry wait", failures: []error{transient}, stopWait: true, wantAttempts: 1, wantLatency: 3 * time.Second, wantInitialUnavailable: true, wantFinalUnavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clock := time.Unix(0, 0)
			observation := &decisionObservation{}
			attempts := 0
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			scan := func() (scanners.Result, promptinjection.Result, error) {
				require.Less(t, attempts, len(tc.failures))
				err := tc.failures[attempts]
				attempts++
				clock = clock.Add(time.Second)
				observation.Calls = append(observation.Calls, callObservation{Latency: time.Second, Err: err})
				verdict := promptinjection.Result{Label: promptinjection.LabelSafe, Completed: true}
				if err != nil {
					verdict.Label = promptinjection.LabelUnavailable
					verdict.Completed = false
				}
				return scanners.Result{}, verdict, err
			}
			wait := func(ctx context.Context, attempt int) bool {
				if tc.stopWait {
					clock = clock.Add(2 * time.Second)
					cancel()
					return ctx.Err() == nil
				}
				clock = clock.Add(caseRetryBaseDelay << (attempt - 1))
				return true
			}
			_, verdict, err, firstUnavailable := runCascadeCase(ctx, observation, scan, func() time.Time { return clock }, wait)
			require.Equal(t, tc.wantAttempts, attempts)
			require.Equal(t, tc.wantLatency, observation.Latency)
			require.Equal(t, tc.wantInitialUnavailable, firstUnavailable)
			require.Equal(t, tc.wantFinalUnavailable, err != nil || verdict.Label == promptinjection.LabelUnavailable)
			require.Len(t, observation.Calls, attempts, "every physical attempt remains in cost/error reporting")
			stats := summarizeEvaluation([]decisionObservation{*observation})
			require.InDelta(t, float64(tc.wantLatency.Milliseconds()), stats.DecisionLatencyP50MS, 0.001)
			require.Equal(t, attempts, stats.PhysicalCalls)
		})
	}
}

func TestBenchmarkAvailabilityReportLabels(t *testing.T) {
	t.Parallel()
	stats := evaluationStats{BenchmarkCases: 2, BenchmarkFirstAttemptUnavailable: 1, FailOpenEvents: 0, PhysicalCalls: 3, DecisionLatencyP50MS: 7000}
	raw, err := json.Marshal(stats)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"benchmark_cases":2`)
	require.Contains(t, string(raw), `"benchmark_first_attempt_unavailable":1`)
	require.Contains(t, string(raw), `"fail_open_events":0`)
	var output bytes.Buffer
	printSummary(&output, []modeSummary{{Evaluation: stats}})
	require.Contains(t, output.String(), "benchmark_first_attempt_unavailable=1 final_unavailable=0")
	require.Contains(t, output.String(), "benchmark attempts include confirmer retries")
	require.Contains(t, output.String(), "total_case_latency_ms[p50=7000")
	stats.BenchmarkFirstAttemptUnavailable = 0
	raw, err = json.Marshal(stats)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"benchmark_first_attempt_unavailable":0`)
	output.Reset()
	printSummary(&output, []modeSummary{{Evaluation: stats}})
	require.Contains(t, output.String(), "benchmark_first_attempt_unavailable=0 final_unavailable=0")
}
