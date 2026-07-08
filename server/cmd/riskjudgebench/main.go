// Command riskjudgebench benchmarks OpenRouter models for prompt-based
// ("LLM-judge") risk policy evaluation — the call implemented in
// server/internal/riskjudge/judge.go.
//
// Unlike a hand-rolled HTTP client, this drives the REAL production
// openrouter.ChatClient (NewUnifiedClient → GetObjectCompletion), so every
// model runs under prod-equivalent conditions:
//   - reasoning disabled (Effort:"none"), as the object-completion path forces,
//   - the production model allowlist + ResolveModel fallback,
//   - the same guardian-policy HTTP transport,
//   - the identical ObjectCompletionRequest shape judge.call() builds
//     (system prompt, strict JSON schema, temperature, UsageSource).
//
// The only things stubbed out are the org-scoped concerns that don't affect
// model quality/latency: a Provisioner that returns the dev key instead of a
// DB-backed per-org key, and nil capture/usage/title/telemetry strategies
// (all nil-guarded in the client).
//
// Usage:
//
//	export OPENROUTER_DEV_KEY=sk-or-...        # or OPENROUTER_API_KEY
//	go run ./server/cmd/riskjudgebench -h
//	go run ./server/cmd/riskjudgebench
//	go run ./server/cmd/riskjudgebench -runs 3 -models anthropic/claude-haiku-4.5,google/gemini-2.5-flash
//
// Models must be in the openrouter allowlist (internal/thirdparty/openrouter).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/riskjudge"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// The judge system prompt, verdict schema, and user-prompt construction come
// straight from server/internal/riskjudge (riskjudge.SystemPrompt,
// riskjudge.VerdictSchema, riskjudge.BuildJudgePrompt) — the bench drives the
// exact production request, with no copy to keep in sync.

// defaultModels are fast/cheap-tier candidates drawn from the production
// allowlist (internal/thirdparty/openrouter). Models not in the allowlist are
// rejected by the client; edit freely.
var defaultModels = []string{
	"anthropic/claude-haiku-4.5", // the previous baseline before the default moved to gemini-3.1-flash-lite
	"anthropic/claude-sonnet-4.6",
	"openai/gpt-5.4-mini",
	"openai/gpt-5.4-nano",
	"google/gemini-3.5-flash",
	"google/gemini-3.1-flash-lite",
	"google/gemini-2.5-flash",
	"deepseek/deepseek-v4-flash",
	"mistralai/mistral-medium-3.1",
}

type testCase struct {
	ID       string `json:"id"`
	Policy   string `json:"policy"`
	Text     string `json:"text"`
	Expected bool   `json:"expected"`
	Note     string `json:"note"`
	// MessageType and ToolName are optional. When set, the case exercises the
	// structured judge payload (actor + tool attribution) instead of the
	// content-only fallback — used by the adversarial cases. MessageType is a
	// message.Type value ("user_message", "tool_request", "tool_response",
	// "assistant_message"); an empty value renders as opaque content.
	MessageType string `json:"message_type"`
	ToolName    string `json:"tool_name"`
}

type verdict struct {
	Matched    bool    `json:"matched"`
	Confidence float64 `json:"confidence"`
	Rationale  string  `json:"rationale"`
}

// result is one (model, case, run) outcome.
type result struct {
	Model    string
	CaseID   string
	Expected bool
	Got      bool
	Conf     float64
	Latency  time.Duration
	Tokens   int
	Err      string
}

