package bench

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
)

// Report is the scored outcome of one eval run. It is plain data so it can
// be stored and rendered by the dashboard as well as printed as markdown.
type Report struct {
	JudgeModel string    `json:"judge_model"`
	Runs       int       `json:"runs"`
	Questions  int       `json:"questions"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`

	// Scores has one entry per answer model and arm, in config order.
	Scores []ArmScore `json:"scores"`

	// JudgeCostUSD is what grading cost across the run.
	JudgeCostUSD float64 `json:"judge_cost_usd"`
}

// KindScore counts correct over graded answers for one question kind,
// summed across runs.
type KindScore struct {
	Correct int `json:"correct"`
	Total   int `json:"total"`
}

// ArmScore aggregates one answer model in one arm across runs.
type ArmScore struct {
	Model string `json:"model"`
	Arm   string `json:"arm"`

	// HasTools is whether the arm gave the agent memory tools; citation rates
	// mean nothing for an arm without them.
	HasTools bool `json:"has_tools"`

	// AccuracyByRun is correct over graded answers per run. Errored answers
	// and judge failures are not graded, so API trouble shows up in Errors
	// instead of lowering accuracy.
	AccuracyByRun []float64 `json:"accuracy_by_run"`
	Accuracy      float64   `json:"accuracy"`

	ByKind map[memoryeval.QuestionKind]KindScore `json:"by_kind"`

	// Answerable counts graded answers to questions that have an answer;
	// CitedAnswers of those cite at least one source and ValidCited cite only
	// sources that contain the answer.
	Answerable   int `json:"answerable"`
	CitedAnswers int `json:"cited_answers"`
	ValidCited   int `json:"valid_cited"`

	// Leaks names every planted secret that reached a tool result or answer;
	// IsolationViolations names every canary from another project or a
	// private session that did.
	Leaks               []string `json:"leaks"`
	IsolationViolations []string `json:"isolation_violations"`

	// NotFound counts negative questions answered "Not found" out of
	// Negatives graded.
	NotFound  int `json:"not_found"`
	Negatives int `json:"negatives"`

	ToolTokensPerQuestion int `json:"tool_tokens_per_question"`
	RetrievalMisses       int `json:"retrieval_misses"`
	ReasoningMisses       int `json:"reasoning_misses"`
	Errors                int `json:"errors"`
	Answers               int `json:"answers"`

	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	LatencyMeanMS    int64   `json:"latency_mean_ms"`
	LatencyP95MS     int64   `json:"latency_p95_ms"`
}

// scoreKey identifies one model and arm.
type scoreKey struct{ model, arm string }

// summarize aggregates answers and verdicts per answer model and arm.
func summarize(corpus *memoryeval.Corpus, cfg Config, arms []Arm, answers []answer, verdicts []verdict) *Report {
	type tally struct {
		score      *ArmScore
		correct    []int
		graded     []int
		leaks      map[string]bool
		isolation  map[string]bool
		toolTokens int
		latencies  []time.Duration
	}
	tallies := map[scoreKey]*tally{}
	report := &Report{JudgeModel: cfg.JudgeModel, Runs: cfg.Runs, Questions: len(corpus.Questions), StartedAt: time.Time{}, FinishedAt: time.Time{}, Scores: nil, JudgeCostUSD: 0}
	for _, model := range cfg.AnswerModels {
		for _, a := range arms {
			report.Scores = append(report.Scores, ArmScore{
				Model: model, Arm: a.Name, HasTools: len(a.Tools) > 0, AccuracyByRun: make([]float64, cfg.Runs), Accuracy: 0, ByKind: make(map[memoryeval.QuestionKind]KindScore),
				Answerable: 0, CitedAnswers: 0, ValidCited: 0, Leaks: nil, IsolationViolations: nil, NotFound: 0, Negatives: 0,
				ToolTokensPerQuestion: 0, RetrievalMisses: 0, ReasoningMisses: 0, Errors: 0, Answers: 0,
				PromptTokens: 0, CompletionTokens: 0, CostUSD: 0, LatencyMeanMS: 0, LatencyP95MS: 0,
			})
		}
	}
	for i := range report.Scores {
		s := &report.Scores[i]
		tallies[scoreKey{s.Model, s.Arm}] = &tally{score: s, correct: make([]int, cfg.Runs), graded: make([]int, cfg.Runs), leaks: map[string]bool{}, isolation: map[string]bool{}, toolTokens: 0, latencies: nil}
	}

	for i, ans := range answers {
		t, v := tallies[scoreKey{ans.model, ans.arm}], verdicts[i]
		s := t.score
		s.Answers++
		s.PromptTokens += ans.usage.promptTokens
		s.CompletionTokens += ans.usage.completionTokens
		s.CostUSD += ans.usage.costUSD
		report.JudgeCostUSD += v.usage.costUSD
		t.latencies = append(t.latencies, ans.usage.latency)
		t.toolTokens += memoryeval.EstimateTokens(ans.toolOutput)

		graded := v.Miss != missError
		kind := s.ByKind[ans.question.Kind]
		if graded {
			t.graded[ans.run]++
			kind.Total++
		}
		if graded && v.Correct {
			t.correct[ans.run]++
			kind.Correct++
		}
		s.ByKind[ans.question.Kind] = kind
		switch v.Miss {
		case "retrieval":
			s.RetrievalMisses++
		case "reasoning":
			s.ReasoningMisses++
		case missError:
			s.Errors++
		}
		seen := ans.toolOutput + "\n" + ans.text
		for _, secret := range corpus.Leaks(seen) {
			t.leaks[secret.Name] = true
		}
		for _, canary := range corpus.IsolationViolations(seen) {
			t.isolation[canary] = true
		}
		if graded && ans.question.Kind == memoryeval.QuestionNegative {
			s.Negatives++
			if v.Correct {
				s.NotFound++
			}
		}
		if graded && !ans.question.ExpectsNotFound() {
			s.Answerable++
			if len(ans.citations) > 0 {
				s.CitedAnswers++
				valid := true
				for _, c := range ans.citations {
					valid = valid && c.kind == "chat" && corpus.ChatCitationValid(ans.question, c.id)
				}
				if valid {
					s.ValidCited++
				}
			}
		}
	}

	for _, t := range tallies {
		s := t.score
		for run := range cfg.Runs {
			s.AccuracyByRun[run] = float64(t.correct[run]) / float64(max(t.graded[run], 1))
			s.Accuracy += s.AccuracyByRun[run] / float64(cfg.Runs)
		}
		s.Leaks = sortedKeys(t.leaks)
		s.IsolationViolations = sortedKeys(t.isolation)
		s.ToolTokensPerQuestion = t.toolTokens / max(s.Answers, 1)
		s.LatencyMeanMS, s.LatencyP95MS = latencyStats(t.latencies)
	}
	return report
}

