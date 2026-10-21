package promptpolicy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The judge model is fixed, so a `model` key persisted by an older policy write
// must be inert rather than an error - the rest of the config still applies.
// A persisted `fail_open` is inert too: the failure mode is the
// organization's, so even a stored fail-closed choice parses as fail open.
func TestParseConfigIgnoresPersistedModelAndFailOpen(t *testing.T) {
	t.Parallel()

	cfg := ParseConfig([]byte(`{"model":"anthropic/claude-haiku-4.5","temperature":0.3,"fail_open":false}`))

	require.NotNil(t, cfg.Temperature)
	require.InDelta(t, 0.3, *cfg.Temperature, 0)
	require.False(t, cfg.FailClosed)
}

func TestParseConfigMalformedUsesDefault(t *testing.T) {
	t.Parallel()

	cfg := ParseConfig([]byte(`{`))

	require.Nil(t, cfg.Temperature)
	require.False(t, cfg.FailClosed)
}
