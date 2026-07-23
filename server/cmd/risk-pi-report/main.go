package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/google/uuid"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/recommendedscopes"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	defaultCorpusDir = "server/internal/scanners/promptinjection/testdata/prompt_injection"
	defaultOutFile   = "server/risk_accuracy_metrics.json"
)

var emptyTypedVerdict = piopenrouter.Verdict{
	DirectiveKind: "",
	Target:        "",
	Operational:   false,
	Rationale:     "",
}

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
	Type                   string         `json:"type,omitempty"`       // message.Type; default user_message
	Tool                   string         `json:"tool,omitempty"`       // tool name for a single-tool tool_request/tool_response
	ToolCalls              []toolCallCase `json:"tool_calls,omitempty"` // multi-call tool_request
	PriorUserRequest       string         `json:"prior_user_request,omitempty"`
	RecentUntrustedContent string         `json:"recent_untrusted_content,omitempty"`
	DirectivePresent       *bool          `json:"directive_present,omitempty"`
	KnownGap               string         `json:"known_gap,omitempty"`
	SeedID                 string         `json:"seed_id,omitempty"`
}

func (c labeledCase) trajectory() judgemessage.Trajectory {
	return judgemessage.Trajectory{
		PriorUserRequest:       c.PriorUserRequest,
		RecentUntrustedContent: c.RecentUntrustedContent,
	}
}

// caseType returns the message type for a case, defaulting to user_message.
func (c labeledCase) caseType() message.Type {
	if c.Type == "" {
		return message.User
	}
	return c.Type
}

// judgeMessage renders a case as the judgemessage the judge evaluates,
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
	FPRateMax         float64            `json:"fp_rate_max"`
	RecallFloor       *float64           `json:"recall_floor"`
	RecallBySourceMin map[string]float64 `json:"recall_by_source_min"`
	LastUpdated       string             `json:"last_updated"`
	LastUpdatedBy     string             `json:"last_updated_by"`
	Notes             string             `json:"notes"`
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
	Evaluation               evaluationStats `json:"evaluation"`
}

type evaluationStats struct {
	PhysicalCalls        int     `json:"physical_calls"`
	Errors               int     `json:"errors"`
	Timeouts             int     `json:"timeouts"`
	Malformed            int     `json:"malformed"`
	FailOpenEvents       int     `json:"fail_open_events"`
	PromptTokens         int     `json:"prompt_tokens"`
	CompletionTokens     int     `json:"completion_tokens"`
	CostUSD              float64 `json:"cost_usd"`
	CallsOver10Seconds   int     `json:"calls_over_10_seconds"`
	CallLatencyP50MS     float64 `json:"call_latency_p50_ms"`
	CallLatencyP95MS     float64 `json:"call_latency_p95_ms"`
	CallLatencyP99MS     float64 `json:"call_latency_p99_ms"`
	DecisionLatencyP50MS float64 `json:"decision_latency_p50_ms"`
	DecisionLatencyP95MS float64 `json:"decision_latency_p95_ms"`
	DecisionLatencyP99MS float64 `json:"decision_latency_p99_ms"`
}

type accuracySummary struct {
	Total          int                 `json:"total"`
	Counts         counts              `json:"counts"`
	Overall        metricsBlock        `json:"overall"`
	Sources        []sourceSummary     `json:"by_source"`
	Rules          []ruleHist          `json:"by_rule"`
	Modes          []modeSummary       `json:"modes,omitempty"`
	Stability      stabilitySummary    `json:"stability"`
	Distributions  distributionSummary `json:"distributions"`
	KnownGaps      []knownGapSummary   `json:"known_gaps,omitempty"`
	RecallGate     recallGateSummary   `json:"recall_gate"`
	RecallGateRuns []recallGateSummary `json:"recall_gate_runs"`
}

type recallGateSummary struct {
	Scope    string          `json:"scope"`
	Counts   counts          `json:"counts"`
	Recall   float64         `json:"recall"`
	BySource []sourceSummary `json:"by_source"`
	Excluded int             `json:"known_gaps_excluded"`
}

type stabilitySummary struct {
	Repeats                 int            `json:"repeats"`
	StablePositive          int            `json:"stable_positive"`
	StableNegative          int            `json:"stable_negative"`
	Flipped                 int            `json:"flipped"`
	FlipRate                float64        `json:"flip_rate"`
	StableFalsePositives    int            `json:"stable_false_positives"`
	FlippedBenign           int            `json:"flipped_benign"`
	FlipsAndStableFalseCore []stabilityRow `json:"flips_and_stable_false_positive_core,omitempty"`
}

