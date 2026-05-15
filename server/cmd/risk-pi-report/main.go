package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
)

const (
	defaultCorpusDir = "server/internal/background/activities/risk_analysis/testdata/prompt_injection"
	defaultOutFile   = "server/risk_accuracy_metrics.json"
	httpBatchSize    = 50
)

type labeledCase struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Text   string `json:"text"`
	Source string `json:"source"`
}

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

type sourceSummary struct {
	Source  string       `json:"source"`
	Counts  counts       `json:"counts"`
	Metrics metricsBlock `json:"metrics"`
}

type ruleHist struct {
	RuleID string `json:"rule_id"`
	TP     int    `json:"tp_count"`
	FP     int    `json:"fp_count"`
}

type exampleCase struct {
	ID     string  `json:"id"`
	Source string  `json:"source"`
	RuleID string  `json:"rule_id"`
	Score  float64 `json:"score"`
	Text   string  `json:"text"`
}

type modeSummary struct {
	Name                  string          `json:"name"`
	Skipped               bool            `json:"skipped,omitempty"`
	SkipReason            string          `json:"skip_reason,omitempty"`
	Total                 int             `json:"total"`
	Counts                counts          `json:"counts"`
	Overall               metricsBlock    `json:"overall"`
	Sources               []sourceSummary `json:"by_source,omitempty"`
	Rules                 []ruleHist      `json:"by_rule,omitempty"`
	NewFalsePositives     []exampleCase   `json:"new_false_positives,omitempty"`
	RecoveredTruePositive []exampleCase   `json:"recovered_true_positives,omitempty"`
}

type accuracySummary struct {
	Total   int             `json:"total"`
	Counts  counts          `json:"counts"`
	Overall metricsBlock    `json:"overall"`
	Sources []sourceSummary `json:"by_source"`
	Rules   []ruleHist      `json:"by_rule"`
	Modes   []modeSummary   `json:"modes,omitempty"`
}

type envelope struct {
	GitSHA    string          `json:"git_sha"`
	Ref       string          `json:"ref"`
	Timestamp string          `json:"timestamp"`
	Summary   accuracySummary `json:"summary"`
}

type detectRequest struct {
	Texts []string `json:"texts"`
}

type detectResponse struct {
	Results []risk_analysis.ClassifierResult `json:"results"`
}

type options struct {
	corpusDir     string
	outFile       string
	classifierURL string
	checkFloors   bool
}

func main() {
	opts := parseFlags()
	if err := run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "risk-pi-report: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() options {
	opts := options{
		corpusDir:     "",
		outFile:       "",
		classifierURL: "",
		checkFloors:   false,
	}
	flag.StringVar(&opts.corpusDir, "corpus-dir", defaultCorpusDir, "directory containing prompt-injection JSONL corpus files")
	flag.StringVar(&opts.outFile, "out", defaultOutFile, "path to write metrics JSON")
	flag.StringVar(&opts.classifierURL, "classifier-url", strings.TrimSpace(os.Getenv("PI_CLASSIFIER_URL")), "base URL for the L1 classifier sidecar")
	flag.BoolVar(&opts.checkFloors, "check-floors", true, "fail if L0 metrics violate floors.json")
	flag.Parse()
	opts.classifierURL = strings.TrimRight(strings.TrimSpace(opts.classifierURL), "/")
	return opts
}

func run(ctx context.Context, opts options) error {
	corpus, err := loadCorpus(opts.corpusDir)
	if err != nil {
		return err
	}
	fl, err := loadFloors(opts.corpusDir)
	if err != nil {
		return err
	}

	l0Findings, err := scanL0(ctx, corpus)
	if err != nil {
		return err
	}
	l0 := summarizeFindings("l0_default", corpus, l0Findings)
	modes := []modeSummary{l0}

	if opts.classifierURL == "" {
		modes = append(modes,
			skippedMode("l1_opt_in", "classifier URL is not set"),
		)
	} else if err := healthcheck(ctx, opts.classifierURL); err != nil {
		modes = append(modes,
			skippedMode("l1_opt_in", err.Error()),
		)
	} else {
		l1OptInFindings, err := scanL1OptIn(ctx, corpus, l0Findings, opts.classifierURL)
		if err != nil {
			return err
		}
		l1OptIn := summarizeFindings("l1_opt_in", corpus, l1OptInFindings)
		l1OptIn.NewFalsePositives = changedExamples(corpus, l0Findings, l1OptInFindings, "benign", 10)
		l1OptIn.RecoveredTruePositive = changedExamples(corpus, l0Findings, l1OptInFindings, "malicious", 10)
		modes = append(modes,
			l1OptIn,
		)
	}

	summary := accuracySummary{
		Total:   l0.Total,
		Counts:  l0.Counts,
		Overall: l0.Overall,
		Sources: l0.Sources,
		Rules:   l0.Rules,
		Modes:   modes,
	}

	if opts.checkFloors && l0.Overall.FPRate > fl.FPRateMax {
		return fmt.Errorf(
			"L0 FP-rate %.4f exceeds floor %.4f (floors.json last updated %s by %s)",
			l0.Overall.FPRate,
			fl.FPRateMax,
			fl.LastUpdated,
			fl.LastUpdatedBy,
		)
	}

	return writeMetrics(opts.outFile, summary)
}

