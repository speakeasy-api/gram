package onboarding_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/onboarding"
)

func stepSlugs(steps []onboarding.Step) []string {
	slugs := make([]string, 0, len(steps))
	for _, s := range steps {
		slugs = append(slugs, s.Slug)
	}
	return slugs
}

func TestStepsFor_ObservabilityClaudeCodeEnterpriseUsesInferenceHooks(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, onboarding.Answers{
		UseCase:  onboarding.UseCaseObservability,
		MDM:      onboarding.MDMNone,
		Products: []onboarding.SelectedProduct{{Product: onboarding.ProductClaudeCodeCLI, Plan: onboarding.PlanAnthropicEnterprise}},
	})
	require.Equal(t, []string{onboarding.TechniqueAnthropicInferenceHooks}, stepSlugs(steps))
	require.Equal(t, onboarding.EvidenceHookEvents, steps[0].Evidence)
	require.Equal(t, []string{"claude-code"}, steps[0].Sources)
}

func TestStepsFor_ObservabilityClaudeCodeTeamWithMDMUsesManagedSettings(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, onboarding.Answers{
		UseCase:  onboarding.UseCaseObservability,
		MDM:      onboarding.MDMJamf,
		Products: []onboarding.SelectedProduct{{Product: onboarding.ProductClaudeCodeCLI, Plan: onboarding.PlanAnthropicTeam}},
	})
	require.Equal(t, []string{onboarding.TechniqueManagedSettings + ":" + onboarding.ProductClaudeCodeCLI}, stepSlugs(steps))
	require.Equal(t, onboarding.DestinationDevices, steps[0].Destination)
}

func TestStepsFor_ObservabilityClaudeCodeProWithoutMDMUsesPlugin(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, onboarding.Answers{
		UseCase:  onboarding.UseCaseObservability,
		MDM:      onboarding.MDMNone,
		Products: []onboarding.SelectedProduct{{Product: onboarding.ProductClaudeCodeCLI, Plan: onboarding.PlanAnthropicPro}},
	})
	require.Equal(t, []string{onboarding.TechniquePluginDistribution + ":" + onboarding.ProductClaudeCodeCLI}, stepSlugs(steps))
	require.Equal(t, onboarding.TechniqueClaudeHooks, steps[0].Technique)
}

func TestStepsFor_ObservabilityClaudeChatProHasNoStep(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, onboarding.Answers{
		UseCase:  onboarding.UseCaseObservability,
		MDM:      onboarding.MDMNone,
		Products: []onboarding.SelectedProduct{{Product: onboarding.ProductClaudeChat, Plan: onboarding.PlanAnthropicPro}},
	})
	require.Empty(t, steps)
}

func TestStepsFor_ObservabilityDedupesSharedSteps(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, onboarding.Answers{
		UseCase: onboarding.UseCaseObservability,
		MDM:     onboarding.MDMNone,
		Products: []onboarding.SelectedProduct{
			{Product: onboarding.ProductClaudeChat, Plan: onboarding.PlanAnthropicEnterprise},
			{Product: onboarding.ProductClaudeCodeWeb, Plan: onboarding.PlanAnthropicEnterprise},
			{Product: onboarding.ProductCursor, Plan: onboarding.PlanCursorPro},
			{Product: onboarding.ProductOpenCode, Plan: ""},
		},
	})
	require.Equal(t, []string{
		onboarding.TechniqueAnthropicInferenceHooks,
		onboarding.TechniquePluginDistribution + ":" + onboarding.ProductCursor,
		onboarding.TechniquePluginDistribution + ":" + onboarding.ProductOpenCode,
	}, stepSlugs(steps))
	require.ElementsMatch(t, []string{"claude-chat-web", "claude", "claude-chat", "claude-code-web"}, steps[0].Sources)
}

func TestStepsFor_ObservabilityOpenAIBusinessImportsConversations(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, onboarding.Answers{
		UseCase: onboarding.UseCaseObservability,
		MDM:     onboarding.MDMNone,
		Products: []onboarding.SelectedProduct{
			{Product: onboarding.ProductChatGPT, Plan: onboarding.PlanOpenAIBusiness},
			{Product: onboarding.ProductCodex, Plan: onboarding.PlanOpenAIBusiness},
		},
	})
	require.Equal(t, []string{
		onboarding.TechniqueOpenAIComplianceAPI + ":conversations",
		onboarding.TechniquePluginDistribution + ":" + onboarding.ProductCodex,
	}, stepSlugs(steps))
}

func TestStepsFor_ObservabilityChatGPTPlusHasNoStep(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, onboarding.Answers{
		UseCase:  onboarding.UseCaseObservability,
		MDM:      onboarding.MDMNone,
		Products: []onboarding.SelectedProduct{{Product: onboarding.ProductChatGPT, Plan: onboarding.PlanOpenAIPlus}},
	})
	require.Empty(t, steps)
}

