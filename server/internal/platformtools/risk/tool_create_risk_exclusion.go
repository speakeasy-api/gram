package risk

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/risk/exclusioncore"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type CreateRiskExclusion struct {
	risk     RiskService
	redactor exclusioncore.Redactor
}

type createRiskExclusionInput struct {
	RiskPolicyID *string `json:"risk_policy_id,omitempty" jsonschema:"Bind the exclusion to a single policy. Omit for a global exclusion that applies to every policy in the project."`
	MatchType    string  `json:"match_type" jsonschema:"How match_value is interpreted: exact (finding text), regex (RE2 pattern over finding text), rule_id, source, or entity_type (presidio entity, matched as rule_id 'pii.<entity>')."`
	MatchValue   string  `json:"match_value" jsonschema:"The value matched against findings, interpreted per match_type."`
	RuleIDFilter *string `json:"rule_id_filter,omitempty" jsonschema:"Narrow an exact/regex/source exclusion to findings with this rule_id. Omit to apply to any rule."`
	SourceFilter *string `json:"source_filter,omitempty" jsonschema:"Narrow an exact/regex/rule_id exclusion to findings from this source. Omit to apply to any source."`
	Enabled      *bool   `json:"enabled,omitempty" jsonschema:"Whether the exclusion is active. Defaults to true."`
}

func NewCreateRiskExclusionTool(riskSvc RiskService, redactionKey string) *CreateRiskExclusion {
	return &CreateRiskExclusion{risk: riskSvc, redactor: exclusioncore.NewRedactor(redactionKey)}
}

func (s *CreateRiskExclusion) Descriptor() core.ToolDescriptor {
	destructive := false
	idempotent := false

	return core.ToolDescriptor{
		SourceSlug:  "risk",
		HandlerName: "create_risk_exclusion",
		Name:        "platform_create_risk_exclusion",
		Description: "Create a risk exclusion so matching findings stop being flagged, now and in future scans. Use this for a whole class of findings; use platform_mark_risk_false_positive to dismiss specific findings without changing future detection. If an equivalent exclusion already exists it is returned unchanged rather than duplicated.",
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

// findEquivalent returns an existing exclusion identical to the one described by
// input, or nil if there is none. Enabled is part of the comparison: reusing a
// disabled row for an enable request would report success while leaving the
// findings flagged, which is the opposite of what the caller asked for.
func (s *CreateRiskExclusion) findEquivalent(ctx context.Context, input createRiskExclusionInput, policyID *string, ruleIDFilter, sourceFilter string, enabled bool) (*types.RiskExclusion, error) {
	// Listed unfiltered: a nil risk_policy_id means "global", not "any policy",
	// so the policy binding has to be compared here rather than pushed down.
	listed, err := s.risk.ListRiskExclusions(ctx, &risk.ListRiskExclusionsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		RiskPolicyID:     nil,
	})
	if err != nil {
		return nil, fmt.Errorf("list risk exclusions: %w", err)
	}

	for _, exclusion := range listed.Exclusions {
		if exclusion.MatchType != input.MatchType || exclusion.MatchValue != input.MatchValue {
			continue
		}
		if exclusion.RuleIDFilter != ruleIDFilter || exclusion.SourceFilter != sourceFilter {
			continue
		}
		if exclusion.Enabled != enabled {
			continue
		}
		if !samePolicyBinding(exclusion.RiskPolicyID, policyID) {
			continue
		}
		return exclusion, nil
	}

	return nil, nil
}

func samePolicyBinding(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
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
	// The service reads an empty risk_policy_id as global, same as an omitted
	// one, so they are collapsed here too — otherwise the two spellings look
	// like distinct scopes to the equivalence check below.
	policyID := input.RiskPolicyID
	if policyID != nil && *policyID == "" {
		policyID = nil
	}

	// platform_list_risk_exclusions fingerprints exact/regex match values, so the
	// model cannot tell from that listing whether the exclusion it is about to
	// create already exists. Dedupe here instead: this call has the raw proposed
	// value and the raw existing ones, so the comparison happens without either
	// pattern reaching the model.
	existing, err := s.findEquivalent(ctx, input, policyID, ruleIDFilter, sourceFilter, enabled)
	if err != nil {
		return err
	}
	if existing != nil {
		return core.EncodeResult(wr, toExclusionView(s.redactor, existing))
	}

	result, err := s.risk.CreateRiskExclusion(ctx, &risk.CreateRiskExclusionPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		RiskPolicyID:     policyID,
		MatchType:        input.MatchType,
		MatchValue:       input.MatchValue,
		RuleIDFilter:     ruleIDFilter,
		SourceFilter:     sourceFilter,
		Enabled:          enabled,
	})
	if err != nil {
		return fmt.Errorf("create risk exclusion: %w", err)
	}

	// Same view as the listing so the two tools agree on shape. The model
	// supplied this match_value itself, so nothing new is hidden from it.
	return core.EncodeResult(wr, toExclusionView(s.redactor, result))
}
