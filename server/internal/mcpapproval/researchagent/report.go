package researchagent

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// ReportVersion is the shape version of the report document this package
// produces. It is stored on every report row. Bump it only when a shape
// change would break a reader of the other version — adding an optional
// field whose absence readers already accept is not a new shape.
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

// Source-reputation labels, the model's per-claim judgment of the sources a
// claim rests on. Deliberately model-asserted rather than a curated domain
// list: a list is stale the day it ships and says nothing about the long tail
// of small publishers where most MCP vendors live. Reputation describes the
// SOURCES, never the claim — a true statement can rest on a planted post, and
// a reputable outlet can be wrong. Absence (reports written before the field
// existed) reads as unknown, never as reputable.
const (
	// ReputationReputable: the vendor's own official documentation (for the
	// vendor's stated posture), independent security organizations, CVE and
	// advisory databases, established security firms, major press.
	ReputationReputable = "reputable"

	// ReputationMixed: a blend of reputable and weaker material, or sources
	// of uncertain standing.
	ReputationMixed = "mixed"

	// ReputationLow: only anonymous forum posts, SEO content farms,
	// freshly-registered blogs — material cheap to plant.
	ReputationLow = "low"
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

	// Injections are pages that tried to instruct the agent reading them,
	// one entry per page the judge flagged. Filled by the runner from the
	// judge's verdicts, never by the model — the finding has to survive an
	// extraction pass that has just read the material trying to suppress it.
	//
	// This is a finding about the server, not only a defence: a vendor page
	// that attempts to steer a reviewer is among the strongest signals the
	// dossier can carry, so the attack is recorded as evidence.
	Injections []InjectionFinding `json:"injections,omitempty"`

	// Run records what produced this report. Filled by the runner, never by
	// the model.
	Run RunMeta `json:"run"`
}

// InjectionFinding is one fetched page the judge called an injection attempt.
type InjectionFinding struct {
	// URL is the page it came from.
	URL string `json:"url"`

	// Rationale is the judge's own account of what the page tried to do.
	Rationale string `json:"rationale,omitempty"`
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

	// SourceReputation is one of the Reputation* constants, or empty on
	// reports that predate the field. Empty renders as unknown: a claim
	// resting only on low-reputation sources must say so, and an old report
	// simply does not say.
	SourceReputation string `json:"source_reputation,omitempty"`
}

// Citation is one source reference.
type Citation struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`

	// TrustedSource names the category the citation's domain holds in the
	// embedded trusted-source registry ("vulnerability database", "security
	// research", …), empty when unlisted. Stamped deterministically by
	// validation, never by the model: it exists so an admin can see at a
	// glance which citations rest on independently recognized ground and
	// judge the model-asserted source_reputation against it. It gates
	// nothing — an unlisted source is not a bad source, only an unvouched
	// one.
	TrustedSource string `json:"trusted_source,omitempty"`
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

	// PagesJudged counts fetched pages the injection judge reached a verdict
	// on; JudgeFailures counts those it could not. The pair is the honest
	// denominator for the injections list: no injections out of eight pages
	// judged is a result, no injections out of eight pages where the judge
	// never answered is not.
	PagesJudged   int `json:"pages_judged,omitempty"`
	JudgeFailures int `json:"judge_failures,omitempty"`
}

//go:embed report_schema.json
var rawExtractionSchema []byte

// extractionSchema is the JSON schema the extraction pass is held to. Kept as
// a raw schema rather than reflection so the enum constraints are explicit.
// Shaped for native strict structured outputs, which require every property
// listed in required — optionality is expressed as nullability, and Go's
// decoder reads null as the zero value.
var extractionSchema = json.RawMessage(rawExtractionSchema)

// validate drops web-tier claims without citations — an untraceable web claim
// is one the admin cannot defend — and rejects unknown tiers, levels, and
// source reputations so a drifted extraction fails loudly rather than storing
// junk.
//
// A citation is only a citation if it can be followed: the model writes these
// after reading pages that are themselves untrusted, so a URL that is empty,
// unparseable, or not http(s) is discarded before it can be stored. That last
// case is the one that matters most — the review page renders citations as
// links, and a javascript: or data: URL there would put attacker-authored
// script one admin click away.
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
			// Empty stays valid — reports stored before the field existed
			// carry none, and unknown is an honest state the review page
			// renders as nothing. It is never promoted to reputable.
			switch claim.SourceReputation {
			case "", ReputationReputable, ReputationMixed, ReputationLow:
			default:
				return 0, fmt.Errorf("unknown source reputation %q", claim.SourceReputation)
			}
			// A claim with nothing to say is not a finding, and the approval
			// page would render it as a blank row inside the top five.
			if strings.TrimSpace(claim.Text) == "" {
				dropped++
				continue
			}
			claim.Citations = followableCitations(claim.Citations)
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

// followableCitations keeps the citations an admin can actually open. The
// scheme check is the security-relevant half: citation URLs are written by a
// model that has just read untrusted pages, and the review page turns them
// into links.
func followableCitations(citations []Citation) []Citation {
	kept := make([]Citation, 0, len(citations))
	for _, citation := range citations {
		citation.URL = strings.TrimSpace(citation.URL)
		if !followableURL(citation.URL) {
			continue
		}
		citation.TrustedSource = trustedSourceCategory(citation.URL)

		kept = append(kept, citation)
	}

	return kept
}

// followableURL reports whether a URL is one an admin can open: https with a
// host. Everything stored by a research run that ends up rendered as a link
// goes through this, because all of it was written while reading pages the
// run treats as hostile. https only, matching the fetch tool: the run cannot
// have read a plaintext page, so a plaintext citation is a URL the model
// wrote without evidence behind it — and a link the admin would open over a
// channel anyone on the path controls.
func followableURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if parsed.Scheme != "https" {
		return false
	}

	return parsed.Host != ""
}