func TestStepsFor_CostTrackingPrefersVendorAdminAPIs(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, onboarding.Answers{
		UseCase: onboarding.UseCaseCostTracking,
		MDM:     onboarding.MDMNone,
		Products: []onboarding.SelectedProduct{
			{Product: onboarding.ProductClaudeCodeCLI, Plan: onboarding.PlanAnthropicEnterprise},
			{Product: onboarding.ProductCursor, Plan: onboarding.PlanCursorTeams},
			{Product: onboarding.ProductCodex, Plan: onboarding.PlanOpenAIEnterprise},
			{Product: onboarding.ProductLiteLLM, Plan: ""},
		},
	})
	require.Equal(t, []string{
		onboarding.TechniqueAnthropicAdminAnalytics,
		onboarding.TechniqueCursorAdminAPI,
		onboarding.TechniqueOpenAIComplianceAPI + ":costs",
		onboarding.TechniqueLiteLLMGuardrail,
	}, stepSlugs(steps))
	for _, step := range steps[:3] {
		require.Equal(t, onboarding.EvidenceCostRows, step.Evidence, step.Slug)
	}
}

func TestStepsFor_CostTrackingIndividualPlansFallBackToInstrumentation(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, onboarding.Answers{
		UseCase: onboarding.UseCaseCostTracking,
		MDM:     onboarding.MDMNone,
		Products: []onboarding.SelectedProduct{
			{Product: onboarding.ProductClaudeCodeCLI, Plan: onboarding.PlanAnthropicMax},
			{Product: onboarding.ProductCursor, Plan: onboarding.PlanCursorPro},
		},
	})
	require.Equal(t, []string{
		onboarding.TechniquePluginDistribution + ":" + onboarding.ProductClaudeCodeCLI,
		onboarding.TechniquePluginDistribution + ":" + onboarding.ProductCursor,
	}, stepSlugs(steps))
}

func TestStepsFor_SecurityStartsWithAPolicy(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, onboarding.Answers{
		UseCase:  onboarding.UseCaseSecurity,
		MDM:      onboarding.MDMNone,
		Products: []onboarding.SelectedProduct{{Product: onboarding.ProductCursor, Plan: onboarding.PlanCursorPro}},
	})
	require.Equal(t, []string{
		onboarding.TechniqueRiskPolicies,
		onboarding.TechniquePluginDistribution + ":" + onboarding.ProductCursor,
	}, stepSlugs(steps))
	require.Equal(t, onboarding.EvidenceActivePolicy, steps[0].Evidence)
}

func TestStepsFor_SecurityAddsDeviceAgentWhenNothingCanBeScanned(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, onboarding.Answers{
		UseCase:  onboarding.UseCaseSecurity,
		MDM:      onboarding.MDMNone,
		Products: []onboarding.SelectedProduct{{Product: onboarding.ProductClaudeChat, Plan: onboarding.PlanAnthropicPro}},
	})
	require.Equal(t, []string{onboarding.TechniqueRiskPolicies, onboarding.TechniqueDeviceAgent}, stepSlugs(steps))
}

func TestStepsFor_SecurityAddsDeviceAgentForManagedFleets(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, onboarding.Answers{
		UseCase:  onboarding.UseCaseSecurity,
		MDM:      onboarding.MDMIntune,
		Products: []onboarding.SelectedProduct{{Product: onboarding.ProductCodex, Plan: onboarding.PlanOpenAIPlus}},
	})
	require.Equal(t, []string{
		onboarding.TechniqueRiskPolicies,
		onboarding.TechniquePluginDistribution + ":" + onboarding.ProductCodex,
		onboarding.TechniqueDeviceAgent,
	}, stepSlugs(steps))
}

func TestStepsFor_MCPGatewayThenDistribution(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, onboarding.Answers{
		UseCase:  onboarding.UseCaseMCPGateway,
		MDM:      onboarding.MDMIru,
		Products: []onboarding.SelectedProduct{{Product: onboarding.ProductClaudeCodeCLI, Plan: onboarding.PlanAnthropicTeam}},
	})
	require.Equal(t, []string{onboarding.TechniqueMCPGateway, onboarding.TechniquePluginDistribution}, stepSlugs(steps))
	require.Equal(t, onboarding.EvidenceGatewayTraffic, steps[0].Evidence)
	require.Equal(t, onboarding.EvidencePluginAssignment, steps[1].Evidence)
	require.Contains(t, steps[1].Description, "Iru")
}

func TestNextStep_SkipsVerifiedSteps(t *testing.T) {
	t.Parallel()
	answers := onboarding.Answers{
		UseCase:  onboarding.UseCaseMCPGateway,
		MDM:      onboarding.MDMNone,
		Products: []onboarding.SelectedProduct{{Product: onboarding.ProductCursor, Plan: onboarding.PlanCursorPro}},
	}
	next, ok := onboarding.NextStep(onboarding.Default, answers, map[string]bool{})
	require.True(t, ok)
	require.Equal(t, onboarding.TechniqueMCPGateway, next.Slug)

	next, ok = onboarding.NextStep(onboarding.Default, answers, map[string]bool{onboarding.TechniqueMCPGateway: true})
	require.True(t, ok)
	require.Equal(t, onboarding.TechniquePluginDistribution, next.Slug)

	_, ok = onboarding.NextStep(onboarding.Default, answers, map[string]bool{onboarding.TechniqueMCPGateway: true, onboarding.TechniquePluginDistribution: true})
	require.False(t, ok)
}

func TestStepsFor_UnknownProductIsIgnored(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, onboarding.Answers{
		UseCase:  onboarding.UseCaseObservability,
		MDM:      onboarding.MDMNone,
		Products: []onboarding.SelectedProduct{{Product: "not-a-product", Plan: ""}},
	})
	require.Empty(t, steps)
}
