package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/pijudge"
	"github.com/speakeasy-api/gram/server/internal/riskjudge"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	defaultCorpusDir = "server/internal/background/activities/risk_analysis/testdata/prompt_injection"
	defaultOutFile   = "server/risk_accuracy_metrics.json"
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

type options struct {
	corpusDir        string
	outFile          string
	checkFloors      bool
	judge            bool
	judgeModel       string
	judgeConcurrency int
}

const (
	// defaultJudgeModel mirrors pijudge's stage-1 default (Haiku 4.5): the model
	// the production L1 engine runs. The bench drives the same model so its
	// accuracy numbers reflect what ships.
	defaultJudgeModel = "anthropic/claude-haiku-4.5"
	// judgeConcurrency bounds concurrent judge calls. The corpus is a few hundred
	// rows; 8 keeps it brisk without tripping provider rate limits.
	defaultJudgeConcurrency = 8
	// judgeTimeout bounds a single judge completion call in the bench. Generous
	// vs prod's 10s — accuracy matters more than latency here.
	judgeTimeout = 30 * time.Second
	// benchOrgID/benchProjectID label the judge calls. The judge needs an
	// org/project for the request shape; these are inert identifiers (the
	// dev-key provisioner ignores the org, projectID must parse as a UUID).
	benchOrgID     = "5a25158b-24dc-4d49-b03d-e85acfbea59c"
	benchProjectID = "00000000-0000-0000-0000-000000000001"
)

