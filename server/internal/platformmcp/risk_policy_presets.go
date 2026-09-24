package platformmcp

import (
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/risk/presets"
)

// applyRiskPolicyPreset expands a preset reference into the same create input
// an explicit request carries, keeping any field the caller set. Validation of
// the expanded request stays with prepareCreate, so a preset can never bypass a
// check an explicit request would face.
func applyRiskPolicyPreset(input createRiskPolicyInput) (createRiskPolicyInput, error) {
	id := strings.TrimSpace(input.Preset)
	if id == "" {
		return input, nil
	}
	preset, ok := presets.ByID(id)
	if !ok || strings.TrimSpace(input.PolicyType) != "" {
		return input, invalidRiskPolicyRequest()
	}
	draft := preset.Draft()

	input.Preset = ""
	input.PolicyType = draft.PolicyType
	if strings.TrimSpace(input.Name) == "" {
		input.Name = draft.Name
	}
	if input.Action == "" {
		input.Action = draft.Action
	}
	if input.Score == nil {
		score := draft.Score
		input.Score = &score
	}
	if input.UserMessage == nil && draft.UserMessage != "" {
		message := draft.UserMessage
		input.UserMessage = &message
	}
	switch draft.PolicyType {
	case presets.PolicyTypeStandard:
		if len(input.Sources) == 0 {
			input.Sources = slices.Clone(draft.Sources)
		}
		if len(input.PresidioEntities) == 0 && slices.Contains(input.Sources, "presidio") {
			input.PresidioEntities = slices.Clone(draft.PresidioEntities)
		}
	case presets.PolicyTypePromptBased:
		if strings.TrimSpace(input.Prompt) == "" {
			input.Prompt = draft.Prompt
		}
	}
	return input, nil
}
