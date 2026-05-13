package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// Bench-validated wording: F1 0.981 / 100% precision on the calibration fixture
// at temperature 0. Rephrasings invalidate those numbers; do not edit.
const contradictionSystemPrompt = `You are a strict classifier. Given two memories about the same user, decide whether memory B contradicts memory A.

# The test

Ask: **Could BOTH A and B be simultaneously true of the same person at the same point in time?**

- If YES → B does NOT contradict A. (B may add information, refine A, or be unrelated — all of these are non-contradictions.)
- If NO → B contradicts A.

# Examples

A: "The user is a developer."
B: "The user is a senior staff engineer at Acme."
Both can be true at once → NOT a contradiction.

A: "The user owns a Tesla Model 3."
B: "The user also owns a 1969 Triumph Bonneville motorcycle."
Both can be true at once → NOT a contradiction.

A: "The user prefers Vim as their editor."
B: "The user prefers Emacs as their editor."
Cannot both be a single preferred editor at once → CONTRADICTION.

A: "The user is engaged."
B: "The user is married."
Cannot simultaneously be in both states → CONTRADICTION.

A: "The user has two children."
B: "The user has three children."
Cannot have two distinct counts at the same time → CONTRADICTION.

# Confidence anchors

Use one of these as your confidence:
- 0.95 — the verdict is unambiguous from the text
- 0.75 — likely, but the wording is loose
- 0.50 — genuinely uncertain; could go either way

# Output format

Return a single JSON object:

{ "contradicts": <true|false>, "confidence": <0.50|0.75|0.95> }

Do not emit reasoning, prose, or any text outside the JSON object.`

var contradictionTemperatureValue = 0.0
var contradictionTemperature = &contradictionTemperatureValue

type contradictionResponse struct {
	Contradicts bool    `json:"contradicts"`
	Confidence  float64 `json:"confidence"`
}

// detectContradiction asks the configured chat model whether b contradicts a.
// Returns (false, err) on transport or parse failure so the caller can fall
// back to an additive insert without supersede.
func (s *MemoryService) detectContradiction(ctx context.Context, orgID, projectID, a, b string) (bool, error) {
	strict := true
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"contradicts": map[string]any{"type": "boolean"},
			"confidence":  map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		},
		"required":             []string{"contradicts", "confidence"},
		"additionalProperties": false,
	}

	jsonSchemaConfig := or.ChatJSONSchemaConfig{
		Name:        "memory_contradiction",
		Schema:      schema,
		Description: nil,
		Strict:      optionalnullable.From(&strict),
	}

	req := openrouter.ObjectCompletionRequest{
		OrgID:          orgID,
		ProjectID:      projectID,
		Model:          s.contradictionModel,
		SystemPrompt:   contradictionSystemPrompt,
		Prompt:         fmt.Sprintf("A: %q\nB: %q", a, b),
		Temperature:    contradictionTemperature,
		JSONSchema:     &jsonSchemaConfig,
		UsageSource:    billing.ModelUsageSourceGram,
		UserID:         "",
		ExternalUserID: "",
		HTTPMetadata:   nil,
	}

	response, err := s.completions.GetObjectCompletion(ctx, req)
	if err != nil {
		return false, fmt.Errorf("contradiction completion: %w", err)
	}
	if response == nil || response.Message == nil {
		return false, fmt.Errorf("contradiction completion: empty response")
	}

	text := strings.TrimSpace(openrouter.GetText(*response.Message))
	if text == "" {
		return false, fmt.Errorf("contradiction completion: empty content")
	}

	var parsed contradictionResponse
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return false, fmt.Errorf("decode contradiction response: %w", err)
	}

	return parsed.Contradicts, nil
}
