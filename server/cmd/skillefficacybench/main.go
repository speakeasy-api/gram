// Command skillefficacybench measures the production skill-efficacy judge
// against a synthetic labeled corpus and exits nonzero below its beta gate.
package main

import (
	"bytes"
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
	"github.com/speakeasy-api/gram/server/internal/skills"
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
	JudgePromptVersion             string               `json:"judge_prompt_version"`
	JudgeModel                     string               `json:"judge_model"`
	MinimumAgreement               float64              `json:"minimum_agreement"`
	RecommendationSchemaVersion    int                  `json:"recommendation_schema_version"`
	MinimumRecommendationAgreement float64              `json:"minimum_recommendation_agreement"`
	Cases                          []testCase           `json:"cases"`
	RecommendationPairs            []recommendationPair `json:"recommendation_pairs,omitempty"`
}

type expectedRecommendation struct {
	AcceptableOutcomes     []skills.FeedbackOutcome `json:"acceptable_outcomes"`
	PersistenceEligible    bool                     `json:"persistence_eligible"`
	AcceptableIssueTypes   []string                 `json:"acceptable_issue_types"`
	AcceptableChangeTypes  []string                 `json:"acceptable_change_types"`
	AllowedEvidenceIndices []int                    `json:"allowed_evidence_indices"`
	RequiredEvidenceGroups [][]int                  `json:"required_evidence_groups"`
}

type actualRecommendation struct {
	Outcome                string `json:"outcome"`
	Note                   string `json:"-"`
	Confidence             string `json:"confidence"`
	IssueType              string `json:"issue_type"`
	ChangeType             string `json:"change_type"`
	EvidenceMessageIndices []int  `json:"evidence_message_indices"`
}

type caseFields struct {
	SkillName    string                `json:"skill_name"`
	SkillContent string                `json:"skill_content"`
	Surface      skills.FeedbackSource `json:"surface"`
	ActivatedAt  time.Time             `json:"activated_at"`
	ScoreMin     float64               `json:"score_min"`
	ScoreMax     float64               `json:"score_max"`
	Note         string                `json:"note"`
}

type testCase struct {
	caseFields
	ID                      string                   `json:"id"`
	Transcript              efficacy.Transcript      `json:"transcript"`
	PairID                  string                   `json:"pair_id,omitempty"`
	PairVariant             string                   `json:"pair_variant,omitempty"`
	ExpectedRecommendations []expectedRecommendation `json:"expected_recommendations"`
}

type recommendationPair struct {
	caseFields
	ID                     string                     `json:"id"`
	Transcript             efficacy.Transcript        `json:"transcript"`
	ExpectedRecommendation expectedRecommendation     `json:"expected_recommendation"`
	BPrescription          efficacy.TranscriptMessage `json:"b_prescription"`
}

func (tc testCase) recommendationOnly() bool {
	return tc.PairID != ""
}

type recommendationGrade struct {
	ExpectedCount                 int  `json:"expected_count"`
	ActualCount                   int  `json:"actual_count"`
	PresenceOK                    bool `json:"presence_ok"`
	CountOK                       bool `json:"count_ok"`
	OutcomeOK                     bool `json:"outcome_ok"`
	PersistenceOK                 bool `json:"persistence_ok"`
	IssueOK                       bool `json:"issue_ok"`
	ChangeOK                      bool `json:"change_ok"`
	EvidenceOK                    bool `json:"evidence_ok"`
	ExactOK                       bool `json:"exact_ok"`
	MatchedCount                  int  `json:"matched_count"`
	OutcomeCorrectCount           int  `json:"outcome_correct_count"`
	PersistenceCorrectCount       int  `json:"persistence_correct_count"`
	IssueCorrectCount             int  `json:"issue_correct_count"`
	ChangeCorrectCount            int  `json:"change_correct_count"`
	EvidenceCorrectCount          int  `json:"evidence_correct_count"`
	EvidenceGroupsMatched         int  `json:"evidence_groups_matched"`
	ExpectedEvidenceGroups        int  `json:"expected_evidence_groups"`
	UnmatchedExpected             int  `json:"unmatched_expected"`
	UnmatchedActual               int  `json:"unmatched_actual"`
	HighConfidenceCount           int  `json:"high_confidence_count"`
	EligibleHighConfidenceMatches int  `json:"eligible_high_confidence_matches"`
	PersistenceEligibleExpected   int  `json:"persistence_eligible_expected"`
	UnexpectedHighConfidenceCount int  `json:"unexpected_high_confidence_count"`
}

type result struct {
	RequestedModel              string   `json:"requested_model"`
	ActualModel                 string   `json:"actual_model,omitempty"`
	PromptVersion               string   `json:"prompt_version"`
	RecommendationSchemaVersion int      `json:"recommendation_schema_version"`
	RequestedReasoningEffort    string   `json:"requested_reasoning_effort"`
	CaseID                      string   `json:"case_id"`
	PairID                      string   `json:"pair_id,omitempty"`
	PairVariant                 string   `json:"pair_variant,omitempty"`
	Phase                       string   `json:"phase"`
	Run                         int      `json:"run"`
	ScoreMin                    float64  `json:"score_min,omitempty"`
	ScoreMax                    float64  `json:"score_max,omitempty"`
	Score                       *float64 `json:"score,omitempty"`
	recommendationGrade
	Latency time.Duration `json:"latency"`
	Tokens  int           `json:"tokens"`
	CostUSD *float64      `json:"cost_usd,omitempty"`
	Error   string        `json:"error,omitempty"`
}

