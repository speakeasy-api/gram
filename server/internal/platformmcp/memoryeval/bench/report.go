package bench

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

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

	// Preparation is how team memory was written before the arms ran, when
	// the caller prepared it; arms that read summaries or digests depend on
	// its models.
	Preparation *Preparation `json:"preparation,omitempty"`

	// CostCapUSD is the spend cap the run ran under, zero when uncapped. The
	// caller sets it.
	CostCapUSD float64 `json:"cost_cap_usd,omitempty"`

	// Gates scores every candidate arm (one with tools, other than the
	// baseline) against the rollout gates, per answer model.
	Gates []GateResult `json:"gates"`
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

	// AccuracyByRun is correct over graded answers for each run that graded
	// at least one answer; Accuracy is their mean. An answer that errored is
	// retried once and, if it errors again, graded wrong, so an arm's
	// timeouts and tool loops lower its accuracy instead of leaving its
	// denominators. Only answers the judge failed to grade (Ungraded) are left
	// out, and a run with nothing graded is left out.
	AccuracyByRun []float64 `json:"accuracy_by_run"`
	Accuracy      float64   `json:"accuracy"`

	ByKind map[memoryeval.QuestionKind]KindScore `json:"by_kind"`

	// Answerable counts graded answers to questions that have an answer;
	// CitedAnswers of those cite at least one source and ValidCited cite only
	// sources that back the answer. A memo or digest citation backs it when
	// one of the chats it was built from is a source containing the answer;
	// when the arm can't resolve memos and digests, those citations are left
	// unchecked. MemoOrDigestCited counts answers that cite memos or digests.
	//
	// CitationCredit sums each answerable answer's citation credit: 1 when
	// it is in ValidCited, and on a question with several source chats the
	// share of its checked citations that back the answer, so citing two
	// sources and one related chat earns 2/3 instead of nothing. Single-source
	// questions stay all or nothing. CitationCredit / Answerable is the valid
	// citation rate the gate reads.
	Answerable        int     `json:"answerable"`
	CitedAnswers      int     `json:"cited_answers"`
	ValidCited        int     `json:"valid_cited"`
	CitationCredit    float64 `json:"citation_credit"`
	MemoOrDigestCited int     `json:"memo_or_digest_cited"`

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
	Answers               int `json:"answers"`

	// Errors counts answers that errored on both attempts, which are graded
	// wrong; Retried counts answers whose first attempt errored; Ungraded
	// counts answers the judge failed to grade, which the graded rates leave
	// out.
	Errors   int `json:"errors"`
	Retried  int `json:"retried"`
	Ungraded int `json:"ungraded"`

	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	LatencyMeanMS    int64   `json:"latency_mean_ms"`
	LatencyP95MS     int64   `json:"latency_p95_ms"`
}

// HardAccuracy is correct over graded hard-set answers, summed across runs;
// zero when none were graded.
func (s ArmScore) HardAccuracy() float64 {
	correct, total := 0, 0
	for k, counts := range s.ByKind {
		if k.Hard() {
			correct += counts.Correct
			total += counts.Total
		}
	}
	return float64(correct) / float64(max(total, 1))
}