func main() {
	var (
		modelsFlag  = flag.String("models", strings.Join(defaultModels, ","), "comma-separated OpenRouter model ids (must be allowlisted)")
		casesFile   = flag.String("cases", "server/cmd/riskjudgebench/cases.json", "path to labeled cases JSON")
		runs        = flag.Int("runs", 1, "evaluations per (model,case)")
		concurrency = flag.Int("concurrency", 6, "max concurrent judge calls")
		temperature = flag.Float64("temperature", 0.0, "sampling temperature (judge default is 0)")
		timeout     = flag.Duration("timeout", 30*time.Second, "per-call timeout (prod judgeTimeout is 10s)")
		orgID       = flag.String("org", "5a25158b-24dc-4d49-b03d-e85acfbea59c", "OrgID label (default: speakeasy-team)")
		outFile     = flag.String("out", "server/cmd/riskjudgebench/results.json", "write raw per-call results here ('' to skip)")
	)
	flag.Parse()

	apiKey := firstEnv("OPENROUTER_DEV_KEY", "OPENROUTER_API_KEY")
	if apiKey == "" || apiKey == "unset" {
		fmt.Fprintln(os.Stderr, "error: set OPENROUTER_DEV_KEY (or OPENROUTER_API_KEY)")
		os.Exit(1)
	}

	cases, err := loadCases(*casesFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load cases: %v\n", err)
		os.Exit(1)
	}
	models := splitNonEmpty(*modelsFlag)

	// Build the real production client with stubbed org-scoped concerns.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	policy := guardian.NewDefaultPolicy(tracenoop.NewTracerProvider())
	client := openrouter.NewUnifiedClient(
		logger,
		policy,
		&devProvisioner{apiKey: apiKey}, // returns the dev key for any org
		nil,                             // message capture  (nil-guarded)
		nil,                             // usage tracking   (nil-guarded)
		nil,                             // chat title gen   (nil-guarded)
		nil,                             // telemetry logger (nil-guarded)
	)
	_ = metricnoop.NewMeterProvider() // (riskjudge.New would need this; we call the client directly)

	projectID := "00000000-0000-0000-0000-000000000001" // must parse as UUID
	fmt.Printf("models=%d  cases=%d  runs=%d  -> %d calls (real openrouter client, temp=%.1f, concurrency=%d)\n\n",
		len(models), len(cases), *runs, len(models)*len(cases)*(*runs), *temperature, *concurrency)

	type job struct {
		model string
		tc    testCase
	}
	var jobs []job
	for _, m := range models {
		for _, tc := range cases {
			for r := 0; r < *runs; r++ {
				jobs = append(jobs, job{model: m, tc: tc})
			}
		}
	}

	results := make([]result, len(jobs))
	sem := make(chan struct{}, *concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var done int

	start := time.Now()
	for i := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, j job) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = evaluate(client, j.model, *orgID, projectID, j.tc, *temperature, *timeout)
			mu.Lock()
			done++
			if done%20 == 0 || done == len(jobs) {
				fmt.Fprintf(os.Stderr, "\r  %d/%d calls done", done, len(jobs))
			}
			mu.Unlock()
		}(i, jobs[i])
	}
	wg.Wait()
	fmt.Fprintf(os.Stderr, "\r  %d/%d calls done (%.1fs)\n\n", len(jobs), len(jobs), time.Since(start).Seconds())

	report(models, results)

	if *outFile != "" {
		if err := writeJSON(*outFile, results); err != nil {
			fmt.Fprintf(os.Stderr, "warn: write %s: %v\n", *outFile, err)
		} else {
			fmt.Printf("\nraw per-call results written to %s\n", *outFile)
		}
	}
}

