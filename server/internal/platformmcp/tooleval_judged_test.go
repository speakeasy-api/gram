//go:build tooleval

package platformmcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/tooleval"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	// defaultToolEvalModels answer every case on every surface.
	// TOOLEVAL_MODELS takes a comma separated list to compare models.
	defaultToolEvalModels = "anthropic/claude-sonnet-5.5"

	// defaultToolEvalRuns is one: the direct surface lists every tool, so a
	// run costs real money. Raise it to see how stable a miss is.
	defaultToolEvalRuns = 1

	defaultToolEvalConcurrency = 8
)

func toolEvalEnv(name, fallback string) string {
	if value := os.Getenv(name); value != "" && value != "unset" {
		return value
	}
	return fallback
}

func toolEvalEnvInt(t *testing.T, name string, fallback int) int {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	require.NoError(t, err, name)
	require.Positive(t, n, name)
	return n
}

// TestToolEvalJudged asks a real model every case. It costs LLM calls, so it
// runs only under the tooleval build tag through mise run eval:platform-tools.
func TestToolEvalJudged(t *testing.T) { //nolint:paralleltest // one long run that fans out internally
	apiKey := toolEvalEnv("OPENROUTER_DEV_KEY", os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		t.Skip("set OPENROUTER_DEV_KEY to run the Platform MCP tool eval")
	}
	cfg := tooleval.Config{
		Models:      strings.Split(toolEvalEnv("TOOLEVAL_MODELS", defaultToolEvalModels), ","),
		Runs:        toolEvalEnvInt(t, "TOOLEVAL_RUNS", defaultToolEvalRuns),
		Concurrency: toolEvalEnvInt(t, "TOOLEVAL_CONCURRENCY", defaultToolEvalConcurrency),
	}
	for i := range cfg.Models {
		cfg.Models[i] = strings.TrimSpace(cfg.Models[i])
	}
	require.NoError(t, cfg.Validate())

	cases, err := tooleval.LoadCases()
	require.NoError(t, err)
	if filter := os.Getenv("TOOLEVAL_CASES"); filter != "" {
		pattern, err := regexp.Compile(filter)
		require.NoError(t, err, "TOOLEVAL_CASES")
		var selected []tooleval.Case
		for _, c := range cases {
			if pattern.MatchString(c.ID) || pattern.MatchString(c.File) {
				selected = append(selected, c)
			}
		}
		require.NotEmpty(t, selected, "TOOLEVAL_CASES matches no case id or file")
		cases = selected
	}

	registrar := toolEvalRegistrar(t)
	var notes []string
	if baseDir := os.Getenv("TOOLEVAL_BASE_DIR"); baseDir != "" {
		var base tooleval.Catalogue
		data, err := os.ReadFile(filepath.Join(baseDir, "catalogue.json"))
		require.NoError(t, err, "base catalogue")
		require.NoError(t, json.Unmarshal(data, &base), "base catalogue")
		baseCases, err := tooleval.LoadCasesFS(os.DirFS(filepath.Join(baseDir, "cases")))
		require.NoError(t, err, "base cases")
		var reason string
		cases, reason = tooleval.SelectChanged(base, toolEvalSnapshot(t, registrar), baseCases, cases)
		t.Log(reason)
		if len(cases) == 0 {
			t.Skip(reason)
		}
		notes = append(notes, "Compared with the base branch: "+reason+".")
	}
	if limit := toolEvalEnvInt(t, "TOOLEVAL_MAX_CASES", 0); limit > 0 && len(cases) > limit {
		notes = append(notes, fmt.Sprintf("Ran a stable sample of %d of the %d selected cases (TOOLEVAL_MAX_CASES).", limit, len(cases)))
		cases = tooleval.SampleCases(cases, limit)
	}

	var surfaces []tooleval.Surface
	for name := range strings.SplitSeq(toolEvalEnv("TOOLEVAL_SURFACES", "direct,progressive"), ",") {
		switch strings.TrimSpace(name) {
		case "direct":
			surfaces = append(surfaces, toolEvalDirectSurface(t, registrar))
		case "progressive":
			surfaces = append(surfaces, toolEvalProgressiveSurface(t, registrar))
		default:
			t.Fatalf("unknown surface %q; use direct or progressive", name)
		}
	}

	provisioner := openrouter.NewDevelopment(apiKey)
	client := openrouter.NewUnifiedClient(
		testenv.NewLogger(t),
		guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)),
		provisioner,
		&openrouter.PlatformKeyResolver{Provisioner: provisioner},
		nil,
		nil,
		nil,
		nil,
	)

	report, err := tooleval.Run(t.Context(), client, cases, toolEvalCatalogue(t, registrar), surfaces, cfg)
	require.NoError(t, err)
	report.Notes = append(notes, report.Notes...)
	markdown := report.Markdown()
	t.Log("\n" + markdown)
	if out := os.Getenv("TOOLEVAL_OUT"); out != "" {
		require.NoError(t, os.WriteFile(out, []byte(markdown), 0o600))
	}
}
