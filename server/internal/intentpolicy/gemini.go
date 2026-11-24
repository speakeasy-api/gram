package intentpolicy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	// GeminiModel is the non-lite flash of the Gemini generation the risk
	// judges run, for stronger labels on a staff-only replay path.
	GeminiModel = "google/gemini-3.5-flash"

	// geminiReasoningEffort must be set: the Gemini 3.5 routes reject a
	// disabled reasoning configuration.
	geminiReasoningEffort = "low"

	// geminiTimeout bounds one labeling call. Seven labels with rationales
	// take a few seconds; the margin absorbs provider queueing.
	geminiTimeout = 60 * time.Second

	// geminiMaxTokens caps generated tokens, reasoning included, so OpenRouter
	// does not reserve the model's full output ceiling against the key.
	geminiMaxTokens = 8192

	geminiPromptVersion = "intent-labels-v1"
)

// GeminiEvaluator labels calls with a Gemini model under a strict JSON schema.
type GeminiEvaluator struct {
	client    openrouter.CompletionClient
	orgID     string
	projectID string
	userID    string
}

var _ Evaluator = (*GeminiEvaluator)(nil)

// NewGeminiEvaluator binds the completion client to the caller's tenant for
// key resolution and usage attribution.
func NewGeminiEvaluator(client openrouter.CompletionClient, orgID, projectID, userID string) *GeminiEvaluator {
	return &GeminiEvaluator{client: client, orgID: orgID, projectID: projectID, userID: userID}
}

// Descriptor identifies the Gemini evaluator.
func (e *GeminiEvaluator) Descriptor() Descriptor {
	return Descriptor{Evaluator: "gemini", Provider: "openrouter", Model: GeminiModel, PromptVersion: geminiPromptVersion}
}

// GeminiSystemPrompt renders the framing and every label question.
func GeminiSystemPrompt(qs []LabelQuestion) string {
	var b strings.Builder
	b.WriteString(Framing)
	b.WriteString("\n\nThe user turn is the state. Answer every label below in one JSON object. Each label has a \"value\", a \"confidence\" in [0,1], and a \"rationale\" of at most 40 words that does not echo secrets or raw payloads.\n")
	for _, q := range qs {
		fmt.Fprintf(&b, "\n- %s: %s", q.Key, q.Instructions)
		switch q.Kind {
		case KindNoul:
			b.WriteString(" Value is true or false.")
		case KindMultiNoul:
			b.WriteString(" Value is the list of every effect class that applies (empty when none):")
			for _, o := range q.Options {
				fmt.Fprintf(&b, "\n  - %s: %s", o.Key, o.Description)
			}
		case KindChoice:
			b.WriteString(" Value is exactly one of:")
			for _, o := range q.Options {
				fmt.Fprintf(&b, "\n  - %s: %s", o.Key, o.Description)
			}
		}
	}
	b.WriteString("\n\nOutput ONLY the JSON object.")
	return b.String()
}

// GeminiSchema mirrors the label set: one property per label.
func GeminiSchema(qs []LabelQuestion) map[string]any {
	props := make(map[string]any, len(qs))
	required := make([]string, 0, len(qs))
	for _, q := range qs {
		var value map[string]any
		switch q.Kind {
		case KindNoul:
			value = map[string]any{"type": "boolean"}
		case KindMultiNoul:
			value = map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": optionKeys(q)}}
		case KindChoice:
			value = map[string]any{"type": "string", "enum": optionKeys(q)}
		}
		props[q.Key] = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"value":      value,
				"confidence": map[string]any{"type": "number"},
				"rationale":  map[string]any{"type": "string"},
			},
			"required":             []string{"value", "confidence", "rationale"},
			"additionalProperties": false,
		}
		required = append(required, q.Key)
	}
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}

func optionKeys(q LabelQuestion) []string {
	keys := make([]string, 0, len(q.Options))
	for _, o := range q.Options {
		keys = append(keys, o.Key)
	}
	return keys
}

