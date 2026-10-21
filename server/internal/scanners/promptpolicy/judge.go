package promptpolicy

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// Source is the policy/finding source for prompt_based (LLM-judge) policy
// evaluations.
const Source = "llm_judge"

// Rule is the canonical rule id emitted for every llm_judge finding. The
// policy that produced it carries the human-meaningful prompt; the rule_id
// just buckets the finding by detection mechanism.
const Rule = "llm_judge"

var ErrRateLimited = errors.New("llm judge rate limited")

// Evaluator evaluates one message against a natural-language guardrail prompt.
// It returns (nil, nil) when there is nothing to judge, a verdict on a
// successful judge call, or an error when the judge degraded.
type Evaluator func(ctx context.Context, in Input) (*Verdict, error)

func NoopEvaluator(context.Context, Input) (*Verdict, error) {
	return nil, nil
}

// Input carries everything needed for one judge evaluation.
type Input struct {
	OrgID     string
	ProjectID string
	// UserID is the Speakeasy user whose message is being scanned (the scanned
	// chat's owner), empty when the session is unattributed. It rides on the
	// judge's completion telemetry so the billing page attributes scanning
	// volume to whose traffic was analyzed; email/roles/division hydrate
	// from it at emit time.
	UserID string
	// Prompt is the policy's operator-authored guardrail.
	Prompt  string
	Message judgemessage.Message
	Config  Config
}

// Verdict is the resolved outcome of a judge evaluation.
type Verdict struct {
	Matched    bool
	Confidence float64
	// Rationale is a short, secret-free explanation of the match.
	Rationale        string
	CostUSD          float64
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	// STokens counts the exact prepared content-bearing values sent to the judge.
	STokens int64
	// Completed reports that a real judge returned a valid verdict.
	Completed bool
	// Model identifies the judge model when applicable.
	Model string
	// Provider identifies the judge provider when applicable.
	Provider string
}

// FailClosedVerdict builds the canonical verdict used when a degraded judge
// call is treated as a policy match.
func FailClosedVerdict(err error) Verdict {
	rationale := "Policy judge was unavailable; flagged because the organization fails closed."
	if errors.Is(err, ErrRateLimited) {
		rationale = "Policy judge was rate limited; flagged because the organization fails closed."
	}
	return Verdict{
		Matched:          true,
		Confidence:       0,
		Rationale:        rationale,
		CostUSD:          0,
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		STokens:          0,
		Completed:        false,
		Model:            "",
		Provider:         "",
	}
}

// Config is one judge evaluation's configuration: the policy's model settings
// from its model_config JSONB column plus the failure mode the caller applies.
type Config struct {
	// Temperature overrides the default judge temperature when non-nil.
	Temperature *float64
	// FailClosed treats a failed or unavailable judge call as a match. The
	// zero value fails open (no finding). Enforcing callers set it from the
	// organization's risk failure mode; analysis that only flags leaves it
	// false, so an outage never surfaces as a finding.
	FailClosed bool
}

// ParseConfig decodes a prompt_based policy's model_config JSONB into a
// Config that fails open. Missing or unparseable config keeps the default
// temperature. Keys persisted before they moved off the policy - `model`
// (the judge model is fixed) and `fail_open` (the failure mode is the
// organization's) - are inert and ignored rather than migrated away.
func ParseConfig(raw []byte) Config {
	cfg := Config{Temperature: nil, FailClosed: false}
	if len(raw) == 0 {
		return cfg
	}
	var parsed struct {
		Temperature *float64 `json:"temperature"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return cfg
	}
	cfg.Temperature = parsed.Temperature
	return cfg
}

// NewFinding builds a canonical llm_judge Finding from a verdict. Shared by
// the batch analyzer and the realtime scanner so the (source, rule_id)
// identity stays consistent across both paths.
func NewFinding(verdict Verdict) scanners.Finding {
	description := verdict.Rationale
	if description == "" {
		description = "Message matched the prompt-based policy."
	}
	return scanners.Finding{
		Source:              Source,
		RuleID:              Rule,
		Description:         description,
		Match:               "",
		StartPos:            0,
		EndPos:              0,
		Tags:                []string{},
		Confidence:          verdict.Confidence,
		DeadLetterReason:    "",
		McpLookupToolCallID: "",
		SpanGroupKey:        "",
		Field:               "",
		Path:                "",
	}
}
