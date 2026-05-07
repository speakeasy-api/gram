package risk_analysis

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// labeledCase is one row of the labeled corpus used by the accuracy suite.
type labeledCase struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Text   string `json:"text"`
	Source string `json:"source"`
}

// floors holds the threshold values committed alongside the corpus. fp_rate_max
// is enforced (CI fails on regression). recall_floor is reported as a soft
// signal; it is logged but never fails CI.
type floors struct {
	FPRateMax     float64  `json:"fp_rate_max"`
	RecallFloor   *float64 `json:"recall_floor"`
	LastUpdated   string   `json:"last_updated"`
	LastUpdatedBy string   `json:"last_updated_by"`
	Notes         string   `json:"notes"`
}

type counts struct {
	TP int `json:"tp"`
	FP int `json:"fp"`
	TN int `json:"tn"`
	FN int `json:"fn"`
}

type metricsBlock struct {
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
	Accuracy  float64 `json:"accuracy"`
	FPRate    float64 `json:"fp_rate"`
}

func safeDiv(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func deriveMetrics(c counts) metricsBlock {
	p := safeDiv(c.TP, c.TP+c.FP)
	r := safeDiv(c.TP, c.TP+c.FN)
	f1 := 0.0
	if p+r > 0 {
		f1 = 2 * p * r / (p + r)
	}
	return metricsBlock{
		Precision: p,
		Recall:    r,
		F1:        f1,
		Accuracy:  safeDiv(c.TP+c.TN, c.TP+c.FP+c.TN+c.FN),
		FPRate:    safeDiv(c.FP, c.FP+c.TN),
	}
}

func loadCorpus(t *testing.T, dir string) []labeledCase {
	t.Helper()

	files := []string{
		"deepset.jsonl",
		"gram_benigns.jsonl",
		"litellm_extended.jsonl",
		"mutations.jsonl",
	}

	seen := map[string]string{}
	var out []labeledCase

	for _, name := range files {
		path := filepath.Join(dir, name)
		f, err := os.Open(path)
		require.NoErrorf(t, err, "open %s", name)

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			raw := strings.TrimSpace(scanner.Text())
			if raw == "" {
				continue
			}
			var c labeledCase
			require.NoErrorf(t, json.Unmarshal([]byte(raw), &c), "%s line %d unmarshal", name, line)
			require.NotEmptyf(t, c.ID, "%s line %d missing id", name, line)
			require.Containsf(t, []string{"malicious", "benign"}, c.Label, "%s line %d invalid label %q", name, line, c.Label)
			if existingID, dup := seen[c.Text]; dup {
				t.Logf("corpus dedup: %s drops in favor of earlier %s", c.ID, existingID)
				continue
			}
			seen[c.Text] = c.ID
			out = append(out, c)
		}
		require.NoError(t, scanner.Err())
		require.NoError(t, f.Close())
	}

	require.NotEmpty(t, out, "loaded corpus is empty")
	return out
}

func loadFloors(t *testing.T, dir string) floors {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "floors.json"))
	require.NoError(t, err)
	var fl floors
	require.NoError(t, json.Unmarshal(raw, &fl))
	require.NotEmpty(t, fl.LastUpdated, "floors.json missing last_updated")
	return fl
}