type stabilityRow struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	Label        string `json:"label"`
	PositiveRuns int    `json:"positive_runs"`
	Outcome      string `json:"outcome"`
}

type distribution struct {
	Min    float64 `json:"min"`
	Median float64 `json:"median"`
	Max    float64 `json:"max"`
	Mean   float64 `json:"mean"`
	StdDev float64 `json:"stddev"`
}

type distributionSummary struct {
	FalsePositiveRate distribution `json:"false_positive_rate"`
	Recall            distribution `json:"recall"`
	CostUSD           distribution `json:"cost_usd"`
}

type knownGapSummary struct {
	ID     string `json:"id"`
	Issue  string `json:"issue"`
	Reason string `json:"reason"`
}

type envelope struct {
	GitSHA          string          `json:"git_sha"`
	Ref             string          `json:"ref"`
	Timestamp       string          `json:"timestamp"`
	Model           string          `json:"model"`
	Reasoning       string          `json:"reasoning"`
	ProviderRoute   string          `json:"provider_route"`
	SamplesPerEvent int             `json:"samples_per_event"`
	TimeoutMS       int64           `json:"timeout_ms"`
	PromptSHA256    string          `json:"prompt_sha256"`
	SchemaSHA256    string          `json:"schema_sha256"`
	CorpusSHA256    string          `json:"corpus_sha256"`
	Summary         accuracySummary `json:"summary"`
}

type options struct {
	corpusDir        string
	outFile          string
	checkFloors      bool
	judgeModel       string
	judgeConcurrency int
	sources          string
	reasoning        string
	extraCorpus      string
	repeats          int
	samples          int
}

const (
	// defaultJudgeModel is the report's judge model when none is provided.
	defaultJudgeModel = piopenrouter.DefaultModel
	// judgeConcurrency bounds concurrent logical events without turning the
	// benchmark into a provider load test.
	defaultJudgeConcurrency = 4
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
		judgeModel:       "",
		judgeConcurrency: 0,
		sources:          "",
		reasoning:        "",
		extraCorpus:      "",
		repeats:          0,
		samples:          0,
	}
	flag.StringVar(&opts.corpusDir, "corpus-dir", defaultCorpusDir, "directory containing prompt-injection JSONL corpus files")
	flag.StringVar(&opts.outFile, "out", defaultOutFile, "path to write metrics JSON")
	flag.BoolVar(&opts.checkFloors, "check-floors", true, "fail if judge metrics violate floors.json")
	flag.StringVar(&opts.judgeModel, "judge-model", defaultJudgeModel, "OpenRouter model id for the judge (must be allowlisted)")
	flag.IntVar(&opts.judgeConcurrency, "judge-concurrency", defaultJudgeConcurrency, "max concurrent logical events")
	flag.StringVar(&opts.sources, "sources", "", "comma-separated source substrings to keep (empty = all); use to judge a cheap iteration slice")
	flag.StringVar(&opts.reasoning, "reasoning", piopenrouter.DefaultReasoningEffort, "OpenRouter reasoning effort for the judge call")
	flag.StringVar(&opts.extraCorpus, "extra-corpus", "", "absolute path to an additional local JSONL corpus; never loaded by default")
	flag.IntVar(&opts.repeats, "repeats", 1, "number of complete repeated trials")
	flag.IntVar(&opts.samples, "samples", piopenrouter.SamplesPerEvent, "physical judge calls per event; production defaults to one")
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
	corpus, err := loadCorpus(opts.corpusDir, opts.extraCorpus)
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

	// Scope-aware modes: apply the candidate policy scope (scopes.json) as a
	// pre-filter, so the report shows the FP reduction from scoping AND flags any
	// malicious case the scope would stop scanning (coverage regression).
	scopeCfg, hasScope, err := loadScopes(opts.corpusDir)
	if err != nil {
		return err
	}
	var scope ra.CompiledScope
	if hasScope {
		scope, err = compileScope(scopeCfg)
		if err != nil {
			return err
		}
	}

	if opts.repeats < 1 {
		return fmt.Errorf("--repeats must be at least 1")
	}
	if opts.samples < 1 {
		return fmt.Errorf("--samples must be at least 1")
	}
	modes := make([]modeSummary, 0, opts.repeats*2)
	allFindings := make([][][]scanners.Finding, 0, opts.repeats)
	for repeat := 1; repeat <= opts.repeats; repeat++ {
		fmt.Fprintf(os.Stderr, "trial %d/%d\n", repeat, opts.repeats)
		judgeMode, judgeFindings, err := scanJudgeMode(ctx, opts, corpus)
		if err != nil {
			return err
		}
		judgeMode.Name = fmt.Sprintf("judge_run_%d", repeat)
		modes = append(modes, judgeMode)
		allFindings = append(allFindings, judgeFindings)
		if hasScope && scope.Active() {
			modes = append(modes, scopedMode(fmt.Sprintf("scoped_run_%d", repeat), corpus, judgeFindings, scope))
		}
	}
	judgeMode := modes[0]
	recallGateRuns := make([]recallGateSummary, len(allFindings))
	for i, findings := range allFindings {
		recallGateRuns[i] = summarizeRecallGate(corpus, findings)
	}
	worstRecallGate := worstRecallGate(recallGateRuns)

	summary := accuracySummary{
		Total:          judgeMode.Total,
		Counts:         judgeMode.Counts,
		Overall:        judgeMode.Overall,
		Sources:        judgeMode.Sources,
		Rules:          judgeMode.Rules,
		Modes:          modes,
		Stability:      summarizeStability(corpus, allFindings),
		Distributions:  summarizeDistributions(modes, recallGateRuns),
		KnownGaps:      summarizeKnownGaps(corpus),
		RecallGate:     worstRecallGate,
		RecallGateRuns: recallGateRuns,
	}

	printSummary(os.Stderr, modes)
	fmt.Fprintf(os.Stderr, "stability: flips=%d/%d (%.2f%%) stable_fp=%d benign_flips=%d\n",
		summary.Stability.Flipped, summary.Total, summary.Stability.FlipRate*100,
		summary.Stability.StableFalsePositives, summary.Stability.FlippedBenign)
	for i, gate := range summary.RecallGateRuns {
		fmt.Fprintf(os.Stderr, "recall gate run %d: TP=%d FN=%d recall=%.3f known_gaps_excluded=%d\n",
			i+1, gate.Counts.TP, gate.Counts.FN, gate.Recall, gate.Excluded)
		for _, source := range gate.BySource {
			fmt.Fprintf(os.Stderr, "  %-24s TP=%-4d FN=%-4d recall=%.3f\n",
				source.Source, source.Counts.TP, source.Counts.FN, source.Metrics.Recall)
		}
	}

	if opts.checkFloors {
		if err := checkRecallFloors(fl, summary.RecallGateRuns); err != nil {
			return err
		}
	}

	return writeMetrics(opts.outFile, opts, corpus, summary)
}

