// Command skillsuggestbench measures the production skill-suggestion generator
// against a labeled corpus and exits nonzero below its gates.
//
// The generator is graded mechanically rather than by a second model. Every
// check here is one the production pipeline already performs, so a case that
// passes is a suggestion that would have reached a reviewer:
//
//   - the decision (propose or decline) matches the label;
//   - every change's "find" text is locatable exactly once, which is what
//     ResolveChanges demands before a change can be applied;
//   - the resolved manifest survives ValidateSkillSuggestion;
//   - each change cites evidence that actually indexes the feedback it was given.
//
// The anchor check is the interesting one. A change is a find/replace against
// the manifest, so the model has to reproduce document text verbatim and
// uniquely. A model that paraphrases instead of copying fails the whole
// suggestion, and that failure is invisible in unit tests because they stub the
// completion client.
package main

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/suggest"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	defaultCasesFile = "server/cmd/skillsuggestbench/cases.json"
	defaultOutFile   = "server/cmd/skillsuggestbench/results.json"
	benchOrgID       = "00000000-0000-0000-0000-000000000001"
	benchProjectID   = "00000000-0000-0000-0000-000000000002"
)

type benchSet struct {
	Model string `json:"model"`
	// MinimumApplyRate gates the share of proposals whose changes all located
	// cleanly and validated. This is the number that decides whether a model can
	// drive the feature at all.
	MinimumApplyRate float64 `json:"minimum_apply_rate"`
	// MinimumDecisionAccuracy gates how often the model proposes when there is a
	// gap and declines when there is not.
	MinimumDecisionAccuracy float64 `json:"minimum_decision_accuracy"`
	// MinimumEvidenceRate gates the share of served changes that cite feedback.
	// Evidence is the point of the feature, so a model that writes plausible
	// edits without citing anything must not win on apply rate alone.
	MinimumEvidenceRate float64    `json:"minimum_evidence_rate"`
	Cases               []testCase `json:"cases"`
}

type feedbackItem struct {
	Outcome string `json:"outcome"`
	Note    string `json:"note"`
	Source  string `json:"source"`
}

type testCase struct {
	ID             string         `json:"id"`
	SkillName      string         `json:"skill_name"`
	SkillContent   string         `json:"skill_content"`
	Feedback       []feedbackItem `json:"feedback"`
	ExpectDecision string         `json:"expect_decision"`
	// ExpectMinChanges is the number of distinct edits the feedback justifies.
	// Only meaningful when the expected decision is propose.
	ExpectMinChanges int    `json:"expect_min_changes"`
	Note             string `json:"note"`
}

type result struct {
	Model         string        `json:"model"`
	CaseID        string        `json:"case_id"`
	Run           int           `json:"run"`
	WantDecision  string        `json:"want_decision"`
	GotDecision   string        `json:"got_decision"`
	Changes       int           `json:"changes"`
	WantChanges   int           `json:"want_changes"`
	Applied       bool          `json:"applied"`
	Corrected     bool          `json:"corrected,omitempty"`
	Validated     bool          `json:"validated"`
	EvidenceCited int           `json:"evidence_cited"`
	Latency       time.Duration `json:"latency"`
	Tokens        int           `json:"tokens"`
	CostUSD       *float64      `json:"cost_usd,omitempty"`
	Failure       string        `json:"failure,omitempty"`
	Error         string        `json:"error,omitempty"`
}

type modelSummary struct {
	Model            string
	Runs             int
	DecisionAccuracy float64
	ApplyRate        float64
	ValidateRate     float64
	EvidenceRate     float64
	ChangeShortfall  int
	Errors           int
	P50              time.Duration
	P95              time.Duration
	AverageTokens    float64
	AverageCostUSD   float64
}

