package efficacy

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	// maxRationaleRunes matches the sink's rationale_valid CHECK
	// (lengthUTF8(rationale) <= 200, server/clickhouse/schema.sql:953). Enforced
	// here rather than in the response schema, because a maxLength keyword makes
	// Anthropic routes reject the request outright (see VerdictSchema).
	maxRationaleRunes = 200
)

// roiConfidenceValues is the sink's roi_confidence_valid CHECK domain
// (server/clickhouse/schema.sql:957). Anything else normalizes to null.
var roiConfidenceValues = []string{"low", "med", "high"}

// verdictFlags is the sink's flags_valid CHECK domain
// (server/clickhouse/schema.sql:958). Unknown flags are dropped.
var verdictFlags = []string{"ignored", "misapplied", "partially_followed", "harmful"}

// Verdict is the judge's structured answer. Field names and shapes match
// VerdictSchema, so the model's raw output unmarshals straight into it, and the
// normalized value maps one-to-one onto skill_efficacy_scores
// (server/clickhouse/schema.sql:943-950).
type Verdict struct {
	Score           float64  `json:"score"`
	Rationale       string   `json:"rationale"`
	EstTurnsSaved   *float64 `json:"est_turns_saved"`
	EstMinutesSaved *float64 `json:"est_minutes_saved"`
	ROIConfidence   *string  `json:"roi_confidence"`
	Flags           []string `json:"flags"`
}

// ParseVerdict decodes the judge's raw structured output and normalizes it.
// Unparseable output is a model failure: the model returned something outside
// the contract it was given, and a retry can produce a different answer.
func ParseVerdict(raw string) (Verdict, error) {
	var v Verdict
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &v); err != nil {
		return Verdict{}, fmt.Errorf("parse efficacy verdict: %w: %w", ErrModelFailure, err)
	}
	return v.Normalize()
}

// Normalize forces the verdict inside every CHECK constraint
// skill_efficacy_scores carries (server/clickhouse/schema.sql:952-958). This is
// not cosmetic: ClickHouse rejects the WHOLE insert batch on a single CHECK
// violation, so one wild number from the model would drop every other score
// inserted alongside it.
//
// A non-finite score is the one unfixable case - clamping NaN would invent a
// score the judge never gave - so it is reported as a model failure. Every other
// out-of-domain value degrades to the sink's null/empty representation.
func (v Verdict) Normalize() (Verdict, error) {
	if math.IsNaN(v.Score) || math.IsInf(v.Score, 0) {
		return Verdict{}, fmt.Errorf("efficacy verdict score is not finite: %w", ErrModelFailure)
	}

	rationale := strings.TrimSpace(v.Rationale)
	if utf8.RuneCountInString(rationale) > maxRationaleRunes {
		// Cut by rune, not byte: a byte cut can split a multi-byte character into
		// invalid UTF-8, which lengthUTF8 then counts differently than Go does.
		rationale = string([]rune(rationale)[:maxRationaleRunes])
	}

	roiConfidence := v.ROIConfidence
	if roiConfidence != nil && !slices.Contains(roiConfidenceValues, *roiConfidence) {
		roiConfidence = nil
	}

	var flags []string
	for _, f := range v.Flags {
		if slices.Contains(verdictFlags, f) && !slices.Contains(flags, f) {
			flags = append(flags, f)
		}
	}

	return Verdict{
		Score:           max(0, min(1, v.Score)),
		Rationale:       rationale,
		EstTurnsSaved:   normalizeROIEstimate(v.EstTurnsSaved),
		EstMinutesSaved: normalizeROIEstimate(v.EstMinutesSaved),
		ROIConfidence:   roiConfidence,
		Flags:           flags,
	}, nil
}

// normalizeROIEstimate keeps an estimate only when the sink would accept it:
// finite and non-negative (server/clickhouse/schema.sql:954-955). A rejected
// estimate becomes null - "the judge did not estimate this" - which is exactly
// what an impossible number means.
func normalizeROIEstimate(v *float64) *float64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 {
		return nil
	}
	return v
}
