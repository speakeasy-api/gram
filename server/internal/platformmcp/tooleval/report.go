package tooleval

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Report holds every outcome and the scores derived from them.
type Report struct {
	Config   Config
	Outcomes []Outcome
	Scores   []Score

	// Notes say what the run left out or narrowed, such as cases selected
	// by a change or tools a surface could not list.
	Notes []string
}

// Score is one model on one surface, across every case and run.
type Score struct {
	Model   string
	Surface string

	Attempts int
	Errors   int

	// Selection is the share of graded attempts that reached an expected
	// tool or one of the case's allowed first steps, or reached none on a
	// negative case.
	Selection float64
	// ExpectedFirst is the share of positive attempts that reached an
	// expected tool directly, with no allowed first step.
	ExpectedFirst float64
	// SchemaValid is the share of hits whose arguments met the tool's input
	// schema.
	SchemaValid float64
	// ArgsMatch is the share of hits on cases with expected arguments that
	// carried them.
	ArgsMatch float64
	// Negatives counts negative attempts answered without a Platform tool.
	NegativesRight, Negatives int
	// ConfirmedFirst counts writes confirmed before any preview.
	ConfirmedFirst int

	MeanTurns           float64
	PromptTokensPerCase float64
	CostUSD             float64
	MeanLatencySeconds  float64
}

func newReport(cfg Config, surfaces []Surface, outcomes []Outcome) *Report {
	report := &Report{Config: cfg, Outcomes: outcomes, Scores: nil, Notes: nil}
	for _, model := range cfg.Models {
		for _, surface := range surfaces {
			report.Scores = append(report.Scores, scoreOf(model, surface.Name, outcomes))
		}
	}
	return report
}

