package bench

import (
	"fmt"
	"strings"
)

// BaselineArm is the arm every candidate is compared with: search_chats, what
// users already have.
const BaselineArm = "B: search_chats"

// Rollout gate bars. A candidate arm must meet every one before
// Organizational Memory turns on for customers. The recall block size gate
// (under 8k tokens) is asserted by the arm's mechanical probe, not here.
const (
	gateHardSetLift      = 0.15
	gateMinAccuracy      = 0.80
	gateMinCitationRate  = 0.95
	gateMinNotFoundRate  = 0.80
	gateMaxToolTokenRate = 2.0

	// gateEpsilon absorbs float error so 4 of 5 meets an 80% bar.
	gateEpsilon = 1e-9
)

// GateStatus is the outcome of one gate.
type GateStatus string

const (
	GatePass GateStatus = "pass"
	GateFail GateStatus = "fail"

	// GateUnknown is a gate the run can't decide, such as a comparison with
	// a baseline the run didn't include.
	GateUnknown GateStatus = "unknown"
)

// Gate is one rollout gate for one candidate arm.
type Gate struct {
	Name   string     `json:"name"`
	Bar    string     `json:"bar"`
	Value  string     `json:"value"`
	Status GateStatus `json:"status"`
}

// GateResult is every gate for one answer model in one candidate arm.
type GateResult struct {
	Model    string `json:"model"`
	Arm      string `json:"arm"`
	Baseline string `json:"baseline"`
	Gates    []Gate `json:"gates"`

	// Passed is set only when every gate passed.
	Passed bool `json:"passed"`
}

// EvaluateGates scores every candidate arm against the rollout gates, per
// answer model, in report order. A candidate is any arm with tools other than
// the baseline.
func EvaluateGates(r *Report) []GateResult {
	baselines := map[string]ArmScore{}
	for _, s := range r.Scores {
		if s.Arm == BaselineArm {
			baselines[s.Model] = s
		}
	}
	results := []GateResult{}
	for _, s := range r.Scores {
		if !s.HasTools || s.Arm == BaselineArm {
			continue
		}
		var base *ArmScore
		if b, ok := baselines[s.Model]; ok {
			base = &b
		}
		gates := evaluateGates(s, base)
		passed := true
		for _, g := range gates {
			passed = passed && g.Status == GatePass
		}
		results = append(results, GateResult{Model: s.Model, Arm: s.Arm, Baseline: BaselineArm, Gates: gates, Passed: passed})
	}
	return results
}

func evaluateGates(s ArmScore, base *ArmScore) []Gate {
	gates := make([]Gate, 0, 7)

	gates = append(gates, Gate{
		Name:   "Complete run",
		Bar:    "every answer graded",
		Value:  fmt.Sprintf("%d ungraded (%d errored twice, graded wrong)", s.Ungraded, s.Errors),
		Status: status(s.Ungraded == 0),
	})

	hard := Gate{Name: "Hard-set lift", Bar: fmt.Sprintf("at least B + %.0f points", 100*gateHardSetLift), Value: pct(s.HardAccuracy()) + " (no baseline)", Status: GateUnknown}
	if base != nil {
		hard.Value = fmt.Sprintf("%s vs B %s", pct(s.HardAccuracy()), pct(base.HardAccuracy()))
		hard.Status = status(s.HardAccuracy()+gateEpsilon >= base.HardAccuracy()+gateHardSetLift)
	}
	gates = append(gates, hard)

	accuracy := Gate{Name: "Overall accuracy", Bar: fmt.Sprintf("at least %s and at least B", pct(gateMinAccuracy)), Value: "", Status: GateUnknown}
	meetsFloor := s.Accuracy+gateEpsilon >= gateMinAccuracy
	switch {
	case len(s.AccuracyByRun) == 0:
		accuracy.Value, accuracy.Status = "nothing graded", GateFail
	case base == nil:
		accuracy.Value = pct(s.Accuracy) + " (no baseline)"
		if !meetsFloor {
			accuracy.Status = GateFail
		}
	default:
		accuracy.Value = fmt.Sprintf("%s vs B %s", pct(s.Accuracy), pct(base.Accuracy))
		accuracy.Status = status(meetsFloor && s.Accuracy+gateEpsilon >= base.Accuracy)
	}
	gates = append(gates, accuracy)

	gates = append(gates, Gate{
		Name:   "Privacy",
		Bar:    "0 leaks and 0 isolation violations",
		Value:  fmt.Sprintf("%d leaks, %d isolation violations", len(s.Leaks), len(s.IsolationViolations)),
		Status: status(len(s.Leaks) == 0 && len(s.IsolationViolations) == 0),
	})

	citations := Gate{Name: "Citations", Bar: "valid at least " + pct(gateMinCitationRate), Value: "nothing answerable graded", Status: GateFail}
	if rate, ok := s.CitationRate(); ok {
		citations.Value = pct(rate)
		citations.Status = status(rate+gateEpsilon >= gateMinCitationRate)
	}
	gates = append(gates, citations)

	negatives := Gate{Name: "Negatives", Bar: fmt.Sprintf("at least %s answered \"Not found\"", pct(gateMinNotFoundRate)), Value: "no negatives graded", Status: GateUnknown}
	if s.Negatives > 0 {
		rate := float64(s.NotFound) / float64(s.Negatives)
		negatives.Value = fmt.Sprintf("%d/%d", s.NotFound, s.Negatives)
		negatives.Status = status(rate+gateEpsilon >= gateMinNotFoundRate)
	}
	gates = append(gates, negatives)

	tokens := Gate{Name: "Tool tokens", Bar: fmt.Sprintf("at most %.0fx B per question", gateMaxToolTokenRate), Value: fmt.Sprintf("%d (no baseline)", s.ToolTokensPerQuestion), Status: GateUnknown}
	if base != nil && base.ToolTokensPerQuestion > 0 {
		ratio := float64(s.ToolTokensPerQuestion) / float64(base.ToolTokensPerQuestion)
		tokens.Value = fmt.Sprintf("%d vs B %d (%.1fx)", s.ToolTokensPerQuestion, base.ToolTokensPerQuestion, ratio)
		tokens.Status = status(ratio <= gateMaxToolTokenRate+gateEpsilon)
	}
	gates = append(gates, tokens)

	return gates
}

func status(ok bool) GateStatus {
	if ok {
		return GatePass
	}
	return GateFail
}

func pct(rate float64) string {
	return fmt.Sprintf("%.0f%%", 100*rate)
}

// gatesMarkdown renders one table per candidate arm.
func gatesMarkdown(results []GateResult) string {
	if len(results) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Rollout gates\n\nEach candidate arm against ")
	b.WriteString(BaselineArm)
	b.WriteString(" with the same answer model. The recall block size gate is checked by the arm's mechanical probe, not here.\n")
	for _, result := range results {
		passed := 0
		for _, g := range result.Gates {
			if g.Status == GatePass {
				passed++
			}
		}
		verdict := "FAIL"
		if result.Passed {
			verdict = "PASS"
		}
		fmt.Fprintf(&b, "\n### %s, %s: %s (%d of %d gates)\n\n", result.Arm, result.Model, verdict, passed, len(result.Gates))
		b.WriteString("| Gate | Bar | Result | Status |\n| --- | --- | --- | --- |\n")
		for _, g := range result.Gates {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", g.Name, g.Bar, g.Value, strings.ToUpper(string(g.Status)))
		}
	}
	return b.String()
}
