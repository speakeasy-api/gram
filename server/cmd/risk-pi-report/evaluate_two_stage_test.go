package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
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

func newObservedCompletion(client openrouter.CompletionClient) (*observedCompletion, *bool) {
	refused := false
	return &observedCompletion{CompletionClient: client, observation: &decisionObservation{}, refused: &refused}, &refused
}

func TestObservedCompletionAsksAgainAfterRefusalAndMalformedVerdict(t *testing.T) {
	t.Parallel()

	client := &scriptedCompletion{responses: []*openrouter.CompletionResponse{refusal(), answer(`{"directive_kind":"none"}`), answer(validVerdictJSON)}}
	observed, refused := newObservedCompletion(client)
	result, err := observed.GetCompletion(t.Context(), openrouter.CompletionRequest{Model: piopenrouter.ConfirmationModel})
	require.NoError(t, err)
	require.True(t, hasVerdict(result))
	require.False(t, *refused)
	require.Len(t, observed.observation.Calls, 3)
}

func TestObservedCompletionStopsAfterMaxVerdictAttempts(t *testing.T) {
	t.Parallel()

	client := &scriptedCompletion{responses: []*openrouter.CompletionResponse{refusal(), refusal(), refusal(), answer(validVerdictJSON)}}
	observed, refused := newObservedCompletion(client)
	result, err := observed.GetCompletion(t.Context(), openrouter.CompletionRequest{Model: piopenrouter.ConfirmationModel})
	require.NoError(t, err)
	require.True(t, isRefusal(result))
	require.True(t, *refused, "the report scores a confirmation refused three times as a refusal")
	require.Len(t, observed.observation.Calls, maxVerdictAttempts)
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
		name                 string
		failures             []error
		stopWait             bool
		wantAttempts         int
		wantLatency          time.Duration
		wantFinalUnavailable bool
	}{
		{name: "immediate success", failures: []error{nil}, wantAttempts: 1, wantLatency: time.Second},
		{name: "recovers", failures: []error{transient, nil}, wantAttempts: 2, wantLatency: 7 * time.Second},
		{name: "exhausts retries", failures: []error{transient, transient, transient, transient}, wantAttempts: 4, wantLatency: 39 * time.Second, wantFinalUnavailable: true},
		{name: "permanent failure", failures: []error{permanent}, wantAttempts: 1, wantLatency: time.Second, wantFinalUnavailable: true},
		{name: "canceled retry wait", failures: []error{transient}, stopWait: true, wantAttempts: 1, wantLatency: 3 * time.Second, wantFinalUnavailable: true},
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
				observation.Calls = append(observation.Calls, callObservation{Err: err})
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
			_, verdict, err := runCascadeCase(ctx, observation, scan, func() time.Time { return clock }, wait)
			require.Equal(t, tc.wantAttempts, attempts)
			require.Equal(t, tc.wantLatency, observation.Latency)
			require.Equal(t, tc.wantFinalUnavailable, err != nil || verdict.Label == promptinjection.LabelUnavailable)
			require.Len(t, observation.Calls, attempts, "every physical attempt remains in cost/error reporting")
		})
	}
}

// noRisk answers every question with probability zero, so the cascade
// clears the case without a confirmation.
func noRisk(questions map[string]typesafe.Question) typesafe.Result {
	probabilities := make(map[string]float64, len(questions))
	for key := range questions {
		probabilities[key] = 0
	}
	return typesafe.Result{Probabilities: probabilities, Model: typesafe.Model}
}

// blockingPrefilter holds every evaluation until release closes, so a test
// controls how many cases are in flight when it cancels.
type blockingPrefilter struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (p *blockingPrefilter) Evaluate(_ context.Context, _ string, _ json.RawMessage, questions map[string]typesafe.Question) (typesafe.Result, error) {
	p.calls.Add(1)
	p.started <- struct{}{}
	<-p.release
	return noRisk(questions), nil
}

func benignCases(n int) []labeledCase {
	cases := make([]labeledCase, n)
	for i := range cases {
		cases[i] = recordsCase(fmt.Sprint(i), "benign", "")
	}
	return cases
}

func TestCascadeCancellationStartsNoMoreCases(t *testing.T) {
	t.Parallel()

	cases := benignCases(judgeConcurrency + 2)
	prefilter := &blockingPrefilter{started: make(chan struct{}, len(cases)), release: make(chan struct{})}
	completion := &scriptedCompletion{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var completed atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- scanCascadeWithClients(ctx, cases, func(int, caseOutcome) { completed.Add(1) }, prefilter, completion)
	}()

	// The first cases hold every slot, so the loop waits for one when the run
	// is cancelled.
	for range judgeConcurrency {
		<-prefilter.started
	}
	cancel()
	close(prefilter.release)
	require.ErrorIs(t, <-done, context.Canceled)
	require.Equal(t, int32(judgeConcurrency), prefilter.calls.Load(), "no case starts after the cancel")
	require.Equal(t, int32(judgeConcurrency), completed.Load(), "every started case is handed back")
	require.Empty(t, completion.models)
}

func TestCascadeCancelledBeforeStartJudgesNothing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	prefilter := &blockingPrefilter{started: make(chan struct{}, 1), release: make(chan struct{})}
	completed := 0
	err := scanCascadeWithClients(ctx, benignCases(3), func(int, caseOutcome) { completed++ }, prefilter, &scriptedCompletion{})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, prefilter.calls.Load())
	require.Zero(t, completed)
}
