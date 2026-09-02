package openrouter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/hostedinference"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type scriptedInferenceCheckpoint struct {
	mu     sync.Mutex
	errors []error
	calls  int
}

func (s *scriptedInferenceCheckpoint) Check(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.calls
	s.calls++
	if index < len(s.errors) {
		return s.errors[index]
	}
	return nil
}

type fixedInferenceEvaluator struct{ result killswitches.EvaluationResult }

func (f fixedInferenceEvaluator) Evaluate(context.Context, killswitches.EvaluationRequest) killswitches.EvaluationResult {
	return f.result
}

type countingKeyResolver struct{ calls int }

type typedCheckpointDenialError struct{}

func (*typedCheckpointDenialError) Error() string { return "hosted inference denied during retry" }

func (r *countingKeyResolver) ResolveKey(context.Context, string, string, billing.ModelUsageSource, KeyType) (ResolvedKey, error) {
	r.calls++
	return ResolvedKey{Key: "key"}, nil
}

func TestUnifiedClientDoesNotFollowProviderRedirects(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			var redirectedRequests atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				redirectedRequests.Add(1)
			}))
			t.Cleanup(target.Close)

			redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", target.URL)
				w.WriteHeader(status)
			}))
			t.Cleanup(redirect.Close)

			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
			require.NoError(t, err)
			client := NewUnifiedClient(testenv.NewLogger(t), policy, nil, &countingKeyResolver{}, nil, nil, nil, nil)
			resp, err := client.httpClient.Get(redirect.URL)
			require.NoError(t, err)
			require.Equal(t, status, resp.StatusCode)
			require.NoError(t, resp.Body.Close())
			require.Zero(t, redirectedRequests.Load(), "redirected provider attempts must never egress")
		})
	}
}

func TestInjectedClientFailsClosedWithoutCheckpointDependency(t *testing.T) {
	t.Parallel()
	resolver := &countingKeyResolver{}
	client := (&ChatClient{logger: testenv.NewLogger(t), keyResolver: resolver}).WithHostedInferenceCheckpoint(nil)

	_, err := client.GetCompletion(t.Context(), CompletionRequest{OrgID: "org", ProjectID: uuid.NewString()})
	require.ErrorContains(t, err, "checkpoint is unavailable")
	require.Zero(t, resolver.calls)
}

func TestInjectedClientFailsClosedWithoutClassification(t *testing.T) {
	t.Parallel()
	registry, err := mcptoolexecution.NewRegistry(nil)
	require.NoError(t, err)
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	checkpoint, err := hostedinference.NewCheckpoint(registry, fixedInferenceEvaluator{result: noMatch}, hostedinference.DefaultEvaluationTimeout)
	require.NoError(t, err)
	resolver := &countingKeyResolver{}
	capture := &mockMessageCaptureStrategy{}
	client := (&ChatClient{logger: testenv.NewLogger(t), keyResolver: resolver, messageCaptureStrategy: capture}).WithHostedInferenceCheckpoint(checkpoint)

	_, err = client.GetCompletion(t.Context(), CompletionRequest{OrgID: "org", ProjectID: uuid.NewString()})
	var unavailable *hostedinference.InfrastructureUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Zero(t, resolver.calls)
	require.False(t, capture.startOrResumeCalled)
}

