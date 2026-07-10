package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	defaultCorpusDir = "server/internal/scanners/promptinjection/testdata/prompt_injection"
	defaultOutFile   = "server/risk_accuracy_metrics.json"
)

type toolCallCase struct {
	Name string `json:"name"`
	Args string `json:"args"`
}

type labeledCase struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Text   string `json:"text"`
	Source string `json:"source"`
	// Optional agent-runtime framing: plain rows omit these (judged as end-user
	// content); typed rows carry the message type + tool the judge and scope use.
	Type      string         `json:"type,omitempty"`       // message.Type; default user_message
	Tool      string         `json:"tool,omitempty"`       // tool name for a single-tool tool_request/tool_response
	ToolCalls []toolCallCase `json:"tool_calls,omitempty"` // multi-call tool_request
}

// caseType returns the message type for a case, defaulting to user_message.
func (c labeledCase) caseType() message.Type {
	if c.Type == "" {
		return message.User
	}
	return c.Type
}

// judgeMessage renders a case as the judgemessage the L1 judge evaluates,
// preserving its agent-runtime framing (produced_by/body_kind/tool).
func (c labeledCase) judgeMessage() judgemessage.Message {
	if len(c.ToolCalls) > 0 {
		calls := make([]judgemessage.ToolCall, len(c.ToolCalls))
		for i, tc := range c.ToolCalls {
			calls[i] = judgemessage.NewToolCall(tc.Name, tc.Args)
		}
		return judgemessage.NewForToolCalls(calls)
	}
	return judgemessage.New(c.caseType(), c.Tool, c.Text)
}

// scopeView renders a case as the MessageView the CEL policy scope evaluates,
// mirroring the production scanner (scanner.go) and batch analyzer construction.
func (c labeledCase) scopeView() ra.MessageView {
	typ := c.caseType()
	view := ra.MessageView{Content: c.Text, Type: typ, Tools: []ra.ToolView{}}
	switch {
	case len(c.ToolCalls) > 0:
		for _, tc := range c.ToolCalls {
			view.Tools = append(view.Tools, ra.NewToolView(tc.Name, tc.Args))
		}
	case typ == message.ToolRequest && c.Tool != "":
		view.Tools = []ra.ToolView{ra.NewToolView(c.Tool, c.Text)}
	}
	return view
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
	Name                     string          `json:"name"`
	Skipped                  bool            `json:"skipped,omitempty"`
	SkipReason               string          `json:"skip_reason,omitempty"`
	Total                    int             `json:"total"`
	Counts                   counts          `json:"counts"`
	Overall                  metricsBlock    `json:"overall"`
	Sources                  []sourceSummary `json:"by_source,omitempty"`
	Rules                    []ruleHist      `json:"by_rule,omitempty"`
	NewFalsePositives        []exampleCase   `json:"new_false_positives,omitempty"`
	RecoveredTruePositive    []exampleCase   `json:"recovered_true_positives,omitempty"`
	MissedAttacks            []exampleCase   `json:"missed_attacks,omitempty"`
	InScope                  int             `json:"in_scope,omitempty"`
	LostTruePositives        []exampleCase   `json:"lost_true_positives,omitempty"`
	SuppressedFalsePositives []exampleCase   `json:"suppressed_false_positives,omitempty"`
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
	sources          string
}

const (
	// defaultJudgeModel is the report's L1 judge model when none is provided.
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
		sources:          "",
	}
	flag.StringVar(&opts.corpusDir, "corpus-dir", defaultCorpusDir, "directory containing prompt-injection JSONL corpus files")
	flag.StringVar(&opts.outFile, "out", defaultOutFile, "path to write metrics JSON")
	flag.BoolVar(&opts.checkFloors, "check-floors", true, "fail if L0 metrics violate floors.json")
	flag.BoolVar(&opts.judge, "judge", false, "also evaluate the L1 LLM judge (needs OPENROUTER_DEV_KEY)")
	flag.StringVar(&opts.judgeModel, "judge-model", defaultJudgeModel, "OpenRouter model id for the L1 judge (must be allowlisted)")
	flag.IntVar(&opts.judgeConcurrency, "judge-concurrency", defaultJudgeConcurrency, "max concurrent judge calls")
	flag.StringVar(&opts.sources, "sources", "", "comma-separated source substrings to keep (empty = all); use to judge a cheap iteration slice")
	flag.Parse()
	return opts
}

