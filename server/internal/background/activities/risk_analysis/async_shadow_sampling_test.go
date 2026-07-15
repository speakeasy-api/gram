package risk_analysis

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSampleAsyncShadowDeterministicPartialSubset(t *testing.T) {
	t.Parallel()

	const sampleRate = 0.5
	first := make([]bool, 100)
	second := make([]bool, 100)
	for i := range first {
		id := uuid.NewSHA1(uuid.NameSpaceURL, fmt.Appendf(nil, "chat-message-%d", i)).String()
		first[i] = sampleAsyncShadow(id, sampleRate)
		second[i] = sampleAsyncShadow(id, sampleRate)
	}

	require.Equal(t, first, second)
	require.Contains(t, first, true)
	require.Contains(t, first, false)
}
