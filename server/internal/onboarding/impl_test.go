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

func cursorProAnswers(useCase onboarding.UseCase) *gen.SaveAnswersPayload {
	return &gen.SaveAnswersPayload{
		SessionToken: nil,
		Products:     []*gen.OnboardingSelectedProduct{{ProductSlug: onboarding.ProductCursor, PlanSlug: conv.PtrEmpty(onboarding.PlanCursorPro)}},
		MdmVendor:    string(onboarding.MDMNone),
		UseCase:      string(useCase),
	}
}

func TestListReferenceData_ReturnsCatalog(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	data, err := ti.service.ListReferenceData(ctx, &gen.ListReferenceDataPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, data.Products, len(onboarding.Default.Products))
	require.Len(t, data.UseCases, len(onboarding.UseCases))
	require.Len(t, data.MdmVendors, len(onboarding.MDMVendors))

	for _, product := range data.Products {
		spec, ok := onboarding.Default.Product(product.Slug)
		require.True(t, ok, product.Slug)
		require.Len(t, product.Plans, len(spec.Plans), product.Slug)
		require.Equal(t, spec.SourceIDs, product.SourceIds)
	}
}

func TestListReferenceData_MembersCanRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	data, err := ti.service.ListReferenceData(asMember(t, ctx, ti), &gen.ListReferenceDataPayload{SessionToken: nil})
	require.NoError(t, err)
	require.NotEmpty(t, data.Products)
}

func TestGetOnboarding_EmptyBeforeAnswers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	state, err := ti.service.GetOnboarding(ctx, &gen.GetOnboardingPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Nil(t, state.Answers)
	require.Nil(t, state.NextStep)
	require.Empty(t, state.Steps)
	require.False(t, state.Done)
}

func TestSaveAnswers_ComputesNextStepAndAudits(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationOnboardingAnswersUpdated)
	require.NoError(t, err)

	state, err := ti.service.SaveAnswers(ctx, cursorProAnswers(onboarding.UseCaseObservability))
	require.NoError(t, err)
	require.NotNil(t, state.Answers)
	require.Equal(t, string(onboarding.UseCaseObservability), state.Answers.UseCase)
	require.Len(t, state.Answers.Products, 1)
	require.Equal(t, onboarding.PlanCursorPro, conv.PtrValOr(state.Answers.Products[0].PlanSlug, ""))
	require.NotNil(t, state.NextStep)
	require.Equal(t, onboarding.TechniquePluginDistribution+":"+onboarding.ProductCursor, state.NextStep.Slug)
	require.Equal(t, "plugins", state.NextStep.Destination)
	require.False(t, state.Done)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationOnboardingAnswersUpdated)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	reloaded, err := ti.service.GetOnboarding(ctx, &gen.GetOnboardingPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, state.NextStep.Slug, reloaded.NextStep.Slug)
}

