package researchagent

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// ReportVersion is the shape version of the report document this package
// produces. It is stored on every report row, so bump it when Document's
// shape changes.
const ReportVersion = 1

// Claim provenance tiers. The separation is the report's core honesty
// property: an admin must always be able to tell what a third party wrote
// from what the vendor says about itself. Deterministic facts are not a
// report tier at all — the evidence panel already shows them, so the report
// restating them is noise (TierObserved remains only so old reports keep
// rendering; new reports never carry it).
const (
	TierObserved              = "observed"
	TierIndependentlyReported = "independently_reported"
	TierVendorClaim           = "vendor_claim"
)

// maxReportClaims caps a report at its most relevant findings. Research runs
// surface dozens of statements; an admin reads five.
const maxReportClaims = 5

// Coverage levels, describing how much independent material exists at all.
const (
	CoverageNone        = "none"
	CoverageThin        = "thin"
	CoverageModerate    = "moderate"
	CoverageSubstantial = "substantial"
)

// Document is one research run's structured findings.
type Document struct {
	// Summary is a short, neutral overview of what the research found —
	// never a recommendation.
	Summary string `json:"summary"`

	// Coverage reports how much independent material exists. Thin or absent
	// coverage is the headline finding, not a gap to paper over.
	Coverage Coverage `json:"coverage"`

	// Claims are the findings, each carrying its provenance tier and
	// citations.
	Claims []Claim `json:"claims,omitempty"`

	// Run records what produced this report. Filled by the runner, never by
	// the model.
	Run RunMeta `json:"run"`
}

// Coverage describes the independent-coverage situation.
type Coverage struct {
	// Level is one of the Coverage* constants.
	Level string `json:"level"`

	// Note says what the level rests on, e.g. "no third-party mentions
	// found; every source is the vendor's own".
	Note string `json:"note,omitempty"`
}

// Claim is one finding with its provenance.
type Claim struct {
	// Tier is one of the Tier* constants.
	Tier string `json:"tier"`

	Text string `json:"text"`

	// Citations name where the claim came from. Required for web-sourced
	// tiers — a web claim without a citation is dropped at validation, since
	// an untraceable claim is one the admin cannot defend. Observed-tier
	// claims cite the deterministic briefing and may carry none.
	Citations []Citation `json:"citations,omitempty"`
}

// Citation is one source reference.
type Citation struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// RunMeta records what produced the report, for the audit trail and for
// spend accounting.
type RunMeta struct {
	Model         string `json:"model"`
	PromptVersion string `json:"prompt_version"`

	// PromptTokens and CompletionTokens are summed across every completion
	// in the run, including the extraction pass.
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`

	// Searches and Fetches count tool calls the agent made.
	Searches int `json:"searches"`
	Fetches  int `json:"fetches"`

	// Turns is how many completion rounds the agent loop ran.
	Turns int `json:"turns"`

	// TurnLimitReached reports the loop hit its round cap and the report was
	// extracted from an unfinished transcript.
	TurnLimitReached bool `json:"turn_limit_reached,omitempty"`

	// DroppedUncitedClaims counts web-tier claims removed at validation for
	// carrying no citation.
	DroppedUncitedClaims int `json:"dropped_uncited_claims,omitempty"`
}

// extractionSchema is the JSON schema the extraction pass is held to. Kept as
// a raw schema rather than reflection so the enum constraints are explicit.
// Shaped for native strict structured outputs, which require every property
// listed in required — optionality is expressed as nullability, and Go's
// decoder reads null as the zero value.
var extractionSchema = json.RawMessage(`{
	"type": "object",
	"additionalProperties": false,
	"required": ["summary", "coverage", "claims"],
	"properties": {
		"summary": {
			"type": "string",
			"description": "Short neutral overview of what the research found. Never a recommendation or verdict."
		},
		"coverage": {
			"type": "object",
			"additionalProperties": false,
			"required": ["level", "note"],
			"properties": {
				"level": {
					"type": "string",
					"enum": ["none", "thin", "moderate", "substantial"],
					"description": "How much INDEPENDENT (non-vendor) material exists."
				},
				"note": {
					"type": ["string", "null"],
					"description": "What the level rests on."
				}
			}
		},
		"claims": {
			"type": "array",
			"description": "At most the 5 most relevant findings, most relevant first. Never restate deterministic briefing facts — the reader already sees those.",
			"items": {
				"type": "object",
				"additionalProperties": false,
				"required": ["tier", "text", "citations"],
				"properties": {
					"tier": {
						"type": "string",
						"enum": ["independently_reported", "vendor_claim"],
						"description": "independently_reported = a third party wrote it about the server or its vendor; vendor_claim = the server's own vendor says it about itself."
					},
					"text": {"type": "string"},
					"citations": {
						"type": "array",
						"description": "Where each web-sourced claim came from. Empty only for observed-tier claims.",
						"items": {
							"type": "object",
							"additionalProperties": false,
							"required": ["url", "title"],
							"properties": {
								"url": {"type": "string"},
								"title": {"type": ["string", "null"]}
							}
						}
					}
				}
			}
		}
	}
}`)

// validate drops web-tier claims without citations — an untraceable web claim
// is one the admin cannot defend — and rejects unknown tiers and levels so a
// drifted extraction fails loudly rather than storing junk.
func (d *Document) validate() (int, error) {
	// A degenerate extraction — empty, literal filler, or the debris of
	// OpenRouter's response healing (which stuffs unparseable output into
	// string fields and invents "placeholder" values) — must fail the run
	// rather than land in front of an admin looking like a report.
	summary := strings.TrimSpace(d.Summary)
	if summary == "" || strings.EqualFold(summary, "placeholder") {
		return 0, fmt.Errorf("extraction produced a degenerate summary %q", d.Summary)
	}
	if strings.Contains(summary, "removing malformed trailing content") {
		return 0, fmt.Errorf("extraction output carries a response-healing artifact")
	}
	for _, claim := range d.Claims {
		if strings.EqualFold(strings.TrimSpace(claim.Text), "placeholder") {
			return 0, fmt.Errorf("extraction produced a placeholder claim")
		}
	}

	switch d.Coverage.Level {
	case CoverageNone, CoverageThin, CoverageModerate, CoverageSubstantial:
	default:
		return 0, fmt.Errorf("unknown coverage level %q", d.Coverage.Level)
	}

	kept := d.Claims[:0]
	dropped := 0
	for _, claim := range d.Claims {
		switch claim.Tier {
		case TierObserved:
			// Deterministic facts are the evidence panel's job; a report
			// restating them is noise. Dropped silently — nothing is lost.
			continue
		case TierIndependentlyReported, TierVendorClaim:
			if len(claim.Citations) == 0 {
				dropped++
				continue
			}
		default:
			return 0, fmt.Errorf("unknown claim tier %q", claim.Tier)
		}
		kept = append(kept, claim)
	}
	// Claims arrive most relevant first, so the cap keeps the top of the
	// list.
	if len(kept) > maxReportClaims {
		kept = kept[:maxReportClaims]
	}
	d.Claims = kept

	return dropped, nil
}