// TestDetectPromptInjection_AccuracyBaseline runs the labeled corpus through
// DetectPromptInjection, computes the confusion matrix, and asserts the FP-rate
// floor from floors.json. Recall + precision + F1 + accuracy and per-source +
// per-rule breakdowns are emitted via t.Log as a structured JSON summary so
// regressions and tuning opportunities are visible in test output.
func TestDetectPromptInjection_AccuracyBaseline(t *testing.T) {
	t.Parallel()

	dir := filepath.Join("testdata", "prompt_injection")
	corpus := loadCorpus(t, dir)
	fl := loadFloors(t, dir)

	overall := counts{}
	bySource := map[string]*counts{}
	ruleTP := map[string]int{}
	ruleFP := map[string]int{}

	ctx := t.Context()
	for _, c := range corpus {
		findings, err := DetectPromptInjection(ctx, c.Text)
		require.NoError(t, err)
		flagged := len(findings) > 0

		bucket, ok := bySource[c.Source]
		if !ok {
			bucket = &counts{}
			bySource[c.Source] = bucket
		}

		switch {
		case c.Label == "malicious" && flagged:
			overall.TP++
			bucket.TP++
			for _, f := range findings {
				ruleTP[f.RuleID]++
			}
		case c.Label == "malicious" && !flagged:
			overall.FN++
			bucket.FN++
		case c.Label == "benign" && flagged:
			overall.FP++
			bucket.FP++
			for _, f := range findings {
				ruleFP[f.RuleID]++
			}
		case c.Label == "benign" && !flagged:
			overall.TN++
			bucket.TN++
		}
	}

	require.Positive(t, overall.FP+overall.TN, "no benign cases in corpus — FP-rate is undefined")

	overallM := deriveMetrics(overall)

	type sourceSummary struct {
		Source  string       `json:"source"`
		Counts  counts       `json:"counts"`
		Metrics metricsBlock `json:"metrics"`
	}
	sources := make([]sourceSummary, 0, len(bySource))
	for src, c := range bySource {
		sources = append(sources, sourceSummary{
			Source:  src,
			Counts:  *c,
			Metrics: deriveMetrics(*c),
		})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Source < sources[j].Source })

	type ruleHist struct {
		RuleID string `json:"rule_id"`
		TP     int    `json:"tp_count"`
		FP     int    `json:"fp_count"`
	}
	ruleSet := map[string]struct{}{}
	for r := range ruleTP {
		ruleSet[r] = struct{}{}
	}
	for r := range ruleFP {
		ruleSet[r] = struct{}{}
	}
	rules := make([]ruleHist, 0, len(ruleSet))
	for r := range ruleSet {
		rules = append(rules, ruleHist{RuleID: r, TP: ruleTP[r], FP: ruleFP[r]})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].RuleID < rules[j].RuleID })

	summary := struct {
		Total   int             `json:"total"`
		Counts  counts          `json:"counts"`
		Overall metricsBlock    `json:"overall"`
		Sources []sourceSummary `json:"by_source"`
		Rules   []ruleHist      `json:"by_rule"`
	}{
		Total:   len(corpus),
		Counts:  overall,
		Overall: overallM,
		Sources: sources,
		Rules:   rules,
	}

	pretty, err := json.MarshalIndent(summary, "", "  ")
	require.NoError(t, err)
	t.Logf("accuracy baseline:\n%s", string(pretty))

	writeMetricsArtifact(t, summary)

	// Hard floor: FP-rate must not exceed the committed cap.
	require.LessOrEqualf(t, overallM.FPRate, fl.FPRateMax,
		"FP-rate %.4f exceeds floor %.4f (floors.json last updated %s by %s). "+
			"Either fix the regression or update floors.json with justification.",
		overallM.FPRate, fl.FPRateMax, fl.LastUpdated, fl.LastUpdatedBy,
	)

	// Soft signal: recall vs the (optional) committed floor. Logged only.
	if fl.RecallFloor != nil {
		delta := overallM.Recall - *fl.RecallFloor
		if delta < 0 {
			t.Logf("recall %.4f is %.4f below soft floor %.4f", overallM.Recall, math.Abs(delta), *fl.RecallFloor)
		} else {
			t.Logf("recall %.4f meets soft floor %.4f (margin %.4f)", overallM.Recall, *fl.RecallFloor, delta)
		}
	}
}

// writeMetricsArtifact persists the run's summary to a stable repo-relative
// path (server/risk_accuracy_metrics.json) so CI can upload it as an artifact
// and the local mise risk:report task can read it back. The path is derived
// from this file's location via runtime.Caller so it doesn't depend on cwd.
// Failures are logged but never fail the test.
func writeMetricsArtifact(t *testing.T, summary any) {
	t.Helper()

	envelope := struct {
		GitSHA    string `json:"git_sha"`
		Ref       string `json:"ref"`
		Timestamp string `json:"timestamp"`
		Summary   any    `json:"summary"`
	}{
		GitSHA:    envOr("GITHUB_SHA", "local"),
		Ref:       envOr("GITHUB_REF_NAME", "local"),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Summary:   summary,
	}

	body, err := json.MarshalIndent(envelope, "", "  ")
	require.NoError(t, err)

	path := metricsArtifactPath(t)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Logf("failed to write metrics artifact to %s: %v", path, err)
		return
	}
	t.Logf("wrote metrics artifact: %s", path)
}

// metricsArtifactPath resolves to <repo>/server/risk_accuracy_metrics.json
// regardless of the test runner's cwd. Anchored on this file's path
// (server/internal/background/activities/risk_analysis/) — four levels up is
// the server/ dir.
func metricsArtifactPath(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller(0) failed")
	serverDir := filepath.Join(filepath.Dir(here), "..", "..", "..", "..")
	return filepath.Clean(filepath.Join(serverDir, "risk_accuracy_metrics.json"))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