func scoreOf(model, surface string, outcomes []Outcome) Score {
	s := Score{Model: model, Surface: surface}
	var graded, hits, onPath, positives, valid, withArgs, matched, turns, prompt int
	var latency float64
	for _, o := range outcomes {
		if o.Model != model || o.Surface != surface {
			continue
		}
		s.Attempts++
		s.CostUSD += o.CostUSD
		if o.Err != nil {
			s.Errors++
			continue
		}
		graded++
		turns += o.Turns
		prompt += o.PromptTokens
		latency += o.Latency.Seconds()
		if o.ConfirmedFirst {
			s.ConfirmedFirst++
		}
		if o.Case.Negative() {
			s.Negatives++
			if o.Hit {
				s.NegativesRight++
			}
		}
		if !o.Case.Negative() {
			positives++
		}
		if o.OnPath {
			onPath++
		}
		if !o.Hit {
			continue
		}
		hits++
		if o.Case.Negative() {
			continue
		}
		if o.SchemaValid {
			valid++
		}
		if len(o.Case.Args) > 0 {
			withArgs++
			if o.ArgsMatch {
				matched++
			}
		}
	}
	positiveHits := hits - s.NegativesRight
	s.Selection = ratio(hits+onPath, graded)
	s.ExpectedFirst = ratio(positiveHits, positives)
	s.SchemaValid = ratio(valid, positiveHits)
	s.ArgsMatch = ratio(matched, withArgs)
	s.MeanTurns = ratio(turns, graded)
	s.PromptTokensPerCase = ratio(prompt, graded)
	s.MeanLatencySeconds = latency / float64(max(graded, 1))
	return s
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// Miss is one case an agent got wrong at least once.
type Miss struct {
	Model, Surface, CaseID string
	Expected               []string
	// Chosen counts what the agent reached for instead; "(none)" is no
	// Platform tool and "(error)" a failed attempt.
	Chosen map[string]int
	Wrong  int
	Total  int
}

// Misses groups failed attempts by model, surface and case, worst first.
func (r *Report) Misses() []Miss {
	type key struct{ model, surface, id string }
	byKey := make(map[key]*Miss)
	var order []key
	for _, o := range r.Outcomes {
		k := key{o.Model, o.Surface, o.Case.ID}
		m, ok := byKey[k]
		if !ok {
			m = &Miss{Model: o.Model, Surface: o.Surface, CaseID: o.Case.ID, Expected: o.Case.Expect, Chosen: map[string]int{}}
			byKey[k] = m
			order = append(order, k)
		}
		m.Total++
		if o.OnPath || (o.Hit && (o.Case.Negative() || o.SchemaValid)) {
			continue
		}
		m.Wrong++
		switch {
		case o.Err != nil:
			m.Chosen["(error)"]++
		case o.Selected == nil:
			m.Chosen["(none)"]++
		case o.Hit:
			m.Chosen[o.Selected.Tool+" (invalid arguments)"]++
		default:
			m.Chosen[o.Selected.Tool]++
		}
	}
	var misses []Miss
	for _, k := range order {
		if byKey[k].Wrong > 0 {
			misses = append(misses, *byKey[k])
		}
	}
	slices.SortStableFunc(misses, func(a, b Miss) int {
		return cmp.Or(cmp.Compare(b.Wrong*a.Total, a.Wrong*b.Total), cmp.Compare(a.CaseID, b.CaseID))
	})
	return misses
}

// Markdown renders the scores and the misses.
func (r *Report) Markdown() string {
	var b strings.Builder
	b.WriteString("## Platform MCP tool eval\n\n")
	fmt.Fprintf(&b, "%d runs per case. Selection counts the first Platform tool reached past orientation reads: an expected tool or a case's allowed first step. Negatives are right when none is.\n\n", r.Config.Runs)
	for _, note := range r.Notes {
		b.WriteString("> " + note + "\n\n")
	}
	b.WriteString("| Model | Surface | Selection | Expected tool first | Valid args | Expected args | Negatives | Confirmed before preview | Turns | Prompt tokens/case | Cost | Errors |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, s := range r.Scores {
		fmt.Fprintf(&b, "| %s | %s | %.0f%% | %.0f%% | %.0f%% | %.0f%% | %d/%d | %d | %.1f | %.0f | $%.2f | %d/%d |\n",
			s.Model, s.Surface, 100*s.Selection, 100*s.ExpectedFirst, 100*s.SchemaValid, 100*s.ArgsMatch, s.NegativesRight, s.Negatives,
			s.ConfirmedFirst, s.MeanTurns, s.PromptTokensPerCase, s.CostUSD, s.Errors, s.Attempts)
	}
	r.writeErrors(&b)
	r.writeUnexpectedAnswers(&b)
	misses := r.Misses()
	if len(misses) == 0 {
		b.WriteString("\nNo misses.\n")
		return b.String()
	}
	b.WriteString("\n### Misses\n\n| Model | Surface | Case | Expected | Chosen instead | Wrong |\n|---|---|---|---|---|---|\n")
	for _, m := range misses {
		expected := strings.Join(m.Expected, ", ")
		if expected == "" {
			expected = "(none)"
		}
		chosen := make([]string, 0, len(m.Chosen))
		for tool, n := range m.Chosen {
			chosen = append(chosen, fmt.Sprintf("%s ×%d", tool, n))
		}
		slices.Sort(chosen)
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %d/%d |\n", m.Model, m.Surface, m.CaseID, expected, strings.Join(chosen, ", "), m.Wrong, m.Total)
	}
	return b.String()
}

// maxErrorBytes keeps one error line readable; provider errors can carry a
// whole response body.
const maxErrorBytes = 300

// writeErrors lists each distinct error once with how many attempts hit it,
// so a broken run says why instead of only how often.
func (r *Report) writeErrors(b *strings.Builder) {
	counts := make(map[string]int)
	var order []string
	for _, o := range r.Outcomes {
		if o.Err == nil {
			continue
		}
		// The case ID and turn lead every error; drop them so one cause
		// groups across cases.
		message := o.Err.Error()
		if _, rest, ok := strings.Cut(message, ": "); ok {
			message = rest
		}
		message = o.Surface + ": " + message
		if len(message) > maxErrorBytes {
			message = message[:maxErrorBytes] + "…"
		}
		if counts[message] == 0 {
			order = append(order, message)
		}
		counts[message]++
	}
	if len(order) == 0 {
		return
	}
	b.WriteString("\n### Errors\n\n")
	for _, message := range order {
		fmt.Fprintf(b, "- %d× %s\n", counts[message], strings.ReplaceAll(message, "\n", " "))
	}
}

// maxAnswerBytes keeps one answer line readable.
const maxAnswerBytes = 240

// writeUnexpectedAnswers shows what the agent said when it answered without a
// tool on a case that expected one, so a refusal, a clarifying question and a
// skipped preview can be told apart.
func (r *Report) writeUnexpectedAnswers(b *strings.Builder) {
	var lines []string
	for _, o := range r.Outcomes {
		if o.Err != nil || o.Selected != nil || o.Case.Negative() {
			continue
		}
		answer := strings.Join(strings.Fields(o.Answer), " ")
		if len(answer) > maxAnswerBytes {
			answer = answer[:maxAnswerBytes] + "…"
		}
		lines = append(lines, fmt.Sprintf("- %s / %s / %s: %s", o.Model, o.Surface, o.Case.ID, answer))
	}
	if len(lines) == 0 {
		return
	}
	b.WriteString("\n### Answered without a tool\n\n")
	b.WriteString(strings.Join(lines, "\n") + "\n")
}
