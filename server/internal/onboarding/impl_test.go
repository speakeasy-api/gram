package onboarding_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/onboarding"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/onboarding"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return testenv.NewLogger(t)
}

// cursorProStack is the smallest useful stack: Cursor on Pro, no MDM.
func cursorProStack() *gen.SaveStackPayload {
	return &gen.SaveStackPayload{
		SessionToken: nil,
		Providers:    []*gen.OnboardingSelectedProvider{{ProviderSlug: onboarding.ProviderCursor, PlanSlug: conv.PtrEmpty(onboarding.PlanCursorPro)}},
		ProductSlugs: []string{onboarding.ProductCursor},
		MdmVendor:    string(onboarding.MDMNone),
	}
}

func useCase(u onboarding.UseCase) *gen.SelectUseCasePayload {
	return &gen.SelectUseCasePayload{SessionToken: nil, UseCase: string(u)}
}

func TestListReferenceData_ReturnsCatalog(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	data, err := ti.service.ListReferenceData(ctx, &gen.ListReferenceDataPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, data.Providers, len(onboarding.Default.Providers))
	require.Len(t, data.UseCases, len(onboarding.UseCases))
	require.Len(t, data.MdmVendors, len(onboarding.MDMVendors))

	var products int
	for _, provider := range data.Providers {
		require.Len(t, provider.Plans, len(onboarding.Default.PlansFor(provider.Slug)), provider.Slug)
		for _, product := range provider.Products {
			spec, ok := onboarding.Default.Product(product.Slug)
			require.True(t, ok, product.Slug)
			require.Equal(t, provider.Slug, spec.Provider)
			require.Equal(t, spec.SourceIDs, product.SourceIds)
			products++
		}
	}
	require.Equal(t, len(onboarding.Default.Products), products)
}

func TestListReferenceData_MembersCanRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	data, err := ti.service.ListReferenceData(asMember(t, ctx, ti), &gen.ListReferenceDataPayload{SessionToken: nil})
	require.NoError(t, err)
	require.NotEmpty(t, data.Providers)
}

func TestGetOnboarding_StackStageBeforeAnswers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	state, err := ti.service.GetOnboarding(ctx, &gen.GetOnboardingPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "stack", state.Stage)
	require.Nil(t, state.Answers)
	require.Nil(t, state.NextStep)
	require.Empty(t, state.Steps)
	require.False(t, state.Done)
}

func TestSaveStack_MovesToUseCaseStageAndAudits(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationOnboardingAnswersUpdated)
	require.NoError(t, err)

	state, err := ti.service.SaveStack(ctx, cursorProStack())
	require.NoError(t, err)
	require.Equal(t, "use-case", state.Stage)
	require.NotNil(t, state.Answers)
	require.Nil(t, state.Answers.UseCase)
	require.Len(t, state.Answers.Providers, 1)
	require.Equal(t, onboarding.PlanCursorPro, conv.PtrValOr(state.Answers.Providers[0].PlanSlug, ""))
	require.Equal(t, []string{onboarding.ProductCursor}, state.Answers.ProductSlugs)
	require.Nil(t, state.NextStep)
	require.Empty(t, state.Steps)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationOnboardingAnswersUpdated)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestSelectUseCase_ComputesNextStep(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.SaveStack(ctx, cursorProStack())
	require.NoError(t, err)
	state, err := ti.service.SelectUseCase(ctx, useCase(onboarding.UseCaseObservability))
	require.NoError(t, err)
	require.Equal(t, "steps", state.Stage)
	require.Equal(t, string(onboarding.UseCaseObservability), conv.PtrValOr(state.Answers.UseCase, ""))
	require.NotNil(t, state.NextStep)
	require.Equal(t, onboarding.TechniquePluginDistribution+":"+onboarding.ProductCursor, state.NextStep.Slug)
	require.Equal(t, "plugins", state.NextStep.Destination)
	require.False(t, state.Done)

	reloaded, err := ti.service.GetOnboarding(ctx, &gen.GetOnboardingPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, state.NextStep.Slug, reloaded.NextStep.Slug)
}

