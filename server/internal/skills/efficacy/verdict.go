package efficacy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	domainskills "github.com/speakeasy-api/gram/server/internal/skills"
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

// recommendationConfidenceValues is deliberately separate from ROI confidence:
// it qualifies whether raw feedback evidence is reliable enough to retain.
var recommendationConfidenceValues = []string{"low", "med", "high"}

var recommendationIssueTypes = []string{
	"requirement_omitted",
	"priority_violated",
	"guidance_gap",
	"prohibition_violated",
	"harmful_overconstraint",
	"obsolete_guidance",
}

var recommendationChangeTypes = []string{
	"reinforce_existing_requirement",
	"reinforce_existing_priority",
	"add_missing_requirement",
	"reinforce_existing_prohibition",
	"relax_constraint",
	"replace_obsolete_guidance",
}

// verdictFlags is the sink's flags_valid CHECK domain
// (server/clickhouse/schema.sql:958). Unknown flags are dropped.
var verdictFlags = []string{"ignored", "misapplied", "partially_followed", "harmful"}

// recommendationOutcomes deliberately excludes helped: recommendations are raw
// non-positive feedback candidates, not active feedback records.
var recommendationOutcomes = []string{"partially_helped", "did_not_help", "misleading", "harmful"}

// RawRecommendation is evidence the judge recommends for downstream feedback
// handling. It is not a find/replace edit or an edit suggestion.
type RawRecommendation struct {
	IssueType              string `json:"issue_type"`
	ChangeType             string `json:"change_type"`
	EvidenceMessageIndices []int  `json:"evidence_message_indices"`
	Outcome                string `json:"outcome"`
	Note                   string `json:"note"`
	Confidence             string `json:"confidence"`
}

// Verdict is the judge's structured answer. Field names and shapes match
// VerdictSchema; ParseVerdict validates the required shape before decoding it.
// The score fields map onto skill_efficacy_scores; Recommendations remain raw
// judge output for possible downstream handling.
type Verdict struct {
	Score           float64             `json:"score"`
	Rationale       string              `json:"rationale"`
	EstTurnsSaved   *float64            `json:"est_turns_saved"`
	EstMinutesSaved *float64            `json:"est_minutes_saved"`
	ROIConfidence   *string             `json:"roi_confidence"`
	Flags           []string            `json:"flags"`
	Recommendations []RawRecommendation `json:"recommendations"`
}