// filterSources keeps only cases whose Source contains one of the comma-separated
// substrings. Empty spec keeps everything. Used to run a cheap iteration slice
// (benigns + adversarial + recall guards) without judging the full corpus.
func filterSources(corpus []labeledCase, spec string) []labeledCase {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return corpus
	}
	var needles []string
	for s := range strings.SplitSeq(spec, ",") {
		if s = strings.TrimSpace(s); s != "" {
			needles = append(needles, s)
		}
	}
	out := corpus[:0:0]
	for _, c := range corpus {
		for _, n := range needles {
			if strings.Contains(c.Source, n) {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func run(ctx context.Context, opts options) error {
	corpus, err := loadCorpus(opts.corpusDir)
	if err != nil {
		return err
	}
	corpus = filterSources(corpus, opts.sources)
	if len(corpus) == 0 {
		return fmt.Errorf("no cases after --sources filter %q", opts.sources)
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
	l0.NewFalsePositives = changedExamples(corpus, make([][]scanners.Finding, len(corpus)), l0Findings, "benign", 500)
	modes := []modeSummary{l0}

	var l1Findings [][]scanners.Finding
	if opts.judge {
		judgeMode, jf, err := scanJudgeMode(ctx, opts, corpus, l0Findings)
		if err != nil {
			return err
		}
		modes = append(modes, judgeMode)
		l1Findings = jf
	}

	// Scope-aware modes: apply the candidate policy scope (scopes.json) as a
	// pre-filter, so the report shows the FP reduction from scoping AND flags any
	// malicious case the scope would stop scanning (coverage regression).
	scopeCfg, hasScope, err := loadScopes(opts.corpusDir)
	if err != nil {
		return err
	}
	if hasScope {
		scope, err := compileScope(scopeCfg)
		if err != nil {
			return err
		}
		if scope.Active() {
			modes = append(modes, scopedMode("l0_scoped", corpus, l0Findings, scope))
			if l1Findings != nil {
				modes = append(modes, scopedMode("l1_scoped", corpus, l1Findings, scope))
			}
		}
	}

	summary := accuracySummary{
		Total:   l0.Total,
		Counts:  l0.Counts,
		Overall: l0.Overall,
		Sources: l0.Sources,
		Rules:   l0.Rules,
		Modes:   modes,
	}

	printSummary(os.Stderr, modes)

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

// scopedMode summarizes findings after applying the policy scope and annotates
// the coverage impact (in-scope count, lost TPs, suppressed FPs).
func scopedMode(name string, corpus []labeledCase, findings [][]scanners.Finding, scope ra.CompiledScope) modeSummary {
	scoped, inScope := applyScope(corpus, findings, scope)
	m := summarizeFindings(name, corpus, scoped)
	n := 0
	for _, in := range inScope {
		if in {
			n++
		}
	}
	m.InScope = n
	m.LostTruePositives, m.SuppressedFalsePositives = scopeImpact(corpus, findings, inScope)
	return m
}

// printSummary writes a compact per-mode table for the tuning loop.
func printSummary(w *os.File, modes []modeSummary) {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	p("\n=== risk-pi-report summary ===\n")
	for _, m := range modes {
		if m.Skipped {
			p("%-12s SKIPPED (%s)\n", m.Name, m.SkipReason)
			continue
		}
		c := m.Counts
		p("%-12s TP=%-4d FP=%-4d TN=%-4d FN=%-4d | P=%.3f R=%.3f F1=%.3f FPr=%.4f\n",
			m.Name, c.TP, c.FP, c.TN, c.FN, m.Overall.Precision, m.Overall.Recall, m.Overall.F1, m.Overall.FPRate)
		if m.InScope > 0 || len(m.LostTruePositives) > 0 || len(m.SuppressedFalsePositives) > 0 {
			p("             scope: in_scope=%d suppressed_FPs=%d LOST_TPs=%d\n",
				m.InScope, len(m.SuppressedFalsePositives), len(m.LostTruePositives))
			for _, ex := range m.LostTruePositives {
				p("               ! lost TP %s (%s)\n", ex.ID, ex.Source)
			}
		}
	}
	p("\n")
}

// requiredCorpusFiles must exist; optionalCorpusFiles are loaded when present
// (the agent-runtime extended slices: FP-category benigns and the adversarial
// coverage set). A missing optional file is skipped, not an error, so CI and
// keyless dev runs still work on the base corpus.
var requiredCorpusFiles = []string{
	"deepset.jsonl",
	"gram_benigns.jsonl",
	"litellm_extended.jsonl",
	"mutations.jsonl",
	"operational_benigns.jsonl",
}

var optionalCorpusFiles = []string{
	"agent_fp_benigns.jsonl",
	"adversarial_fable.jsonl",
	"adversarial_codex.jsonl",
}

func loadCorpus(dir string) ([]labeledCase, error) {
	seen := map[string]string{}
	var out []labeledCase

	load := func(name string, optional bool) error {
		path := filepath.Join(dir, name)
		f, err := os.Open(path) // #nosec G304 -- local developer/CI harness intentionally reads a configured corpus path.
		if err != nil {
			if optional && errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("open %s: %w", path, err)
		}
		defer func() { _ = f.Close() }()

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
				return fmt.Errorf("%s line %d unmarshal: %w", name, line, err)
			}
			if c.ID == "" {
				return fmt.Errorf("%s line %d missing id", name, line)
			}
			if c.Label != "malicious" && c.Label != "benign" {
				return fmt.Errorf("%s line %d invalid label %q", name, line, c.Label)
			}
			if c.Type != "" && !message.IsTypeValid(c.Type) {
				return fmt.Errorf("%s line %d invalid type %q", name, line, c.Type)
			}
			if _, dup := seen[c.Text]; dup {
				continue
			}
			seen[c.Text] = c.ID
			out = append(out, c)
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("scan %s: %w", path, err)
		}
		return nil
	}

	for _, name := range requiredCorpusFiles {
		if err := load(name, false); err != nil {
			return nil, err
		}
	}
	for _, name := range optionalCorpusFiles {
		if err := load(name, true); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("loaded corpus is empty")
	}
	return out, nil
}

type scopeConfig struct {
	ScopeInclude string `json:"scope_include"`
	ScopeExempt  string `json:"scope_exempt"`
}

// loadScopes reads the optional scopes.json policy-scope fixture. Returns
// present=false when the file is absent so the harness runs unscoped.
func loadScopes(dir string) (cfg scopeConfig, present bool, err error) {
	raw, err := os.ReadFile(filepath.Join(dir, "scopes.json")) // #nosec G304 -- local harness corpus path.
	if errors.Is(err, os.ErrNotExist) {
		return scopeConfig{ScopeInclude: "", ScopeExempt: ""}, false, nil
	}
	if err != nil {
		return scopeConfig{ScopeInclude: "", ScopeExempt: ""}, false, fmt.Errorf("read scopes.json: %w", err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return scopeConfig{ScopeInclude: "", ScopeExempt: ""}, false, fmt.Errorf("unmarshal scopes.json: %w", err)
	}
	return cfg, true, nil
}

// compileScope compiles the policy scope against the production CEL env, so an
// invalid CEL expression fails loudly here rather than silently mis-scoping.
func compileScope(cfg scopeConfig) (ra.CompiledScope, error) {
	eng, err := celenv.New()
	if err != nil {
		return ra.CompiledScope{}, fmt.Errorf("build cel engine: %w", err)
	}
	scope, err := ra.CompileScope(eng, cfg.ScopeInclude, cfg.ScopeExempt)
	if err != nil {
		return ra.CompiledScope{}, fmt.Errorf("compile scope: %w", err)
	}
	return scope, nil
}

// applyScope zeroes findings for out-of-scope cases (as the policy would skip
// them) and returns the scoped findings plus a per-case in-scope mask.
func applyScope(corpus []labeledCase, findings [][]scanners.Finding, scope ra.CompiledScope) (scoped [][]scanners.Finding, inScope []bool) {
	scoped = make([][]scanners.Finding, len(findings))
	inScope = make([]bool, len(corpus))
	for i, c := range corpus {
		view := c.scopeView()
		in := scope.Includes(view) && !scope.Exempts(view)
		inScope[i] = in
		if in {
			scoped[i] = findings[i]
		}
	}
	return scoped, inScope
}

// scopeImpact splits exempted-but-flagged cases into suppressed benign FPs (the
// win) and exempted malicious cases the detector would have caught (coverage loss).
func scopeImpact(corpus []labeledCase, findings [][]scanners.Finding, inScope []bool) (lostTPs, suppressedFPs []exampleCase) {
	for i, c := range corpus {
		if inScope[i] || len(findings[i]) == 0 {
			continue
		}
		f := highestConfidenceFinding(findings[i])
		ex := exampleCase{ID: c.ID, Source: c.Source, RuleID: f.RuleID, Score: f.Confidence, Text: c.Text}
		if c.Label == "malicious" {
			lostTPs = append(lostTPs, ex)
		} else {
			suppressedFPs = append(suppressedFPs, ex)
		}
	}
	sort.Slice(lostTPs, func(i, j int) bool { return lostTPs[i].ID < lostTPs[j].ID })
	sort.Slice(suppressedFPs, func(i, j int) bool { return suppressedFPs[i].ID < suppressedFPs[j].ID })
	return lostTPs, suppressedFPs
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

func scanL0(ctx context.Context, corpus []labeledCase) ([][]scanners.Finding, error) {
	out := make([][]scanners.Finding, len(corpus))
	for i, c := range corpus {
		findings, err := promptinjection.Detect(ctx, c.Text)
		if err != nil {
			return nil, fmt.Errorf("scan L0 %s: %w", c.ID, err)
		}
		out[i] = findings
	}
	return out, nil
}

// scanJudgeMode evaluates the L1 LLM judge over the corpus and folds its
// findings on top of L0 — the "L0 + L1" operational mode an opted-in org runs.
// It calls GetObjectCompletion directly (the same request piopenrouter builds, minus
// the engine's per-org rate limiter and fail-open), so the numbers reflect the
// model's raw accuracy on every case rather than a throttled subset. Returns a
// skipped mode when no OpenRouter key is configured, so CI and keyless dev runs
// still produce the L0 report.
func scanJudgeMode(ctx context.Context, opts options, corpus []labeledCase, l0Findings [][]scanners.Finding) (modeSummary, [][]scanners.Finding, error) {
	apiKey := firstEnv("OPENROUTER_DEV_KEY", "OPENROUTER_API_KEY")
	if apiKey == "" || apiKey == "unset" {
		return skippedMode("l1_opt_in", "OPENROUTER_DEV_KEY not set"), nil, nil
	}

	fmt.Fprintf(os.Stderr, "judging %d cases with %s (concurrency=%d)\n", len(corpus), opts.judgeModel, opts.judgeConcurrency)
	findings := scanJudge(ctx, opts, newOpenRouterClient(apiKey), corpus, l0Findings)

	mode := summarizeFindings("l1_opt_in", corpus, findings)
	mode.NewFalsePositives = changedExamples(corpus, l0Findings, findings, "benign", 500)
	mode.RecoveredTruePositive = changedExamples(corpus, l0Findings, findings, "malicious", 500)
	mode.MissedAttacks = missedExamples(corpus, findings, 500)
	return mode, findings, nil
}

// scanJudge runs the judge for every corpus row (bounded concurrency) and
// appends an L1 finding wherever it flags an attack, on a clone of the L0
// findings. A judge failure leaves the row with its L0 verdict — a stuck or
// erroring model degrades to L0, matching the engine's fail-open posture.
func scanJudge(ctx context.Context, opts options, client openrouter.CompletionClient, corpus []labeledCase, l0Findings [][]scanners.Finding) [][]scanners.Finding {
	out := cloneFindings(l0Findings)
	ruleID, description := promptinjection.Describe()

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
			msg := corpus[i].judgeMessage()
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
			out[i] = append(out[i], scanners.Finding{
				RuleID:           ruleID,
				Description:      description,
				Match:            text,
				StartPos:         0,
				EndPos:           len(text),
				Source:           promptinjection.Source,
				Confidence:       confidence,
				Tags:             []string{"llm-judge", "layer-1"},
				DeadLetterReason: "",

				McpLookupToolCallID: "",
				SpanGroupKey:        "",
				Field:               "",
				Path:                "",
			})
		}(i)
	}
	wg.Wait()
	fmt.Fprintln(os.Stderr)
	return out
}

// judgeOne issues one GetObjectCompletion shaped exactly like piopenrouter's call:
// the structured message payload, piopenrouter's system prompt and verdict
// schema, temperature 0. No copy of the prompt/schema to keep in sync - it
// drives the production constants directly.
func judgeOne(ctx context.Context, client openrouter.CompletionClient, model string, msg judgemessage.Message) (isAttack bool, confidence float64, err error) {
	payload, err := json.Marshal(struct {
		Message judgemessage.Payload `json:"message"`
	}{Message: judgemessage.RenderPayload(msg)})
	if err != nil {
		return false, 0, fmt.Errorf("marshal judge payload: %w", err)
	}

	strict := true
	schema := or.ChatJSONSchemaConfig{
		Name:        "prompt_attack_verdict",
		Schema:      piopenrouter.VerdictSchema(),
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
		SystemPrompt:   piopenrouter.SystemPrompt,
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
func (d *devProvisioner) GetModelUsage(_ context.Context, _ string, _ string) (*openrouter.ModelUsage, error) {
	return nil, fmt.Errorf("not implemented in bench")
}

var _ openrouter.Provisioner = (*devProvisioner)(nil)

func summarizeFindings(mode string, corpus []labeledCase, findings [][]scanners.Finding) modeSummary {
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
		Name:                     mode,
		Skipped:                  false,
		SkipReason:               "",
		Total:                    len(corpus),
		Counts:                   overall,
		Overall:                  deriveMetrics(overall),
		Sources:                  sources,
		Rules:                    rules,
		NewFalsePositives:        nil,
		RecoveredTruePositive:    nil,
		MissedAttacks:            nil,
		InScope:                  0,
		LostTruePositives:        nil,
		SuppressedFalsePositives: nil,
	}
}

// changedExamples lists rows of the given label that L0 missed (empty baseline)
// but the candidate mode now flags — i.e. judge-recovered true positives
// (label "malicious") or judge-introduced false positives (label "benign").
// Sorted by confidence desc, capped at limit.
func changedExamples(corpus []labeledCase, baseline, candidate [][]scanners.Finding, label string, limit int) []exampleCase {
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

// missedExamples lists malicious cases the candidate did not flag (FNs) — the
// recall-loss watchlist for prompt tuning.
func missedExamples(corpus []labeledCase, candidate [][]scanners.Finding, limit int) []exampleCase {
	examples := []exampleCase{}
	for i, c := range corpus {
		if c.Label != "malicious" || len(candidate[i]) > 0 {
			continue
		}
		examples = append(examples, exampleCase{ID: c.ID, Source: c.Source, RuleID: "", Score: 0, Text: c.Text})
	}
	sort.Slice(examples, func(i, j int) bool { return examples[i].ID < examples[j].ID })
	if len(examples) > limit {
		return examples[:limit]
	}
	return examples
}

func highestConfidenceFinding(findings []scanners.Finding) scanners.Finding {
	out := findings[0]
	for _, f := range findings[1:] {
		if f.Confidence > out.Confidence {
			out = f
		}
	}
	return out
}

func cloneFindings(lhs [][]scanners.Finding) [][]scanners.Finding {
	out := make([][]scanners.Finding, len(lhs))
	for i := range lhs {
		out[i] = append([]scanners.Finding{}, lhs[i]...)
	}
	return out
}

func skippedMode(name, reason string) modeSummary {
	return modeSummary{
		Name:                     name,
		Skipped:                  true,
		SkipReason:               reason,
		Total:                    0,
		Counts:                   counts{TP: 0, FP: 0, TN: 0, FN: 0},
		Overall:                  metricsBlock{Precision: 0, Recall: 0, F1: 0, Accuracy: 0, FPRate: 0},
		Sources:                  nil,
		Rules:                    nil,
		NewFalsePositives:        nil,
		RecoveredTruePositive:    nil,
		MissedAttacks:            nil,
		InScope:                  0,
		LostTruePositives:        nil,
		SuppressedFalsePositives: nil,
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