func TestHostedInferenceDenialPrecedesAllRequestSideEffects(t *testing.T) {
	t.Parallel()
	denied := errors.New("denied before side effects")
	projectID := uuid.NewString()
	completion := CompletionRequest{OrgID: "org", ProjectID: projectID, Messages: []or.ChatMessages{CreateMessageUser("hello")}}

	tests := []struct {
		name string
		call func(*testing.T, *ChatClient) error
	}{
		{name: "completion", call: func(t *testing.T, client *ChatClient) error {
			t.Helper()
			_, err := client.GetCompletion(t.Context(), completion)
			return err
		}},
		{name: "stream", call: func(t *testing.T, client *ChatClient) error {
			t.Helper()
			_, err := client.GetCompletionStream(t.Context(), completion)
			return err
		}},
		{name: "object completion", call: func(t *testing.T, client *ChatClient) error {
			t.Helper()
			_, err := client.GetObjectCompletion(t.Context(), ObjectCompletionRequest{OrgID: "org", ProjectID: projectID, Prompt: "hello"})
			return err
		}},
		{name: "embeddings", call: func(t *testing.T, client *ChatClient) error {
			t.Helper()
			_, err := client.CreateEmbeddings(t.Context(), "org", "openai/text-embedding-3-small", []string{"hello"})
			return err
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checkpoint := &scriptedInferenceCheckpoint{errors: []error{denied}}
			resolver := &countingKeyResolver{}
			capture := &mockMessageCaptureStrategy{}
			client := (&ChatClient{logger: testenv.NewLogger(t), keyResolver: resolver, messageCaptureStrategy: capture}).WithHostedInferenceCheckpoint(checkpoint)

			err := test.call(t, client)
			require.ErrorIs(t, err, denied)
			require.Equal(t, 1, checkpoint.calls)
			require.Zero(t, resolver.calls, "key resolution/provisioning must not run")
			require.False(t, capture.startOrResumeCalled, "capture must not start")
		})
	}
}

func TestEmbeddingSDKRetryRechecksHostedInferenceBeforeHTTPAttempt(t *testing.T) {
	t.Parallel()

	var providerRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"retryable provider failure"}}`))
	}))
	defer server.Close()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	resolver := &countingKeyResolver{}
	denial := &typedCheckpointDenialError{}
	checkpoint := &scriptedInferenceCheckpoint{errors: []error{nil, nil, denial}}
	client := NewUnifiedClient(testenv.NewLogger(t), policy, nil, resolver, nil, nil, nil, nil).WithHostedInferenceCheckpoint(checkpoint)
	client.httpClient = &http.Client{Transport: &testTransport{server: server}}

	_, err = client.CreateEmbeddings(t.Context(), "org", "openai/text-embedding-3-small", []string{"hello"})
	var typedDenial *typedCheckpointDenialError
	require.ErrorAs(t, err, &typedDenial)
	require.Equal(t, 3, checkpoint.calls, "pre-key guard plus one check for each SDK HTTP attempt")
	require.EqualValues(t, 1, providerRequests.Load(), "the denied SDK retry must not reach the provider")
	require.Equal(t, 1, resolver.calls, "key resolution remains behind the initial guard and before SDK attempts")
}

func TestHostedInferenceReevaluatesBeforeRetryAttempt(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"empty","model":"openai/gpt-5.4","choices":[]}`))
	}))
	defer server.Close()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	resolver := &countingKeyResolver{}
	capture := &mockMessageCaptureStrategy{}
	retryDenied := errors.New("activated before retry")
	checkpoint := &scriptedInferenceCheckpoint{errors: []error{nil, nil, retryDenied}}
	client := NewUnifiedClient(testenv.NewLogger(t), policy, nil, resolver, capture, nil, nil, nil).WithHostedInferenceCheckpoint(checkpoint)
	client.httpClient = &http.Client{Transport: &testTransport{server: server}}

	_, err = client.GetCompletion(t.Context(), CompletionRequest{
		OrgID: "org", ProjectID: uuid.NewString(), Messages: []or.ChatMessages{CreateMessageUser("hello")}, Model: "openai/gpt-5.4",
	})
	require.ErrorIs(t, err, retryDenied)
	require.Equal(t, 3, checkpoint.calls, "preflight plus one check per attempted provider call")
	require.Equal(t, 1, requests, "the denied retry must not reach the provider")
	require.Equal(t, 1, resolver.calls)
	require.True(t, capture.startOrResumeCalled)
}
