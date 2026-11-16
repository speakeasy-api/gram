package tooleval

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type rateLimitedClient struct {
	openrouter.CompletionClient
	limited int32
	calls   atomic.Int32
	err     error
}

func (c *rateLimitedClient) GetCompletion(context.Context, openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	if c.calls.Add(1) <= c.limited {
		return nil, c.err
	}
	return &openrouter.CompletionResponse{}, nil
}

func TestCompleteWithRetryWaitsOutARateLimit(t *testing.T) {
	t.Parallel()

	limited := &rateLimitedClient{limited: 1, err: &openrouter.HTTPError{StatusCode: 429, Err: openrouter.ErrRateLimited}}
	response, err := completeWithRetry(t.Context(), limited, openrouter.CompletionRequest{})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.EqualValues(t, 2, limited.calls.Load(), "a rate-limited call is retried")

	failing := &rateLimitedClient{limited: 5, err: errors.New("bad request")}
	_, err = completeWithRetry(t.Context(), failing, openrouter.CompletionRequest{})
	require.Error(t, err)
	require.EqualValues(t, 1, failing.calls.Load(), "other errors are not retried")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	stuck := &rateLimitedClient{limited: 100, err: &openrouter.HTTPError{StatusCode: 429, Err: openrouter.ErrRateLimited}}
	_, err = completeWithRetry(ctx, stuck, openrouter.CompletionRequest{})
	require.ErrorIs(t, err, openrouter.ErrRateLimited, "a cancelled wait reports the rate limit")
}