func checkRecallFloors(fl floors, runs []recallGateSummary) error {
	for runIndex, gate := range runs {
		presentSources := make(map[string]struct{}, len(gate.BySource))
		for _, source := range gate.BySource {
			presentSources[source.Source] = struct{}{}
		}
		fullGateSuite := len(fl.RecallBySourceMin) > 0
		for source := range fl.RecallBySourceMin {
			if _, ok := presentSources[source]; !ok {
				fullGateSuite = false
				break
			}
		}
		if fl.RecallFloor != nil && fullGateSuite && gate.Counts.TP+gate.Counts.FN > 0 && gate.Recall < *fl.RecallFloor {
			return fmt.Errorf(
				"judge recall %.4f in run %d is below floor %.4f (floors.json last updated %s by %s)",
				gate.Recall,
				runIndex+1,
				*fl.RecallFloor,
				fl.LastUpdated,
				fl.LastUpdatedBy,
			)
		}
		for _, source := range gate.BySource {
			floor, ok := fl.RecallBySourceMin[source.Source]
			if !ok || source.Counts.TP+source.Counts.FN == 0 || source.Metrics.Recall >= floor {
				continue
			}
			return fmt.Errorf(
				"judge recall %.4f for source %s in run %d is below floor %.4f (floors.json last updated %s by %s)",
				source.Metrics.Recall,
				source.Source,
				runIndex+1,
				floor,
				fl.LastUpdated,
				fl.LastUpdatedBy,
			)
		}
	}
	return nil
}

func summarizeStability(corpus []labeledCase, runs [][][]scanners.Finding) stabilitySummary {
	out := stabilitySummary{
		Repeats: len(runs), StablePositive: 0, StableNegative: 0, Flipped: 0,
		FlipRate: 0, StableFalsePositives: 0, FlippedBenign: 0, FlipsAndStableFalseCore: nil,
	}
	if len(runs) == 0 {
		return out
	}
	for i := range runs[0] {
		positives := 0
		for _, run := range runs {
			if len(run[i]) > 0 {
				positives++
			}
		}
		switch {
		case positives == len(runs):
			out.StablePositive++
			if corpus[i].Label == "benign" {
				out.StableFalsePositives++
				out.FlipsAndStableFalseCore = append(out.FlipsAndStableFalseCore, stabilityRow{
					ID: corpus[i].ID, Source: corpus[i].Source, Label: corpus[i].Label,
					PositiveRuns: positives, Outcome: "stable_false_positive",
				})
			}
		case positives == 0:
			out.StableNegative++
		default:
			out.Flipped++
			if corpus[i].Label == "benign" {
				out.FlippedBenign++
			}
			out.FlipsAndStableFalseCore = append(out.FlipsAndStableFalseCore, stabilityRow{
				ID: corpus[i].ID, Source: corpus[i].Source, Label: corpus[i].Label,
				PositiveRuns: positives, Outcome: "flipped",
			})
		}
	}
	out.FlipRate = safeDiv(out.Flipped, len(corpus))
	return out
}

