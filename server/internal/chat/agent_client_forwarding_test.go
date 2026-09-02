package chat

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/killswitches/hostedinference"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type forwardingCompletionClient struct {
	t       *testing.T
	wantCtx context.Context //nolint:containedctx // This test spy verifies that the exact context is forwarded.
	calls   map[string]int
}

func (c *forwardingCompletionClient) record(ctx context.Context, method string) {
	c.t.Helper()
	require.Equal(c.t, c.wantCtx, ctx)
	c.calls[method]++
}

func (c *forwardingCompletionClient) GetCompletion(ctx context.Context, _ openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	c.record(ctx, "GetCompletion")
	return &openrouter.CompletionResponse{}, nil
}

func (c *forwardingCompletionClient) GetCompletionStream(ctx context.Context, _ openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	c.record(ctx, "GetCompletionStream")
	return nil, nil
}

func (c *forwardingCompletionClient) GetObjectCompletion(ctx context.Context, _ openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	c.record(ctx, "GetObjectCompletion")
	return &openrouter.CompletionResponse{}, nil
}

func (c *forwardingCompletionClient) CreateEmbeddings(ctx context.Context, _ string, _ string, _ []string, _ ...openrouter.EmbeddingOption) ([][]float32, error) {
	c.record(ctx, "CreateEmbeddings")
	return [][]float32{{1}}, nil
}

func (c *forwardingCompletionClient) ResolveKey(ctx context.Context, _ string, _ string, _ billing.ModelUsageSource, _ openrouter.KeyType) (openrouter.ResolvedKey, error) {
	c.record(ctx, "ResolveKey")
	return openrouter.ResolvedKey{}, nil
}

func TestAgenticClientGenericMethodsPreserveCallerClassification(t *testing.T) {
	t.Parallel()

	ctx, err := hostedinference.WithUnsupported(context.Background(), hostedinference.CallCategoryAPIKeyChat)
	require.NoError(t, err)
	underlying := &forwardingCompletionClient{t: t, wantCtx: ctx, calls: map[string]int{}}
	client := &Client{completionClient: underlying}

	_, err = client.GetCompletion(ctx, openrouter.CompletionRequest{})
	require.NoError(t, err)
	_, err = client.GetCompletionStream(ctx, openrouter.CompletionRequest{})
	require.NoError(t, err)
	_, err = client.GetObjectCompletion(ctx, openrouter.ObjectCompletionRequest{})
	require.NoError(t, err)
	_, err = client.CreateEmbeddings(ctx, "org", "model", []string{"input"})
	require.NoError(t, err)
	_, err = client.ResolveKey(ctx, "org", "project", billing.ModelUsageSourceGram, openrouter.KeyTypeChat)
	require.NoError(t, err)

	require.Equal(t, map[string]int{
		"GetCompletion":       1,
		"GetCompletionStream": 1,
		"GetObjectCompletion": 1,
		"CreateEmbeddings":    1,
		"ResolveKey":          1,
	}, underlying.calls)
}

func TestChatInferenceExclusionsRemainUnsupported(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		authCtx *contextvalues.AuthContext
		keySlot billing.ModelUsageSource
		prepare func(context.Context) context.Context
	}{
		"api key":             {authCtx: &contextvalues.AuthContext{APIKeyID: "key"}},
		"nonordinary session": {authCtx: &contextvalues.AuthContext{}},
		"chat-session jwt":    {authCtx: &contextvalues.AuthContext{}, keySlot: billing.ModelUsageSourceElements},
		"assistant": {
			authCtx: &contextvalues.AuthContext{},
			prepare: func(ctx context.Context) context.Context {
				return contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: uuid.New()})
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := contextvalues.SetAuthContext(t.Context(), tt.authCtx)
			if tt.prepare != nil {
				ctx = tt.prepare(ctx)
			}
			classified, err := classifyChatInference(ctx, tt.keySlot)
			require.NoError(t, err)
			// Unsupported classes bypass before a checkpoint needs any injected
			// principal, resource, evaluator, or transport dependency.
			require.NoError(t, (&hostedinference.Checkpoint{}).Check(classified, "org"))
		})
	}
}