func TestSelectUseCase_NeedsAStack(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.SelectUseCase(ctx, useCase(onboarding.UseCaseObservability))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSaveStack_RejectsMembers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.SaveStack(asMember(t, ctx, ti), cursorProStack())
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.SelectUseCase(asMember(t, ctx, ti), useCase(onboarding.UseCaseObservability))
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSaveStack_RejectsBadProvidersPlansAndProducts(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	payload := cursorProStack()
	payload.Providers[0].ProviderSlug = "nope"
	_, err := ti.service.SaveStack(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)

	payload = cursorProStack()
	payload.Providers[0].PlanSlug = conv.PtrEmpty(onboarding.PlanAnthropicPro)
	_, err = ti.service.SaveStack(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)

	payload = cursorProStack()
	payload.Providers[0].PlanSlug = nil
	_, err = ti.service.SaveStack(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)

	payload = cursorProStack()
	payload.Providers = []*gen.OnboardingSelectedProvider{{ProviderSlug: onboarding.ProviderOpenCode, PlanSlug: conv.PtrEmpty("anything")}}
	_, err = ti.service.SaveStack(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)

	// A product whose provider was not selected.
	payload = cursorProStack()
	payload.ProductSlugs = []string{onboarding.ProductCodex}
	_, err = ti.service.SaveStack(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)

	payload = cursorProStack()
	payload.ProductSlugs = nil
	_, err = ti.service.SaveStack(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)

	payload = cursorProStack()
	payload.Providers = nil
	_, err = ti.service.SaveStack(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSaveStack_PlanlessProviderNeedsNoPlan(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	state, err := ti.service.SaveStack(ctx, &gen.SaveStackPayload{
		SessionToken: nil,
		Providers:    []*gen.OnboardingSelectedProvider{{ProviderSlug: onboarding.ProviderOpenCode, PlanSlug: nil}},
		ProductSlugs: []string{onboarding.ProductOpenCode},
		MdmVendor:    string(onboarding.MDMNone),
	})
	require.NoError(t, err)
	require.Nil(t, state.Answers.Providers[0].PlanSlug)
	state, err = ti.service.SelectUseCase(ctx, useCase(onboarding.UseCaseObservability))
	require.NoError(t, err)
	require.Equal(t, onboarding.TechniquePluginDistribution+":"+onboarding.ProductOpenCode, state.NextStep.Slug)
}

func TestVerifyStep_PassesAndCompletesWhenUseCaseCovered(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.SaveStack(ctx, cursorProStack())
	require.NoError(t, err)
	state, err := ti.service.SelectUseCase(ctx, useCase(onboarding.UseCaseObservability))
	require.NoError(t, err)
	stepSlug := state.NextStep.Slug

	result, err := ti.service.VerifyStep(ctx, &gen.VerifyStepPayload{SessionToken: nil, StepSlug: stepSlug})
	require.NoError(t, err)
	require.False(t, result.Verified)
	require.Contains(t, result.Evidence, "No hook event")
	require.Equal(t, stepSlug, result.State.NextStep.Slug)
	require.Equal(t, "steps", result.State.Stage)

	ti.evidence.seeHookEvents("cursor", 3)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationOnboardingStepVerified)
	require.NoError(t, err)

	result, err = ti.service.VerifyStep(ctx, &gen.VerifyStepPayload{SessionToken: nil, StepSlug: stepSlug})
	require.NoError(t, err)
	require.True(t, result.Verified)
	require.Contains(t, result.Evidence, "3 hook events")
	require.True(t, result.State.Done)
	require.Equal(t, "done", result.State.Stage)
	require.Nil(t, result.State.NextStep)
	require.NotNil(t, result.State.Answers.CompletedAt)
	require.Len(t, result.State.Steps, 1)
	require.NotNil(t, result.State.Steps[0].VerifiedAt)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationOnboardingStepVerified)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestVerifyStep_MovesToNextStepUntilCovered(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.SaveStack(ctx, cursorProStack())
	require.NoError(t, err)
	state, err := ti.service.SelectUseCase(ctx, useCase(onboarding.UseCaseMCPGateway))
	require.NoError(t, err)
	require.Equal(t, onboarding.TechniqueMCPGateway, state.NextStep.Slug)

	result, err := ti.service.VerifyStep(ctx, &gen.VerifyStepPayload{SessionToken: nil, StepSlug: onboarding.TechniqueMCPGateway})
	require.NoError(t, err)
	require.False(t, result.Verified)

	ti.evidence.gatewayTraffic = 1
	result, err = ti.service.VerifyStep(ctx, &gen.VerifyStepPayload{SessionToken: nil, StepSlug: onboarding.TechniqueMCPGateway})
	require.NoError(t, err)
	require.True(t, result.Verified)
	// Gateway traffic covers the MCP gateway use case on its own.
	require.True(t, result.State.Done)
}

func TestVerifyStep_RejectsStepsOutsideThePlan(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.VerifyStep(ctx, &gen.VerifyStepPayload{SessionToken: nil, StepSlug: onboarding.TechniqueMCPGateway})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.SaveStack(ctx, cursorProStack())
	require.NoError(t, err)
	// A stack without a use case has no plan yet.
	_, err = ti.service.VerifyStep(ctx, &gen.VerifyStepPayload{SessionToken: nil, StepSlug: onboarding.TechniqueMCPGateway})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.SelectUseCase(ctx, useCase(onboarding.UseCaseObservability))
	require.NoError(t, err)
	_, err = ti.service.VerifyStep(ctx, &gen.VerifyStepPayload{SessionToken: nil, StepSlug: onboarding.TechniqueMCPGateway})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestVerifyStep_RejectsMembers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.SaveStack(ctx, cursorProStack())
	require.NoError(t, err)
	_, err = ti.service.SelectUseCase(ctx, useCase(onboarding.UseCaseObservability))
	require.NoError(t, err)
	_, err = ti.service.VerifyStep(asMember(t, ctx, ti), &gen.VerifyStepPayload{SessionToken: nil, StepSlug: onboarding.TechniquePluginDistribution + ":" + onboarding.ProductCursor})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSelectUseCase_ChangingUseCaseRestartsCompletion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.SaveStack(ctx, cursorProStack())
	require.NoError(t, err)
	_, err = ti.service.SelectUseCase(ctx, useCase(onboarding.UseCaseObservability))
	require.NoError(t, err)
	ti.evidence.seeHookEvents("cursor", 1)
	result, err := ti.service.VerifyStep(ctx, &gen.VerifyStepPayload{SessionToken: nil, StepSlug: onboarding.TechniquePluginDistribution + ":" + onboarding.ProductCursor})
	require.NoError(t, err)
	require.True(t, result.State.Done)

	state, err := ti.service.SelectUseCase(ctx, useCase(onboarding.UseCaseSecurity))
	require.NoError(t, err)
	require.False(t, state.Done)
	require.Equal(t, "steps", state.Stage)
	require.Nil(t, state.Answers.CompletedAt)
	require.Equal(t, onboarding.TechniqueRiskPolicies, state.NextStep.Slug)
	// The cursor plugin step stays verified: its evidence still holds.
	require.Len(t, state.Steps, 2)
	require.NotNil(t, state.Steps[1].VerifiedAt)
}

func TestSaveStack_KeepsTheUseCaseAndReplans(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.SaveStack(ctx, cursorProStack())
	require.NoError(t, err)
	_, err = ti.service.SelectUseCase(ctx, useCase(onboarding.UseCaseObservability))
	require.NoError(t, err)

	// Upgrading Cursor to Teams and adding Codex keeps the use case; the plan
	// now has two steps.
	stack := &gen.SaveStackPayload{
		SessionToken: nil,
		Providers: []*gen.OnboardingSelectedProvider{
			{ProviderSlug: onboarding.ProviderCursor, PlanSlug: conv.PtrEmpty(onboarding.PlanCursorTeams)},
			{ProviderSlug: onboarding.ProviderOpenAI, PlanSlug: conv.PtrEmpty(onboarding.PlanOpenAIBusiness)},
		},
		ProductSlugs: []string{onboarding.ProductCursor, onboarding.ProductCodex},
		MdmVendor:    string(onboarding.MDMNone),
	}
	state, err := ti.service.SaveStack(ctx, stack)
	require.NoError(t, err)
	require.Equal(t, "steps", state.Stage)
	require.Equal(t, string(onboarding.UseCaseObservability), conv.PtrValOr(state.Answers.UseCase, ""))
	require.Len(t, state.Steps, 2)
}

func TestGetUseCaseStatus_ReportsEveryUseCase(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	result, err := ti.service.GetUseCaseStatus(asMember(t, ctx, ti), &gen.GetUseCaseStatusPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, result.Statuses, len(onboarding.UseCases))
	for _, status := range result.Statuses {
		require.False(t, status.Verified, status.UseCase)
		require.False(t, status.Selected, status.UseCase)
		require.Nil(t, status.NextStep, status.UseCase)
	}

	_, err = ti.service.SaveStack(ctx, cursorProStack())
	require.NoError(t, err)
	_, err = ti.service.SelectUseCase(ctx, useCase(onboarding.UseCaseCostTracking))
	require.NoError(t, err)
	ti.evidence.gatewayTraffic = 2

	result, err = ti.service.GetUseCaseStatus(ctx, &gen.GetUseCaseStatusPayload{SessionToken: nil})
	require.NoError(t, err)
	byUseCase := map[string]*gen.OnboardingUseCaseStatus{}
	for _, status := range result.Statuses {
		byUseCase[status.UseCase] = status
	}
	require.True(t, byUseCase[string(onboarding.UseCaseMCPGateway)].Verified)
	require.False(t, byUseCase[string(onboarding.UseCaseMCPGateway)].Selected)
	cost := byUseCase[string(onboarding.UseCaseCostTracking)]
	require.True(t, cost.Selected)
	require.False(t, cost.Verified)
	require.NotNil(t, cost.NextStep)
	require.Equal(t, onboarding.TechniquePluginDistribution+":"+onboarding.ProductCursor, cost.NextStep.Slug)
}
