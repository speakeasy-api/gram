package gram

import (
	"os"
	"strconv"
	"strings"

	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
)

func piJudgeConfigFromEnv() piopenrouter.Config {
	rawSamples := strings.TrimSpace(os.Getenv("GRAM_PI_JUDGE_SAMPLES"))
	samples, err := strconv.Atoi(rawSamples)
	if rawSamples == "" || err != nil || samples < 1 {
		samples = piopenrouter.SamplesPerEvent
	}

	return piopenrouter.Config{
		Profile:   strings.TrimSpace(os.Getenv("GRAM_PI_JUDGE_PROFILE")),
		Samples:   samples,
		Model:     strings.TrimSpace(os.Getenv("GRAM_PI_JUDGE_MODEL")),
		Reasoning: strings.TrimSpace(os.Getenv("GRAM_PI_JUDGE_REASONING")),
	}
}
