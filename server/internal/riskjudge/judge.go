// Package riskjudge holds the concrete OpenRouter-backed implementation of the
// prompt-based ("LLM-judge") risk policy evaluator. It lives outside the
// risk_analysis package so that package — which testenv imports — does not pull
// in the OpenRouter client dependency chain (openrouter -> productfeatures ->
// authz), which would otherwise create an import cycle in authz tests.
package riskjudge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"

	"github.com/speakeasy-api/gram/server/internal/attr"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	// judgeTimeout bounds a single judge call on both the realtime and batch
	// paths.
	judgeTimeout = 10 * time.Second
	// defaultJudgeTemperature keeps verdicts deterministic when a policy does
	// not pin its own temperature.
	defaultJudgeTemperature = 0.0
)

const systemPrompt = `You are a security guardrail judge for an AI agent runtime.

You are given an operator-authored policy describing what the operator wants to catch, and a single message from an agent session (typically a tool call). Decide whether the message violates the policy.

Return a JSON object:
- "matched": true if the message violates the policy, false otherwise.
- "confidence": a number in [0,1] for how confident you are.
- "rationale": one short sentence (no more than ~40 words) explaining the decision. Do not echo secrets or raw payloads verbatim.

Judge only against the provided policy. Be precise: do not flag content the policy does not describe. Output ONLY the JSON object, no prose or markdown fences.`

// Judge is the OpenRouter-backed ra.PromptJudge. The judge call mirrors the
// custom-rule suggestion path: strict JSON schema, low temperature, hard
// timeout, OpenRouter object completion.
type Judge struct {
	logger *slog.Logger
	client openrouter.CompletionClient
}

var _ ra.PromptJudge = (*Judge)(nil)

// New constructs a Judge. A nil client yields a judge whose Evaluate always
// returns nil, so callers can wire it unconditionally.
func New(logger *slog.Logger, client openrouter.CompletionClient) *Judge {
	return &Judge{
		logger: logger.With(attr.SlogComponent("risk-llm-judge")),
		client: client,
	}
}

// Evaluate runs the judge and returns a non-nil verdict when the message
// violates the policy prompt, or nil when it does not. A nil client or an empty
// prompt/text yields nil. On judge error or timeout the configured fail-mode
// decides: fail-open returns nil (allow), fail-closed returns a verdict.
func (j *Judge) Evaluate(ctx context.Context, in ra.JudgeInput) *ra.JudgeVerdict {
	if j == nil || j.client == nil {
		return nil
	}
	if strings.TrimSpace(in.Prompt) == "" || strings.TrimSpace(in.Text) == "" {
		return nil
	}

	matched, confidence, rationale, err := j.call(ctx, in)
	if err != nil {
		j.logger.WarnContext(ctx, "llm judge call failed",
			attr.SlogError(err),
			attr.SlogOrganizationID(in.OrgID),
		)
		if in.Config.FailOpen {
			return nil
		}
		return &ra.JudgeVerdict{
			Confidence: 0,
			Rationale:  "Policy judge was unavailable; flagged by fail-closed policy.",
		}
	}
	if !matched {
		return nil
	}
	return &ra.JudgeVerdict{
		Confidence: confidence,
		Rationale:  strings.TrimSpace(rationale),
	}
}

func (j *Judge) call(ctx context.Context, in ra.JudgeInput) (matched bool, confidence float64, rationale string, err error) {
	strict := true
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"matched":    map[string]any{"type": "boolean"},
			"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"rationale":  map[string]any{"type": "string", "maxLength": 500},
		},
		"required":             []string{"matched", "confidence", "rationale"},
		"additionalProperties": false,
	}
	jsonSchema := or.ChatJSONSchemaConfig{
		Name:        "risk_policy_judge_verdict",
		Schema:      schema,
		Description: nil,
		Strict:      optionalnullable.From(&strict),
	}

	temperature := defaultJudgeTemperature
	if in.Config.Temperature != nil {
		temperature = *in.Config.Temperature
	}

	userMessage := fmt.Sprintf("Policy:\n%s\n\nMessage to evaluate:\n%s", in.Prompt, in.Text)

	callCtx, cancel := context.WithTimeout(ctx, judgeTimeout)
	defer cancel()

	response, err := j.client.GetObjectCompletion(callCtx, openrouter.ObjectCompletionRequest{
		OrgID:          in.OrgID,
		ProjectID:      in.ProjectID,
		Model:          in.Config.Model,
		SystemPrompt:   systemPrompt,
		Prompt:         userMessage,
		Temperature:    &temperature,
		UsageSource:    billing.ModelUsageSourceGram,
		UserID:         "",
		ExternalUserID: "",
		HTTPMetadata:   nil,
		JSONSchema:     &jsonSchema,
	})
	if err != nil {
		return false, 0, "", fmt.Errorf("openrouter object completion: %w", err)
	}
	if response == nil || response.Message == nil {
		return false, 0, "", fmt.Errorf("empty completion response")
	}
	raw := strings.TrimSpace(openrouter.GetText(*response.Message))
	if raw == "" {
		return false, 0, "", fmt.Errorf("empty completion content")
	}

	var verdict struct {
		Matched    bool    `json:"matched"`
		Confidence float64 `json:"confidence"`
		Rationale  string  `json:"rationale"`
	}
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil {
		return false, 0, "", fmt.Errorf("parse judge response: %w", err)
	}
	return verdict.Matched, max(0, min(1, verdict.Confidence)), verdict.Rationale, nil
}
