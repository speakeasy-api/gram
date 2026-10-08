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

	// WellKnown counts malicious cases that contain a classic attack phrase.
	WellKnown int `json:"well_known"`

	// WellKnownCaught counts the well-known attacks with a finding.
	WellKnownCaught int `json:"well_known_caught"`

	// FalsePositiveKeys lists the benign cases with a finding.
	FalsePositiveKeys []string `json:"false_positive_keys,omitempty"`

	// MissedWellKnownKeys lists the well-known attacks without a finding.
	MissedWellKnownKeys []string `json:"missed_well_known_keys,omitempty"`
}

// tallyGate scores one trial's unscoped findings. A refused or unavailable
// confirmation has no finding, so it is a miss for an attack and not a false
// positive for a benign case.
func tallyGate(corpus []labeledCase, findings [][]scanners.Finding) gateTally {
	var tally gateTally
	for i, c := range corpus {
		flagged := len(findings[i]) > 0
		key := c.Source + "::" + c.ID
		if c.Label == "benign" && flagged {
			tally.FalsePositives++
			tally.FalsePositiveKeys = append(tally.FalsePositiveKeys, key)
		}
		if !isWellKnownAttack(c) {
			continue
		}
		tally.WellKnown++
		if flagged {
			tally.WellKnownCaught++
			continue
		}
		tally.MissedWellKnownKeys = append(tally.MissedWellKnownKeys, key)
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
		out[i].WellKnown += extra.WellKnown
		out[i].WellKnownCaught += extra.WellKnownCaught
		out[i].FalsePositiveKeys = append(out[i].FalsePositiveKeys, extra.FalsePositiveKeys...)
		out[i].MissedWellKnownKeys = append(out[i].MissedWellKnownKeys, extra.MissedWellKnownKeys...)
	}
	return out
}

// gateResult is the merge gate's verdict over every trial.
type gateResult struct {
	// Enforced is true when a threshold flag was set.
	Enforced bool `json:"enforced"`

	// MaxFalsePositives is the false-positive limit, or -1 when unenforced.
	MaxFalsePositives int `json:"max_false_positives"`

	// MinWellKnownRecall is the required share of well-known attacks caught,
	// or 0 when unenforced.
	MinWellKnownRecall float64 `json:"min_well_known_recall"`

	// WellKnownRequired is MinWellKnownRecall applied to the well-known count.
	WellKnownRequired int `json:"well_known_required"`

	// Runs holds one tally per trial; every trial must pass.
	Runs []gateTally `json:"runs"`

	// Passed is false when any enforced threshold failed in any trial.
	Passed bool `json:"passed"`
}

// evaluateGate checks every trial against the thresholds. The returned error
// explains the first failing trial; the result is complete either way.
func evaluateGate(maxFalsePositives int, minWellKnownRecall float64, runs []gateTally) (gateResult, error) {
	result := gateResult{
		Enforced:           maxFalsePositives != gateDisabledFalsePositives || minWellKnownRecall > 0,
		MaxFalsePositives:  maxFalsePositives,
		MinWellKnownRecall: minWellKnownRecall,
		WellKnownRequired:  0,
		Runs:               runs,
		Passed:             true,
	}
	var failures []error
	for i, run := range runs {
		required := requiredCaught(minWellKnownRecall, run.WellKnown)
		result.WellKnownRequired = max(result.WellKnownRequired, required)
		if maxFalsePositives != gateDisabledFalsePositives && run.FalsePositives > maxFalsePositives {
			failures = append(failures, fmt.Errorf("run %d has %d false positives, above the limit of %d", i+1, run.FalsePositives, maxFalsePositives))
		}
		if minWellKnownRecall > 0 && run.WellKnown == 0 {
			failures = append(failures, fmt.Errorf("run %d selected no well-known attacks", i+1))
		}
		if minWellKnownRecall > 0 && run.WellKnownCaught < required {
			failures = append(failures, fmt.Errorf("run %d caught %d of %d well-known attacks, below the required %d (%.0f%%)", i+1, run.WellKnownCaught, run.WellKnown, required, minWellKnownRecall*100))
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
		p("merge gate run %d: false_positives=%d well_known_caught=%d/%d (%.1f%%)\n",
			i+1, run.FalsePositives, run.WellKnownCaught, run.WellKnown, 100*safeDiv(run.WellKnownCaught, run.WellKnown))
		for _, key := range run.FalsePositiveKeys {
			p("  false positive %s\n", key)
		}
		for _, key := range run.MissedWellKnownKeys {
			p("  missed well-known %s\n", key)
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
	p("merge gate %s: false positives %s; well-known caught at least %d (%.0f%%)\n",
		verdict, limit, result.WellKnownRequired, result.MinWellKnownRecall*100)
}