// ParseVerdict decodes the judge's raw structured output and normalizes it.
// Recommendation evidence must cite message indices in the shown transcript.
// Unparseable output is a model failure: the model returned something outside
// the contract it was given, and a retry can produce a different answer.
func ParseVerdict(raw string, transcript Transcript) (Verdict, error) {

	required := map[string]bool{
		"score":             false,
		"rationale":         false,
		"est_turns_saved":   true,
		"est_minutes_saved": true,
		"roi_confidence":    true,
		"flags":             false,
		"recommendations":   false,
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &fields); err != nil {
		return Verdict{}, fmt.Errorf("parse efficacy verdict: %w: %w", ErrModelFailure, err)
	}
	for name, value := range fields {
		if _, ok := required[name]; !ok {
			return Verdict{}, fmt.Errorf("parse efficacy verdict: unknown field %q: %w", name, ErrModelFailure)
		}
		if !required[name] && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return Verdict{}, fmt.Errorf("parse efficacy verdict: field %q must not be null: %w", name, ErrModelFailure)
		}
	}
	for name := range required {
		if _, ok := fields[name]; !ok {
			return Verdict{}, fmt.Errorf("parse efficacy verdict: missing field %q: %w", name, ErrModelFailure)
		}
	}
	if err := validateRawRecommendations(fields["recommendations"]); err != nil {
		return Verdict{}, err
	}

	var v Verdict
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &v); err != nil {
		return Verdict{}, fmt.Errorf("parse efficacy verdict: %w: %w", ErrModelFailure, err)
	}
	if err := validateRecommendationEvidence(v.Recommendations, &transcript); err != nil {
		return Verdict{}, err
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

	if err := validateRecommendationEvidence(v.Recommendations, nil); err != nil {
		return Verdict{}, err
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

	recommendations := make([]RawRecommendation, 0, len(v.Recommendations))
	for i, recommendation := range v.Recommendations {
		if !slices.Contains(recommendationIssueTypes, recommendation.IssueType) {
			return Verdict{}, fmt.Errorf("efficacy verdict recommendation %d has invalid issue type %q: %w", i, recommendation.IssueType, ErrModelFailure)
		}
		if !slices.Contains(recommendationChangeTypes, recommendation.ChangeType) {
			return Verdict{}, fmt.Errorf("efficacy verdict recommendation %d has invalid change type %q: %w", i, recommendation.ChangeType, ErrModelFailure)
		}
		if !slices.Contains(recommendationOutcomes, recommendation.Outcome) {
			return Verdict{}, fmt.Errorf("efficacy verdict recommendation %d has invalid outcome %q: %w", i, recommendation.Outcome, ErrModelFailure)
		}
		if !slices.Contains(recommendationConfidenceValues, recommendation.Confidence) {
			return Verdict{}, fmt.Errorf("efficacy verdict recommendation %d has invalid confidence %q: %w", i, recommendation.Confidence, ErrModelFailure)
		}
		note := []rune(strings.TrimSpace(recommendation.Note))
		if len(note) == 0 {
			return Verdict{}, fmt.Errorf("efficacy verdict recommendation %d has an empty note: %w", i, ErrModelFailure)
		}
		if len(note) > domainskills.MaxFeedbackNoteRunes {
			note = note[:domainskills.MaxFeedbackNoteRunes]
		}
		normalized := RawRecommendation{
			IssueType:              recommendation.IssueType,
			ChangeType:             recommendation.ChangeType,
			EvidenceMessageIndices: slices.Clone(recommendation.EvidenceMessageIndices),
			Outcome:                recommendation.Outcome,
			Note:                   string(note),
			Confidence:             recommendation.Confidence,
		}
		if slices.ContainsFunc(recommendations, func(existing RawRecommendation) bool {
			// Taxonomy and evidence are transient judge metadata. Persistence and its
			// stable feedback ID use only these three fields, so normalize to that
			// same identity before publishing.
			return existing.Outcome == normalized.Outcome &&
				existing.Note == normalized.Note &&
				existing.Confidence == normalized.Confidence
		}) {
			continue
		}
		recommendations = append(recommendations, normalized)
	}

	return Verdict{
		Score:           max(0, min(1, v.Score)),
		Rationale:       rationale,
		EstTurnsSaved:   normalizeROIEstimate(v.EstTurnsSaved),
		EstMinutesSaved: normalizeROIEstimate(v.EstMinutesSaved),
		ROIConfidence:   roiConfidence,
		Flags:           flags,
		Recommendations: recommendations,
	}, nil
}

func validateRawRecommendations(raw json.RawMessage) error {
	var recommendations []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &recommendations); err != nil {
		return fmt.Errorf("parse efficacy verdict recommendations: %w: %w", ErrModelFailure, err)
	}
	required := []string{"issue_type", "change_type", "evidence_message_indices", "outcome", "note", "confidence"}
	for i, recommendation := range recommendations {
		for name, value := range recommendation {
			if !slices.Contains(required, name) {
				return fmt.Errorf("parse efficacy verdict recommendation %d: unknown field %q: %w", i, name, ErrModelFailure)
			}
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return fmt.Errorf("parse efficacy verdict recommendation %d: field %q must not be null: %w", i, name, ErrModelFailure)
			}
		}
		for _, name := range required {
			if _, ok := recommendation[name]; !ok {
				return fmt.Errorf("parse efficacy verdict recommendation %d: missing field %q: %w", i, name, ErrModelFailure)
			}
		}
	}
	return nil
}

func validateRecommendationEvidence(recommendations []RawRecommendation, transcript *Transcript) error {
	var shownIndices map[int]struct{}
	if transcript != nil {
		shownIndices = make(map[int]struct{}, len(transcript.Messages))
		for _, message := range transcript.Messages {
			shownIndices[message.Index] = struct{}{}
		}
	}

	for i, recommendation := range recommendations {
		if len(recommendation.EvidenceMessageIndices) == 0 {
			return fmt.Errorf("efficacy verdict recommendation %d has no evidence message indices: %w", i, ErrModelFailure)
		}

		seen := make(map[int]struct{}, len(recommendation.EvidenceMessageIndices))
		previous := 0
		for j, index := range recommendation.EvidenceMessageIndices {
			if index <= 0 {
				return fmt.Errorf("efficacy verdict recommendation %d has non-positive evidence message index %d: %w", i, index, ErrModelFailure)
			}
			if _, ok := seen[index]; ok {
				return fmt.Errorf("efficacy verdict recommendation %d has duplicate evidence message index %d: %w", i, index, ErrModelFailure)
			}
			if j > 0 && index < previous {
				return fmt.Errorf("efficacy verdict recommendation %d has unsorted evidence message indices: %w", i, ErrModelFailure)
			}
			if shownIndices != nil {
				if _, ok := shownIndices[index]; !ok {
					return fmt.Errorf("efficacy verdict recommendation %d cites message index %d that is not shown: %w", i, index, ErrModelFailure)
				}
			}
			seen[index] = struct{}{}
			previous = index
		}
	}
	return nil
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