// evaluate issues one GetObjectCompletion call shaped exactly like
// riskjudge.Judge.call() and records the outcome.
func evaluate(client openrouter.CompletionClient, model, orgID, projectID string, tc testCase, temp float64, timeout time.Duration) result {
	res := result{Model: model, CaseID: tc.ID, Expected: tc.Expected}

	strict := true
	jsonSchema := or.ChatJSONSchemaConfig{
		Name:        "risk_policy_judge_verdict",
		Schema:      riskjudge.VerdictSchema(),
		Description: nil,
		Strict:      optionalnullable.From(&strict),
	}
	userMessage := riskjudge.BuildJudgePrompt(ra.JudgeInput{
		OrgID:     orgID,
		ProjectID: projectID,
		Prompt:    tc.Policy,
		Message:   judgemessage.New(tc.MessageType, tc.ToolName, tc.Text),
		Config:    ra.JudgeConfig{Model: "", Temperature: nil, FailOpen: true},
	})

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	t0 := time.Now()
	resp, err := client.GetObjectCompletion(ctx, openrouter.ObjectCompletionRequest{
		OrgID:          orgID,
		ProjectID:      projectID,
		Model:          model,
		SystemPrompt:   riskjudge.SystemPrompt,
		Prompt:         userMessage,
		Temperature:    &temp,
		UsageSource:    billing.ModelUsageSourceGram,
		UserID:         "",
		ExternalUserID: "",
		HTTPMetadata:   nil,
		JSONSchema:     &jsonSchema,
	})
	res.Latency = time.Since(t0)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	if resp == nil || resp.Message == nil {
		res.Err = "empty completion response"
		return res
	}
	res.Tokens = resp.Usage.TotalTokens
	raw := strings.TrimSpace(openrouter.GetText(*resp.Message))
	if raw == "" {
		res.Err = "empty completion content"
		return res
	}
	var v verdict
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		res.Err = fmt.Sprintf("parse verdict: %v", err)
		return res
	}
	res.Got = v.Matched
	res.Conf = v.Confidence
	return res
}

// --- prod-client stubs ---

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

// --- reporting ---

type modelStats struct {
	model                  string
	tp, fp, tn, fn, errors int
	latencies              []time.Duration
	tokens, tokenN         int
}

func report(models []string, results []result) {
	byModel := map[string]*modelStats{}
	for _, m := range models {
		byModel[m] = &modelStats{model: m}
	}
	for _, r := range results {
		s := byModel[r.Model]
		if s == nil {
			continue
		}
		if r.Tokens > 0 {
			s.tokens += r.Tokens
			s.tokenN++
		}
		if r.Err != "" {
			s.errors++
			continue
		}
		s.latencies = append(s.latencies, r.Latency)
		switch {
		case r.Expected && r.Got:
			s.tp++
		case !r.Expected && r.Got:
			s.fp++
		case !r.Expected && !r.Got:
			s.tn++
		case r.Expected && !r.Got:
			s.fn++
		}
	}

	stats := make([]*modelStats, 0, len(models))
	for _, m := range models {
		stats = append(stats, byModel[m])
	}
	sort.SliceStable(stats, func(i, j int) bool {
		fi, fj := f1(stats[i]), f1(stats[j])
		if fi != fj {
			return fi > fj
		}
		return p(stats[i].latencies, 50) < p(stats[j].latencies, 50)
	})

	fmt.Printf("%-32s %5s %5s %5s %5s  %6s %6s %6s  %7s %7s  %4s  %7s\n",
		"model", "TP", "FP", "TN", "FN", "acc", "prec", "rec", "p50ms", "p95ms", "err", "avgTok")
	fmt.Println(strings.Repeat("-", 110))
	for _, s := range stats {
		fmt.Printf("%-32s %5d %5d %5d %5d  %6.3f %6.3f %6.3f  %7.0f %7.0f  %4d  %7d\n",
			trunc(s.model, 32), s.tp, s.fp, s.tn, s.fn,
			accuracy(s), precision(s), recall(s),
			ms(p(s.latencies, 50)), ms(p(s.latencies, 95)),
			s.errors, avgTok(s))
	}

	fmt.Println("\nlegend: acc=accuracy prec=precision rec=recall (on matched=true); ranked by F1, tie-broken by p50.")
	fmt.Println("        reasoning is disabled by the object-completion path, so latency reflects the prod judge call.")

	confidenceSweep(models, results)

	fmt.Println("\nmisclassifications (first run per case):")
	seen := map[string]bool{}
	any := false
	for _, r := range results {
		key := r.Model + "|" + r.CaseID
		if seen[key] {
			continue
		}
		seen[key] = true
		if r.Err == "" && r.Expected != r.Got {
			kind := "FN(missed)"
			if r.Got {
				kind = "FP(false-alarm)"
			}
			fmt.Printf("  %-32s %-36s %-14s conf=%.2f\n", trunc(r.Model, 32), r.CaseID, kind, r.Conf)
			any = true
		}
	}
	if !any {
		fmt.Println("  (none)")
	}

	errSeen := map[string]bool{}
	first := true
	for _, r := range results {
		if r.Err == "" || errSeen[r.Model] {
			continue
		}
		errSeen[r.Model] = true
		if first {
			fmt.Println("\nerrors (first per model):")
			first = false
		}
		fmt.Printf("  %-32s %s\n", trunc(r.Model, 32), r.Err)
	}
}