// Label runs one structured completion and maps it to labels.
func (e *GeminiEvaluator) Label(ctx context.Context, in LabelInput) (LabelSet, Usage, error) {
	state, err := RenderState(in)
	if err != nil {
		return nil, Usage{}, err
	}

	strict := true
	temperature := 0.0
	callCtx, cancel := context.WithTimeout(ctx, geminiTimeout)
	defer cancel()

	start := time.Now()
	response, err := e.client.GetObjectCompletion(callCtx, openrouter.ObjectCompletionRequest{
		OrgID:          e.orgID,
		ProjectID:      e.projectID,
		Model:          GeminiModel,
		SystemPrompt:   GeminiSystemPrompt(in.Questions),
		Prompt:         string(state),
		Temperature:    &temperature,
		UsageSource:    billing.ModelUsageSourceRiskAnalysis,
		UserID:         e.userID,
		ExternalUserID: "",
		UserEmail:      "",
		HTTPMetadata:   nil,
		JSONSchema: &or.ChatJSONSchemaConfig{
			Name:        "intent_policy_labels",
			Schema:      GeminiSchema(in.Questions),
			Description: nil,
			Strict:      optionalnullable.From(&strict),
		},
		MaxTokens:              new(geminiMaxTokens),
		KeyType:                openrouter.KeyTypeInternal,
		KeySlot:                billing.ModelUsageSourceRiskPolicy,
		Reasoning:              &openrouter.Reasoning{Effort: geminiReasoningEffort, MaxTokens: nil, Exclude: nil, Enabled: nil},
		DisableResponseHealing: true,
	})
	usage := Usage{InputTokens: 0, OutputTokens: 0, CostUSD: 0, LatencyMs: time.Since(start).Milliseconds()}
	if err != nil {
		return nil, usage, fmt.Errorf("gemini object completion: %w", err)
	}
	if response == nil || response.Message == nil {
		return nil, usage, errors.New("empty gemini completion")
	}
	usage.InputTokens = int64(response.Usage.PromptTokens)
	usage.OutputTokens = int64(response.Usage.CompletionTokens)
	if response.Usage.Cost != nil {
		usage.CostUSD = *response.Usage.Cost
	}
	if response.FinishReason != nil && *response.FinishReason == openrouter.FinishReasonLength {
		return nil, usage, fmt.Errorf("gemini completion hit the %d-token cap", geminiMaxTokens)
	}

	labels, err := ParseGeminiLabels(in.Questions, []byte(openrouter.GetText(*response.Message)))
	return labels, usage, err
}

type geminiLabel struct {
	Value      json.RawMessage `json:"value"`
	Confidence float64         `json:"confidence"`
	Rationale  string          `json:"rationale"`
}

// ParseGeminiLabels validates the structured response. Labels with invalid
// values are omitted and reported so the contracts reading them fail closed.
func ParseGeminiLabels(qs []LabelQuestion, raw []byte) (LabelSet, error) {
	var parsed map[string]geminiLabel
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &parsed); err != nil {
		return nil, fmt.Errorf("parse gemini labels: %w", err)
	}

	labels := LabelSet{}
	var problems []string
	for _, q := range qs {
		got, ok := parsed[q.Key]
		if !ok {
			problems = append(problems, q.Key+": missing")
			continue
		}
		// Unmarshal treats null as a no-op, which would read as false or empty.
		if v := bytes.TrimSpace(got.Value); len(v) == 0 || bytes.Equal(v, []byte("null")) {
			problems = append(problems, q.Key+": null")
			continue
		}
		confidence := max(0, min(1, got.Confidence))
		switch q.Kind {
		case KindNoul:
			var v bool
			if err := json.Unmarshal(got.Value, &v); err != nil {
				problems = append(problems, q.Key+": not a boolean")
				continue
			}
			labels[q.Key] = LabelValue{Value: v, Rank: -1, Probability: confidence, Distribution: nil, Rationale: got.Rationale}
		case KindMultiNoul:
			var v []string
			if err := json.Unmarshal(got.Value, &v); err != nil {
				problems = append(problems, q.Key+": not a list")
				continue
			}
			members := []string{}
			valid := true
			for _, o := range q.Options {
				if slices.Contains(v, o.Key) {
					members = append(members, o.Key)
				}
			}
			for _, m := range v {
				if !slices.Contains(optionKeys(q), m) {
					valid = false
				}
			}
			if !valid {
				problems = append(problems, q.Key+": unknown member")
				continue
			}
			labels[q.Key] = LabelValue{Value: members, Rank: -1, Probability: confidence, Distribution: nil, Rationale: got.Rationale}
		case KindChoice:
			var v string
			if err := json.Unmarshal(got.Value, &v); err != nil || !slices.Contains(optionKeys(q), v) {
				problems = append(problems, q.Key+": not an option")
				continue
			}
			labels[q.Key] = LabelValue{Value: v, Rank: optionRank(q, v), Probability: confidence, Distribution: nil, Rationale: got.Rationale}
		}
	}
	if len(problems) > 0 {
		return labels, fmt.Errorf("invalid gemini labels: %s", strings.Join(problems, "; "))
	}
	return labels, nil
}