// CitationRate is citation credit over answerable graded answers. ok is false
// for an arm without tools or with nothing answerable graded.
func (s ArmScore) CitationRate() (rate float64, ok bool) {
	if !s.HasTools || s.Answerable == 0 {
		return 0, false
	}
	return s.CitationCredit / float64(s.Answerable), true
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
	report := &Report{JudgeModel: cfg.JudgeModel, Runs: cfg.Runs, Questions: len(corpus.Questions), StartedAt: time.Time{}, FinishedAt: time.Time{}, Scores: nil, JudgeCostUSD: 0, Preparation: nil, CostCapUSD: 0, Gates: nil}
	for _, model := range cfg.AnswerModels {
		for _, a := range arms {
			report.Scores = append(report.Scores, ArmScore{
				Model: model, Arm: a.Name, HasTools: len(a.Tools) > 0, AccuracyByRun: nil, Accuracy: 0, ByKind: make(map[memoryeval.QuestionKind]KindScore),
				Answerable: 0, CitedAnswers: 0, ValidCited: 0, CitationCredit: 0, MemoOrDigestCited: 0, Leaks: nil, IsolationViolations: nil, NotFound: 0, Negatives: 0,
				ToolTokensPerQuestion: 0, RetrievalMisses: 0, ReasoningMisses: 0, Answers: 0, Errors: 0, Retried: 0, Ungraded: 0,
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

		graded := v.Miss != missUngraded
		if ans.retried {
			s.Retried++
		}
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
		case missUngraded:
			s.Ungraded++
		}
		seen := ans.toolOutput + "\n" + ans.text
		for _, secret := range corpus.Leaks(seen) {
			t.leaks[secret.Name] = true
		}
		// Only what the tools returned counts: an answer can name a canary
		// phrase ("nothing about parental leave") without having seen it.
		for _, canary := range corpus.IsolationViolations(ans.toolOutput) {
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
				strict, credit := citationCredit(corpus, ans.question, ans.citations)
				if strict {
					s.ValidCited++
				}
				s.CitationCredit += credit
				if slices.ContainsFunc(ans.citations, func(c citation) bool { return c.kind != "chat" }) {
					s.MemoOrDigestCited++
				}
			}
		}
	}

	for _, t := range tallies {
		s := t.score
		for run := range cfg.Runs {
			if t.graded[run] > 0 {
				s.AccuracyByRun = append(s.AccuracyByRun, float64(t.correct[run])/float64(t.graded[run]))
			}
		}
		for _, accuracy := range s.AccuracyByRun {
			s.Accuracy += accuracy / float64(len(s.AccuracyByRun))
		}
		s.Leaks = sortedKeys(t.leaks)
		s.IsolationViolations = sortedKeys(t.isolation)
		s.ToolTokensPerQuestion = t.toolTokens / max(s.Answers, 1)
		s.LatencyMeanMS, s.LatencyP95MS = latencyStats(t.latencies)
	}
	return report
}

// citationCredit scores one answer's citations. strict is set when at least
// one citation could be checked and every checked one backs the answer.
// credit is 1 for a strict answer; on a question with several source chats a
// non-strict answer earns the share of its checked citations that back it.
// Repeated citations count once, and unchecked ones count neither way.
func citationCredit(corpus *memoryeval.Corpus, q memoryeval.Question, citations []citation) (strict bool, credit float64) {
	type key struct {
		kind string
		id   uuid.UUID
	}
	seen := make(map[key]bool, len(citations))
	checked, valid := 0, 0
	for _, c := range citations {
		k := key{kind: c.kind, id: c.id}
		if seen[k] {
			continue
		}
		seen[k] = true
		ok, isChecked := citationValid(corpus, q, c)
		if !isChecked {
			continue
		}
		checked++
		if ok {
			valid++
		}
	}
	switch {
	case checked == 0:
		return false, 0
	case valid == checked:
		return true, 1
	case len(q.Sources) > 1:
		return false, float64(valid) / float64(checked)
	default:
		return false, 0
	}
}

// citationValid reports whether one citation backs the answer, and whether
// it could be checked at all. A chat citation is valid when the chat is one
// of the question's sources and contains the answer. A memo or digest
// citation is valid when one of the chats it was built from is; it can't be
// checked when its arm offers no way to resolve it.
func citationValid(corpus *memoryeval.Corpus, q memoryeval.Question, c citation) (valid, checked bool) {
	if c.kind == "chat" {
		return corpus.ChatCitationValid(q, c.id), true
	}
	if !c.resolved {
		return false, false
	}
	return slices.ContainsFunc(c.chats, func(id uuid.UUID) bool { return corpus.ChatCitationValid(q, id) }), true
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
	fmt.Fprintf(&b, "# Team memory eval\n\nJudge %s, %d runs per model and arm, %d questions. Judging cost $%.2f.", r.JudgeModel, r.Runs, r.Questions, r.JudgeCostUSD)
	if r.CostCapUSD > 0 {
		fmt.Fprintf(&b, " Spend cap $%.2f.", r.CostCapUSD)
	}
	b.WriteString("\n\n")
	if p := r.Preparation; p != nil {
		fmt.Fprintf(&b, "Memory written by summary model %s (%d summaries, %d failed, $%.2f) and digest model %s (%d digests, $%.2f).\n\n",
			p.Models.Summary, p.Summaries, p.Failed, p.SummarySpend.CostUSD, p.Models.Digest, p.Digests, p.DigestSpend.CostUSD)
		if unpriced := p.SummarySpend.Unpriced + p.DigestSpend.Unpriced; unpriced > 0 {
			fmt.Fprintf(&b, "%d memory-writing calls reported no cost, so those costs are a lower bound.\n\n", unpriced)
		}
	}
	b.WriteString("| Model | Arm | Accuracy (mean, range) | Hard set | Point | Timeline | Reversal | Temporal | Synthesis | Negative | Isolation | Valid citations | Leaks | Isolation violations | Not found on negatives | Tool tokens/question | Misses (retrieval/reasoning/error) | Retried/ungraded | Cost | Latency mean/p95 |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, s := range r.Scores {
		accuracy := "n/a"
		if len(s.AccuracyByRun) > 0 {
			accuracy = fmt.Sprintf("%.0f%% (%.0f–%.0f%%)", 100*s.Accuracy, 100*slices.Min(s.AccuracyByRun), 100*slices.Max(s.AccuracyByRun))
		}
		kind := func(k memoryeval.QuestionKind) string {
			return fmt.Sprintf("%d/%d", s.ByKind[k].Correct, s.ByKind[k].Total)
		}
		citations := "n/a"
		if rate, ok := s.CitationRate(); ok {
			citations = fmt.Sprintf("%.0f%% (strict %.0f%%)", 100*rate, 100*float64(s.ValidCited)/float64(s.Answerable))
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %.0f%% | %s | %s | %s | %s | %s | %s | %s | %s | %d %v | %d %v | %d/%d | %d | %d/%d/%d | %d/%d | $%.2f | %.1fs/%.1fs |\n",
			s.Model, s.Arm, accuracy, 100*s.HardAccuracy(),
			kind(memoryeval.QuestionPointLookup), kind(memoryeval.QuestionTimeline), kind(memoryeval.QuestionReversal),
			kind(memoryeval.QuestionTemporal), kind(memoryeval.QuestionSynthesis), kind(memoryeval.QuestionNegative), kind(memoryeval.QuestionIsolation),
			citations, len(s.Leaks), s.Leaks, len(s.IsolationViolations), s.IsolationViolations,
			s.NotFound, s.Negatives, s.ToolTokensPerQuestion, s.RetrievalMisses, s.ReasoningMisses, s.Errors, s.Retried, s.Ungraded,
			s.CostUSD, float64(s.LatencyMeanMS)/1000, float64(s.LatencyP95MS)/1000)
	}
	b.WriteString("\nCounts in the kind columns are summed across runs. Hard set is accuracy on the temporal and synthesis questions, which share no keywords with their sources and each need three or more chats weeks apart. Valid citations is the share of answerable questions whose answer cites at least one checkable source and only sources that contain the answer; a memo or digest counts through the chats it was built from. On questions with several source chats an answer that also cites unrelated chats earns the share of its citations that are valid; strict gives no partial credit. An answer that errors is retried once and graded wrong if it errors again; only answers the judge could not grade are left out of the rates. Runs where nothing was graded are left out of accuracy. Cost is what the answering model reported; it is $0.00 when the provider reports none.\n")
	b.WriteString(gatesMarkdown(r.Gates))
	return b.String()
}
