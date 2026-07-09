package risk_analysis

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

const (
	// SourceLLMJudge is the policy/finding source for prompt_based (LLM-judge)
	// policy evaluations.
	SourceLLMJudge = "llm_judge"
	// RuleLLMJudge is the canonical rule id emitted for every llm_judge
	// finding. The policy that produced it carries the human-meaningful prompt;
	// the rule_id just buckets the finding by detection mechanism.
	RuleLLMJudge = "llm_judge"
)

// PromptJudge evaluates a single message against a natural-language guardrail
// prompt and returns a verdict. The concrete OpenRouter-backed implementation
// lives in the internal/riskjudge package; this package only depends on the
// interface so it stays free of the LLM-client dependency chain (which would
// otherwise pull authz in through testenv and create an import cycle).
type PromptJudge interface {
	// Evaluate returns a verdict for a successful judge call. Nil means the
	// message was not judged, such as fail-open on judge error.
	Evaluate(ctx context.Context, in JudgeInput) *JudgeVerdict
}

// JudgeInput carries everything needed for one judge evaluation.
type JudgeInput struct {
	OrgID     string
	ProjectID string
	// UserID is the Gram user whose message is being scanned (the scanned
	// chat's owner), empty when the session is unattributed. It rides on the
	// judge's completion telemetry so the billing page attributes scanning
	// volume to whose traffic was analyzed; email/roles/division hydrate
	// from it at emit time.
	UserID string
	// Prompt is the policy's operator-authored guardrail.
	Prompt  string
	Message judgemessage.Message
	Config  JudgeConfig
}

// JudgeVerdict is the resolved outcome of a judge evaluation.
type JudgeVerdict struct {
	Matched    bool
	Confidence float64
	// Rationale is a short, secret-free explanation of the match.
	Rationale        string
	CostUSD          float64
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// JudgeConfig is the per-policy judge model configuration parsed from a
// prompt_based policy's model_config JSONB column.
type JudgeConfig struct {
	// Model is the OpenRouter model id; empty selects the default judge model.
	Model string
	// Temperature overrides the default judge temperature when non-nil.
	Temperature *float64
	// FailOpen decides the verdict when the judge call fails: true => allow
	// (no finding), false => treat as matched. Defaults to true.
	FailOpen bool
}

// ParseJudgeConfig decodes a prompt_based policy's model_config JSONB into a
// JudgeConfig. Missing or unparseable config defaults to fail-open with the
// default model and temperature.
func ParseJudgeConfig(raw []byte) JudgeConfig {
	cfg := JudgeConfig{Model: "", Temperature: nil, FailOpen: true}
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

// JudgeFinding builds a canonical llm_judge Finding from a verdict. Shared by
// the batch analyzer so the (source, rule_id) identity stays consistent with
// the realtime scanner.
func JudgeFinding(verdict JudgeVerdict) scanners.Finding {
	description := verdict.Rationale
	if description == "" {
		description = "Message matched the prompt-based policy."
	}
	return scanners.Finding{
		Source:              SourceLLMJudge,
		RuleID:              RuleLLMJudge,
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
