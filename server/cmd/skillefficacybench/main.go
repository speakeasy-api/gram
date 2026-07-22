// Command skillefficacybench measures the production skill-efficacy judge
// against a synthetic labeled corpus and exits nonzero below its beta gate.
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
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	defaultCasesFile = "server/cmd/skillefficacybench/cases.json"
	defaultOutFile   = "server/cmd/skillefficacybench/results.json"
	benchOrgID       = "00000000-0000-0000-0000-000000000001"
	benchProjectID   = "00000000-0000-0000-0000-000000000002"
)

type benchSet struct {
	JudgePromptVersion string     `json:"judge_prompt_version"`
	JudgeModel         string     `json:"judge_model"`
	MinimumAgreement   float64    `json:"minimum_agreement"`
	Cases              []testCase `json:"cases"`
}

type testCase struct {
	ID           string              `json:"id"`
	SkillName    string              `json:"skill_name"`
	SkillContent string              `json:"skill_content"`
	Surface      string              `json:"surface"`
	ActivatedAt  time.Time           `json:"activated_at"`
	Transcript   efficacy.Transcript `json:"transcript"`
	ScoreMin     float64             `json:"score_min"`
	ScoreMax     float64             `json:"score_max"`
	Note         string              `json:"note"`
}

type result struct {
	RequestedModel string        `json:"requested_model"`
	ActualModel    string        `json:"actual_model,omitempty"`
	PromptVersion  string        `json:"prompt_version"`
	CaseID         string        `json:"case_id"`
	Run            int           `json:"run"`
	ScoreMin       float64       `json:"score_min"`
	ScoreMax       float64       `json:"score_max"`
	Score          *float64      `json:"score,omitempty"`
	Latency        time.Duration `json:"latency"`
	Tokens         int           `json:"tokens"`
	CostUSD        *float64      `json:"cost_usd,omitempty"`
	Error          string        `json:"error,omitempty"`
}

type modelSummary struct {
	Model          string
	Cases          int
	AgreedCases    int
	Agreement      float64
	RunAgreement   float64
	MeanBandDrift  float64
	Errors         int
	P50            time.Duration
	P95            time.Duration
	AverageTokens  float64
	AverageCostUSD float64
}

func main() {
	modelsFlag := flag.String("models", "", "comma-separated allowlisted model ids (defaults to the corpus model)")
	casesFile := flag.String("cases", defaultCasesFile, "path to the labeled bench set")
	runs := flag.Int("runs", 3, "evaluations per model and case")
	concurrency := flag.Int("concurrency", 4, "maximum concurrent judge calls")
	timeout := flag.Duration("timeout", 60*time.Second, "per-call timeout")
	baselineFile := flag.String("baseline", "", "prior results JSON used to report per-case score drift")
	outFile := flag.String("out", defaultOutFile, "write sanitized per-call results here (empty to skip)")
	flag.Parse()

	set, err := loadBenchSet(*casesFile)
	if err != nil {
		exitf("load cases: %v", err)
	}
	if *runs <= 0 || *concurrency <= 0 || *timeout <= 0 {
		exitf("runs, concurrency, and timeout must be positive")
	}
	models := splitNonEmpty(*modelsFlag)
	if len(models) == 0 {
		models = []string{set.JudgeModel}
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
	client := openrouter.NewUnifiedClient(
		logger,
		guardian.NewDefaultPolicy(tracenoop.NewTracerProvider()),
		provisioner,
		&openrouter.PlatformKeyResolver{Provisioner: provisioner},
		nil,
		nil,
		nil,
		nil,
	)

	results := runBench(client, set, models, *runs, *concurrency, *timeout)
	passed := true
	for _, model := range models {
		summary := summarize(set, model, results)
		printSummary(summary, set.MinimumAgreement)
		passed = passed && summary.Agreement >= set.MinimumAgreement
	}

	if *baselineFile != "" {
		baseline, err := loadBaseline(*baselineFile)
		if err != nil {
			exitf("load baseline: %v", err)
		}
		printDrift(results, baseline)
	}
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

func runBench(client openrouter.CompletionClient, set benchSet, models []string, runs, concurrency int, timeout time.Duration) []result {
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
			results[i] = evaluate(client, job.model, job.tc, job.run, timeout)
		}()
	}
	wg.Wait()
	return results
}

