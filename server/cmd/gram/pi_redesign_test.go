package gram

import (
	"testing"

	"github.com/stretchr/testify/require"

	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
)

func TestPIRedesignConfigDefaultsOffWhenGatesUnset(t *testing.T) {
	t.Setenv("GRAM_PI_REDESIGN_SAMPLES", "")
	t.Setenv("GRAM_PI_REDESIGN_MODEL", "")
	t.Setenv("GRAM_PI_REDESIGN_REASONING", "")

	require.Equal(t, piopenrouter.RedesignConfig{Samples: 0, Model: "", Reasoning: ""}, piRedesignConfigFromEnv())
}

func TestPIRedesignConfigReadsEnabledProfile(t *testing.T) {
	t.Setenv("GRAM_PI_REDESIGN_SAMPLES", "3")
	t.Setenv("GRAM_PI_REDESIGN_MODEL", "model")
	t.Setenv("GRAM_PI_REDESIGN_REASONING", "low")

	require.Equal(t, piopenrouter.RedesignConfig{Samples: 3, Model: "model", Reasoning: "low"}, piRedesignConfigFromEnv())
}