func summarizeDistributions(modes []modeSummary, recallGates []recallGateSummary) distributionSummary {
	var falsePositiveRates, recalls, costs []float64
	for _, mode := range modes {
		if strings.HasPrefix(mode.Name, "scoped_") {
			continue
		}
		falsePositiveRates = append(falsePositiveRates, mode.Overall.FPRate)
		costs = append(costs, mode.Evaluation.CostUSD)
	}
	for _, gate := range recallGates {
		if gate.Counts.TP+gate.Counts.FN > 0 {
			recalls = append(recalls, gate.Recall)
		}
	}
	return distributionSummary{
		FalsePositiveRate: describeDistribution(falsePositiveRates),
		Recall:            describeDistribution(recalls),
		CostUSD:           describeDistribution(costs),
	}
}

func worstRecallGate(runs []recallGateSummary) recallGateSummary {
	if len(runs) == 0 {
		return recallGateSummary{
			Scope:  "explicit directive-present, in-taxonomy malicious rows; AGE-3048 known gaps excluded",
			Counts: counts{TP: 0, FP: 0, TN: 0, FN: 0}, Recall: 0, BySource: nil, Excluded: 0,
		}
	}
	worst := runs[0]
	for _, run := range runs[1:] {
		if run.Recall < worst.Recall {
			worst = run
		}
	}
	return worst
}

