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

// answers builds the rules' input from provider/plan pairs and product slugs.
func answers(useCase onboarding.UseCase, mdm onboarding.MDMVendor, providers []onboarding.SelectedProvider, products ...string) onboarding.Answers {
	return onboarding.Answers{UseCase: useCase, MDM: mdm, Providers: providers, Products: products}
}

func anthropic(plan string) onboarding.SelectedProvider {
	return onboarding.SelectedProvider{Provider: onboarding.ProviderAnthropic, Plan: plan}
}

func openai(plan string) onboarding.SelectedProvider {
	return onboarding.SelectedProvider{Provider: onboarding.ProviderOpenAI, Plan: plan}
}

func cursor(plan string) onboarding.SelectedProvider {
	return onboarding.SelectedProvider{Provider: onboarding.ProviderCursor, Plan: plan}
}

func planless(provider string) onboarding.SelectedProvider {
	return onboarding.SelectedProvider{Provider: provider, Plan: ""}
}

func TestStepsFor_ObservabilityClaudeCodeEnterpriseUsesInferenceHooks(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, answers(onboarding.UseCaseObservability, onboarding.MDMNone,
		[]onboarding.SelectedProvider{anthropic(onboarding.PlanAnthropicEnterprise)}, onboarding.ProductClaudeCodeCLI))
	require.Equal(t, []string{onboarding.TechniqueAnthropicInferenceHooks}, stepSlugs(steps))
	require.Equal(t, onboarding.EvidenceHookEvents, steps[0].Evidence)
	require.Equal(t, []string{"claude-code"}, steps[0].Sources)
}

func TestStepsFor_PlanAppliesToEveryProductOfTheProvider(t *testing.T) {
	t.Parallel()
	// One Anthropic plan covers Claude Code and Claude Chat alike: both fold
	// into the single inference hooks step.
	steps := onboarding.StepsFor(onboarding.Default, answers(onboarding.UseCaseObservability, onboarding.MDMNone,
		[]onboarding.SelectedProvider{anthropic(onboarding.PlanAnthropicEnterprise)},
		onboarding.ProductClaudeCodeCLI, onboarding.ProductClaudeChat, onboarding.ProductCowork))
	require.Equal(t, []string{onboarding.TechniqueAnthropicInferenceHooks}, stepSlugs(steps))
	require.ElementsMatch(t, []string{"claude-code", "claude-chat-web", "claude", "claude-chat", "cowork"}, steps[0].Sources)
}

func TestStepsFor_ObservabilityClaudeCodeTeamWithMDMUsesManagedSettings(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, answers(onboarding.UseCaseObservability, onboarding.MDMJamf,
		[]onboarding.SelectedProvider{anthropic(onboarding.PlanAnthropicTeam)}, onboarding.ProductClaudeCodeCLI))
	require.Equal(t, []string{onboarding.TechniqueManagedSettings + ":" + onboarding.ProductClaudeCodeCLI}, stepSlugs(steps))
	require.Equal(t, onboarding.DestinationDevices, steps[0].Destination)
}

func TestStepsFor_ObservabilityClaudeCodeProWithoutMDMUsesPlugin(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, answers(onboarding.UseCaseObservability, onboarding.MDMNone,
		[]onboarding.SelectedProvider{anthropic(onboarding.PlanAnthropicPro)}, onboarding.ProductClaudeCodeCLI))
	require.Equal(t, []string{onboarding.TechniquePluginDistribution + ":" + onboarding.ProductClaudeCodeCLI}, stepSlugs(steps))
	require.Equal(t, onboarding.TechniqueClaudeHooks, steps[0].Technique)
}

func TestStepsFor_ObservabilityClaudeChatProHasNoStep(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, answers(onboarding.UseCaseObservability, onboarding.MDMNone,
		[]onboarding.SelectedProvider{anthropic(onboarding.PlanAnthropicPro)}, onboarding.ProductClaudeChat))
	require.Empty(t, steps)
}

func TestStepsFor_ObservabilityDedupesSharedSteps(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, answers(onboarding.UseCaseObservability, onboarding.MDMNone,
		[]onboarding.SelectedProvider{anthropic(onboarding.PlanAnthropicEnterprise), cursor(onboarding.PlanCursorPro), planless(onboarding.ProviderOpenCode)},
		onboarding.ProductClaudeChat, onboarding.ProductClaudeCodeWeb, onboarding.ProductCursor, onboarding.ProductOpenCode))
	require.Equal(t, []string{
		onboarding.TechniqueAnthropicInferenceHooks,
		onboarding.TechniquePluginDistribution + ":" + onboarding.ProductCursor,
		onboarding.TechniquePluginDistribution + ":" + onboarding.ProductOpenCode,
	}, stepSlugs(steps))
	require.ElementsMatch(t, []string{"claude-chat-web", "claude", "claude-chat", "claude-code-web"}, steps[0].Sources)
}

func TestStepsFor_ObservabilityOpenAIBusinessImportsConversations(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, answers(onboarding.UseCaseObservability, onboarding.MDMNone,
		[]onboarding.SelectedProvider{openai(onboarding.PlanOpenAIBusiness)}, onboarding.ProductChatGPT, onboarding.ProductCodex))
	require.Equal(t, []string{
		onboarding.TechniqueOpenAIComplianceAPI + ":conversations",
		onboarding.TechniquePluginDistribution + ":" + onboarding.ProductCodex,
	}, stepSlugs(steps))
}

