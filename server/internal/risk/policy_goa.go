package risk

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
)

func policyToGoa(policy policycore.Policy) *types.RiskPolicy {
	detectionScopes := make([]*types.RiskDetectionScope, 0, len(policy.DetectionScopes))
	for _, scope := range policy.DetectionScopes {
		detectionScopes = append(detectionScopes, &types.RiskDetectionScope{
			Category:     scope.Category,
			ScopeInclude: scope.ScopeInclude,
			ScopeExempt:  scope.ScopeExempt,
		})
	}
	if len(detectionScopes) == 0 {
		detectionScopes = nil
	}

	var modelConfig *types.RiskPolicyModelConfig
	if policy.ModelConfig != nil {
		modelConfig = &types.RiskPolicyModelConfig{
			Model:       policy.ModelConfig.Model,
			Temperature: policy.ModelConfig.Temperature,
			FailOpen:    policy.ModelConfig.FailOpen,
		}
	}

	return &types.RiskPolicy{
		ID:                     policy.ID.String(),
		ProjectID:              policy.ProjectID.String(),
		Name:                   policy.Name,
		PolicyType:             policy.PolicyType,
		Sources:                policy.Sources,
		PresidioEntities:       policy.PresidioEntities,
		PresidioScoreThreshold: policy.PresidioScoreThreshold,
		ApprovedEmailDomains:   policy.ApprovedEmailDomains,
		DetectionScopes:        detectionScopes,
		PromptInjectionRules:   policy.PromptInjectionRules,
		DisabledRules:          policy.DisabledRules,
		CustomRuleIds:          policy.CustomRuleIDs,
		MessageTypes:           policy.MessageTypes,
		ScopeInclude:           policy.ScopeInclude,
		ScopeExempt:            policy.ScopeExempt,
		Enabled:                policy.Enabled,
		Action:                 policy.Action,
		AudienceType:           policy.AudienceType,
		AudiencePrincipalUrns:  policy.AudiencePrincipalURNs,
		ShadowMcpDisposition:   policy.ShadowMCPDisposition,
		AutoName:               policy.AutoName,
		UserMessage:            policy.UserMessage,
		Prompt:                 policy.Prompt,
		ModelConfig:            modelConfig,
		Score:                  policy.Score,
		Version:                policy.Version,
		CreatedAt:              policy.CreatedAt.Format(time.RFC3339),
		UpdatedAt:              policy.UpdatedAt.Format(time.RFC3339),
		PendingMessages:        policy.PendingMessages,
		TotalMessages:          policy.TotalMessages,
	}
}
