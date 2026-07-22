package gram

import (
	"os"
	"strconv"
	"strings"

	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
)

func piRedesignConfigFromEnv() piopenrouter.RedesignConfig {
	rawSamples := strings.TrimSpace(os.Getenv("GRAM_PI_REDESIGN_SAMPLES"))
	samples, err := strconv.Atoi(rawSamples)
	if rawSamples == "" || err != nil || samples < 1 {
		return piopenrouter.RedesignConfig{Samples: 0, Model: "", Reasoning: ""}
	}

	return piopenrouter.RedesignConfig{
		Samples:   samples,
		Model:     strings.TrimSpace(os.Getenv("GRAM_PI_REDESIGN_MODEL")),
		Reasoning: strings.TrimSpace(os.Getenv("GRAM_PI_REDESIGN_REASONING")),
	}
}