func describeDistribution(values []float64) distribution {
	if len(values) == 0 {
		return distribution{Min: 0, Median: 0, Max: 0, Mean: 0, StdDev: 0}
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	var sum float64
	for _, value := range sorted {
		sum += value
	}
	mean := sum / float64(len(sorted))
	var squaredDiffs float64
	for _, value := range sorted {
		diff := value - mean
		squaredDiffs += diff * diff
	}
	median := sorted[len(sorted)/2]
	if len(sorted)%2 == 0 {
		median = (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
	}
	return distribution{
		Min: sorted[0], Median: median, Max: sorted[len(sorted)-1],
		Mean: mean, StdDev: math.Sqrt(squaredDiffs / float64(len(sorted))),
	}
}

func summarizeKnownGaps(corpus []labeledCase) []knownGapSummary {
	var gaps []knownGapSummary
	for _, c := range corpus {
		if c.KnownGap == "" {
			continue
		}
		gaps = append(gaps, knownGapSummary{ID: c.ID, Issue: "AGE-3048", Reason: c.KnownGap})
	}
	return gaps
}

func summarizeRecallGate(corpus []labeledCase, findings [][]scanners.Finding) recallGateSummary {
	out := recallGateSummary{
		Scope:  "explicit directive-present, in-taxonomy malicious rows; AGE-3048 known gaps excluded",
		Counts: counts{TP: 0, FP: 0, TN: 0, FN: 0},
		Recall: 0, BySource: nil, Excluded: 0,
	}
	bySource := map[string]*counts{}
	for i, c := range corpus {
		if !directivePresentForGate(c) || c.Label != "malicious" {
			continue
		}
		if c.KnownGap != "" {
			out.Excluded++
			continue
		}
		source := recallGateSource(c.Source)
		bucket := bySource[source]
		if bucket == nil {
			bucket = &counts{TP: 0, FP: 0, TN: 0, FN: 0}
			bySource[source] = bucket
		}
		if len(findings[i]) > 0 {
			out.Counts.TP++
			bucket.TP++
		} else {
			out.Counts.FN++
			bucket.FN++
		}
	}
	out.Recall = safeDiv(out.Counts.TP, out.Counts.TP+out.Counts.FN)
	for source, c := range bySource {
		out.BySource = append(out.BySource, sourceSummary{Source: source, Counts: *c, Metrics: deriveMetrics(*c)})
	}
	sort.Slice(out.BySource, func(i, j int) bool { return out.BySource[i].Source < out.BySource[j].Source })
	return out
}

// directivePresentForGate limits recall to curated, in-taxonomy fixtures. The
// external deepset labels intentionally remain outside this gate because that
// corpus includes generic harmful or task-changing text that the typed PI
// contract correctly classifies as non-PI.
func directivePresentForGate(c labeledCase) bool {
	return c.DirectivePresent != nil && *c.DirectivePresent
}

func recallGateSource(source string) string {
	if strings.HasPrefix(source, "mutation:") {
		return "mutations"
	}
	return source
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
		if m.Evaluation.PhysicalCalls > 0 {
			p("             calls=%d errors=%d fail_open=%d over_10s=%d latency_ms[p50=%.0f p95=%.0f p99=%.0f] tokens[prompt=%d completion=%d] cost=$%.6f\n",
				m.Evaluation.PhysicalCalls, m.Evaluation.Errors, m.Evaluation.FailOpenEvents,
				m.Evaluation.CallsOver10Seconds,
				m.Evaluation.DecisionLatencyP50MS, m.Evaluation.DecisionLatencyP95MS, m.Evaluation.DecisionLatencyP99MS,
				m.Evaluation.PromptTokens, m.Evaluation.CompletionTokens, m.Evaluation.CostUSD)
		}
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
	"agent_fp_ais324.jsonl",
	"adversarial_ais324.jsonl",
	"trajectory_twins.jsonl",
}

func loadCorpus(dir, extraCorpus string) ([]labeledCase, error) {
	seen := map[string]string{}
	var out []labeledCase

	load := func(path string, optional, dedupe bool) error {
		name := filepath.Base(path)
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
			if dedupe {
				if _, dup := seen[c.Text]; dup {
					continue
				}
				seen[c.Text] = c.ID
			}
			out = append(out, c)
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("scan %s: %w", path, err)
		}
		return nil
	}

	// A separately supplied corpus is an evaluation population. Preserve every
	// occurrence and load it first so fixture dedupe cannot hide repeated events.
	if extraCorpus != "" {
		if !filepath.IsAbs(extraCorpus) {
			return nil, fmt.Errorf("--extra-corpus must be an absolute path")
		}
		if err := load(extraCorpus, false, false); err != nil {
			return nil, err
		}
	}
	for _, name := range requiredCorpusFiles {
		if err := load(filepath.Join(dir, name), false, true); err != nil {
			return nil, err
		}
	}
	for _, name := range optionalCorpusFiles {
		// Paired trajectory rows intentionally share current-event text. Preserve
		// those semantics; the committed merge is deduped across source corpora.
		dedupe := name != "trajectory_twins.jsonl"
		if err := load(filepath.Join(dir, name), true, dedupe); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("loaded corpus is empty")
	}
	if err := resolveDirectivePresence(out); err != nil {
		return nil, err
	}
	return out, nil
}

func resolveDirectivePresence(corpus []labeledCase) error {
	byID := make(map[string][]int, len(corpus))
	for i, c := range corpus {
		byID[c.ID] = append(byID[c.ID], i)
	}

	const (
		unvisited = iota
		visiting
		resolved
	)
	state := make([]int, len(corpus))
	var resolve func(int) error
	resolve = func(i int) error {
		if state[i] == resolved {
			return nil
		}
		if state[i] == visiting {
			return fmt.Errorf("directive-present seed cycle at %q", corpus[i].ID)
		}
		state[i] = visiting
		if corpus[i].SeedID != "" {
			seedMatches := byID[corpus[i].SeedID]
			if len(seedMatches) == 0 {
				return fmt.Errorf("corpus row %q has unknown seed_id %q", corpus[i].ID, corpus[i].SeedID)
			}
			if len(seedMatches) > 1 {
				return fmt.Errorf("corpus row %q has ambiguous seed_id %q", corpus[i].ID, corpus[i].SeedID)
			}
			seedIndex := seedMatches[0]
			if err := resolve(seedIndex); err != nil {
				return err
			}
			if corpus[seedIndex].DirectivePresent == nil {
				return fmt.Errorf("corpus seed %q has no directive_present annotation", corpus[i].SeedID)
			}
			value := *corpus[seedIndex].DirectivePresent
			corpus[i].DirectivePresent = &value
		}
		state[i] = resolved
		return nil
	}
	for i := range corpus {
		if err := resolve(i); err != nil {
			return err
		}
	}
	return nil
}

type scopeConfig struct {
	ScopeInclude string `json:"scope_include"`
	ScopeExempt  string `json:"scope_exempt"`
}

// loadScopes returns the candidate policy scope: the scopes.json fixture in
// the corpus dir when present (an experimental override), otherwise the
// production recommended scope for the prompt_injection category — so the
// harness validates exactly what ships in the registry.
func loadScopes(dir string) (cfg scopeConfig, present bool, err error) {
	raw, err := os.ReadFile(filepath.Join(dir, "scopes.json")) // #nosec G304 -- local harness corpus path.
	if errors.Is(err, os.ErrNotExist) {
		rec, ok := recommendedscopes.For(categories.CategoryPromptInjection)
		if !ok {
			return scopeConfig{ScopeInclude: "", ScopeExempt: ""}, false, nil
		}
		return scopeConfig{ScopeInclude: rec.ScopeInclude, ScopeExempt: rec.ScopeExempt}, true, nil
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

// scanJudgeMode evaluates the LLM judge over the corpus.
// It calls GetObjectCompletion directly (the same request piopenrouter builds, minus
// the engine's per-org rate limiter and fail-open), so the numbers reflect the
// model's raw accuracy on every case rather than a throttled subset.
func scanJudgeMode(ctx context.Context, opts options, corpus []labeledCase) (modeSummary, [][]scanners.Finding, error) {
	apiKey := firstEnv("OPENROUTER_DEV_KEY", "OPENROUTER_API_KEY")
	if apiKey == "" || apiKey == "unset" {
		return modeSummary{}, nil, fmt.Errorf("OPENROUTER_DEV_KEY not set")
	}

	fmt.Fprintf(os.Stderr, "judging %d cases with %s (concurrency=%d)\n", len(corpus), opts.judgeModel, opts.judgeConcurrency)
	findings, eval, err := scanJudge(ctx, opts, newOpenRouterClient(apiKey), corpus)
	if err != nil {
		return modeSummary{}, nil, err
	}

	mode := summarizeFindings("judge", corpus, findings)
	mode.Evaluation = eval
	empty := make([][]scanners.Finding, len(corpus))
	mode.NewFalsePositives = changedExamples(corpus, empty, findings, "benign", 500)
	mode.RecoveredTruePositive = changedExamples(corpus, empty, findings, "malicious", 500)
	mode.MissedAttacks = missedExamples(corpus, findings, 500)
	return mode, findings, nil
}

// scanJudge runs the judge for every corpus row and records positive verdicts.
type callObservation struct {
	Latency          time.Duration
	PromptTokens     int
	CompletionTokens int
	CostUSD          float64
	Err              error
}

type decisionObservation struct {
	Calls   []callObservation
	Latency time.Duration
}

func scanJudge(ctx context.Context, opts options, client openrouter.CompletionClient, corpus []labeledCase) ([][]scanners.Finding, evaluationStats, error) {
	out := make([][]scanners.Finding, len(corpus))
	ruleID, description := promptinjection.Describe()

	sem := make(chan struct{}, opts.judgeConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var done int
	observations := make([]decisionObservation, len(corpus))

	for i := range corpus {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()

			text := corpus[i].Text
			msg := corpus[i].judgeMessage()
			result, observation := judgeOne(ctx, client, opts.judgeModel, opts.reasoning, opts.samples, msg, corpus[i].trajectory())

			mu.Lock()
			defer mu.Unlock()
			done++
			if done%20 == 0 || done == len(corpus) {
				fmt.Fprintf(os.Stderr, "\r  judge %d/%d", done, len(corpus))
			}
			observations[i] = observation
			if !result.IsInjection {
				return
			}
			out[i] = append(out[i], scanners.Finding{
				RuleID:           ruleID,
				Description:      description,
				Match:            text,
				StartPos:         0,
				EndPos:           len(text),
				Source:           promptinjection.Source,
				Confidence:       0,
				Tags:             []string{"llm-judge", "layer-1", "semantic-typed", "directive_kind:" + result.DirectiveKind, "target:" + result.Target, "operational:true"},
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
	return out, summarizeEvaluation(observations), nil
}

func summarizeEvaluation(observations []decisionObservation) evaluationStats {
	var stats evaluationStats
	var callLatencies, decisionLatencies []time.Duration
	for _, observation := range observations {
		decisionLatencies = append(decisionLatencies, observation.Latency)
		eventErrors := 0
		for _, call := range observation.Calls {
			stats.PhysicalCalls++
			stats.PromptTokens += call.PromptTokens
			stats.CompletionTokens += call.CompletionTokens
			stats.CostUSD += call.CostUSD
			callLatencies = append(callLatencies, call.Latency)
			if call.Latency > 10*time.Second {
				stats.CallsOver10Seconds++
			}
			if call.Err == nil {
				continue
			}
			stats.Errors++
			eventErrors++
			if errors.Is(call.Err, context.DeadlineExceeded) {
				stats.Timeouts++
			}
			if strings.Contains(call.Err.Error(), "parse judge response") {
				stats.Malformed++
			}
		}
		if eventErrors > 0 {
			stats.FailOpenEvents++
		}
	}
	stats.CallLatencyP50MS = durationQuantileMS(callLatencies, 0.50)
	stats.CallLatencyP95MS = durationQuantileMS(callLatencies, 0.95)
	stats.CallLatencyP99MS = durationQuantileMS(callLatencies, 0.99)
	stats.DecisionLatencyP50MS = durationQuantileMS(decisionLatencies, 0.50)
	stats.DecisionLatencyP95MS = durationQuantileMS(decisionLatencies, 0.95)
	stats.DecisionLatencyP99MS = durationQuantileMS(decisionLatencies, 0.99)
	return stats
}

func durationQuantileMS(values []time.Duration, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	slices.Sort(values)
	index := int(float64(len(values)-1) * q)
	return float64(values[index]) / float64(time.Millisecond)
}

// judgeOne uses the production typed prompt and schema. The production default
// is a direct single call; optional multi-sample runs share one event deadline.
func judgeOne(ctx context.Context, client openrouter.CompletionClient, model, reasoning string, samples int, msg judgemessage.Message, trajectory judgemessage.Trajectory) (piopenrouter.Stabilized, decisionObservation) {
	decisionCtx, cancel := context.WithTimeout(ctx, piopenrouter.JudgeTimeout)
	defer cancel()
	start := time.Now()

	verdicts := make([]piopenrouter.Verdict, samples)
	observation := decisionObservation{Calls: make([]callObservation, samples), Latency: 0}
	var wg sync.WaitGroup
	for sample := range samples {
		wg.Go(func() {
			verdicts[sample], observation.Calls[sample] = judgeVote(decisionCtx, client, model, reasoning, msg, trajectory)
		})
	}
	wg.Wait()
	observation.Latency = time.Since(start)

	if samples == 1 {
		return piopenrouter.StabilizeSingle(verdicts[0]), observation
	}
	return piopenrouter.Aggregate(verdicts), observation
}

func judgeVote(ctx context.Context, client openrouter.CompletionClient, model, reasoning string, msg judgemessage.Message, trajectory judgemessage.Trajectory) (piopenrouter.Verdict, callObservation) {
	var trajectoryPayload *judgemessage.TrajectoryPayload
	if trajectory.HasContent() {
		rendered := judgemessage.RenderTrajectory(trajectory)
		trajectoryPayload = &rendered
	}
	payload, err := json.Marshal(struct {
		Message    judgemessage.Payload            `json:"message"`
		Trajectory *judgemessage.TrajectoryPayload `json:"trajectory,omitempty"`
	}{Message: judgemessage.RenderPayload(msg), Trajectory: trajectoryPayload})
	if err != nil {
		return emptyTypedVerdict, callObservation{Latency: 0, PromptTokens: 0, CompletionTokens: 0, CostUSD: 0, Err: fmt.Errorf("marshal judge payload: %w", err)}
	}

	strict := true
	schema := or.ChatJSONSchemaConfig{
		Name:        "prompt_attack_verdict",
		Schema:      piopenrouter.VerdictSchema(),
		Description: nil,
		Strict:      optionalnullable.From(&strict),
	}
	temp := 0.0
	messages := []or.ChatMessages{
		piopenrouter.RedesignedSystemMessage(),
		or.CreateChatMessagesUser(or.ChatUserMessage{Role: or.ChatUserMessageRoleUser, Content: or.CreateChatUserMessageContentStr(string(payload)), Name: nil}),
	}

	start := time.Now()
	resp, err := client.GetCompletion(ctx, openrouter.CompletionRequest{
		OrgID: benchOrgID, ProjectID: benchProjectID, Model: model, Messages: messages,
		Temperature: &temp, UsageSource: billing.ModelUsageSourceGram, KeyType: openrouter.KeyTypeInternal,
		KeySlot: "", ChatID: uuid.Nil, UserID: "", ExternalUserID: "", UserEmail: "",
		HTTPMetadata: nil, APIKeyID: "", Tools: nil, Stream: false, JSONSchema: &schema,
		Reasoning:    &openrouter.Reasoning{Effort: reasoning, MaxTokens: nil, Exclude: nil, Enabled: nil},
		CacheControl: nil, NormalizeOutboundMessages: false,
	})
	observation := callObservation{Latency: time.Since(start), PromptTokens: 0, CompletionTokens: 0, CostUSD: 0, Err: nil}
	if err != nil {
		observation.Err = fmt.Errorf("openrouter completion: %w", err)
		return emptyTypedVerdict, observation
	}
	if resp == nil || resp.Message == nil {
		observation.Err = fmt.Errorf("empty completion response")
		return emptyTypedVerdict, observation
	}
	observation.PromptTokens = resp.Usage.PromptTokens
	observation.CompletionTokens = resp.Usage.CompletionTokens
	if resp.Usage.Cost != nil {
		observation.CostUSD = *resp.Usage.Cost
	}
	raw := strings.TrimSpace(openrouter.GetText(*resp.Message))
	if raw == "" {
		observation.Err = fmt.Errorf("empty completion content")
		return emptyTypedVerdict, observation
	}
	var verdict piopenrouter.Verdict
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil {
		observation.Err = fmt.Errorf("parse judge response: %w", err)
		return emptyTypedVerdict, observation
	}
	if !piopenrouter.ValidVerdict(verdict) {
		observation.Err = fmt.Errorf("parse judge response: typed verdict contract is invalid")
		return emptyTypedVerdict, observation
	}
	return verdict, observation
}

// newOpenRouterClient builds the real production OpenRouter client with the
// org-scoped concerns stubbed: a dev-key provisioner, and nil capture/usage/
// title/telemetry strategies (all nil-guarded). Same construction as
// riskjudgebench, so the bench runs under prod-equivalent conditions.
func newOpenRouterClient(apiKey string) openrouter.CompletionClient {
	logger := slog.New(slog.DiscardHandler)
	policy := guardian.NewDefaultPolicy(tracenoop.NewTracerProvider())
	prov := &devProvisioner{apiKey: apiKey}
	return openrouter.NewUnifiedClient(
		logger,
		policy,
		prov,
		&openrouter.PlatformKeyResolver{Provisioner: prov},
		nil, // message capture  (nil-guarded)
		nil, // usage tracking   (nil-guarded)
		nil, // chat title gen   (nil-guarded)
		nil, // telemetry logger (nil-guarded)
	)
}

// devProvisioner satisfies openrouter.Provisioner but skips the DB/billing path:
// it hands back the dev key for every org.
type devProvisioner struct{ apiKey string }

func (d *devProvisioner) ProvisionAPIKey(_ context.Context, _ string, _ openrouter.KeyType) (string, error) {
	return d.apiKey, nil
}
func (d *devProvisioner) RefreshAPIKeyLimit(_ context.Context, _ string, _ openrouter.KeyType, _ *int) (int, error) {
	return 0, fmt.Errorf("not implemented in bench")
}
func (d *devProvisioner) GetCreditsUsed(_ context.Context, _ string, _ openrouter.KeyType) (float64, int, error) {
	return 0, 0, fmt.Errorf("not implemented in bench")
}
func (d *devProvisioner) GetKeyUsage(_ context.Context, _ string) (float64, *int64, error) {
	return 0, nil, fmt.Errorf("not implemented in bench")
}
func (d *devProvisioner) ReconcileMonthlyCredits(_ context.Context, _ string, _ openrouter.KeyType, currentLimit int64, _ *int64) (int64, error) {
	return currentLimit, nil
}
func (d *devProvisioner) GetModelUsage(_ context.Context, _ string, _ string, _ openrouter.KeyType) (*openrouter.ModelUsage, error) {
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
	var evaluation evaluationStats

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
		Evaluation:               evaluation,
	}
}

// changedExamples lists rows of the given label that the baseline missed.
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

// missedExamples lists malicious cases the candidate did not flag.
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

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func writeMetrics(path string, opts options, corpus []labeledCase, summary accuracySummary) error {
	schemaJSON, err := json.Marshal(piopenrouter.VerdictSchema())
	if err != nil {
		return fmt.Errorf("marshal verdict schema for hash: %w", err)
	}
	corpusJSON, err := json.Marshal(corpus)
	if err != nil {
		return fmt.Errorf("marshal corpus for hash: %w", err)
	}
	promptHash := sha256.Sum256([]byte(piopenrouter.SystemPrompt))
	schemaHash := sha256.Sum256(schemaJSON)
	corpusHash := sha256.Sum256(corpusJSON)
	payload := envelope{
		GitSHA:          envOr("GITHUB_SHA", "local"),
		Ref:             envOr("GITHUB_REF_NAME", "local"),
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		Model:           opts.judgeModel,
		Reasoning:       opts.reasoning,
		ProviderRoute:   "OpenRouter default routing",
		SamplesPerEvent: opts.samples,
		TimeoutMS:       piopenrouter.JudgeTimeout.Milliseconds(),
		PromptSHA256:    fmt.Sprintf("%x", promptHash),
		SchemaSHA256:    fmt.Sprintf("%x", schemaHash),
		CorpusSHA256:    fmt.Sprintf("%x", corpusHash),
		Summary:         summary,
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
