package gram

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
)

func piJudgeConfigFromEnv() (piopenrouter.Config, error) {
	profile := strings.TrimSpace(os.Getenv("GRAM_PI_JUDGE_PROFILE"))
	switch profile {
	case "", piopenrouter.ProfileTyped, piopenrouter.ProfileLegacy:
	default:
		return piopenrouter.Config{}, fmt.Errorf("invalid GRAM_PI_JUDGE_PROFILE %q: must be %q or %q", profile, piopenrouter.ProfileTyped, piopenrouter.ProfileLegacy)
	}

	rawSamples := strings.TrimSpace(os.Getenv("GRAM_PI_JUDGE_SAMPLES"))
	samples, err := strconv.Atoi(rawSamples)
	if rawSamples == "" || err != nil || samples < 1 {
		samples = piopenrouter.SamplesPerEvent
	}

	return piopenrouter.Config{
		Profile:   profile,
		Samples:   samples,
		Model:     strings.TrimSpace(os.Getenv("GRAM_PI_JUDGE_MODEL")),
		Reasoning: strings.TrimSpace(os.Getenv("GRAM_PI_JUDGE_REASONING")),
	}, nil
}
