package efficacy

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
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
// VerdictSchema; ParseVerdict validates the required shape before decoding it,
// and the normalized value maps one-to-one onto skill_efficacy_scores
// (server/clickhouse/schema.sql:943-950).
type Verdict struct {
	Score           float64  `json:"score"`
	Rationale       string   `json:"rationale"`
	EstTurnsSaved   *float64 `json:"est_turns_saved"`
	EstMinutesSaved *float64 `json:"est_minutes_saved"`
	ROIConfidence   *string  `json:"roi_confidence"`
	Flags           []string `json:"flags"`
}

// sessionVerdict is the judge's raw structured answer: one indexed verdict
// per judged skill.
type sessionVerdict struct {
	Verdicts []indexedVerdict `json:"verdicts"`
}

type indexedVerdict struct {
	Index int `json:"index"`
	Verdict
}

// ParseSessionVerdict decodes the judge's raw structured output and normalizes
// it into one verdict per judged skill, ordered by the skill index the prompt
// assigned. Anything outside the contract — a missing or duplicated index, a
// count mismatch, unparseable JSON — is a model failure: the model returned
// something other than what it was asked for, and a retry can produce a
// different answer.
func ParseSessionVerdict(raw string, skills int) ([]Verdict, error) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()

	var parsed sessionVerdict
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("parse efficacy session verdict: %w: %w", analysis.ErrModelFailure, err)
	}
	if len(parsed.Verdicts) != skills {
		return nil, fmt.Errorf("parse efficacy session verdict: got %d verdicts for %d skills: %w", len(parsed.Verdicts), skills, analysis.ErrModelFailure)
	}

	ordered := make([]Verdict, skills)
	seen := make([]bool, skills)
	for _, entry := range parsed.Verdicts {
		if entry.Index < 0 || entry.Index >= skills || seen[entry.Index] {
			return nil, fmt.Errorf("parse efficacy session verdict: invalid skill index %d: %w", entry.Index, analysis.ErrModelFailure)
		}
		normalized, err := entry.Normalize()
		if err != nil {
			return nil, err
		}
		ordered[entry.Index] = normalized
		seen[entry.Index] = true
	}

	return ordered, nil
}

// SessionVerdictSchema is the judge's structured-output JSON schema.
// Deliberately no minimum/maximum on the numbers and no maxLength on the
// rationale: Anthropic routes (via Amazon Bedrock) reject those keywords with a
// 400, which would make every Anthropic route fail. Those bounds are enforced
// by Verdict.Normalize instead, which the sink's CHECK constraints require
// anyway.
func SessionVerdictSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"verdicts": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"index":             map[string]any{"type": "number"},
						"score":             map[string]any{"type": "number"},
						"rationale":         map[string]any{"type": "string"},
						"est_turns_saved":   map[string]any{"type": []string{"number", "null"}},
						"est_minutes_saved": map[string]any{"type": []string{"number", "null"}},
						"roi_confidence": map[string]any{
							"type": []string{"string", "null"},
							"enum": []any{"low", "med", "high", nil},
						},
						"flags": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string", "enum": verdictFlags},
						},
					},
					// Strict structured output requires every declared property to
					// be required; optionality is expressed by the null-typed
					// variants above.
					"required":             []string{"index", "score", "rationale", "est_turns_saved", "est_minutes_saved", "roi_confidence", "flags"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"verdicts"},
		"additionalProperties": false,
	}
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
		return Verdict{}, fmt.Errorf("efficacy verdict score is not finite: %w", analysis.ErrModelFailure)
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
