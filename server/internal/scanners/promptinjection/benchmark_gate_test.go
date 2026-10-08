package promptinjection_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	// benchmarkWorkflow runs the paid cascade benchmark, relative to the
	// repository root.
	benchmarkWorkflow = ".github/workflows/pi-benchmark.yml"

	// benchmarkCheck is the status check the benchmark job reports.
	benchmarkCheck = "Prompt injection benchmark"

	// benchmarkPath is the path filter that must start the benchmark.
	benchmarkPath = "server/internal/scanners/promptinjection/**"
)

// TestPromptInjectionBenchmarkGateIsEnabled keeps prompt-injection changes
// from merging without the benchmark: the workflow must run on pull requests
// that touch this package.
func TestPromptInjectionBenchmarkGateIsEnabled(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", benchmarkWorkflow))
	require.NoError(t, err)

	var workflow struct {
		On struct {
			PullRequest *struct {
				Paths []string `yaml:"paths"`
			} `yaml:"pull_request"`
		} `yaml:"on"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &workflow))
	pullRequest := workflow.On.PullRequest
	if pullRequest != nil && slices.Contains(pullRequest.Paths, benchmarkPath) {
		return
	}
	t.Fatal(enableBenchmarkInstructions(string(raw)))
}

// enableBenchmarkInstructions names the commented-out trigger lines to
// restore, or the trigger to add when they are gone.
func enableBenchmarkInstructions(workflow string) string {
	const steps = "push, and wait for the %q check to pass. The job reads the OPENROUTER_API_KEY repository secret, so it must exist first."
	lines := strings.Split(workflow, "\n")
	first := slices.IndexFunc(lines, func(line string) bool { return strings.TrimSpace(line) == "# pull_request:" })
	if first < 0 {
		return fmt.Sprintf("Enable the prompt-injection benchmark gate: add a pull_request trigger with the path %q to %s, "+steps, benchmarkPath, benchmarkWorkflow, benchmarkCheck)
	}
	last := first
	for last+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[last+1]), "#  ") {
		last++
	}
	return fmt.Sprintf("Enable the prompt-injection benchmark gate: uncomment the pull_request trigger in %s lines %d-%d, "+steps, benchmarkWorkflow, first+1, last+1, benchmarkCheck)
}