type modelSummary struct {
	Model                           string
	Cases                           int
	AgreedCases                     int
	Agreement                       float64
	RunAgreement                    float64
	MeanBandDrift                   float64
	Errors                          int
	P50                             time.Duration
	P95                             time.Duration
	AverageTokens                   float64
	AverageCostUSD                  float64
	RecommendationCases             int
	RecommendationAgreedCases       int
	RecommendationCaseAgreement     float64
	RecommendationRuns              int
	RecommendationAgreedRuns        int
	RecommendationRunAgreement      float64
	PositiveCases                   int
	PositiveAgreedCases             int
	PositiveAgreement               float64
	ZeroCases                       int
	ZeroAgreedCases                 int
	ZeroAgreement                   float64
	Pairs                           int
	AgreedPairs                     int
	PairAgreement                   float64
	PresenceAccuracy                float64
	CountAccuracy                   float64
	OutcomeAccuracy                 float64
	PersistenceAccuracy             float64
	IssueAccuracy                   float64
	ChangeAccuracy                  float64
	EvidenceAccuracy                float64
	EvidenceGroupRecall             float64
	PersistencePrecision            float64
	PersistenceRecall               float64
	RecommendationOnlyErrors        int
	RecommendationOnlyP50           time.Duration
	RecommendationOnlyP95           time.Duration
	RecommendationOnlyAverageTokens float64
	RecommendationOnlyAverageCost   float64
}

func main() {
	modelsFlag := flag.String("models", "", "comma-separated allowlisted model ids (defaults to the corpus model)")
	casesFile := flag.String("cases", defaultCasesFile, "path to the labeled bench set")
	runs := flag.Int("runs", 3, "evaluations per model and case")
	concurrency := flag.Int("concurrency", 4, "maximum concurrent judge calls")
	timeout := flag.Duration("timeout", 60*time.Second, "per-call timeout")
	baselineFile := flag.String("baseline", "", "prior results JSON used to report per-case score drift")
	outFile := flag.String("out", defaultOutFile, "write sanitized per-call results here (empty to skip)")
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

	results := runBench(client, set, models, *runs, *concurrency, *timeout, reasoning)
	passed := true
	for _, model := range models {
		summary := summarize(set, model, results)
		printSummary(summary, set.MinimumAgreement, set.MinimumRecommendationAgreement)
		passed = passed && summary.passes(set.MinimumAgreement, set.MinimumRecommendationAgreement)
	}

	if *baselineFile != "" {
		baseline, err := loadBaseline(*baselineFile, set.RecommendationSchemaVersion, *reasoningEffort)
		if err != nil {
			exitf("load baseline: %v", err)
		}
		if err := printDrift(os.Stdout, results, baseline); err != nil {
			exitf("print drift: %v", err)
		}
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

func runBench(client openrouter.CompletionClient, set benchSet, models []string, runs, concurrency int, timeout time.Duration, reasoning *openrouter.Reasoning) []result {
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
			results[i] = evaluate(client, set.RecommendationSchemaVersion, job.model, job.tc, job.run, timeout, reasoning)
		}()
	}
	wg.Wait()
	return results
}