func loadCorpus(dir string) ([]labeledCase, error) {
	files := []string{
		"deepset.jsonl",
		"gram_benigns.jsonl",
		"litellm_extended.jsonl",
		"mutations.jsonl",
		"operational_benigns.jsonl",
	}

	seen := map[string]string{}
	var out []labeledCase
	for _, name := range files {
		path := filepath.Join(dir, name)
		f, err := os.Open(path) // #nosec G304 -- local developer/CI harness intentionally reads a configured corpus path.
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", path, err)
		}

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
			if err := json.Unmarshal([]byte(raw), &c); err != nil {
				_ = f.Close()
				return nil, fmt.Errorf("%s line %d unmarshal: %w", name, line, err)
			}
			if c.ID == "" {
				_ = f.Close()
				return nil, fmt.Errorf("%s line %d missing id", name, line)
			}
			if c.Label != "malicious" && c.Label != "benign" {
				_ = f.Close()
				return nil, fmt.Errorf("%s line %d invalid label %q", name, line, c.Label)
			}
			if _, dup := seen[c.Text]; dup {
				continue
			}
			seen[c.Text] = c.ID
			out = append(out, c)
		}
		if err := scanner.Err(); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("scan %s: %w", path, err)
		}
		if err := f.Close(); err != nil {
			return nil, fmt.Errorf("close %s: %w", path, err)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("loaded corpus is empty")
	}
	return out, nil
}

func loadFloors(dir string) (floors, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "floors.json")) // #nosec G304 -- local developer/CI harness intentionally reads a configured corpus path.
	if err != nil {
		return floors{}, fmt.Errorf("read floors.json: %w", err)
	}
	var fl floors
	if err := json.Unmarshal(raw, &fl); err != nil {
		return floors{}, fmt.Errorf("unmarshal floors.json: %w", err)
	}
	if fl.LastUpdated == "" {
		return floors{}, fmt.Errorf("floors.json missing last_updated")
	}
	return fl, nil
}

func scanL0(ctx context.Context, corpus []labeledCase) ([][]risk_analysis.Finding, error) {
	out := make([][]risk_analysis.Finding, len(corpus))
	for i, c := range corpus {
		findings, err := risk_analysis.DetectPromptInjection(ctx, c.Text)
		if err != nil {
			return nil, fmt.Errorf("scan L0 %s: %w", c.ID, err)
		}
		out[i] = findings
	}
	return out, nil
}

func scanL1OptIn(ctx context.Context, corpus []labeledCase, l0Findings [][]risk_analysis.Finding, baseURL string) ([][]risk_analysis.Finding, error) {
	texts := make([]string, len(corpus))
	for i, c := range corpus {
		texts[i] = c.Text
	}

	results, err := classify(ctx, baseURL, texts)
	if err != nil {
		return nil, err
	}
	if len(results) != len(corpus) {
		return nil, fmt.Errorf("classifier returned %d results for %d corpus rows", len(results), len(corpus))
	}

	out := cloneFindings(l0Findings)
	for i, r := range results {
		if r.Label != risk_analysis.LabelInjection {
			continue
		}
		out[i] = append(out[i], risk_analysis.Finding{
			RuleID:           risk_analysis.RulePromptInjectionClassifier,
			Description:      "ML classifier flagged prompt injection",
			Match:            corpus[i].Text,
			StartPos:         0,
			EndPos:           len(corpus[i].Text),
			Source:           risk_analysis.SourcePromptInjection,
			Confidence:       r.Score,
			Tags:             []string{"ml", "layer-1"},
			DeadLetterReason: "",
		})
	}
	return out, nil
}

func healthcheck(ctx context.Context, baseURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("create classifier health request: %w", err)
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("classifier health failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("classifier health returned status %d", resp.StatusCode)
	}
	return nil
}