// confidenceSweep shows what precision/recall/F1 would be if the judge gated a
// flag on the model's self-reported confidence — positive = matched && conf>=tau
// — instead of on `matched` alone. tau=0 reproduces the main table (and current
// prod behavior, judge.go:187). Reuses the already-collected per-call results,
// so it costs no extra API calls. Useful because some models are confidently
// wrong (FPs at conf=1.0, where no threshold helps) while others put their
// mistakes at low confidence (suppressible by raising tau).
func confidenceSweep(models []string, results []result) {
	taus := []float64{0.0, 0.5, 0.7, 0.8, 0.9, 0.95, 0.99}

	fmt.Println("\nconfidence-threshold sweep (positive = matched && conf>=tau; tau=0.00 = main table / current prod):")
	for _, m := range models {
		var mr []result
		errs := 0
		for _, r := range results {
			if r.Model != m {
				continue
			}
			mr = append(mr, r)
			if r.Err != "" {
				errs++
			}
		}
		if len(mr) == 0 || errs == len(mr) {
			fmt.Printf("\n  %s  (no scorable calls — skipped)\n", m)
			continue
		}
		fmt.Printf("\n  %s\n", m)
		fmt.Printf("    %5s %6s %6s %6s %6s  %3s %3s\n", "tau", "acc", "prec", "rec", "f1", "FP", "FN")
		for _, t := range taus {
			s := &modelStats{model: m}
			for _, r := range mr {
				if r.Err != "" {
					continue
				}
				pos := r.Got && r.Conf >= t
				switch {
				case r.Expected && pos:
					s.tp++
				case !r.Expected && pos:
					s.fp++
				case !r.Expected && !pos:
					s.tn++
				default:
					s.fn++
				}
			}
			fmt.Printf("    %5.2f %6.3f %6.3f %6.3f %6.3f  %3d %3d\n",
				t, accuracy(s), precision(s), recall(s), f1(s), s.fp, s.fn)
		}
	}
}

// --- metric helpers ---

func accuracy(s *modelStats) float64 {
	n := s.tp + s.fp + s.tn + s.fn
	if n == 0 {
		return 0
	}
	return float64(s.tp+s.tn) / float64(n)
}
func precision(s *modelStats) float64 {
	if d := s.tp + s.fp; d > 0 {
		return float64(s.tp) / float64(d)
	}
	return 0
}
func recall(s *modelStats) float64 {
	if d := s.tp + s.fn; d > 0 {
		return float64(s.tp) / float64(d)
	}
	return 0
}
func f1(s *modelStats) float64 {
	p, r := precision(s), recall(s)
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}
func avgTok(s *modelStats) int {
	if s.tokenN == 0 {
		return 0
	}
	return s.tokens / s.tokenN
}

func p(ds []time.Duration, q int) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), ds...)
	slices.Sort(cp)
	return cp[(q*(len(cp)-1))/100]
}
func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// --- small utilities ---

func loadCases(path string) ([]testCase, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cs []testCase
	if err := json.Unmarshal(b, &cs); err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return nil, fmt.Errorf("no cases in %s", path)
	}
	// Fail fast on a bad message_type: an unrecognized value would silently
	// render as opaque content (changing what the judge sees) instead of the
	// intended actor/tool framing.
	for _, c := range cs {
		if c.MessageType != "" && !message.IsTypeValid(c.MessageType) {
			return nil, fmt.Errorf("case %q: invalid message_type %q (want one of %v or empty)", c.ID, c.MessageType, message.AllTypes())
		}
	}
	return cs, nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func splitNonEmpty(s string) []string {
	var out []string
	for p := range strings.SplitSeq(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
