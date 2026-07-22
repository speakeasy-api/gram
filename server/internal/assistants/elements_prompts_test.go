package assistants

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestElementsPromptsMatchCanonicalSources(t *testing.T) {
	t.Parallel()

	prompts := []struct {
		path      string
		generated string
	}{
		{path: "../../../client/dashboard/src/elements/prompts/system.txt", generated: elementsSystemPrompt},
		{path: "../../../client/dashboard/src/elements/prompts/chart.txt", generated: elementsChartPrompt},
		{path: "../../../client/dashboard/src/elements/prompts/generative-ui.txt", generated: elementsGenerativeUIPrompt},
	}

	for _, prompt := range prompts {
		canonical, err := os.ReadFile(prompt.path)
		require.NoError(t, err, "read canonical prompt %s", prompt.path)
		require.Equal(t, string(canonical), prompt.generated, "run mise gen:elements-prompts after editing %s", prompt.path)
	}
}
