package tooleval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Baseline is how often each case passed on each surface, for one model,
// as committed in baseline.json. The pull request gate fails when a case that
// passed in most baseline runs now fails in most runs.
type Baseline struct {
	Model string `json:"model"`
	// Cases maps a case ID to its result on each surface.
	Cases map[string]map[string]CaseResult `json:"cases"`
}

// CaseResult counts the graded runs of one case on one surface. Errored runs
// are left out, so they neither pass nor fail.
type CaseResult struct {
	Passes int `json:"passes"`
	Runs   int `json:"runs"`
}

// Passing reports whether the case passed in most of its runs.
func (r CaseResult) Passing() bool { return r.Runs > 0 && 2*r.Passes > r.Runs }

// Failing reports whether the case failed in most of its runs.
func (r CaseResult) Failing() bool { return r.Runs > 0 && 2*r.Passes < r.Runs }

// Pass reports whether an attempt did what the case asks: an expected tool
// with valid and expected arguments, an allowed first step, or, on a
// negative case, a final answer without a Platform tool.
func (o Outcome) Pass() bool {
	if o.Err != nil {
		return false
	}
	if o.OnPath {
		return true
	}
	return o.Hit && (o.Case.Negative() || (o.SchemaValid && (len(o.Case.Args) == 0 || o.ArgsMatch)))
}

// Results counts one model's graded runs per case and surface.
func (r *Report) Results(model string) map[string]map[string]CaseResult {
	results := make(map[string]map[string]CaseResult)
	for _, o := range r.Outcomes {
		if o.Model != model || o.Err != nil {
			continue
		}
		bySurface, ok := results[o.Case.ID]
		if !ok {
			bySurface = make(map[string]CaseResult)
			results[o.Case.ID] = bySurface
		}
		result := bySurface[o.Surface]
		result.Runs++
		if o.Pass() {
			result.Passes++
		}
		bySurface[o.Surface] = result
	}
	return results
}

// Merge returns the baseline with these results replacing the entries they
// cover, and without cases that no longer exist.
func (b Baseline) Merge(model string, results map[string]map[string]CaseResult, cases []Case) Baseline {
	merged := Baseline{Model: model, Cases: make(map[string]map[string]CaseResult, len(cases))}
	for _, c := range cases {
		bySurface := make(map[string]CaseResult)
		if b.Model == model {
			maps.Copy(bySurface, b.Cases[c.ID])
		}
		maps.Copy(bySurface, results[c.ID])
		if len(bySurface) > 0 {
			merged.Cases[c.ID] = bySurface
		}
	}
	return merged
}

// Marshal renders the baseline the way baseline.json stores it.
func (b Baseline) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(b); err != nil {
		return nil, fmt.Errorf("encode baseline: %w", err)
	}
	return buf.Bytes(), nil
}

// Regression is a case that passed in the baseline and now fails.
type Regression struct {
	CaseID, Surface string
	Before, After   CaseResult
}

// Regressions lists the cases that passed in most baseline runs and failed in
// most runs now. A case with no baseline entry, or that only became flaky,
// is not a regression.
func Regressions(b Baseline, results map[string]map[string]CaseResult) []Regression {
	var regressions []Regression
	for id, bySurface := range results {
		for surface, after := range bySurface {
			before, ok := b.Cases[id][surface]
			if ok && before.Passing() && after.Failing() {
				regressions = append(regressions, Regression{CaseID: id, Surface: surface, Before: before, After: after})
			}
		}
	}
	slices.SortFunc(regressions, func(a, b Regression) int {
		return strings.Compare(a.CaseID+"/"+a.Surface, b.CaseID+"/"+b.Surface)
	})
	return regressions
}

// RegressionsMarkdown renders regressions for the report and the failure.
func RegressionsMarkdown(regressions []Regression) string {
	if len(regressions) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("### Regressions\n\nThese cases passed in the committed baseline and now fail in most runs.\n\n| Case | Surface | Baseline | Now |\n|---|---|---|---|\n")
	for _, r := range regressions {
		fmt.Fprintf(&b, "| %s | %s | %d/%d | %d/%d |\n", r.CaseID, r.Surface, r.Before.Passes, r.Before.Runs, r.After.Passes, r.After.Runs)
	}
	return b.String()
}