// latencyStats returns the mean and 95th percentile in milliseconds.
func latencyStats(latencies []time.Duration) (mean, p95 int64) {
	if len(latencies) == 0 {
		return 0, 0
	}
	sorted := slices.Clone(latencies)
	slices.Sort(sorted)
	var total time.Duration
	for _, l := range sorted {
		total += l
	}
	index := (len(sorted)*95+99)/100 - 1
	return (total / time.Duration(len(sorted))).Milliseconds(), sorted[index].Milliseconds()
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// Markdown renders the report as one table row per answer model and arm.
func (r *Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Team memory eval\n\nJudge %s, %d runs per model and arm, %d questions. Judging cost $%.2f.\n\n", r.JudgeModel, r.Runs, r.Questions, r.JudgeCostUSD)
	b.WriteString("| Model | Arm | Accuracy (mean, range) | Point | Timeline | Reversal | Negative | Isolation | Valid citations | Leaks | Isolation violations | Not found on negatives | Tool tokens/question | Misses (retrieval/reasoning/error) | Cost | Latency mean/p95 |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, s := range r.Scores {
		lo, hi := slices.Min(s.AccuracyByRun), slices.Max(s.AccuracyByRun)
		kind := func(k memoryeval.QuestionKind) string {
			return fmt.Sprintf("%d/%d", s.ByKind[k].Correct, s.ByKind[k].Total)
		}
		citations := "n/a"
		if s.Answerable > 0 && s.HasTools {
			citations = fmt.Sprintf("%.0f%%", 100*float64(s.ValidCited)/float64(s.Answerable))
		}
		fmt.Fprintf(&b, "| %s | %s | %.0f%% (%.0f–%.0f%%) | %s | %s | %s | %s | %s | %s | %d %v | %d %v | %d/%d | %d | %d/%d/%d | $%.2f | %.1fs/%.1fs |\n",
			s.Model, s.Arm, 100*s.Accuracy, 100*lo, 100*hi,
			kind(memoryeval.QuestionPointLookup), kind(memoryeval.QuestionTimeline), kind(memoryeval.QuestionReversal), kind(memoryeval.QuestionNegative), kind(memoryeval.QuestionIsolation),
			citations, len(s.Leaks), s.Leaks, len(s.IsolationViolations), s.IsolationViolations,
			s.NotFound, s.Negatives, s.ToolTokensPerQuestion, s.RetrievalMisses, s.ReasoningMisses, s.Errors,
			s.CostUSD, float64(s.LatencyMeanMS)/1000, float64(s.LatencyP95MS)/1000)
	}
	b.WriteString("\nCounts in the kind columns are summed across runs. Valid citations is the share of answerable questions whose answer cites at least one source and only sources that contain the answer. Cost is what the answering model reported; it is $0.00 when the provider reports none.\n")
	return b.String()
}
