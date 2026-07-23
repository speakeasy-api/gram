package gram

import (
	"testing"

	"github.com/stretchr/testify/require"

	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
)

func TestPIJudgeConfigDefaultsToTypedSingleCall(t *testing.T) {
	t.Setenv("GRAM_PI_JUDGE_PROFILE", "")
	t.Setenv("GRAM_PI_JUDGE_SAMPLES", "")
	t.Setenv("GRAM_PI_JUDGE_MODEL", "")
	t.Setenv("GRAM_PI_JUDGE_REASONING", "")

	config, err := piJudgeConfigFromEnv()
	require.NoError(t, err)
	require.Equal(t, piopenrouter.Config{Profile: "", Samples: 1, Model: "", Reasoning: ""}, config)
}

func TestPIJudgeConfigReadsLegacyRollbackProfile(t *testing.T) {
	t.Setenv("GRAM_PI_JUDGE_PROFILE", piopenrouter.ProfileLegacy)
	t.Setenv("GRAM_PI_JUDGE_SAMPLES", "3")
	t.Setenv("GRAM_PI_JUDGE_MODEL", "model")
	t.Setenv("GRAM_PI_JUDGE_REASONING", "low")

	config, err := piJudgeConfigFromEnv()
	require.NoError(t, err)
	require.Equal(t, piopenrouter.Config{Profile: piopenrouter.ProfileLegacy, Samples: 3, Model: "model", Reasoning: "low"}, config)
}

func TestPIJudgeConfigRejectsUnknownProfile(t *testing.T) {
	t.Setenv("GRAM_PI_JUDGE_PROFILE", "legcay")

	_, err := piJudgeConfigFromEnv()
	require.ErrorContains(t, err, "invalid GRAM_PI_JUDGE_PROFILE")
}