func main() {
	modelsFlag := flag.String("models", "", "comma-separated allowlisted model ids (defaults to the corpus model)")
	casesFile := flag.String("cases", defaultCasesFile, "path to the labeled bench set")
	runs := flag.Int("runs", 3, "generations per model and case")
	concurrency := flag.Int("concurrency", 4, "maximum concurrent generator calls")
	timeout := flag.Duration("timeout", 120*time.Second, "per-call timeout")
	outFile := flag.String("out", defaultOutFile, "write per-call results here (empty to skip)")
	reasoningEffort := flag.String("reasoning-effort", "", "reasoning effort override; empty matches production, which disables reasoning. Routes that reject a disabled setting need an effort such as \"low\"")
	flag.Parse()

	// A route that refuses a disabled setting (Gemini 3.5+ answers "Reasoning is
	// mandatory for this endpoint") cannot be benched against production defaults
	// at all, so the effort is a knob rather than an assumption.
	var reasoning *openrouter.Reasoning
	if *reasoningEffort != "" {
		reasoning = &openrouter.Reasoning{Effort: *reasoningEffort, MaxTokens: nil, Exclude: nil, Enabled: nil}
	}

	set, err := loadBenchSet(*casesFile)
	if err != nil {
		exitf("load cases: %v", err)
	}
	if *runs <= 0 || *concurrency <= 0 || *timeout <= 0 {
		exitf("runs, concurrency, and timeout must be positive")
	}
	models := splitNonEmpty(*modelsFlag)
	if len(models) == 0 {
		models = []string{set.Model}
	}
	for _, model := range models {
		if !openrouter.IsModelAllowed(model) {
			exitf("model %q is not allowlisted", model)
		}
	}

	apiKey := firstEnv("OPENROUTER_DEV_KEY", "OPENROUTER_API_KEY")
	if apiKey == "" || apiKey == "unset" {
		exitf("set OPENROUTER_DEV_KEY or OPENROUTER_API_KEY")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	provisioner := openrouter.NewDevelopment(apiKey)
	client := openrouter.NewUncheckedUnifiedClient(
		logger,
		guardian.NewDefaultPolicy(tracenoop.NewTracerProvider()),
		provisioner,
		&openrouter.PlatformKeyResolver{Provisioner: provisioner},
		nil,
		nil,
		nil,
		nil,
	)

	results := runBench(client, set, models, *runs, *concurrency, *timeout, reasoning)

	passed := true
	for _, model := range models {
		summary := summarize(model, results)
		printSummary(summary, set)
		passed = passed && summary.ApplyRate >= set.MinimumApplyRate && summary.DecisionAccuracy >= set.MinimumDecisionAccuracy && summary.EvidenceRate >= set.MinimumEvidenceRate
	}
	printFailures(results)

	if *outFile != "" {
		if err := writeJSON(*outFile, results); err != nil {
			exitf("write results: %v", err)
		}
		fmt.Printf("results=%s\n", *outFile)
	}
	if !passed {
		os.Exit(1)
	}
}

func runBench(
	client openrouter.CompletionClient,
	set benchSet,
	models []string,
	runs, concurrency int,
	timeout time.Duration,
	reasoning *openrouter.Reasoning,
) []result {
	type job struct {
		model string
		tc    testCase
		run   int
	}
	jobs := make([]job, 0, len(models)*len(set.Cases)*runs)
	for _, model := range models {
		for _, tc := range set.Cases {
			for run := 1; run <= runs; run++ {
				jobs = append(jobs, job{model: model, tc: tc, run: run})
			}
		}
	}

	results := make([]result, len(jobs))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, job := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = evaluate(client, job.model, job.tc, job.run, timeout, reasoning)
		}()
	}
	wg.Wait()
	return results
}