func TestStepsFor_ObservabilityChatGPTPlusHasNoStep(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, answers(onboarding.UseCaseObservability, onboarding.MDMNone,
		[]onboarding.SelectedProvider{openai(onboarding.PlanOpenAIPlus)}, onboarding.ProductChatGPT))
	require.Empty(t, steps)
}

func TestStepsFor_CostTrackingPrefersProviderAdminAPIs(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, answers(onboarding.UseCaseCostTracking, onboarding.MDMNone,
		[]onboarding.SelectedProvider{anthropic(onboarding.PlanAnthropicEnterprise), cursor(onboarding.PlanCursorTeams), openai(onboarding.PlanOpenAIEnterprise), planless(onboarding.ProviderLiteLLM)},
		onboarding.ProductClaudeCodeCLI, onboarding.ProductCursor, onboarding.ProductCodex, onboarding.ProductLiteLLM))
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
	steps := onboarding.StepsFor(onboarding.Default, answers(onboarding.UseCaseCostTracking, onboarding.MDMNone,
		[]onboarding.SelectedProvider{anthropic(onboarding.PlanAnthropicMax), cursor(onboarding.PlanCursorPro)},
		onboarding.ProductClaudeCodeCLI, onboarding.ProductCursor))
	require.Equal(t, []string{
		onboarding.TechniquePluginDistribution + ":" + onboarding.ProductClaudeCodeCLI,
		onboarding.TechniquePluginDistribution + ":" + onboarding.ProductCursor,
	}, stepSlugs(steps))
}

func TestStepsFor_SecurityStartsWithAPolicy(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, answers(onboarding.UseCaseSecurity, onboarding.MDMNone,
		[]onboarding.SelectedProvider{cursor(onboarding.PlanCursorPro)}, onboarding.ProductCursor))
	require.Equal(t, []string{
		onboarding.TechniqueRiskPolicies,
		onboarding.TechniquePluginDistribution + ":" + onboarding.ProductCursor,
	}, stepSlugs(steps))
	require.Equal(t, onboarding.EvidenceActivePolicy, steps[0].Evidence)
}

func TestStepsFor_SecurityAddsDeviceAgentWhenNothingCanBeScanned(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, answers(onboarding.UseCaseSecurity, onboarding.MDMNone,
		[]onboarding.SelectedProvider{anthropic(onboarding.PlanAnthropicPro)}, onboarding.ProductClaudeChat))
	require.Equal(t, []string{onboarding.TechniqueRiskPolicies, onboarding.TechniqueDeviceAgent}, stepSlugs(steps))
}

func TestStepsFor_SecurityAddsDeviceAgentForManagedFleets(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, answers(onboarding.UseCaseSecurity, onboarding.MDMIntune,
		[]onboarding.SelectedProvider{openai(onboarding.PlanOpenAIPlus)}, onboarding.ProductCodex))
	require.Equal(t, []string{
		onboarding.TechniqueRiskPolicies,
		onboarding.TechniquePluginDistribution + ":" + onboarding.ProductCodex,
		onboarding.TechniqueDeviceAgent,
	}, stepSlugs(steps))
}

func TestStepsFor_MCPGatewayThenDistribution(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, answers(onboarding.UseCaseMCPGateway, onboarding.MDMIru,
		[]onboarding.SelectedProvider{anthropic(onboarding.PlanAnthropicTeam)}, onboarding.ProductClaudeCodeCLI))
	require.Equal(t, []string{onboarding.TechniqueMCPGateway, onboarding.TechniquePluginDistribution}, stepSlugs(steps))
	require.Equal(t, onboarding.EvidenceGatewayTraffic, steps[0].Evidence)
	require.Equal(t, onboarding.EvidencePluginAssignment, steps[1].Evidence)
	require.Contains(t, steps[1].Description, "Iru")
}

func TestNextStep_SkipsVerifiedSteps(t *testing.T) {
	t.Parallel()
	a := answers(onboarding.UseCaseMCPGateway, onboarding.MDMNone, []onboarding.SelectedProvider{cursor(onboarding.PlanCursorPro)}, onboarding.ProductCursor)
	next, ok := onboarding.NextStep(onboarding.Default, a, map[string]bool{})
	require.True(t, ok)
	require.Equal(t, onboarding.TechniqueMCPGateway, next.Slug)

	next, ok = onboarding.NextStep(onboarding.Default, a, map[string]bool{onboarding.TechniqueMCPGateway: true})
	require.True(t, ok)
	require.Equal(t, onboarding.TechniquePluginDistribution, next.Slug)

	_, ok = onboarding.NextStep(onboarding.Default, a, map[string]bool{onboarding.TechniqueMCPGateway: true, onboarding.TechniquePluginDistribution: true})
	require.False(t, ok)
}

func TestStepsFor_UnknownProductIsIgnored(t *testing.T) {
	t.Parallel()
	steps := onboarding.StepsFor(onboarding.Default, answers(onboarding.UseCaseObservability, onboarding.MDMNone, nil, "not-a-product"))
	require.Empty(t, steps)
}

func TestPlanFor_ProductWithoutSelectedProviderHasNoPlan(t *testing.T) {
	t.Parallel()
	product, ok := onboarding.Default.Product(onboarding.ProductCursor)
	require.True(t, ok)
	a := answers(onboarding.UseCaseObservability, onboarding.MDMNone, []onboarding.SelectedProvider{anthropic(onboarding.PlanAnthropicPro)}, onboarding.ProductCursor)
	require.Empty(t, a.PlanFor(product))
	a.Providers = append(a.Providers, cursor(onboarding.PlanCursorTeams))
	require.Equal(t, onboarding.PlanCursorTeams, a.PlanFor(product))
}
