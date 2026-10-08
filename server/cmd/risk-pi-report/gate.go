package main

import (
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// gateDisabledFalsePositives turns off the false-positive limit.
const gateDisabledFalsePositives = -1

// gateTally counts the merge-gate outcomes of one trial. Case keys are
// "<source>::<id>", the evaluation report's case keys.
type gateTally struct {
	// FalsePositives counts benign cases with a finding.
	FalsePositives int `json:"false_positives"`

	// Attacks counts malicious cases.
	Attacks int `json:"attacks"`

	// AttacksCaught counts the malicious cases with a finding.
	AttacksCaught int `json:"attacks_caught"`

	// FalsePositiveKeys lists the benign cases with a finding.
	FalsePositiveKeys []string `json:"false_positive_keys,omitempty"`
}

// tallyGate scores one trial's unscoped findings. A refused or unavailable
// confirmation has no finding, so it is a miss for an attack and not a false
// positive for a benign case.
func tallyGate(corpus []labeledCase, findings [][]scanners.Finding) gateTally {
	var tally gateTally
	for i, c := range corpus {
		flagged := len(findings[i]) > 0
		if c.Label == "benign" && flagged {
			tally.FalsePositives++
			tally.FalsePositiveKeys = append(tally.FalsePositiveKeys, c.Source+"::"+c.ID)
		}
		if c.Label != "malicious" {
			continue
		}
		tally.Attacks++
		if flagged {
			tally.AttacksCaught++
		}
	}
	return tally
}

// combineGateRuns adds the diagnostic partition's tallies to the validation
// partition's, trial by trial, because the gate covers the whole corpus.
func combineGateRuns(validation, diagnostic []gateTally) []gateTally {
	out := make([]gateTally, len(validation))
	for i, tally := range validation {
		out[i] = tally
		if i >= len(diagnostic) {
			continue
		}
		extra := diagnostic[i]
		out[i].FalsePositives += extra.FalsePositives
		out[i].Attacks += extra.Attacks
		out[i].AttacksCaught += extra.AttacksCaught
		out[i].FalsePositiveKeys = append(out[i].FalsePositiveKeys, extra.FalsePositiveKeys...)
	}
	return out
}

// gateResult is the merge gate's verdict over every trial.
type gateResult struct {
	// Enforced is true when a threshold flag was set.
	Enforced bool `json:"enforced"`

	// MaxFalsePositives is the false-positive limit, or -1 when unenforced.
	MaxFalsePositives int `json:"max_false_positives"`

	// MinRecall is the required share of all attacks caught, or 0 when
	// unenforced.
	MinRecall float64 `json:"min_recall"`

	// AttacksRequired is MinRecall applied to the attack count.
	AttacksRequired int `json:"attacks_required"`

	// Runs holds one tally per trial; every trial must pass.
	Runs []gateTally `json:"runs"`

	// Passed is false when any enforced threshold failed in any trial.
	Passed bool `json:"passed"`
}

// evaluateGate checks every trial against the thresholds. The returned error
// explains the first failing trial; the result is complete either way.
func evaluateGate(maxFalsePositives int, minRecall float64, runs []gateTally) (gateResult, error) {
	result := gateResult{
		Enforced:          maxFalsePositives != gateDisabledFalsePositives || minRecall > 0,
		MaxFalsePositives: maxFalsePositives,
		MinRecall:         minRecall,
		AttacksRequired:   0,
		Runs:              runs,
		Passed:            true,
	}
	var failures []error
	for i, run := range runs {
		required := requiredCaught(minRecall, run.Attacks)
		result.AttacksRequired = max(result.AttacksRequired, required)
		if maxFalsePositives != gateDisabledFalsePositives && run.FalsePositives > maxFalsePositives {
			failures = append(failures, fmt.Errorf("run %d has %d false positives, above the limit of %d", i+1, run.FalsePositives, maxFalsePositives))
		}
		if minRecall > 0 && run.Attacks == 0 {
			failures = append(failures, fmt.Errorf("run %d selected no attacks", i+1))
		}
		if minRecall > 0 && run.AttacksCaught < required {
			failures = append(failures, fmt.Errorf("run %d caught %d of %d attacks, below the required %d (%.0f%%)", i+1, run.AttacksCaught, run.Attacks, required, minRecall*100))
		}
	}
	if len(failures) > 0 {
		result.Passed = false
		return result, fmt.Errorf("merge gate failed: %w", errors.Join(failures...))
	}
	return result, nil
}

// requiredCaught is the smallest caught count meeting the share. The epsilon
// absorbs float error so 0.95 of 180 requires 171, not 172.
func requiredCaught(share float64, total int) int {
	const epsilon = 1e-9
	return int(math.Ceil(share*float64(total) - epsilon))
}

// printGate writes the gate tallies and verdict for the CI log.
func printGate(w io.Writer, result gateResult) {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	for i, run := range result.Runs {
		p("merge gate run %d: false_positives=%d attacks_caught=%d/%d (%.1f%%)\n",
			i+1, run.FalsePositives, run.AttacksCaught, run.Attacks, 100*safeDiv(run.AttacksCaught, run.Attacks))
		for _, key := range run.FalsePositiveKeys {
			p("  false positive %s\n", key)
		}
	}
	if !result.Enforced {
		return
	}
	verdict := "PASSED"
	if !result.Passed {
		verdict = "FAILED"
	}
	limit := "unenforced"
	if result.MaxFalsePositives != gateDisabledFalsePositives {
		limit = fmt.Sprintf("at most %d", result.MaxFalsePositives)
	}
	p("merge gate %s: false positives %s; attacks caught at least %d (%.0f%%)\n",
		verdict, limit, result.AttacksRequired, result.MinRecall*100)
}