func evaluate(client openrouter.CompletionClient, model string, tc testCase, run int, timeout time.Duration) result {
	res := result{
		RequestedModel: model,
		ActualModel:    "",
		PromptVersion:  efficacy.JudgePromptVersion,
		CaseID:         tc.ID,
		Run:            run,
		ScoreMin:       tc.ScoreMin,
		ScoreMax:       tc.ScoreMax,
		Score:          nil,
		Latency:        0,
		Tokens:         0,
		CostUSD:        nil,
		Error:          "",
	}
	request, err := buildRequest(model, tc)
	if err != nil {
		res.Error = "invalid case"
		return res
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	started := time.Now()
	response, err := client.GetObjectCompletion(ctx, request)
	res.Latency = time.Since(started)
	if err != nil {
		res.Error = fmt.Sprintf("completion failed: %v", err)
		return res
	}
	if response == nil || response.Message == nil {
		res.Error = "empty completion"
		return res
	}
	res.ActualModel = response.Model
	res.Tokens = response.Usage.TotalTokens
	res.CostUSD = response.Usage.Cost
	verdict, err := efficacy.ParseVerdict(strings.TrimSpace(openrouter.GetText(*response.Message)))
	if err != nil {
		res.Error = fmt.Sprintf("invalid verdict: %v", err)
		return res
	}
	res.Score = &verdict.Score
	return res
}

func buildRequest(model string, tc testCase) (openrouter.ObjectCompletionRequest, error) {
	prompt, err := efficacy.BuildJudgePrompt(efficacy.JudgeInput{
		OrgID:        benchOrgID,
		ProjectID:    benchProjectID,
		SkillName:    tc.SkillName,
		SkillURN:     "",
		SkillContent: tc.SkillContent,
		Surface:      tc.Surface,
		ActivatedAt:  tc.ActivatedAt,
		Transcript:   tc.Transcript,
	})
	if err != nil {
		return openrouter.ObjectCompletionRequest{}, err
	}

	strict := true
	schema := or.ChatJSONSchemaConfig{
		Name:        "skill_efficacy_verdict",
		Schema:      efficacy.VerdictSchema(),
		Description: nil,
		Strict:      optionalnullable.From(&strict),
	}
	temperature := 0.0
	return openrouter.ObjectCompletionRequest{
		OrgID:          benchOrgID,
		ProjectID:      benchProjectID,
		Model:          model,
		SystemPrompt:   efficacy.SystemPrompt,
		Prompt:         prompt,
		Temperature:    &temperature,
		UsageSource:    billing.ModelUsageSourceSkillEfficacy,
		UserID:         "",
		ExternalUserID: "",
		UserEmail:      "",
		HTTPMetadata:   nil,
		JSONSchema:     &schema,
		KeyType:        openrouter.KeyTypeInternal,
		KeySlot:        billing.ModelUsageSourceSkillEfficacy,
	}, nil
}

func loadBenchSet(path string) (benchSet, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return benchSet{}, err
	}
	var set benchSet
	if err := json.Unmarshal(b, &set); err != nil {
		return benchSet{}, err
	}
	if set.JudgePromptVersion != efficacy.JudgePromptVersion {
		return benchSet{}, fmt.Errorf("corpus prompt version %q does not match production %q", set.JudgePromptVersion, efficacy.JudgePromptVersion)
	}
	if !openrouter.IsModelAllowed(set.JudgeModel) {
		return benchSet{}, fmt.Errorf("corpus model %q is not allowlisted", set.JudgeModel)
	}
	if set.MinimumAgreement <= 0 || set.MinimumAgreement > 1 {
		return benchSet{}, errors.New("minimum_agreement must be greater than 0 and at most 1")
	}
	if len(set.Cases) == 0 {
		return benchSet{}, errors.New("cases must not be empty")
	}
	seen := make(map[string]struct{}, len(set.Cases))
	for _, tc := range set.Cases {
		if tc.ID == "" || tc.SkillName == "" || tc.SkillContent == "" || tc.ActivatedAt.IsZero() || len(tc.Transcript.Messages) == 0 {
			return benchSet{}, fmt.Errorf("case %q is missing required input", tc.ID)
		}
		if tc.Surface != "dev" && tc.Surface != "assistant" {
			return benchSet{}, fmt.Errorf("case %q has invalid surface %q", tc.ID, tc.Surface)
		}
		if tc.ScoreMin < 0 || tc.ScoreMax > 1 || tc.ScoreMin > tc.ScoreMax {
			return benchSet{}, fmt.Errorf("case %q has invalid score band", tc.ID)
		}
		if _, ok := seen[tc.ID]; ok {
			return benchSet{}, fmt.Errorf("duplicate case id %q", tc.ID)
		}
		seen[tc.ID] = struct{}{}
	}
	return set, nil
}

