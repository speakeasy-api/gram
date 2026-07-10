package promptpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

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

// Evaluator evaluates one message against a natural-language guardrail
// prompt. The concrete OpenRouter-backed implementation lives in the nested
// openrouter package; consumers depend on this interface so they stay free of
// the LLM-client dependency chain.
type Evaluator interface {
	// Evaluate returns (nil, nil) when there is nothing to judge, a verdict on a
	// successful judge call, or an error when the judge degraded.
	Evaluate(ctx context.Context, in Input) (*Verdict, error)
}

// Input carries everything needed for one judge evaluation.
type Input struct {
	OrgID     string
	ProjectID string
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
}

// FailClosedVerdict builds the canonical verdict used when a degraded judge
// call is treated as a policy match.
func FailClosedVerdict(err error) Verdict {
	rationale := "Policy judge was unavailable; flagged by fail-closed policy."
	if errors.Is(err, ErrRateLimited) {
		rationale = "Policy judge was rate limited; flagged by fail-closed policy."
	}
	return Verdict{
		Matched:          true,
		Confidence:       0,
		Rationale:        rationale,
		CostUSD:          0,
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
	}
}

// Config is the per-policy judge model configuration parsed from a
// prompt_based policy's model_config JSONB column.
type Config struct {
	// Model is the OpenRouter model id; empty selects the default judge model.
	Model string
	// Temperature overrides the default judge temperature when non-nil.
	Temperature *float64
	// FailOpen decides the verdict when the judge call fails: true => allow
	// (no finding), false => treat as matched. Defaults to true.
	FailOpen bool
}

// ParseConfig decodes a prompt_based policy's model_config JSONB into a
// Config. Missing or unparseable config defaults to fail-open with the
// default model and temperature.
func ParseConfig(raw []byte) Config {
	cfg := Config{Model: "", Temperature: nil, FailOpen: true}
	if len(raw) == 0 {
		return cfg
	}
	var parsed struct {
		Model       *string  `json:"model"`
		Temperature *float64 `json:"temperature"`
		FailOpen    *bool    `json:"fail_open"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return cfg
	}
	if parsed.Model != nil {
		cfg.Model = strings.TrimSpace(*parsed.Model)
	}
	cfg.Temperature = parsed.Temperature
	if parsed.FailOpen != nil {
		cfg.FailOpen = *parsed.FailOpen
	}
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