func evaluate(
	client openrouter.CompletionClient,
	model string,
	tc testCase,
	run int,
	timeout time.Duration,
	reasoning *openrouter.Reasoning,
) result {
	res := result{
		Model:         model,
		CaseID:        tc.ID,
		Run:           run,
		WantDecision:  tc.ExpectDecision,
		GotDecision:   "",
		Changes:       0,
		WantChanges:   tc.ExpectMinChanges,
		Applied:       false,
		Corrected:     false,
		Validated:     false,
		EvidenceCited: 0,
		Latency:       0,
		Tokens:        0,
		CostUSD:       nil,
		Failure:       "",
		Error:         "",
	}

	generation, ok := generateOnce(client, model, tc, "", timeout, reasoning, &res)
	if !ok {
		return res
	}

	// Mirror Engine.Run: an invalid proposal gets exactly one correction round
	// with the validation error, a no-op becomes a decline, and a proposal that
	// stays invalid after correction is served as a decline. The bench judges
	// the decision production would serve, not the model's first draft.
	if generation.Decision == suggest.DecisionPropose {
		resolvedCount, failure := applyProposal(tc, generation)
		switch {
		case failure == nil:
			res.Applied = true
			res.Validated = true
			res.Changes = resolvedCount
		case errors.Is(failure, skills.ErrSkillSuggestionNoOp):
			generation = suggest.Generation{Decision: suggest.DecisionDecline, Changes: nil, Rationale: "no-op"}
		default:
			res.Corrected = true
			generation, ok = generateOnce(client, model, tc, failure.Error(), timeout, reasoning, &res)
			if !ok {
				return res
			}
			if generation.Decision == suggest.DecisionPropose {
				resolvedCount, failure = applyProposal(tc, generation)
				switch {
				case failure == nil:
					res.Applied = true
					res.Validated = true
					res.Changes = resolvedCount
				case errors.Is(failure, skills.ErrSkillSuggestionNoOp):
					generation = suggest.Generation{Decision: suggest.DecisionDecline, Changes: nil, Rationale: "no-op"}
				default:
					res.Failure = fmt.Sprintf("invalid after correction: %v", failure)
					generation = suggest.Generation{Decision: suggest.DecisionDecline, Changes: nil, Rationale: "discarded"}
				}
			}
		}
	}

	res.GotDecision = string(generation.Decision)
	res.Changes = 0
	res.EvidenceCited = 0
	if generation.Decision == suggest.DecisionPropose {
		res.Changes = len(generation.Changes)
		for _, change := range generation.Changes {
			if len(change.Evidence) > 0 {
				res.EvidenceCited++
			}
		}
	}
	return res
}

