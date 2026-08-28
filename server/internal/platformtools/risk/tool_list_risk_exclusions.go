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

// exclusionView is the model-facing shape of an exclusion. For the free-text
// match types, match_value is the literal string the author wanted suppressed —
// often the very secret or email that triggered the finding — so it is replaced
// with a project-scoped keyed fingerprint. The remaining match types and both
// filters are rule/source identifiers, not captured content, so they stay
// readable: without them the model cannot tell what an exclusion covers.
type exclusionView struct {
	ID           string  `json:"id"`
	RiskPolicyID *string `json:"risk_policy_id,omitempty"`
	MatchType    string  `json:"match_type"`
	MatchValue   string  `json:"match_value"`
	RuleIDFilter string  `json:"rule_id_filter"`
	SourceFilter string  `json:"source_filter"`
	Enabled      bool    `json:"enabled"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type listRiskExclusionsResult struct {
	Exclusions []exclusionView `json:"exclusions"`
}

func redactMatchValue(redactor exclusioncore.Redactor, projectID, matchType, matchValue string) string {
	if matchType != "exact" && matchType != "regex" {
		return matchValue
	}
	if matchValue == "" {
		return ""
	}
	return redactor.Redact(projectID, "match_value", matchValue)
}

func toExclusionView(redactor exclusioncore.Redactor, exclusion *types.RiskExclusion) exclusionView {
	return exclusionView{
		ID:           exclusion.ID,
		RiskPolicyID: exclusion.RiskPolicyID,
		MatchType:    exclusion.MatchType,
		MatchValue:   redactMatchValue(redactor, exclusion.ProjectID, exclusion.MatchType, exclusion.MatchValue),
		RuleIDFilter: exclusion.RuleIDFilter,
		SourceFilter: exclusion.SourceFilter,
		Enabled:      exclusion.Enabled,
		CreatedAt:    exclusion.CreatedAt,
		UpdatedAt:    exclusion.UpdatedAt,
	}
}

type ListRiskExclusions struct {
	risk     RiskService
	redactor exclusioncore.Redactor
}

type listRiskExclusionsInput struct {
	RiskPolicyID *string `json:"risk_policy_id,omitempty" jsonschema:"Only return exclusions bound to this policy ID. Omit to return every exclusion in the project (global plus policy-bound)."`
}

func NewListRiskExclusionsTool(riskSvc RiskService, redactionKey string) *ListRiskExclusions {
	return &ListRiskExclusions{risk: riskSvc, redactor: exclusioncore.NewRedactor(redactionKey)}
}

func (s *ListRiskExclusions) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "risk",
		HandlerName: "list_risk_exclusions",
		Name:        "platform_list_risk_exclusions",
		Description: "List the risk exclusions configured for the current project. For exact and regex exclusions, match_value is replaced with a stable project-scoped keyed fingerprint so suppressed secret content never enters the model context.",
		InputSchema: core.BuildInputSchema[listRiskExclusionsInput](
			core.WithPropertyFormat("risk_policy_id", "uuid"),
		),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *ListRiskExclusions) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.risk == nil {
		return fmt.Errorf("risk service not configured")
	}

	input := listRiskExclusionsInput{RiskPolicyID: nil}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}

	result, err := s.risk.ListRiskExclusions(ctx, &risk.ListRiskExclusionsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		RiskPolicyID:     input.RiskPolicyID,
	})
	if err != nil {
		return fmt.Errorf("list risk exclusions: %w", err)
	}

	views := make([]exclusionView, 0, len(result.Exclusions))
	for _, exclusion := range result.Exclusions {
		views = append(views, toExclusionView(s.redactor, exclusion))
	}

	return core.EncodeResult(wr, listRiskExclusionsResult{Exclusions: views})
}
