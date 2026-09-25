package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/risk/presets"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func (s *Service) ListRiskPresets(ctx context.Context, _ *gen.ListRiskPresetsPayload) (*gen.RiskPresetsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	all := presets.All()
	out := make([]*gen.RiskPreset, 0, len(all))
	for _, preset := range all {
		out = append(out, presetToGen(preset))
	}
	return &gen.RiskPresetsResult{Presets: out}, nil
}

func (s *Service) SuggestRiskPolicy(ctx context.Context, payload *gen.SuggestRiskPolicyPayload) (*gen.SuggestRiskPolicyResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	prompt := strings.TrimSpace(payload.Prompt)
	if prompt == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "prompt is required")
	}

	heuristic := presets.Suggest(prompt)
	if s.completionClient == nil {
		return suggestionToGen(heuristic), nil
	}
	suggestion, err := s.suggestRiskPolicyViaLLM(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), authCtx.UserID, conv.PtrValOr(authCtx.Email, ""), prompt, heuristic)
	if err != nil {
		s.logger.WarnContext(ctx, "openrouter policy suggestion failed; returning keyword suggestion", attr.SlogError(err))
		return suggestionToGen(heuristic), nil
	}
	return suggestion, nil
}

func presetToGen(preset presets.Preset) *gen.RiskPreset {
	return &gen.RiskPreset{
		ID:                           preset.ID,
		Label:                        preset.Label,
		Description:                  preset.Description,
		PolicyType:                   preset.PolicyType,
		Sources:                      nonNilStrings(preset.Sources),
		PresidioEntities:             nonNilStrings(preset.PresidioEntities),
		Action:                       preset.Action,
		Score:                        preset.Score,
		Prompt:                       preset.Prompt,
		UserMessage:                  preset.UserMessage,
		RequiresApprovedEmailDomains: preset.RequiresApprovedEmailDomains,
	}
}

