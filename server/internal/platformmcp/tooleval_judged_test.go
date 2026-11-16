//go:build tooleval

package platformmcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	// Zero is allowed: TOOLEVAL_MAX_CASES=0 means no cap. Config.Validate
	// still rejects zero runs or concurrency.
	require.GreaterOrEqual(t, n, 0, name)
	return n
}

// TestToolEvalJudged asks a real model every case. It costs LLM calls, so it
// runs only under the tooleval build tag through mise run eval:platform-tools.
func TestToolEvalJudged(t *testing.T) { //nolint:paralleltest // one long run that fans out internally
	apiKey := toolEvalEnv("OPENROUTER_DEV_KEY", os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		t.Log("set OPENROUTER_DEV_KEY or OPENROUTER_API_KEY to run the Platform MCP tool eval")
		return
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
	allCases := cases
	baseline := readToolEvalBaseline(t)
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
	var filler []tooleval.Case
	if baseDir := os.Getenv("TOOLEVAL_BASE_DIR"); baseDir != "" {
		var base tooleval.Catalogue
		data, err := os.ReadFile(filepath.Join(baseDir, "catalogue.json"))
		require.NoError(t, err, "base catalogue")
		require.NoError(t, json.Unmarshal(data, &base), "base catalogue")
		baseCases, err := tooleval.LoadCasesFS(os.DirFS(filepath.Join(baseDir, "cases")))
		require.NoError(t, err, "base cases")
		changed, negatives, reason := tooleval.SelectChanged(base, toolEvalSnapshot(t, registrar), baseCases, cases)
		t.Log(reason)
		if len(changed) == 0 {
			t.Log(reason)
			return
		}
		notes = append(notes, "Compared with the base branch: "+reason+".")
		cases, filler = changed, negatives
	}
	if limit := toolEvalEnvInt(t, "TOOLEVAL_MAX_CASES", 0); limit > 0 && len(cases)+len(filler) > limit {
		notes = append(notes, fmt.Sprintf("Ran %d of the %d selected cases (TOOLEVAL_MAX_CASES), the cases the change selected first.", limit, len(cases)+len(filler)))
	}
	cases = tooleval.CapCases(cases, filler, toolEvalEnvInt(t, "TOOLEVAL_MAX_CASES", 0))

	var surfaces []tooleval.Surface
	var seen []string
	for name := range strings.SplitSeq(toolEvalEnv("TOOLEVAL_SURFACES", "direct,progressive"), ",") {
		name = strings.TrimSpace(name)
		// A repeated surface would run, and pay for, every case twice.
		if slices.Contains(seen, name) {
			continue
		}
		seen = append(seen, name)
		switch name {
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

	if !slices.ContainsFunc(cfg.Models, func(m string) bool { return !strings.HasPrefix(m, "anthropic/") }) {
		// OpenRouter otherwise picks among Bedrock, Vertex, Azure and others
		// per request, and their results drift apart; the baseline is only
		// comparable when every run is served the same way. Bedrock is the
		// provider OpenRouter picks for this account by default; Anthropic's
		// own endpoint is filtered out by the account's routing settings.
		client.WithProviderRouting(&openrouter.ProviderRouting{Order: []string{"amazon-bedrock"}, AllowFallbacks: false})
	}
	report, err := tooleval.Run(t.Context(), client, cases, toolEvalCatalogue(t, registrar), surfaces, cfg)
	require.NoError(t, err)

	// The gate compares the one model the baseline was taken with.
	model := cfg.Models[0]
	results := report.Results(model)
	var regressions []tooleval.Regression
	gate := os.Getenv("TOOLEVAL_GATE") == "1"
	switch {
	case !gate:
	case len(baseline.Cases) == 0:
		notes = append(notes, "There is no committed baseline yet, so nothing is gated. Commit this run's baseline.json as tooleval/baseline.json.")
	case len(cfg.Models) != 1 || model != baseline.Model:
		notes = append(notes, fmt.Sprintf("The baseline was taken with %s, so a run of %s is not gated.", baseline.Model, strings.Join(cfg.Models, ", ")))
	default:
		regressions = tooleval.Regressions(baseline, results)
		var missing []string
		for _, c := range cases {
			if _, ok := baseline.Cases[c.ID]; !ok {
				missing = append(missing, c.ID)
			}
		}
		if len(missing) > 0 {
			notes = append(notes, fmt.Sprintf("%d cases have no baseline entry yet, so they are not gated: %s. Commit this run's baseline.json as tooleval/baseline.json to start gating them.", len(missing), strings.Join(missing, ", ")))
		}
	}

	report.Notes = append(notes, report.Notes...)
	markdown := tooleval.RegressionsMarkdown(regressions) + report.Markdown()
	t.Log("\n" + markdown)
	if out := os.Getenv("TOOLEVAL_OUT"); out != "" {
		require.NoError(t, os.WriteFile(out, []byte(markdown), 0o600))
	}
	if out := os.Getenv("TOOLEVAL_BASELINE_OUT"); out != "" {
		data, err := baseline.Merge(model, results, allCases).Marshal()
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(out, data, 0o600))
	}
	require.Empty(t, regressions, "Platform MCP tool eval cases regressed against tooleval/baseline.json; see the report")
}

// toolEvalBaselinePath is the committed per-case baseline the gate compares
// against.
const toolEvalBaselinePath = "tooleval/baseline.json"

func readToolEvalBaseline(t *testing.T) tooleval.Baseline {
	t.Helper()
	var baseline tooleval.Baseline
	data, err := os.ReadFile(toolEvalBaselinePath)
	if errors.Is(err, fs.ErrNotExist) {
		return baseline
	}
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &baseline), toolEvalBaselinePath)
	return baseline
}