func summarize(set benchSet, model string, results []result) modelSummary {
	byCase := make(map[string][]result, len(set.Cases))
	latencies := make([]time.Duration, 0)
	totalTokens := 0
	totalCost := 0.0
	costs := 0
	errorsCount := 0
	runsInBand := 0
	modelRuns := 0
	totalBandDrift := 0.0
	for _, res := range results {
		if res.RequestedModel != model {
			continue
		}
		modelRuns++
		byCase[res.CaseID] = append(byCase[res.CaseID], res)
		latencies = append(latencies, res.Latency)
		if res.Error != "" || res.Score == nil {
			errorsCount++
			continue
		}
		totalTokens += res.Tokens
		if res.CostUSD != nil {
			totalCost += *res.CostUSD
			costs++
		}
		drift := distanceFromBand(*res.Score, res.ScoreMin, res.ScoreMax)
		totalBandDrift += drift
		if drift == 0 {
			runsInBand++
		}
	}

	agreedCases := 0
	for _, tc := range set.Cases {
		caseResults := byCase[tc.ID]
		scores := make([]float64, 0, len(caseResults))
		for _, res := range caseResults {
			if res.Score == nil || res.Error != "" {
				continue
			}
			scores = append(scores, *res.Score)
		}
		if len(scores) > 0 && distanceFromBand(median(scores), tc.ScoreMin, tc.ScoreMax) == 0 {
			agreedCases++
		}
	}

	successes := modelRuns - errorsCount
	return modelSummary{
		Model:          model,
		Cases:          len(set.Cases),
		AgreedCases:    agreedCases,
		Agreement:      divide(agreedCases, len(set.Cases)),
		RunAgreement:   divide(runsInBand, modelRuns),
		MeanBandDrift:  divideFloat(totalBandDrift, successes),
		Errors:         errorsCount,
		P50:            percentile(latencies, 0.50),
		P95:            percentile(latencies, 0.95),
		AverageTokens:  divideFloat(float64(totalTokens), successes),
		AverageCostUSD: divideFloat(totalCost, costs),
	}
}

func printSummary(summary modelSummary, minimum float64) {
	status := "PASS"
	if summary.Agreement < minimum {
		status = "FAIL"
	}
	fmt.Printf("%s model=%s prompt=%s agreement=%.1f%% (%d/%d, gate=%.0f%%) run_agreement=%.1f%% mean_band_drift=%.3f errors=%d p50=%s p95=%s avg_tokens=%.0f avg_cost_usd=%.6f\n",
		status,
		summary.Model,
		efficacy.JudgePromptVersion,
		summary.Agreement*100,
		summary.AgreedCases,
		summary.Cases,
		minimum*100,
		summary.RunAgreement*100,
		summary.MeanBandDrift,
		summary.Errors,
		summary.P50.Round(time.Millisecond),
		summary.P95.Round(time.Millisecond),
		summary.AverageTokens,
		summary.AverageCostUSD,
	)
}

func printDrift(current, baseline []result) {
	baselineMeans := caseMeans(baseline, "")
	baselineModel := "unknown"
	if len(baseline) > 0 {
		baselineModel = baseline[0].RequestedModel
	}
	models := make([]string, 0)
	for _, res := range current {
		if !slices.Contains(models, res.RequestedModel) {
			models = append(models, res.RequestedModel)
		}
	}
	sort.Strings(models)
	for _, model := range models {
		currentMeans := caseMeans(current, model)
		caseIDs := make([]string, 0, len(currentMeans))
		for caseID := range currentMeans {
			if _, ok := baselineMeans[caseID]; ok {
				caseIDs = append(caseIDs, caseID)
			}
		}
		sort.Strings(caseIDs)
		for _, caseID := range caseIDs {
			fmt.Printf("drift baseline_model=%s model=%s case=%s delta=%+.3f\n", baselineModel, model, caseID, currentMeans[caseID]-baselineMeans[caseID])
		}
	}
}

func caseMeans(results []result, model string) map[string]float64 {
	totals := map[string]float64{}
	counts := map[string]int{}
	for _, res := range results {
		if model != "" && res.RequestedModel != model || res.Score == nil || res.Error != "" {
			continue
		}
		totals[res.CaseID] += *res.Score
		counts[res.CaseID]++
	}
	for caseID, total := range totals {
		totals[caseID] = total / float64(counts[caseID])
	}
	return totals
}

func loadResults(path string) ([]result, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var results []result
	if err := json.Unmarshal(b, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func loadBaseline(path string) ([]result, error) {
	results, err := loadResults(path)
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, 1)
	for _, res := range results {
		if res.RequestedModel != "" && !slices.Contains(models, res.RequestedModel) {
			models = append(models, res.RequestedModel)
		}
	}
	if len(models) != 1 {
		return nil, fmt.Errorf("baseline must contain exactly one requested model, found %d", len(models))
	}
	return results, nil
}

func writeJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o600)
}

func splitNonEmpty(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" && !slices.Contains(result, part) {
			result = append(result, part)
		}
	}
	return result
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func distanceFromBand(score, minimum, maximum float64) float64 {
	if score < minimum {
		return minimum - score
	}
	if score > maximum {
		return score - maximum
	}
	return 0
}

func median(values []float64) float64 {
	values = slices.Clone(values)
	sort.Float64s(values)
	mid := len(values) / 2
	if len(values)%2 == 0 {
		return (values[mid-1] + values[mid]) / 2
	}
	return values[mid]
}

func percentile(values []time.Duration, fraction float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	values = slices.Clone(values)
	slices.Sort(values)
	index := int(float64(len(values)-1) * fraction)
	return values[index]
}

func divide(numerator, denominator int) float64 {
	return divideFloat(float64(numerator), denominator)
}

func divideFloat(numerator float64, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / float64(denominator)
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(2)
}