func TestSaveAnswers_RejectsMembers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.SaveAnswers(asMember(t, ctx, ti), cursorProAnswers(onboarding.UseCaseObservability))
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSaveAnswers_RejectsUnknownProductAndWrongPlan(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	payload := cursorProAnswers(onboarding.UseCaseObservability)
	payload.Products[0].ProductSlug = "nope"
	_, err := ti.service.SaveAnswers(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)

	payload = cursorProAnswers(onboarding.UseCaseObservability)
	payload.Products[0].PlanSlug = conv.PtrEmpty(onboarding.PlanAnthropicPro)
	_, err = ti.service.SaveAnswers(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)

	payload = cursorProAnswers(onboarding.UseCaseObservability)
	payload.Products[0].PlanSlug = nil
	_, err = ti.service.SaveAnswers(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)

	payload = cursorProAnswers(onboarding.UseCaseObservability)
	payload.Products = []*gen.OnboardingSelectedProduct{{ProductSlug: onboarding.ProductOpenCode, PlanSlug: conv.PtrEmpty("anything")}}
	_, err = ti.service.SaveAnswers(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)

	payload = cursorProAnswers(onboarding.UseCaseObservability)
	payload.Products = nil
	_, err = ti.service.SaveAnswers(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSaveAnswers_PlanlessProductNeedsNoPlan(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	state, err := ti.service.SaveAnswers(ctx, &gen.SaveAnswersPayload{
		SessionToken: nil,
		Products:     []*gen.OnboardingSelectedProduct{{ProductSlug: onboarding.ProductOpenCode, PlanSlug: nil}},
		MdmVendor:    string(onboarding.MDMNone),
		UseCase:      string(onboarding.UseCaseObservability),
	})
	require.NoError(t, err)
	require.Nil(t, state.Answers.Products[0].PlanSlug)
	require.Equal(t, onboarding.TechniquePluginDistribution+":"+onboarding.ProductOpenCode, state.NextStep.Slug)
}

func TestVerifyStep_PassesAndCompletesWhenUseCaseCovered(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	state, err := ti.service.SaveAnswers(ctx, cursorProAnswers(onboarding.UseCaseObservability))
	require.NoError(t, err)
	stepSlug := state.NextStep.Slug

	result, err := ti.service.VerifyStep(ctx, &gen.VerifyStepPayload{SessionToken: nil, StepSlug: stepSlug})
	require.NoError(t, err)
	require.False(t, result.Verified)
	require.Contains(t, result.Evidence, "No hook event")
	require.Equal(t, stepSlug, result.State.NextStep.Slug)
	require.False(t, result.State.Done)

	ti.evidence.seeHookEvents("cursor", 3)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationOnboardingStepVerified)
	require.NoError(t, err)

	result, err = ti.service.VerifyStep(ctx, &gen.VerifyStepPayload{SessionToken: nil, StepSlug: stepSlug})
	require.NoError(t, err)
	require.True(t, result.Verified)
	require.Contains(t, result.Evidence, "3 hook events")
	require.True(t, result.State.Done)
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

	state, err := ti.service.SaveAnswers(ctx, cursorProAnswers(onboarding.UseCaseMCPGateway))
	require.NoError(t, err)
	require.Equal(t, onboarding.TechniqueMCPGateway, state.NextStep.Slug)

	// A plugin assignment counts for both the distribution step and the use
	// case, but the gateway step itself still needs traffic.
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

	_, err = ti.service.SaveAnswers(ctx, cursorProAnswers(onboarding.UseCaseObservability))
	require.NoError(t, err)
	_, err = ti.service.VerifyStep(ctx, &gen.VerifyStepPayload{SessionToken: nil, StepSlug: onboarding.TechniqueMCPGateway})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestVerifyStep_RejectsMembers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.SaveAnswers(ctx, cursorProAnswers(onboarding.UseCaseObservability))
	require.NoError(t, err)
	_, err = ti.service.VerifyStep(asMember(t, ctx, ti), &gen.VerifyStepPayload{SessionToken: nil, StepSlug: onboarding.TechniquePluginDistribution + ":" + onboarding.ProductCursor})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSaveAnswers_ChangingUseCaseRestartsCompletion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.SaveAnswers(ctx, cursorProAnswers(onboarding.UseCaseObservability))
	require.NoError(t, err)
	ti.evidence.seeHookEvents("cursor", 1)
	result, err := ti.service.VerifyStep(ctx, &gen.VerifyStepPayload{SessionToken: nil, StepSlug: onboarding.TechniquePluginDistribution + ":" + onboarding.ProductCursor})
	require.NoError(t, err)
	require.True(t, result.State.Done)

	state, err := ti.service.SaveAnswers(ctx, cursorProAnswers(onboarding.UseCaseSecurity))
	require.NoError(t, err)
	require.False(t, state.Done)
	require.Nil(t, state.Answers.CompletedAt)
	require.Equal(t, onboarding.TechniqueRiskPolicies, state.NextStep.Slug)
	// The cursor plugin step stays verified: its evidence still holds.
	require.Len(t, state.Steps, 2)
	require.NotNil(t, state.Steps[1].VerifiedAt)
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

	_, err = ti.service.SaveAnswers(ctx, cursorProAnswers(onboarding.UseCaseCostTracking))
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