func classify(ctx context.Context, baseURL string, texts []string) ([]risk_analysis.ClassifierResult, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	results := make([]risk_analysis.ClassifierResult, 0, len(texts))
	for start := 0; start < len(texts); start += httpBatchSize {
		end := min(start+httpBatchSize, len(texts))
		batch, err := detect(ctx, client, baseURL, texts[start:end])
		if err != nil {
			return nil, err
		}
		results = append(results, batch...)
	}
	return results, nil
}

func detect(ctx context.Context, client *http.Client, baseURL string, texts []string) ([]risk_analysis.ClassifierResult, error) {
	body, err := json.Marshal(detectRequest{Texts: texts})
	if err != nil {
		return nil, fmt.Errorf("marshal classifier request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/detect", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create classifier request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("classifier request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("classifier returned status %d", resp.StatusCode)
	}

	var decoded detectResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode classifier response: %w", err)
	}
	if len(decoded.Results) != len(texts) {
		return nil, fmt.Errorf("classifier returned %d results for %d texts", len(decoded.Results), len(texts))
	}
	return decoded.Results, nil
}

func summarizeFindings(mode string, corpus []labeledCase, findings [][]risk_analysis.Finding) modeSummary {
	overall := counts{TP: 0, FP: 0, TN: 0, FN: 0}
	bySource := map[string]*counts{}
	ruleTP := map[string]int{}
	ruleFP := map[string]int{}

	for i, c := range corpus {
		fs := findings[i]
		flagged := len(fs) > 0

		bucket, ok := bySource[c.Source]
		if !ok {
			bucket = &counts{TP: 0, FP: 0, TN: 0, FN: 0}
			bySource[c.Source] = bucket
		}

		switch {
		case c.Label == "malicious" && flagged:
			overall.TP++
			bucket.TP++
			for _, f := range fs {
				ruleTP[f.RuleID]++
			}
		case c.Label == "malicious" && !flagged:
			overall.FN++
			bucket.FN++
		case c.Label == "benign" && flagged:
			overall.FP++
			bucket.FP++
			for _, f := range fs {
				ruleFP[f.RuleID]++
			}
		case c.Label == "benign" && !flagged:
			overall.TN++
			bucket.TN++
		}
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

	return modeSummary{
		Name:                  mode,
		Skipped:               false,
		SkipReason:            "",
		Total:                 len(corpus),
		Counts:                overall,
		Overall:               deriveMetrics(overall),
		Sources:               sources,
		Rules:                 rules,
		NewFalsePositives:     nil,
		RecoveredTruePositive: nil,
	}
}

func changedExamples(corpus []labeledCase, baseline, candidate [][]risk_analysis.Finding, label string, limit int) []exampleCase {
	examples := []exampleCase{}
	for i, c := range corpus {
		if c.Label != label || len(baseline[i]) > 0 || len(candidate[i]) == 0 {
			continue
		}
		f := highestConfidenceFinding(candidate[i])
		examples = append(examples, exampleCase{
			ID:     c.ID,
			Source: c.Source,
			RuleID: f.RuleID,
			Score:  f.Confidence,
			Text:   c.Text,
		})
	}
	sort.Slice(examples, func(i, j int) bool {
		if examples[i].Score == examples[j].Score {
			return examples[i].ID < examples[j].ID
		}
		return examples[i].Score > examples[j].Score
	})
	if len(examples) > limit {
		return examples[:limit]
	}
	return examples
}

func highestConfidenceFinding(findings []risk_analysis.Finding) risk_analysis.Finding {
	out := findings[0]
	for _, f := range findings[1:] {
		if f.Confidence > out.Confidence {
			out = f
		}
	}
	return out
}

func cloneFindings(lhs [][]risk_analysis.Finding) [][]risk_analysis.Finding {
	out := make([][]risk_analysis.Finding, len(lhs))
	for i := range lhs {
		out[i] = append([]risk_analysis.Finding{}, lhs[i]...)
	}
	return out
}

func skippedMode(name, reason string) modeSummary {
	return modeSummary{
		Name:                  name,
		Skipped:               true,
		SkipReason:            reason,
		Total:                 0,
		Counts:                counts{TP: 0, FP: 0, TN: 0, FN: 0},
		Overall:               metricsBlock{Precision: 0, Recall: 0, F1: 0, Accuracy: 0, FPRate: 0},
		Sources:               nil,
		Rules:                 nil,
		NewFalsePositives:     nil,
		RecoveredTruePositive: nil,
	}
}

func writeMetrics(path string, summary accuracySummary) error {
	payload := envelope{
		GitSHA:    envOr("GITHUB_SHA", "local"),
		Ref:       envOr("GITHUB_REF_NAME", "local"),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Summary:   summary,
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metrics: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create metrics dir: %w", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("write metrics: %w", err)
	}
	return nil
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

func safeDiv(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
