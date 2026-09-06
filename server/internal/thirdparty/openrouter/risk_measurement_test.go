package openrouter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCompletionObserverPreservesPhysicalAttemptsAndUnknownUsage(t *testing.T) {
	t.Parallel()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = fmt.Fprint(w, `{"id":"attempt-empty","model":"openai/gpt-5.4","choices":[],"usage":{"prompt_tokens":17,"completion_tokens":3,"cost":0.01}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"id":"attempt-final","model":"openai/gpt-5.4","choices":[{"message":{"role":"assistant","content":"not valid judge JSON"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)
	logger := testenv.NewLogger(t)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	observer := &captureCompletionAttemptObserver{attempts: nil}
	provisioner := &mockProvisioner{apiKey: "test-api-key"}
	client := NewUnifiedClient(logger, policy, provisioner, &PlatformKeyResolver{Provisioner: provisioner}, nil, nil, nil, nil, observer)
	client.httpClient = &http.Client{Transport: &testTransport{server: server}}
	projectID := uuid.NewString()
	var req ObjectCompletionRequest
	req.OrgID = "test-org"
	req.ProjectID = projectID
	req.Prompt = "Classify this content"
	req.Model = "openai/gpt-5.4"
	req.UsageSource = billing.ModelUsageSourceRiskAnalysis
	req.KeyType = KeyTypeInternal
	response, err := client.GetObjectCompletion(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "not valid judge JSON", response.Content)
	require.Len(t, observer.attempts, 2)
	first, second := observer.attempts[0], observer.attempts[1]
	require.Equal(t, "attempt-empty", first.ProviderRequestID)
	require.Equal(t, CompletionAttemptFailed, first.Status)
	require.NoError(t, first.Err)
	require.Equal(t, "openai/gpt-5.4", first.RequestedModel)
	require.Equal(t, "openai/gpt-5.4", first.ReturnedModel)
	require.NotNil(t, first.CostUSD)
	require.InEpsilon(t, 0.01, *first.CostUSD, 1e-12)
	require.Equal(t, int64(17), *first.PromptTokens)
	require.Equal(t, int64(3), *first.CompletionTokens)
	require.False(t, first.StartedAt.IsZero())
	require.False(t, first.CompletedAt.Before(first.StartedAt))
	require.Equal(t, "attempt-final", second.ProviderRequestID)
	require.Equal(t, CompletionAttemptSucceeded, second.Status)
	require.NoError(t, second.Err)
	require.Equal(t, "openai/gpt-5.4", second.RequestedModel)
	require.Equal(t, "openai/gpt-5.4", second.ReturnedModel)
	require.Nil(t, second.CostUSD)
	require.Nil(t, second.PromptTokens)
	require.Nil(t, second.CompletionTokens)
	require.False(t, second.StartedAt.Before(first.CompletedAt))
	require.False(t, second.CompletedAt.Before(second.StartedAt))
}

type captureCompletionAttemptObserver struct {
	attempts []CompletionAttempt
}

func (o *captureCompletionAttemptObserver) ObserveCompletionAttempt(_ context.Context, attempt CompletionAttempt) error {
	o.attempts = append(o.attempts, attempt)
	return nil
}