func main() {
	opts := parseFlags()
	if err := run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "risk-pi-report: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() options {
	opts := options{
		corpusDir:        "",
		outFile:          "",
		checkFloors:      false,
		judge:            false,
		judgeModel:       "",
		judgeConcurrency: 0,
	}
	flag.StringVar(&opts.corpusDir, "corpus-dir", defaultCorpusDir, "directory containing prompt-injection JSONL corpus files")
	flag.StringVar(&opts.outFile, "out", defaultOutFile, "path to write metrics JSON")
	flag.BoolVar(&opts.checkFloors, "check-floors", true, "fail if L0 metrics violate floors.json")
	flag.BoolVar(&opts.judge, "judge", false, "also evaluate the L1 LLM judge (needs OPENROUTER_DEV_KEY)")
	flag.StringVar(&opts.judgeModel, "judge-model", defaultJudgeModel, "OpenRouter model id for the L1 judge (must be allowlisted)")
	flag.IntVar(&opts.judgeConcurrency, "judge-concurrency", defaultJudgeConcurrency, "max concurrent judge calls")
	flag.Parse()
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

	if opts.judge {
		judgeMode, err := scanJudgeMode(ctx, opts, corpus, l0Findings)
		if err != nil {
			return err
		}
		modes = append(modes, judgeMode)
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

// scanJudgeMode evaluates the L1 LLM judge over the corpus and folds its
// findings on top of L0 — the "L0 + L1" operational mode an opted-in org runs.
// It calls GetObjectCompletion directly (the same request pijudge builds, minus
// the engine's per-org rate limiter and fail-open), so the numbers reflect the
// model's raw accuracy on every case rather than a throttled subset. Returns a
// skipped mode when no OpenRouter key is configured, so CI and keyless dev runs
// still produce the L0 report.
func scanJudgeMode(ctx context.Context, opts options, corpus []labeledCase, l0Findings [][]risk_analysis.Finding) (modeSummary, error) {
	apiKey := firstEnv("OPENROUTER_DEV_KEY", "OPENROUTER_API_KEY")
	if apiKey == "" || apiKey == "unset" {
		return skippedMode("l1_opt_in", "OPENROUTER_DEV_KEY not set"), nil
	}

	fmt.Fprintf(os.Stderr, "judging %d cases with %s (concurrency=%d)\n", len(corpus), opts.judgeModel, opts.judgeConcurrency)
	findings := scanJudge(ctx, opts, newOpenRouterClient(apiKey), corpus, l0Findings)

	mode := summarizeFindings("l1_opt_in", corpus, findings)
	mode.NewFalsePositives = changedExamples(corpus, l0Findings, findings, "benign", 10)
	mode.RecoveredTruePositive = changedExamples(corpus, l0Findings, findings, "malicious", 10)
	return mode, nil
}

// scanJudge runs the judge for every corpus row (bounded concurrency) and
// appends an L1 finding wherever it flags an attack, on a clone of the L0
// findings. A judge failure leaves the row with its L0 verdict — a stuck or
// erroring model degrades to L0, matching the engine's fail-open posture.
func scanJudge(ctx context.Context, opts options, client openrouter.CompletionClient, corpus []labeledCase, l0Findings [][]risk_analysis.Finding) [][]risk_analysis.Finding {
	out := cloneFindings(l0Findings)
	ruleID, description := risk_analysis.DescribePromptInjection()

	sem := make(chan struct{}, opts.judgeConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var done int

	for i := range corpus {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()

			text := corpus[i].Text
			msg := risk_analysis.NewJudgeMessage(message.User, "", text)
			isAttack, confidence, err := judgeOne(ctx, client, opts.judgeModel, msg)

			mu.Lock()
			defer mu.Unlock()
			done++
			if done%20 == 0 || done == len(corpus) {
				fmt.Fprintf(os.Stderr, "\r  judge %d/%d", done, len(corpus))
			}
			if err != nil || !isAttack {
				return
			}
			out[i] = append(out[i], risk_analysis.Finding{
				RuleID:           ruleID,
				Description:      description,
				Match:            text,
				StartPos:         0,
				EndPos:           len(text),
				Source:           risk_analysis.SourcePromptInjection,
				Confidence:       confidence,
				Tags:             []string{"llm-judge", "layer-1"},
				DeadLetterReason: "",
			})
		}(i)
	}
	wg.Wait()
	fmt.Fprintln(os.Stderr)
	return out
}

// judgeOne issues one GetObjectCompletion shaped exactly like pijudge's call:
// the structured "message" payload (riskjudge.RenderMessage), pijudge's system
// prompt and verdict schema, temperature 0. No copy of the prompt/schema to keep
// in sync — it drives the production constants directly.
func judgeOne(ctx context.Context, client openrouter.CompletionClient, model string, msg risk_analysis.JudgeMessage) (isAttack bool, confidence float64, err error) {
	payload, err := json.Marshal(struct {
		Message riskjudge.MessagePayload `json:"message"`
	}{Message: riskjudge.RenderMessage(msg)})
	if err != nil {
		return false, 0, fmt.Errorf("marshal judge payload: %w", err)
	}

	strict := true
	schema := or.ChatJSONSchemaConfig{
		Name:        "prompt_attack_verdict",
		Schema:      pijudge.VerdictSchema(),
		Description: nil,
		Strict:      optionalnullable.From(&strict),
	}
	temp := 0.0

	callCtx, cancel := context.WithTimeout(ctx, judgeTimeout)
	defer cancel()

	resp, err := client.GetObjectCompletion(callCtx, openrouter.ObjectCompletionRequest{
		OrgID:          benchOrgID,
		ProjectID:      benchProjectID,
		Model:          model,
		SystemPrompt:   pijudge.SystemPrompt,
		Prompt:         string(payload),
		Temperature:    &temp,
		UsageSource:    billing.ModelUsageSourceGram,
		UserID:         "",
		ExternalUserID: "",
		HTTPMetadata:   nil,
		JSONSchema:     &schema,
	})
	if err != nil {
		return false, 0, fmt.Errorf("openrouter object completion: %w", err)
	}
	if resp == nil || resp.Message == nil {
		return false, 0, fmt.Errorf("empty completion response")
	}
	raw := strings.TrimSpace(openrouter.GetText(*resp.Message))
	if raw == "" {
		return false, 0, fmt.Errorf("empty completion content")
	}
	var verdict struct {
		IsAttack   bool    `json:"is_attack"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil {
		return false, 0, fmt.Errorf("parse judge response: %w", err)
	}
	return verdict.IsAttack, max(0, min(1, verdict.Confidence)), nil
}

// newOpenRouterClient builds the real production OpenRouter client with the
// org-scoped concerns stubbed: a dev-key provisioner, and nil capture/usage/
// title/telemetry strategies (all nil-guarded). Same construction as
// riskjudgebench, so the bench runs under prod-equivalent conditions.
func newOpenRouterClient(apiKey string) openrouter.CompletionClient {
	logger := slog.New(slog.DiscardHandler)
	policy := guardian.NewDefaultPolicy(tracenoop.NewTracerProvider())
	return openrouter.NewUnifiedClient(
		logger,
		policy,
		&devProvisioner{apiKey: apiKey},
		nil, // message capture  (nil-guarded)
		nil, // usage tracking   (nil-guarded)
		nil, // chat title gen   (nil-guarded)
		nil, // telemetry logger (nil-guarded)
	)
}

// devProvisioner satisfies openrouter.Provisioner but skips the DB/billing path:
// it hands back the dev key for every org. Only ProvisionAPIKey is exercised by
// the GetObjectCompletion path; the rest are unreachable here.
type devProvisioner struct{ apiKey string }

func (d *devProvisioner) ProvisionAPIKey(_ context.Context, _ string) (string, error) {
	return d.apiKey, nil
}
func (d *devProvisioner) RefreshAPIKeyLimit(_ context.Context, _ string, _ *int) (int, error) {
	return 0, fmt.Errorf("not implemented in bench")
}
func (d *devProvisioner) GetCreditsUsed(_ context.Context, _ string) (float64, int, error) {
	return 0, 0, fmt.Errorf("not implemented in bench")
}
func (d *devProvisioner) GetKeyUsage(_ context.Context, _ string) (float64, *int64, error) {
	return 0, nil, fmt.Errorf("not implemented in bench")
}
func (d *devProvisioner) ReconcileMonthlyCredits(_ context.Context, _ string, currentLimit int64, _ *int64) (int64, error) {
	return currentLimit, nil
}

var _ openrouter.Provisioner = (*devProvisioner)(nil)

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

// changedExamples lists rows of the given label that L0 missed (empty baseline)
// but the candidate mode now flags — i.e. judge-recovered true positives
// (label "malicious") or judge-introduced false positives (label "benign").
// Sorted by confidence desc, capped at limit.
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

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
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