// generateOnce performs a single production-shaped generator call and folds
// its latency, tokens, and cost into the result. It reports false when the
// call or its validation failed, with the reason recorded on the result.
func generateOnce(
	client openrouter.CompletionClient,
	model string,
	tc testCase,
	validationError string,
	timeout time.Duration,
	reasoning *openrouter.Reasoning,
	res *result,
) (suggest.Generation, bool) {
	request, err := buildRequest(model, tc, validationError, reasoning)
	if err != nil {
		res.Error = fmt.Sprintf("invalid case: %v", err)
		return suggest.Generation{}, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	started := time.Now()
	response, err := client.GetObjectCompletion(ctx, request)
	res.Latency += time.Since(started)
	if err != nil {
		res.Error = fmt.Sprintf("completion failed: %v", err)
		return suggest.Generation{}, false
	}
	if response == nil || response.Message == nil {
		res.Error = "empty response"
		return suggest.Generation{}, false
	}
	res.Tokens += response.Usage.TotalTokens
	if response.Usage.Cost != nil {
		total := *response.Usage.Cost
		if res.CostUSD != nil {
			total += *res.CostUSD
		}
		res.CostUSD = &total
	}

	var generation suggest.Generation
	raw := strings.TrimSpace(openrouter.GetText(*response.Message))
	if err := json.Unmarshal([]byte(raw), &generation); err != nil {
		res.Failure = "response is not the declared schema"
		return suggest.Generation{}, false
	}
	if err := suggest.ValidateGeneration(&generation, len(tc.Feedback)); err != nil {
		res.Failure = fmt.Sprintf("invalid generation: %v", err)
		return suggest.Generation{}, false
	}
	return generation, true
}

// applyProposal anchors and validates a proposal exactly as resolveGeneration
// does in production, returning the number of applied changes.
func applyProposal(tc testCase, generation suggest.Generation) (int, error) {
	content, resolved, err := suggest.ResolveChanges(tc.SkillContent, generation.Changes)
	if err != nil {
		return 0, fmt.Errorf("anchor: %w", err)
	}
	baseSHA, err := canonicalSHA(tc.SkillContent, tc.SkillName)
	if err != nil {
		return 0, fmt.Errorf("invalid case manifest: %w", err)
	}
	if _, err := skills.ValidateSkillSuggestion(content, tc.SkillName, baseSHA); err != nil {
		return 0, fmt.Errorf("validate: %w", err)
	}
	return len(resolved), nil
}

// canonicalSHA returns the canonical digest of a manifest by validating it
// against a digest it cannot equal, which is the only exported route to the
// parser. The bench needs it so the no-op check in ValidateSkillSuggestion is
// exercised exactly as production exercises it.
func canonicalSHA(content, name string) (string, error) {
	validated, err := skills.ValidateSkillSuggestion(content, name, "")
	if err != nil {
		return "", err
	}
	return validated.CanonicalSHA256, nil
}

func buildRequest(model string, tc testCase, validationError string, reasoning *openrouter.Reasoning) (openrouter.ObjectCompletionRequest, error) {
	projectID, err := uuid.Parse(benchProjectID)
	if err != nil {
		return openrouter.ObjectCompletionRequest{}, fmt.Errorf("parse bench project id: %w", err)
	}

	feedback := make([]repo.SkillFeedback, len(tc.Feedback))
	for i, item := range tc.Feedback {
		feedback[i] = repo.SkillFeedback{
			ID:             uuid.Nil,
			ProjectID:      projectID,
			SkillID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			SkillVersionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			SkillName:      tc.SkillName,
			Source:         item.Source,
			Outcome:        item.Outcome,
			Note:           pgtype.Text{String: item.Note, Valid: item.Note != ""},
			SessionID:      pgtype.Text{String: fmt.Sprintf("session-%d", i+1), Valid: true},
			UserID:         pgtype.Text{String: "", Valid: false},
			UserEmail:      pgtype.Text{String: "", Valid: false},
			ReviewedAt:     pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
			CreatedAt:      pgtype.Timestamptz{Time: time.Unix(int64(1750000000+i*3600), 0).UTC(), InfinityModifier: 0, Valid: true},
		}
	}

	prompt, err := suggest.BuildPrompt(suggest.DefaultConfig(), suggest.GenerateInput{
		OrganizationID: benchOrgID,
		ProjectID:      projectID,
		SkillName:      tc.SkillName,
		Base: repo.ResolveSkillSuggestionBaseRow{
			BaseVersionID:              uuid.Nil,
			BaseFloorReferenceAt:       pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
			BaseContent:                tc.SkillContent,
			BaseCanonicalSha256:        "",
			PredecessorVersionID:       uuid.Nil,
			PredecessorContent:         "",
			PredecessorCanonicalSha256: "",
		},
		Feedback: feedback,
		Trend: suggest.Trend{
			CurrentCount:    uint64(len(tc.Feedback)),
			CurrentAverage:  0,
			PreviousCount:   0,
			PreviousAverage: 0,
			AbsoluteDelta:   0,
			Comparable:      false,
			Regression:      false,
		},
		Transcripts:     nil,
		ValidationError: validationError,
	})
	if err != nil {
		return openrouter.ObjectCompletionRequest{}, fmt.Errorf("build prompt: %w", err)
	}

	strict := true
	schema := or.ChatJSONSchemaConfig{
		Name:        "skill_edit_suggestion",
		Schema:      suggest.GenerationSchema(),
		Description: nil,
		Strict:      optionalnullable.From(&strict),
	}
	temperature := 0.0
	return openrouter.ObjectCompletionRequest{
		OrgID:          benchOrgID,
		ProjectID:      benchProjectID,
		Model:          model,
		SystemPrompt:   suggest.SystemPrompt,
		Prompt:         string(prompt),
		Temperature:    &temperature,
		UsageSource:    billing.ModelUsageSourceSkillSuggestions,
		UserID:         "",
		ExternalUserID: "",
		UserEmail:      "",
		HTTPMetadata:   nil,
		JSONSchema:     &schema,
		KeyType:        openrouter.KeyTypeInternal,
		KeySlot:        billing.ModelUsageSourceSkillSuggestions,
		Reasoning:      reasoning,
	}, nil
}

func summarize(model string, results []result) modelSummary {
	summary := modelSummary{
		Model: model, Runs: 0, DecisionAccuracy: 0, ApplyRate: 0, ValidateRate: 0,
		EvidenceRate: 0, ChangeShortfall: 0, Errors: 0, P50: 0, P95: 0,
		AverageTokens: 0, AverageCostUSD: 0,
	}
	var latencies []time.Duration
	var decisionHits, proposeRuns, applied, validated, changes, evidence, tokens int
	var cost float64

	// Every scheduled run stays in every denominator. A run that errored is a
	// run production would have failed, so it counts as a miss in both gates
	// rather than shrinking them.
	for _, res := range results {
		if res.Model != model {
			continue
		}
		summary.Runs++
		if res.WantDecision == string(suggest.DecisionPropose) {
			proposeRuns++
			if res.Changes < res.WantChanges {
				summary.ChangeShortfall++
			}
		}
		if res.Error != "" {
			summary.Errors++
			continue
		}
		latencies = append(latencies, res.Latency)
		tokens += res.Tokens
		if res.CostUSD != nil {
			cost += *res.CostUSD
		}
		if res.GotDecision == res.WantDecision {
			decisionHits++
		}
		// Apply is only meaningful where the label demands an edit: a decline on
		// a propose-labeled case is a failure to produce the edit, and a decline
		// on a decline-labeled case has nothing to apply.
		if res.WantDecision == string(suggest.DecisionPropose) {
			if res.Applied && res.Validated {
				applied++
			}
			if res.Validated {
				validated++
			}
		}
		changes += res.Changes
		evidence += res.EvidenceCited
	}

	if summary.Runs > 0 {
		summary.DecisionAccuracy = float64(decisionHits) / float64(summary.Runs)
	}
	if proposeRuns > 0 {
		summary.ApplyRate = float64(applied) / float64(proposeRuns)
		summary.ValidateRate = float64(validated) / float64(proposeRuns)
	}
	scored := summary.Runs - summary.Errors
	if scored > 0 {
		summary.AverageTokens = float64(tokens) / float64(scored)
		summary.AverageCostUSD = cost / float64(scored)
	}
	if changes > 0 {
		summary.EvidenceRate = float64(evidence) / float64(changes)
	}
	if len(latencies) > 0 {
		slices.Sort(latencies)
		summary.P50 = latencies[len(latencies)*50/100]
		summary.P95 = latencies[min(len(latencies)*95/100, len(latencies)-1)]
	}
	return summary
}

func printSummary(summary modelSummary, set benchSet) {
	verdict := "PASS"
	if summary.ApplyRate < set.MinimumApplyRate || summary.DecisionAccuracy < set.MinimumDecisionAccuracy || summary.EvidenceRate < set.MinimumEvidenceRate {
		verdict = "FAIL"
	}
	fmt.Printf(
		"%s model=%s apply=%.1f%% (gate=%.0f%%) decision=%.1f%% (gate=%.0f%%) validate=%.1f%% evidence=%.1f%% (gate=%.0f%%) shortfall=%d errors=%d p50=%s p95=%s avg_tokens=%.0f avg_cost_usd=%.6f\n",
		verdict, summary.Model,
		summary.ApplyRate*100, set.MinimumApplyRate*100,
		summary.DecisionAccuracy*100, set.MinimumDecisionAccuracy*100,
		summary.ValidateRate*100, summary.EvidenceRate*100, set.MinimumEvidenceRate*100,
		summary.ChangeShortfall, summary.Errors,
		summary.P50.Round(time.Millisecond), summary.P95.Round(time.Millisecond),
		summary.AverageTokens, summary.AverageCostUSD,
	)
}

// printFailures lists what actually went wrong, deduplicated. An apply rate on
// its own does not say whether a model paraphrased an anchor or picked one that
// occurs twice, and those call for different fixes.
func printFailures(results []result) {
	seen := map[string]int{}
	for _, res := range results {
		switch {
		case res.Error != "":
			seen[fmt.Sprintf("  %-34s %-28s error: %s", res.Model, res.CaseID, res.Error)]++
		case res.Failure != "":
			seen[fmt.Sprintf("  %-34s %-28s %s", res.Model, res.CaseID, res.Failure)]++
		case res.GotDecision != res.WantDecision:
			seen[fmt.Sprintf("  %-34s %-28s decision %s, wanted %s", res.Model, res.CaseID, res.GotDecision, res.WantDecision)]++
		case res.WantDecision == string(suggest.DecisionPropose) && res.Changes < res.WantChanges:
			seen[fmt.Sprintf("  %-34s %-28s %d changes, wanted %d", res.Model, res.CaseID, res.Changes, res.WantChanges)]++
		}
	}
	if len(seen) == 0 {
		return
	}
	lines := make([]string, 0, len(seen))
	for line, count := range seen {
		lines = append(lines, fmt.Sprintf("%s (x%d)", line, count))
	}
	sort.Strings(lines)
	fmt.Println("\nfailures:")
	for _, line := range lines {
		fmt.Println(line)
	}
}

func loadBenchSet(path string) (benchSet, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return benchSet{}, fmt.Errorf("read cases: %w", err)
	}
	var set benchSet
	if err := json.Unmarshal(raw, &set); err != nil {
		return benchSet{}, fmt.Errorf("decode cases: %w", err)
	}
	if len(set.Cases) == 0 {
		return benchSet{}, errors.New("bench set has no cases")
	}
	if set.MinimumApplyRate <= 0 || set.MinimumApplyRate > 1 {
		return benchSet{}, errors.New("minimum_apply_rate must be greater than 0 and at most 1")
	}
	if set.MinimumDecisionAccuracy <= 0 || set.MinimumDecisionAccuracy > 1 {
		return benchSet{}, errors.New("minimum_decision_accuracy must be greater than 0 and at most 1")
	}
	if set.MinimumEvidenceRate <= 0 || set.MinimumEvidenceRate > 1 {
		return benchSet{}, errors.New("minimum_evidence_rate must be greater than 0 and at most 1")
	}
	if !openrouter.IsModelAllowed(set.Model) {
		return benchSet{}, fmt.Errorf("corpus model %q is not allowlisted", set.Model)
	}
	for _, tc := range set.Cases {
		switch tc.ExpectDecision {
		case string(suggest.DecisionPropose), string(suggest.DecisionDecline):
		default:
			return benchSet{}, fmt.Errorf("case %q has invalid expect_decision %q", tc.ID, tc.ExpectDecision)
		}
		if len(tc.Feedback) == 0 {
			return benchSet{}, fmt.Errorf("case %q has no feedback", tc.ID)
		}
		// A corpus manifest that does not parse would fail every model equally
		// and read as a model regression, so it is caught at load time.
		if _, err := canonicalSHA(tc.SkillContent, tc.SkillName); err != nil {
			return benchSet{}, fmt.Errorf("case %q has an invalid manifest: %w", tc.ID, err)
		}
	}
	return set, nil
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode results: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write results: %w", err)
	}
	return nil
}

func splitNonEmpty(value string) []string {
	out := make([]string, 0)
	for part := range strings.SplitSeq(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(2)
}
