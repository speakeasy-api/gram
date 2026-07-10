package promptpolicy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseConfigTrimsWhitespaceModel(t *testing.T) {
	t.Parallel()

	cfg := ParseConfig([]byte(`{"model":"   ","fail_open":false}`))

	require.Empty(t, cfg.Model, "whitespace-only model should default to empty")
	require.False(t, cfg.FailOpen)
}
