package assistanttokens

import (
	"context"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestInferenceTranscriptOwnership(t *testing.T) {
	t.Parallel()

	require.False(t, InferenceTranscriptOwned(context.Background()))
	ctx := context.WithValue(context.Background(), inferenceContextKey{}, assistantidentity.Identity{})
	require.True(t, InferenceTranscriptOwned(ctx))
}