func evaluate(client openrouter.CompletionClient, recommendationSchemaVersion int, model string, tc testCase, run int, timeout time.Duration, reasoning *openrouter.Reasoning) result {
	res := result{
		RequestedModel:              model,
		ActualModel:                 "",
		PromptVersion:               efficacy.JudgePromptVersion,
		RecommendationSchemaVersion: recommendationSchemaVersion,
		RequestedReasoningEffort:    reasoningEffort(reasoning),
		CaseID:                      tc.ID,
		PairID:                      tc.PairID,
		PairVariant:                 tc.PairVariant,
		Phase:                       "score",
		Run:                         run,
		ScoreMin:                    tc.ScoreMin,
		ScoreMax:                    tc.ScoreMax,
		recommendationGrade:         recommendationGrade{ExpectedCount: len(tc.ExpectedRecommendations)},
		Score:                       nil,
		Latency:                     0,
		Tokens:                      0,
		CostUSD:                     nil,
		Error:                       "",
	}
	if tc.recommendationOnly() {
		res.Phase = "recommendation_only"
		res.ScoreMin = 0
		res.ScoreMax = 0
	}
	request, err := buildRequest(model, tc, reasoning)
	if err != nil {
		res.Error = "invalid_case"
		return res
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	started := time.Now()
	response, err := client.GetObjectCompletion(ctx, request)
	res.Latency = time.Since(started)
	if err != nil {
		res.Error = "completion_failed"
		return res
	}
	if response == nil || response.Message == nil {
		res.Error = "empty_completion"
		return res
	}
	res.ActualModel = response.Model
	res.Tokens = response.Usage.TotalTokens
	res.CostUSD = response.Usage.Cost
	verdict, err := efficacy.ParseVerdict(strings.TrimSpace(openrouter.GetText(*response.Message)), tc.Transcript)
	if err != nil {
		res.Error = "invalid_verdict"
		return res
	}
	if !tc.recommendationOnly() {
		res.Score = &verdict.Score
	}
	res.recommendationGrade = gradeRecommendations(tc.ExpectedRecommendations, verdict.Recommendations)
	return res
}

func buildRequest(model string, tc testCase, reasoning *openrouter.Reasoning) (openrouter.ObjectCompletionRequest, error) {
	prompt, err := efficacy.BuildJudgePrompt(efficacy.JudgeInput{
		OrgID:        benchOrgID,
		ProjectID:    benchProjectID,
		SkillName:    tc.SkillName,
		SkillURN:     "",
		SkillContent: tc.SkillContent,
		Surface:      string(tc.Surface),
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
		Reasoning:      reasoning,
	}, nil
}

func matchBit(value bool) int {
	if value {
		return 1
	}
	return 0
}

func reasoningEffort(reasoning *openrouter.Reasoning) string {
	if reasoning == nil {
		return ""
	}
	return reasoning.Effort
}

func structuredRecommendations(raw []efficacy.RawRecommendation) []actualRecommendation {
	recommendations := make([]actualRecommendation, 0, len(raw))
	for _, recommendation := range raw {
		recommendations = append(recommendations, actualRecommendation{
			Outcome:                recommendation.Outcome,
			Note:                   recommendation.Note,
			Confidence:             recommendation.Confidence,
			IssueType:              recommendation.IssueType,
			ChangeType:             recommendation.ChangeType,
			EvidenceMessageIndices: slices.Clone(recommendation.EvidenceMessageIndices),
		})
	}
	return recommendations
}

func matchedEvidenceGroups(expected expectedRecommendation, actual actualRecommendation) (int, bool) {
	for i, index := range actual.EvidenceMessageIndices {
		if !slices.Contains(expected.AllowedEvidenceIndices, index) || i > 0 && index <= actual.EvidenceMessageIndices[i-1] {
			return 0, false
		}
	}
	matched := 0
	for _, group := range expected.RequiredEvidenceGroups {
		for _, alternative := range group {
			if slices.Contains(actual.EvidenceMessageIndices, alternative) {
				matched++
				break
			}
		}
	}
	return matched, matched == len(expected.RequiredEvidenceGroups)
}

func recommendationMatchQuality(expected expectedRecommendation, actual actualRecommendation) ([6]int, int) {
	groups, evidenceOK := matchedEvidenceGroups(expected, actual)
	return [6]int{
		matchBit(evidenceOK),
		groups,
		matchBit(slices.Contains(expected.AcceptableIssueTypes, actual.IssueType)),
		matchBit(slices.Contains(expected.AcceptableChangeTypes, actual.ChangeType)),
		matchBit(slices.Contains(expected.AcceptableOutcomes, skills.FeedbackOutcome(actual.Outcome))),
		matchBit(expected.PersistenceEligible == (actual.Confidence == "high")),
	}, groups
}

func bestRecommendation(expected expectedRecommendation, actual []actualRecommendation, used map[int]struct{}) (actualRecommendation, int, int, bool) {
	bestIndex, bestGroups := -1, 0
	var bestQuality [6]int
	for index, recommendation := range actual {
		if _, alreadyUsed := used[index]; alreadyUsed {
			continue
		}
		quality, groups := recommendationMatchQuality(expected, recommendation)
		if bestIndex == -1 || slices.Compare(quality[:], bestQuality[:]) > 0 {
			bestIndex, bestGroups, bestQuality = index, groups, quality
		}
	}
	if bestIndex == -1 {
		return actualRecommendation{}, 0, 0, false
	}
	return actual[bestIndex], bestGroups, bestIndex, true
}

func gradeRecommendations(expected []expectedRecommendation, raw []efficacy.RawRecommendation) recommendationGrade {
	return gradeStructuredRecommendations(expected, structuredRecommendations(raw))
}

func gradeStructuredRecommendations(expected []expectedRecommendation, actual []actualRecommendation) recommendationGrade {
	grade := recommendationGrade{
		ExpectedCount: len(expected),
		ActualCount:   len(actual),
		PresenceOK:    (len(expected) == 0) == (len(actual) == 0),
		CountOK:       len(expected) == len(actual),
	}
	for _, recommendation := range actual {
		if recommendation.Confidence == "high" {
			grade.HighConfidenceCount++
		}
	}
	for _, recommendation := range expected {
		grade.ExpectedEvidenceGroups += len(recommendation.RequiredEvidenceGroups)
		if recommendation.PersistenceEligible {
			grade.PersistenceEligibleExpected++
		}
	}

	used := make(map[int]struct{}, min(len(expected), len(actual)))
	for _, want := range expected {
		got, groups, actualIndex, matched := bestRecommendation(want, actual, used)
		if !matched {
			continue
		}
		used[actualIndex] = struct{}{}
		grade.MatchedCount++
		grade.EvidenceGroupsMatched += groups
		_, evidenceCorrect := matchedEvidenceGroups(want, got)
		outcomeCorrect := slices.Contains(want.AcceptableOutcomes, skills.FeedbackOutcome(got.Outcome))
		persistenceCorrect := want.PersistenceEligible == (got.Confidence == "high")
		issueCorrect := slices.Contains(want.AcceptableIssueTypes, got.IssueType)
		changeCorrect := slices.Contains(want.AcceptableChangeTypes, got.ChangeType)
		if outcomeCorrect {
			grade.OutcomeCorrectCount++
		}
		if persistenceCorrect {
			grade.PersistenceCorrectCount++
		}
		if issueCorrect {
			grade.IssueCorrectCount++
		}
		if changeCorrect {
			grade.ChangeCorrectCount++
		}
		if evidenceCorrect {
			grade.EvidenceCorrectCount++
		}
		if want.PersistenceEligible && got.Confidence == "high" && outcomeCorrect && issueCorrect && changeCorrect && evidenceCorrect {
			grade.EligibleHighConfidenceMatches++
		}
	}
	grade.UnmatchedExpected = len(expected) - grade.MatchedCount
	grade.UnmatchedActual = len(actual) - grade.MatchedCount
	grade.UnexpectedHighConfidenceCount = grade.HighConfidenceCount - grade.EligibleHighConfidenceMatches
	grade.OutcomeOK = grade.OutcomeCorrectCount == len(expected)
	grade.PersistenceOK = grade.PersistenceCorrectCount == len(expected)
	grade.IssueOK = grade.IssueCorrectCount == len(expected)
	grade.ChangeOK = grade.ChangeCorrectCount == len(expected)
	grade.EvidenceOK = grade.EvidenceCorrectCount == len(expected)
	grade.ExactOK = grade.CountOK && grade.OutcomeOK && grade.PersistenceOK && grade.IssueOK && grade.ChangeOK && grade.EvidenceOK
	return grade
}

func validateTranscript(transcript efficacy.Transcript) error {
	var previousIndex int
	var previousTime time.Time
	for i, message := range transcript.Messages {
		meaningful := false
		switch message.Role {
		case "user":
			meaningful = strings.TrimSpace(message.Content) != ""
		case "assistant":
			meaningful = strings.TrimSpace(message.Content) != "" || len(message.ToolCalls) > 0
		case "tool":
			meaningful = strings.TrimSpace(message.ToolOutcome) != "" || strings.TrimSpace(message.ToolOutcomeNotes) != ""
		default:
			return fmt.Errorf("message %d has unsupported role %q", i+1, message.Role)
		}
		if !meaningful {
			return fmt.Errorf("message %d has no meaningful payload", i+1)
		}
		if message.Index <= 0 {
			return fmt.Errorf("message %d has non-positive index", i+1)
		}
		createdAt, err := time.Parse(time.RFC3339Nano, message.CreatedAt)
		if err != nil {
			return fmt.Errorf("message %d has invalid created_at: %w", i+1, err)
		}
		if i > 0 && message.Index <= previousIndex {
			return errors.New("message indices must be strictly increasing")
		}
		if i > 0 && !createdAt.After(previousTime) {
			return errors.New("message timestamps must be strictly increasing")
		}
		previousIndex = message.Index
		previousTime = createdAt
	}
	return nil
}

func (pair recommendationPair) expand() (testCase, testCase) {
	a := testCase{
		caseFields: pair.caseFields,
		ID:         pair.ID + "-a-pre-prescription", Transcript: pair.Transcript, PairID: pair.ID, PairVariant: "A",
		ExpectedRecommendations: []expectedRecommendation{pair.ExpectedRecommendation},
	}
	b := a
	b.ID = pair.ID + "-b-post-prescription-silent"
	b.PairVariant = "B"
	b.Transcript.Messages = append(slices.Clone(pair.Transcript.Messages), pair.BPrescription)
	b.ExpectedRecommendations = []expectedRecommendation{}
	return a, b
}

var acceptableIssueTypes = []string{
	"requirement_omitted",
	"priority_violated",
	"guidance_gap",
	"prohibition_violated",
	"harmful_overconstraint",
	"obsolete_guidance",
}

var acceptableChangeTypes = []string{
	"reinforce_existing_requirement",
	"reinforce_existing_priority",
	"add_missing_requirement",
	"reinforce_existing_prohibition",
	"relax_constraint",
	"replace_obsolete_guidance",
}

func strictlyIncreasing(values []int) bool {
	if len(values) == 0 {
		return false
	}
	for i, value := range values {
		if value <= 0 || i > 0 && value <= values[i-1] {
			return false
		}
	}
	return true
}

func validateExpectedRecommendation(caseID string, transcript efficacy.Transcript, expected expectedRecommendation) error {
	if len(expected.AcceptableOutcomes) == 0 || len(expected.AcceptableIssueTypes) != 1 || len(expected.AcceptableChangeTypes) != 1 ||
		!slices.Contains(acceptableIssueTypes, expected.AcceptableIssueTypes[0]) || !slices.Contains(acceptableChangeTypes, expected.AcceptableChangeTypes[0]) ||
		!strictlyIncreasing(expected.AllowedEvidenceIndices) || len(expected.RequiredEvidenceGroups) == 0 {
		return fmt.Errorf("case %q has invalid expected recommendation", caseID)
	}
	seenOutcomes := make(map[skills.FeedbackOutcome]struct{}, len(expected.AcceptableOutcomes))
	for _, outcome := range expected.AcceptableOutcomes {
		if !outcome.Valid() || outcome == skills.FeedbackOutcomeHelped {
			return fmt.Errorf("case %q has invalid acceptable outcome %q", caseID, outcome)
		}
		if _, ok := seenOutcomes[outcome]; ok {
			return fmt.Errorf("case %q has duplicate acceptable outcome %q", caseID, outcome)
		}
		seenOutcomes[outcome] = struct{}{}
	}
	transcriptIndices := make([]int, 0, len(transcript.Messages))
	for _, message := range transcript.Messages {
		transcriptIndices = append(transcriptIndices, message.Index)
	}
	for _, index := range expected.AllowedEvidenceIndices {
		if !slices.Contains(transcriptIndices, index) {
			return fmt.Errorf("case %q has allowed evidence index %d outside its transcript", caseID, index)
		}
	}
	for _, group := range expected.RequiredEvidenceGroups {
		if !strictlyIncreasing(group) {
			return fmt.Errorf("case %q has invalid required evidence group", caseID)
		}
		for _, index := range group {
			if !slices.Contains(expected.AllowedEvidenceIndices, index) {
				return fmt.Errorf("case %q has required evidence index %d outside its allowed evidence", caseID, index)
			}
		}
	}
	return nil
}

func loadBenchSet(path string) (benchSet, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return benchSet{}, err
	}
	var set benchSet
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&set); err != nil {
		return benchSet{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return benchSet{}, errors.New("corpus must contain exactly one JSON value")
		}
		return benchSet{}, fmt.Errorf("corpus has trailing data: %w", err)
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
	if set.RecommendationSchemaVersion != 3 {
		return benchSet{}, errors.New("recommendation_schema_version must be 3")
	}
	if set.MinimumRecommendationAgreement <= 0 || set.MinimumRecommendationAgreement > 1 {
		return benchSet{}, errors.New("minimum_recommendation_agreement must be greater than 0 and at most 1")
	}
	for _, pair := range set.RecommendationPairs {
		if pair.ID == "" || pair.BPrescription.Role != "user" || strings.TrimSpace(pair.BPrescription.Content) == "" {
			return benchSet{}, fmt.Errorf("recommendation pair %q has invalid pair input", pair.ID)
		}
		a, b := pair.expand()
		set.Cases = append(set.Cases, a, b)
	}
	set.RecommendationPairs = nil
	if len(set.Cases) == 0 {
		return benchSet{}, errors.New("cases must not be empty")
	}
	seen := make(map[string]struct{}, len(set.Cases))
	pairs := map[string]map[string]bool{}
	positiveCases, zeroCases := 0, 0
	for _, tc := range set.Cases {
		if tc.ID == "" || tc.SkillName == "" || tc.SkillContent == "" || tc.ActivatedAt.IsZero() || len(tc.Transcript.Messages) == 0 {
			return benchSet{}, fmt.Errorf("case %q is missing required input", tc.ID)
		}
		if err := validateTranscript(tc.Transcript); err != nil {
			return benchSet{}, fmt.Errorf("case %q has invalid transcript: %w", tc.ID, err)
		}
		if !tc.Surface.Valid() {
			return benchSet{}, fmt.Errorf("case %q has invalid surface %q", tc.ID, tc.Surface)
		}
		if tc.ScoreMin < 0 || tc.ScoreMax > 1 || tc.ScoreMin > tc.ScoreMax {
			return benchSet{}, fmt.Errorf("case %q has invalid score band", tc.ID)
		}
		if tc.ExpectedRecommendations == nil {
			return benchSet{}, fmt.Errorf("case %q must declare expected_recommendations", tc.ID)
		}
		if len(tc.ExpectedRecommendations) > 1 {
			return benchSet{}, fmt.Errorf("case %q has more than one expected recommendation for schema version 3", tc.ID)
		}
		if (tc.PairID == "") != (tc.PairVariant == "") || tc.PairVariant != "" && tc.PairVariant != "A" && tc.PairVariant != "B" {
			return benchSet{}, fmt.Errorf("case %q has invalid pair fields", tc.ID)
		}
		if tc.PairVariant == "A" && len(tc.ExpectedRecommendations) == 0 || tc.PairVariant == "B" && len(tc.ExpectedRecommendations) != 0 {
			return benchSet{}, fmt.Errorf("case %q has expectations inconsistent with pair variant", tc.ID)
		}
		if tc.PairID != "" {
			if pairs[tc.PairID] == nil {
				pairs[tc.PairID] = map[string]bool{}
			}
			if pairs[tc.PairID][tc.PairVariant] {
				return benchSet{}, fmt.Errorf("pair %q has duplicate variant %q", tc.PairID, tc.PairVariant)
			}
			pairs[tc.PairID][tc.PairVariant] = true
		}
		if len(tc.ExpectedRecommendations) == 0 {
			zeroCases++
		} else {
			positiveCases++
		}
		for _, expected := range tc.ExpectedRecommendations {
			if err := validateExpectedRecommendation(tc.ID, tc.Transcript, expected); err != nil {
				return benchSet{}, err
			}
		}
		if _, ok := seen[tc.ID]; ok {
			return benchSet{}, fmt.Errorf("duplicate case id %q", tc.ID)
		}
		seen[tc.ID] = struct{}{}
	}
	if positiveCases == 0 {
		return benchSet{}, errors.New("expanded corpus must contain at least one positive recommendation case")
	}
	if zeroCases == 0 {
		return benchSet{}, errors.New("expanded corpus must contain at least one zero-recommendation case")
	}
	if len(pairs) == 0 {
		return benchSet{}, errors.New("expanded corpus must contain at least one complete A/B pair")
	}
	for pairID, variants := range pairs {
		if !variants["A"] || !variants["B"] || len(variants) != 2 {
			return benchSet{}, fmt.Errorf("pair %q must contain exactly variants A and B", pairID)
		}
	}
	return set, nil
}

type usageStats struct {
	latencies []time.Duration
	tokens    int
	tokenUses int
	cost      float64
	costUses  int
	errors    int
}

func (stats *usageStats) add(res result) {
	stats.latencies = append(stats.latencies, res.Latency)
	if res.Tokens != 0 || res.CostUSD != nil {
		stats.tokens += res.Tokens
		stats.tokenUses++
	}
	if res.CostUSD != nil {
		stats.cost += *res.CostUSD
		stats.costUses++
	}
	if res.Error != "" {
		stats.errors++
	}
}

func (stats usageStats) percentiles() (time.Duration, time.Duration) {
	if len(stats.latencies) == 0 {
		return 0, 0
	}
	latencies := slices.Clone(stats.latencies)
	slices.Sort(latencies)
	return percentile(latencies, 0.50), percentile(latencies, 0.95)
}

func summarize(set benchSet, model string, results []result) modelSummary {
	byCase := make(map[string][]result, len(set.Cases))
	var scoreUsage, recommendationOnlyUsage usageStats
	runsInBand, scoreRuns, scoreSuccesses := 0, 0, 0
	totalBandDrift := 0.0

	recommendationRuns, recommendationAgreedRuns := 0, 0
	presenceCorrect, countCorrect := 0, 0
	matched, outcomeCorrect, persistenceCorrect := 0, 0, 0
	issueCorrect, changeCorrect, evidenceCorrect := 0, 0, 0
	evidenceGroupsMatched, expectedEvidenceGroups := 0, 0
	highConfidence, eligibleHighConfidenceMatches, persistenceEligibleExpected := 0, 0, 0

	for _, res := range results {
		if res.RequestedModel != model {
			continue
		}
		byCase[res.CaseID] = append(byCase[res.CaseID], res)
		recommendationRuns++
		if res.Error == "" && res.ExactOK {
			recommendationAgreedRuns++
		}
		if res.Error == "" {
			if res.PresenceOK {
				presenceCorrect++
			}
			if res.CountOK {
				countCorrect++
			}
			matched += res.MatchedCount
			outcomeCorrect += res.OutcomeCorrectCount
			persistenceCorrect += res.PersistenceCorrectCount
			issueCorrect += res.IssueCorrectCount
			changeCorrect += res.ChangeCorrectCount
			evidenceCorrect += res.EvidenceCorrectCount
			evidenceGroupsMatched += res.EvidenceGroupsMatched
			expectedEvidenceGroups += res.ExpectedEvidenceGroups
			highConfidence += res.HighConfidenceCount
			eligibleHighConfidenceMatches += res.EligibleHighConfidenceMatches
			persistenceEligibleExpected += res.PersistenceEligibleExpected
		}

		if res.Phase == "recommendation_only" {
			recommendationOnlyUsage.add(res)
			continue
		}

		scoreRuns++
		scoreUsage.add(res)
		if res.Error != "" || res.Score == nil {
			if res.Error == "" {
				scoreUsage.errors++
			}
			continue
		}
		scoreSuccesses++
		drift := distanceFromBand(*res.Score, res.ScoreMin, res.ScoreMax)
		totalBandDrift += drift
		if drift == 0 {
			runsInBand++
		}
	}

	caseAgreement := make(map[string]bool, len(set.Cases))
	scoreCases, agreedScoreCases := 0, 0
	positiveCases, positiveAgreed := 0, 0
	zeroCases, zeroAgreed := 0, 0
	for _, tc := range set.Cases {
		caseResults := byCase[tc.ID]
		exact := 0
		scores := make([]float64, 0, len(caseResults))
		for _, res := range caseResults {
			if res.Error == "" {
				if res.ExactOK {
					exact++
				}
			}
			if res.Score != nil && res.Error == "" {
				scores = append(scores, *res.Score)
			}
		}
		caseAgreement[tc.ID] = len(caseResults) > 0 && exact > len(caseResults)/2
		if len(tc.ExpectedRecommendations) > 0 {
			positiveCases++
			if caseAgreement[tc.ID] {
				positiveAgreed++
			}
		} else {
			zeroCases++
			if caseAgreement[tc.ID] {
				zeroAgreed++
			}
		}
		if !tc.recommendationOnly() {
			scoreCases++
			if len(scores) > len(caseResults)/2 && distanceFromBand(median(scores), tc.ScoreMin, tc.ScoreMax) == 0 {
				agreedScoreCases++
			}
		}
	}

	pairCases := map[string]map[string]string{}
	for _, tc := range set.Cases {
		if tc.PairID == "" {
			continue
		}
		if pairCases[tc.PairID] == nil {
			pairCases[tc.PairID] = map[string]string{}
		}
		pairCases[tc.PairID][tc.PairVariant] = tc.ID
	}
	agreedPairs := 0
	for _, variants := range pairCases {
		if caseAgreement[variants["A"]] && caseAgreement[variants["B"]] {
			agreedPairs++
		}
	}

	persistencePrecision := 1.0
	if highConfidence > 0 {
		persistencePrecision = divide(eligibleHighConfidenceMatches, highConfidence)
	}
	scoreP50, scoreP95 := scoreUsage.percentiles()
	recommendationP50, recommendationP95 := recommendationOnlyUsage.percentiles()
	return modelSummary{
		Model: model, Cases: scoreCases, AgreedCases: agreedScoreCases,
		Agreement: divide(agreedScoreCases, scoreCases), RunAgreement: divide(runsInBand, scoreRuns),
		MeanBandDrift: divideFloat(totalBandDrift, scoreSuccesses), Errors: scoreUsage.errors,
		P50: scoreP50, P95: scoreP95,
		AverageTokens: divideFloat(float64(scoreUsage.tokens), scoreUsage.tokenUses), AverageCostUSD: divideFloat(scoreUsage.cost, scoreUsage.costUses),
		RecommendationCases: len(set.Cases), RecommendationAgreedCases: positiveAgreed + zeroAgreed,
		RecommendationCaseAgreement: divide(positiveAgreed+zeroAgreed, len(set.Cases)),
		RecommendationRuns:          recommendationRuns, RecommendationAgreedRuns: recommendationAgreedRuns,
		RecommendationRunAgreement: divide(recommendationAgreedRuns, recommendationRuns),
		PositiveCases:              positiveCases, PositiveAgreedCases: positiveAgreed, PositiveAgreement: divide(positiveAgreed, positiveCases),
		ZeroCases: zeroCases, ZeroAgreedCases: zeroAgreed, ZeroAgreement: divide(zeroAgreed, zeroCases),
		Pairs: len(pairCases), AgreedPairs: agreedPairs, PairAgreement: divide(agreedPairs, len(pairCases)),
		PresenceAccuracy: divide(presenceCorrect, recommendationRuns), CountAccuracy: divide(countCorrect, recommendationRuns),
		OutcomeAccuracy: divide(outcomeCorrect, matched), PersistenceAccuracy: divide(persistenceCorrect, matched),
		IssueAccuracy: divide(issueCorrect, matched), ChangeAccuracy: divide(changeCorrect, matched),
		EvidenceAccuracy: divide(evidenceCorrect, matched), EvidenceGroupRecall: divide(evidenceGroupsMatched, expectedEvidenceGroups),
		PersistencePrecision:     persistencePrecision,
		PersistenceRecall:        divide(eligibleHighConfidenceMatches, persistenceEligibleExpected),
		RecommendationOnlyErrors: recommendationOnlyUsage.errors,
		RecommendationOnlyP50:    recommendationP50, RecommendationOnlyP95: recommendationP95,
		RecommendationOnlyAverageTokens: divideFloat(float64(recommendationOnlyUsage.tokens), recommendationOnlyUsage.tokenUses),
		RecommendationOnlyAverageCost:   divideFloat(recommendationOnlyUsage.cost, recommendationOnlyUsage.costUses),
	}
}

func (summary modelSummary) passes(scoreMinimum, recommendationMinimum float64) bool {
	return summary.Agreement >= scoreMinimum &&
		summary.PositiveAgreement >= recommendationMinimum &&
		summary.ZeroAgreement >= recommendationMinimum &&
		summary.PairAgreement >= recommendationMinimum &&
		summary.PersistencePrecision == 1
}

func printSummary(summary modelSummary, scoreMinimum, recommendationMinimum float64) {
	status := "FAIL"
	if summary.passes(scoreMinimum, recommendationMinimum) {
		status = "PASS"
	}
	fmt.Printf("%s model=%s prompt=%s score_agreement=%.1f%% (%d/%d gate=%.0f%%) score_run_agreement=%.1f%% mean_band_drift=%.3f errors=%d p50=%s p95=%s avg_tokens=%.0f avg_cost_usd=%.6f\n",
		status, summary.Model, efficacy.JudgePromptVersion, summary.Agreement*100, summary.AgreedCases, summary.Cases, scoreMinimum*100,
		summary.RunAgreement*100, summary.MeanBandDrift, summary.Errors, summary.P50.Round(time.Millisecond), summary.P95.Round(time.Millisecond), summary.AverageTokens, summary.AverageCostUSD)
	fmt.Printf("recommendations case_agreement=%.1f%% (%d/%d) run_agreement=%.1f%% (%d/%d) positive=%.1f%% (%d/%d gate=%.0f%%) zero_suppression=%.1f%% (%d/%d gate=%.0f%%) pairs=%.1f%% (%d/%d gate=%.0f%%) persistence_precision=%.1f%% (gate=100%%) persistence_recall=%.1f%% presence=%.1f%% count=%.1f%% outcome=%.1f%% persistence=%.1f%% issue=%.1f%% change=%.1f%% evidence=%.1f%% evidence_group_recall=%.1f%%\n",
		summary.RecommendationCaseAgreement*100, summary.RecommendationAgreedCases, summary.RecommendationCases,
		summary.RecommendationRunAgreement*100, summary.RecommendationAgreedRuns, summary.RecommendationRuns,
		summary.PositiveAgreement*100, summary.PositiveAgreedCases, summary.PositiveCases, recommendationMinimum*100,
		summary.ZeroAgreement*100, summary.ZeroAgreedCases, summary.ZeroCases, recommendationMinimum*100,
		summary.PairAgreement*100, summary.AgreedPairs, summary.Pairs, recommendationMinimum*100,
		summary.PersistencePrecision*100, summary.PersistenceRecall*100, summary.PresenceAccuracy*100, summary.CountAccuracy*100,
		summary.OutcomeAccuracy*100, summary.PersistenceAccuracy*100, summary.IssueAccuracy*100, summary.ChangeAccuracy*100,
		summary.EvidenceAccuracy*100, summary.EvidenceGroupRecall*100)
	fmt.Printf("recommendation_only errors=%d p50=%s p95=%s avg_tokens=%.0f avg_cost_usd=%.6f\n", summary.RecommendationOnlyErrors, summary.RecommendationOnlyP50.Round(time.Millisecond), summary.RecommendationOnlyP95.Round(time.Millisecond), summary.RecommendationOnlyAverageTokens, summary.RecommendationOnlyAverageCost)
}

func printDrift(writer io.Writer, current, baseline []result) error {
	baselineMeans := caseMeans(baseline, "")
	baselineModel := "unknown"
	if len(baseline) > 0 {
		baselineModel = baseline[0].RequestedModel
	}
	baselinePromptVersion := promptVersionLabel(baseline, "")
	models := make([]string, 0)
	for _, res := range current {
		if !slices.Contains(models, res.RequestedModel) {
			models = append(models, res.RequestedModel)
		}
	}
	sort.Strings(models)
	for _, model := range models {
		currentMeans := caseMeans(current, model)
		currentPromptVersion := promptVersionLabel(current, model)
		caseIDs := make([]string, 0, len(currentMeans))
		for caseID := range currentMeans {
			if _, ok := baselineMeans[caseID]; ok {
				caseIDs = append(caseIDs, caseID)
			}
		}
		sort.Strings(caseIDs)
		for _, caseID := range caseIDs {
			if _, err := fmt.Fprintf(writer, "drift baseline_model=%s baseline_prompt_version=%s model=%s prompt_version=%s case=%s delta=%+.3f\n", baselineModel, baselinePromptVersion, model, currentPromptVersion, caseID, currentMeans[caseID]-baselineMeans[caseID]); err != nil {
				return err
			}
		}
	}
	return nil
}

func promptVersionLabel(results []result, model string) string {
	versions := make([]string, 0, 1)
	for _, res := range results {
		if model != "" && res.RequestedModel != model || res.PromptVersion == "" || slices.Contains(versions, res.PromptVersion) {
			continue
		}
		versions = append(versions, res.PromptVersion)
	}
	if len(versions) == 0 {
		return "unknown"
	}
	sort.Strings(versions)
	return strings.Join(versions, ",")
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

func loadBaseline(path string, recommendationSchemaVersion int, requestedReasoningEffort string) ([]result, error) {
	results, err := loadResults(path)
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, 1)
	promptVersions := make([]string, 0, 1)
	for _, res := range results {
		if strings.TrimSpace(res.RequestedModel) == "" {
			return nil, errors.New("baseline must contain exactly one nonempty requested model, found an empty value")
		}
		if !slices.Contains(models, res.RequestedModel) {
			models = append(models, res.RequestedModel)
		}
		if strings.TrimSpace(res.PromptVersion) == "" {
			return nil, errors.New("baseline must contain exactly one nonempty prompt version, found an empty value")
		}
		if !slices.Contains(promptVersions, res.PromptVersion) {
			promptVersions = append(promptVersions, res.PromptVersion)
		}
	}
	if len(models) != 1 {
		return nil, fmt.Errorf("baseline must contain exactly one requested model, found %d", len(models))
	}
	if len(promptVersions) != 1 {
		return nil, fmt.Errorf("baseline must contain exactly one nonempty prompt version, found %d", len(promptVersions))
	}
	for _, res := range results {
		if res.RecommendationSchemaVersion != recommendationSchemaVersion {
			return nil, fmt.Errorf("baseline recommendation schema version %d is incompatible with requested version %d", res.RecommendationSchemaVersion, recommendationSchemaVersion)
		}
		if res.RequestedReasoningEffort != requestedReasoningEffort {
			return nil, fmt.Errorf("baseline reasoning effort %q is incompatible with requested effort %q", res.RequestedReasoningEffort, requestedReasoningEffort)
		}
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

func percentile(sorted []time.Duration, fraction float64) time.Duration {
	index := int(float64(len(sorted)-1) * fraction)
	return sorted[index]
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
