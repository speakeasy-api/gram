package chat

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type closeCountingStream struct {
	io.Reader
	closes int
}

func (s *closeCountingStream) Close() error {
	s.closes++
	return nil
}

type emptyStreamThenPlainClient struct {
	openrouter.CompletionClient
	stream            *closeCountingStream
	closesAtFallback  int
	fallbackResponded bool
}

func (c *emptyStreamThenPlainClient) GetCompletionStream(context.Context, openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	return c.stream, nil
}

func (c *emptyStreamThenPlainClient) GetCompletion(context.Context, openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	c.closesAtFallback = c.stream.closes
	c.fallbackResponded = true
	return &openrouter.CompletionResponse{Content: ""}, nil
}

// Closing the stream records its span attributes, so an empty stream must be
// closed before the plain fallback runs. Otherwise the deferred close would
// overwrite the fallback's provider and token details on the span.
func TestTeedCompletionClosesEmptyStreamBeforeFallback(t *testing.T) {
	t.Parallel()

	client := &emptyStreamThenPlainClient{
		stream: &closeCountingStream{Reader: strings.NewReader("data: [DONE]\n")},
	}
	svc := &Service{logger: testenv.NewLogger(t), completionClient: client}

	_, err := svc.teedCompletion(t.Context(), openrouter.CompletionRequest{}, uuid.New())
	require.NoError(t, err)
	require.True(t, client.fallbackResponded)
	require.Equal(t, 1, client.closesAtFallback, "stream must be closed before the fallback")
	require.Equal(t, 1, client.stream.closes, "stream must be closed exactly once")
}