func suggestionToGen(suggestion presets.Suggestion) *gen.SuggestRiskPolicyResult {
	alternatives := make([]*gen.RiskPresetMatch, 0, len(suggestion.Alternatives))
	for _, match := range suggestion.Alternatives {
		alternatives = append(alternatives, &gen.RiskPresetMatch{PresetID: match.Preset.ID, Label: match.Preset.Label, Confidence: match.Confidence})
	}
	draft := suggestion.Draft
	return &gen.SuggestRiskPolicyResult{
		PresetID:         draft.PresetID,
		PolicyType:       draft.PolicyType,
		Name:             draft.Name,
		Action:           draft.Action,
		Score:            draft.Score,
		Sources:          nonNilStrings(draft.Sources),
		PresidioEntities: nonNilStrings(draft.PresidioEntities),
		Prompt:           draft.Prompt,
		UserMessage:      draft.UserMessage,
		Rationale:        suggestion.Rationale,
		Confidence:       suggestion.Confidence,
		Alternatives:     alternatives,
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

var suggestPolicyActions = []string{"flag", "warn", "block", "quarantine"}

func (s *Service) suggestRiskPolicyViaLLM(ctx context.Context, orgID, projectID, userID, userEmail, userPrompt string, heuristic presets.Suggestion) (*gen.SuggestRiskPolicyResult, error) {
	all := presets.All()
	var catalog strings.Builder
	ids := make([]string, 0, len(all)+1)
	for _, preset := range all {
		ids = append(ids, preset.ID)
		fmt.Fprintf(&catalog, "- %s (%s, %s policy, default action %s, severity %.0f): %s\n", preset.ID, preset.Label, preset.PolicyType, preset.Action, preset.Score, preset.Description)
	}
	ids = append(ids, "")

	systemPrompt := `You map an administrator's plain-language description of a risk to a policy draft for a runtime risk detection product.

Presets (id, label, type, defaults, what they cover):
` + catalog.String() + `
Rules:
- Choose the preset whose coverage best matches the description. Set "preset_id" to its id.
- If no preset fits, set "preset_id" to "" and write "prompt": a precise instruction for a policy model that flags a message when the described behaviour occurs. Name what is in scope and what is not.
- "name": 2-6 words, title case, specific to the request.
- "action": "flag" (log only), "warn" (user must acknowledge), "block" (deny the request) or "quarantine" (deny and freeze the session until an administrator releases it). Only escalate above the preset default when the description asks to stop, block, deny, prevent or quarantine something.
- "score": severity 0.1-10 by impact; keep the preset default unless the description signals higher or lower impact.
- "confidence": 0-1, how well the chosen preset (or bespoke prompt) covers the description.
- "rationale": one sentence for the administrator.

Output ONLY the JSON object. No prose, no markdown fences.`

	strict := false
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"preset_id":  map[string]any{"type": "string", "enum": ids},
			"name":       map[string]any{"type": "string", "minLength": 1, "maxLength": 100},
			"action":     map[string]any{"type": "string", "enum": suggestPolicyActions},
			"score":      map[string]any{"type": "number", "minimum": 0.1, "maximum": 10},
			"prompt":     map[string]any{"type": "string", "maxLength": 4000},
			"rationale":  map[string]any{"type": "string", "minLength": 1, "maxLength": 400},
			"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		},
		"required":             []string{"preset_id", "name", "action", "score", "prompt", "rationale", "confidence"},
		"additionalProperties": false,
	}
	jsonSchema := or.ChatJSONSchemaConfig{
		Name:        "risk_policy_suggestion",
		Schema:      schema,
		Description: nil,
		Strict:      optionalnullable.From(&strict),
	}

	suggestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	temperature := 0.2
	response, err := s.completionClient.GetObjectCompletion(suggestCtx, openrouter.ObjectCompletionRequest{
		OrgID:                  orgID,
		ProjectID:              projectID,
		Model:                  "",
		SystemPrompt:           systemPrompt,
		Prompt:                 "Administrator request: " + userPrompt,
		Temperature:            &temperature,
		UsageSource:            billing.ModelUsageSourceGram,
		KeyType:                openrouter.KeyTypeInternal,
		KeySlot:                "",
		UserID:                 userID,
		ExternalUserID:         "",
		UserEmail:              userEmail,
		HTTPMetadata:           nil,
		JSONSchema:             &jsonSchema,
		Reasoning:              nil,
		DisableResponseHealing: false,
	})
	if err != nil {
		return nil, fmt.Errorf("openrouter object completion: %w", err)
	}
	if response == nil || response.Message == nil {
		return nil, fmt.Errorf("empty completion response")
	}
	raw := strings.TrimSpace(openrouter.GetText(*response.Message))
	if raw == "" {
		return nil, fmt.Errorf("empty completion content")
	}

	var parsed struct {
		PresetID   string  `json:"preset_id"`
		Name       string  `json:"name"`
		Action     string  `json:"action"`
		Score      float64 `json:"score"`
		Prompt     string  `json:"prompt"`
		Rationale  string  `json:"rationale"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	suggestion := heuristic
	if parsed.PresetID != "" {
		preset, ok := presets.ByID(parsed.PresetID)
		if !ok {
			return nil, fmt.Errorf("model returned unknown preset %q", parsed.PresetID)
		}
		suggestion.Preset = &preset
		suggestion.Draft = preset.Draft()
		suggestion.Alternatives = slices.DeleteFunc(slices.Clone(heuristic.Alternatives), func(match presets.Match) bool { return match.Preset.ID == preset.ID })
	} else {
		instruction := strings.TrimSpace(parsed.Prompt)
		if instruction == "" {
			instruction = userPrompt
		}
		bespoke := presets.Suggest(instruction)
		suggestion = presets.Suggestion{
			Preset:       nil,
			Draft:        bespoke.Draft,
			Confidence:   0,
			Rationale:    bespoke.Rationale,
			Alternatives: heuristic.Alternatives,
		}
		suggestion.Draft.PresetID = ""
		suggestion.Draft.PolicyType = presets.PolicyTypePromptBased
		suggestion.Draft.Sources = nil
		suggestion.Draft.PresidioEntities = nil
		suggestion.Draft.Prompt = instruction
		suggestion.Draft.Action = "flag"
		suggestion.Draft.Score = 5
	}

	if name := strings.TrimSpace(parsed.Name); name != "" && policycore.ValidateName(name) == nil {
		suggestion.Draft.Name = name
	}
	if slices.Contains(suggestPolicyActions, parsed.Action) && policycore.ValidateSourceAction(suggestion.Draft.Sources, parsed.Action) == nil {
		suggestion.Draft.Action = parsed.Action
	}
	if parsed.Score >= 0.1 && parsed.Score <= 10 {
		suggestion.Draft.Score = parsed.Score
	}
	if rationale := strings.TrimSpace(parsed.Rationale); rationale != "" {
		suggestion.Rationale = rationale
	}
	if suggestion.Preset != nil && parsed.Confidence >= 0 && parsed.Confidence <= 1 {
		suggestion.Confidence = parsed.Confidence
	}
	return suggestionToGen(suggestion), nil
}
