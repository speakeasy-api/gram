package risk

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type CreateRiskExclusion struct {
	risk RiskService
}

type createRiskExclusionInput struct {
	RiskPolicyID *string `json:"risk_policy_id,omitempty" jsonschema:"Bind the exclusion to a single policy. Omit for a global exclusion that applies to every policy in the project."`
	MatchType    string  `json:"match_type" jsonschema:"How match_value is interpreted: exact (finding text), regex (RE2 pattern over finding text), rule_id, source, or entity_type (presidio entity, matched as rule_id 'pii.<entity>')."`
	MatchValue   string  `json:"match_value" jsonschema:"The value matched against findings, interpreted per match_type."`
	RuleIDFilter *string `json:"rule_id_filter,omitempty" jsonschema:"Narrow an exact/regex/source exclusion to findings with this rule_id. Omit to apply to any rule."`
	SourceFilter *string `json:"source_filter,omitempty" jsonschema:"Narrow an exact/regex/rule_id exclusion to findings from this source. Omit to apply to any source."`
	Enabled      *bool   `json:"enabled,omitempty" jsonschema:"Whether the exclusion is active. Defaults to true."`
}

func NewCreateRiskExclusionTool(riskSvc RiskService) *CreateRiskExclusion {
	return &CreateRiskExclusion{risk: riskSvc}
}

func (s *CreateRiskExclusion) Descriptor() core.ToolDescriptor {
	destructive := false
	idempotent := false

	return core.ToolDescriptor{
		SourceSlug:  "risk",
		HandlerName: "create_risk_exclusion",
		Name:        "platform_create_risk_exclusion",
		Description: "Create a risk exclusion so matching findings stop being flagged, now and in future scans. Use this for a whole class of findings; use platform_mark_risk_false_positive to dismiss specific findings without changing future detection.",
		InputSchema: core.BuildInputSchema[createRiskExclusionInput](
			core.WithPropertyFormat("risk_policy_id", "uuid"),
			core.WithPropertyEnum("match_type", "exact", "regex", "rule_id", "source", "entity_type"),
		),
		Variables:   nil,
		Annotations: writeAnnotations(destructive, idempotent),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *CreateRiskExclusion) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.risk == nil {
		return fmt.Errorf("risk service not configured")
	}

	input := createRiskExclusionInput{
		RiskPolicyID: nil,
		MatchType:    "",
		MatchValue:   "",
		RuleIDFilter: nil,
		SourceFilter: nil,
		Enabled:      nil,
	}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.MatchType == "" {
		return fmt.Errorf("match_type is required")
	}
	if input.MatchValue == "" {
		return fmt.Errorf("match_value is required")
	}

	// The Goa defaults for these fields are applied by the HTTP decoder, which
	// this in-process call bypasses, so they are applied here instead.
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	ruleIDFilter := ""
	if input.RuleIDFilter != nil {
		ruleIDFilter = *input.RuleIDFilter
	}
	sourceFilter := ""
	if input.SourceFilter != nil {
		sourceFilter = *input.SourceFilter
	}

	result, err := s.risk.CreateRiskExclusion(ctx, &risk.CreateRiskExclusionPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		RiskPolicyID:     input.RiskPolicyID,
		MatchType:        input.MatchType,
		MatchValue:       input.MatchValue,
		RuleIDFilter:     ruleIDFilter,
		SourceFilter:     sourceFilter,
		Enabled:          enabled,
	})
	if err != nil {
		return fmt.Errorf("create risk exclusion: %w", err)
	}

	return core.EncodeResult(wr, result)
}
