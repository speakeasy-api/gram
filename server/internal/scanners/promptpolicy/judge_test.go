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

func TestParseConfigMalformedUsesDefault(t *testing.T) {
	t.Parallel()

	cfg := ParseConfig([]byte(`{`))

	require.Empty(t, cfg.Model)
	require.Nil(t, cfg.Temperature)
	require.True(t, cfg.FailOpen)
}
