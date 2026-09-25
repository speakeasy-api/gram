//nolint:exhaustruct // MCP schemas rely on documented optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/risk/presets"
)

type ListRiskPresetsInput struct{}

type RiskPresetSummary struct {
	ID                           string   `json:"id"`
	Label                        string   `json:"label"`
	Description                  string   `json:"description"`
	PolicyType                   string   `json:"policy_type"`
	Sources                      []string `json:"sources"`
	PresidioEntities             []string `json:"presidio_entities"`
	Action                       string   `json:"action"`
	Score                        float64  `json:"score"`
	Prompt                       string   `json:"prompt,omitempty"`
	UserMessage                  string   `json:"user_message,omitempty"`
	RequiresApprovedEmailDomains bool     `json:"requires_approved_email_domains"`
	RequiresPromptPolicies       bool     `json:"requires_prompt_policies"`
}

type ListRiskPresetsOutput struct {
	Presets []RiskPresetSummary `json:"presets"`
}

type SuggestRiskPolicyInput struct {
	Description string `json:"description"`
}

type RiskPolicyDraft struct {
	Preset           string   `json:"preset,omitempty"`
	PolicyType       string   `json:"policy_type"`
	Name             string   `json:"name"`
	Enabled          bool     `json:"enabled"`
	Action           string   `json:"action"`
	Score            float64  `json:"score"`
	Sources          []string `json:"sources,omitempty"`
	PresidioEntities []string `json:"presidio_entities,omitempty"`
	Prompt           string   `json:"prompt,omitempty"`
	UserMessage      string   `json:"user_message,omitempty"`
}

type RiskPresetAlternative struct {
	Preset     string  `json:"preset"`
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
}

type SuggestRiskPolicyOutput struct {
	Preset                 string                  `json:"preset,omitempty"`
	Confidence             float64                 `json:"confidence"`
	Rationale              string                  `json:"rationale"`
	Draft                  RiskPolicyDraft         `json:"draft"`
	RequiresPromptPolicies bool                    `json:"requires_prompt_policies"`
	Alternatives           []RiskPresetAlternative `json:"alternatives"`
	NextStep               string                  `json:"next_step"`
}

const (
	suggestRiskPolicyMinDescriptionRunes = 3
	suggestRiskPolicyNextStep            = "Show the draft to the administrator in plain words: what it detects, what happens when it fires, how severe findings are, and that it is enabled as soon as it is created unless enabled is false. After they confirm, call create_risk_policy with project_slug, idempotency_key and either the preset id plus any agreed overrides, or the draft's fields with policy_type, name and enabled. Prompt-based drafts need prompt policies switched on for the project. Presets that require approved email domains need approved_email_domains before they detect anything. Then read the policy back with get_risk_policy."
)

func registerRiskPresetTools(reg *Registrar) {
	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeNone}

	addTool(reg, &mcp.Tool{
		Name:        "list_risk_presets",
		Title:       "List Risk Policy Presets",
		Description: "List the use-case presets a risk policy can start from, with what each one detects, its policy type, default action and severity. Use a preset id with create_risk_policy to create the policy without choosing detectors by hand. Presets marked requires_prompt_policies only work in projects where prompt policies are switched on.",
		Annotations: readOnlyAnnotations(),
		InputSchema: closedObject(map[string]*jsonschema.Schema{}, nil),
	}, meta, func(_ context.Context, _ *mcp.CallToolRequest, _ ListRiskPresetsInput) (*mcp.CallToolResult, ListRiskPresetsOutput, error) {
		all := presets.All()
		out := ListRiskPresetsOutput{Presets: make([]RiskPresetSummary, 0, len(all))}
		for _, preset := range all {
			out.Presets = append(out.Presets, RiskPresetSummary{
				ID: preset.ID, Label: preset.Label, Description: preset.Description, PolicyType: preset.PolicyType,
				Sources: emptyIfNil(preset.Sources), PresidioEntities: emptyIfNil(preset.PresidioEntities),
				Action: preset.Action, Score: preset.Score, Prompt: preset.Prompt, UserMessage: preset.UserMessage,
				RequiresApprovedEmailDomains: preset.RequiresApprovedEmailDomains,
				RequiresPromptPolicies:       preset.PolicyType == presets.PolicyTypePromptBased,
			})
		}
		return nil, out, nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "suggest_risk_policy",
		Title:       "Suggest Risk Policy",
		Description: "Turn a plain-language description of a risk into a policy draft without creating anything: the closest preset with its detectors, action and severity, or a bespoke prompt-based guardrail carrying the description as its instruction when no preset fits. The match is a deterministic keyword mapping over the preset catalog; you are the model, so refine the draft with the administrator before creating it. Returns the draft in create_risk_policy's shape plus the runner-up presets.",
		Annotations: readOnlyAnnotations(),
		InputSchema: closedObject(map[string]*jsonschema.Schema{
			"description": stringSchema("What the policy should catch or prevent, in the administrator's own words.", suggestRiskPolicyMinDescriptionRunes, 500),
		}, []string{"description"}),
	}, meta, func(_ context.Context, _ *mcp.CallToolRequest, input SuggestRiskPolicyInput) (*mcp.CallToolResult, SuggestRiskPolicyOutput, error) {
		description := strings.TrimSpace(input.Description)
		if len([]rune(description)) < suggestRiskPolicyMinDescriptionRunes {
			return riskPresetRefusal("invalid_request", "Describe the risk in a few words before asking for a draft.")
		}
		suggestion := presets.Suggest(description)
		out := SuggestRiskPolicyOutput{
			Preset:     suggestion.Draft.PresetID,
			Confidence: suggestion.Confidence,
			Rationale:  suggestion.Rationale,
			Draft: RiskPolicyDraft{
				Preset: suggestion.Draft.PresetID, PolicyType: suggestion.Draft.PolicyType, Name: suggestion.Draft.Name, Enabled: true,
				Action: suggestion.Draft.Action, Score: suggestion.Draft.Score,
				Sources: suggestion.Draft.Sources, PresidioEntities: suggestion.Draft.PresidioEntities,
				Prompt: suggestion.Draft.Prompt, UserMessage: suggestion.Draft.UserMessage,
			},
			RequiresPromptPolicies: suggestion.Draft.PolicyType == presets.PolicyTypePromptBased,
			Alternatives:           make([]RiskPresetAlternative, 0, len(suggestion.Alternatives)),
			NextStep:               suggestRiskPolicyNextStep,
		}
		for _, match := range suggestion.Alternatives {
			out.Alternatives = append(out.Alternatives, RiskPresetAlternative{Preset: match.Preset.ID, Label: match.Preset.Label, Confidence: match.Confidence})
		}
		return nil, out, nil
	})
}

func riskPresetRefusal(code, message string) (*mcp.CallToolResult, SuggestRiskPolicyOutput, error) {
	var zero SuggestRiskPolicyOutput
	payload, err := json.Marshal(map[string]string{"code": code, "feature": "risk_presets", "message": message})
	if err != nil {
		return nil, zero, fmt.Errorf("encode risk preset refusal: %w", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, IsError: true}, zero, nil
}

func emptyIfNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func riskPresetDescription(id string) string {
	preset, ok := presets.ByID(id)
	if !ok {
		return ""
	}
	return preset.Label + ". " + preset.Description
}
