package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

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
