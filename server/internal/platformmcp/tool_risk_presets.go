//nolint:exhaustruct // MCP schemas rely on documented optional zero values.
package platformmcp

import (
	"context"
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
	Preset       string                  `json:"preset,omitempty"`
	Confidence   float64                 `json:"confidence"`
	Rationale    string                  `json:"rationale"`
	Draft        RiskPolicyDraft         `json:"draft"`
	Alternatives []RiskPresetAlternative `json:"alternatives"`
	NextStep     string                  `json:"next_step"`
}

const suggestRiskPolicyNextStep = "Show the draft to the administrator in plain words: what it detects, what happens when it fires, and how severe findings are. After they confirm, call create_risk_policy with project_slug, idempotency_key and either the preset id plus any overrides, or the draft's fields. Presets that require approved email domains need approved_email_domains before they detect anything."

func registerRiskPresetTools(reg *Registrar) {
	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeNone}

	addTool(reg, &mcp.Tool{
		Name:        "list_risk_presets",
		Title:       "List Risk Policy Presets",
		Description: "List the use-case presets a risk policy can start from, with what each one detects, its policy type, default action and severity. Use a preset id with create_risk_policy to create the policy without choosing detectors by hand.",
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
			})
		}
		return nil, out, nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "suggest_risk_policy",
		Title:       "Suggest Risk Policy",
		Description: "Turn a plain-language description of a risk into a policy draft without creating anything: the closest preset with its detectors, action and severity, or a bespoke prompt-based guardrail when no preset fits. Returns the draft in create_risk_policy's shape plus the runner-up presets.",
		Annotations: readOnlyAnnotations(),
		InputSchema: closedObject(map[string]*jsonschema.Schema{
			"description": stringSchema("What the policy should catch or prevent, in the administrator's own words.", 3, 500),
		}, []string{"description"}),
	}, meta, func(_ context.Context, _ *mcp.CallToolRequest, input SuggestRiskPolicyInput) (*mcp.CallToolResult, SuggestRiskPolicyOutput, error) {
		suggestion := presets.Suggest(strings.TrimSpace(input.Description))
		out := SuggestRiskPolicyOutput{
			Preset:     suggestion.Draft.PresetID,
			Confidence: suggestion.Confidence,
			Rationale:  suggestion.Rationale,
			Draft: RiskPolicyDraft{
				Preset: suggestion.Draft.PresetID, PolicyType: suggestion.Draft.PolicyType, Name: suggestion.Draft.Name,
				Action: suggestion.Draft.Action, Score: suggestion.Draft.Score,
				Sources: suggestion.Draft.Sources, PresidioEntities: suggestion.Draft.PresidioEntities,
				Prompt: suggestion.Draft.Prompt, UserMessage: suggestion.Draft.UserMessage,
			},
			Alternatives: make([]RiskPresetAlternative, 0, len(suggestion.Alternatives)),
			NextStep:     suggestRiskPolicyNextStep,
		}
		for _, match := range suggestion.Alternatives {
			out.Alternatives = append(out.Alternatives, RiskPresetAlternative{Preset: match.Preset.ID, Label: match.Preset.Label, Confidence: match.Confidence})
		}
		return nil, out, nil
	})
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
