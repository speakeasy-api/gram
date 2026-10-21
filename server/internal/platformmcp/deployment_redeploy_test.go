package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gendeployments "github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

type redeployManagementFake struct {
	source           *gendeployments.GetDeploymentResult
	committed        *gendeployments.GetDeploymentResult
	getErr           error
	processErr       error
	prepareCalls     int
	processCalls     int
	getCalls         int
	dispatchedActive uuid.UUID
}

func (f *redeployManagementFake) GetDeployment(_ context.Context, payload *gendeployments.GetDeploymentPayload) (*gendeployments.GetDeploymentResult, error) {
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.source != nil && payload.ID == f.source.ID {
		return f.source, nil
	}
	return f.committed, nil
}

func (f *redeployManagementFake) PrepareRedeploy(_ context.Context, _ pgx.Tx, payload *gendeployments.RedeployPayload) (*types.Deployment, error) {
	f.prepareCalls++
	clone := uuid.NewString()
	f.committed = &gendeployments.GetDeploymentResult{ID: clone, OrganizationID: f.source.OrganizationID, ProjectID: f.source.ProjectID, Status: "created", ClonedFrom: &payload.DeploymentID}
	return &types.Deployment{ID: clone, OrganizationID: f.source.OrganizationID, ProjectID: f.source.ProjectID, Status: "created", ClonedFrom: &payload.DeploymentID}, nil
}

func (f *redeployManagementFake) DispatchRedeploy(_ context.Context, _ string, expectedActive uuid.UUID) (*gendeployments.RedeployResult, error) {
	f.dispatchedActive = expectedActive
	f.processCalls++
	return nil, f.processErr
}

type redeployReceiptFake struct {
	receipt *OperationReceipt
	calls   int
}

func (f *redeployReceiptFake) Execute(ctx context.Context, _ Principal, _ ResolvedProject, _ RedeployDeploymentInput, charge func(context.Context) error, mutate deploymentRedeployTransaction) (OperationReceipt, error) {
	f.calls++
	if f.receipt != nil {
		replayed := *f.receipt
		replayed.Replayed = true
		return replayed, nil
	}
	if err := charge(ctx); err != nil {
		return OperationReceipt{}, err
	}
	result, err := mutate(ctx, nil)
	if err != nil {
		return OperationReceipt{}, err
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("marshal fake redeploy receipt: %w", err)
	}
	receipt := OperationReceipt{ResultPayload: payload}
	f.receipt = &receipt
	return receipt, nil
}

func newRedeployServiceFixture(t *testing.T) (*DeploymentRedeployService, *redeployManagementFake, *redeployReceiptFake, Principal, ResolvedProject, context.Context, string) {
	t.Helper()
	project := ResolvedProject{ID: uuid.New(), Name: "Test", Slug: "test"}
	principal := Principal{UserID: "user", OrganizationID: "org", ConnectionID: uuid.NewString(), Generation: uuid.NewString()}
	sourceID := uuid.NewString()
	management := &redeployManagementFake{source: &gendeployments.GetDeploymentResult{ID: sourceID, OrganizationID: principal.OrganizationID, ProjectID: project.ID.String(), Status: "completed"}}
	receipts := &redeployReceiptFake{}
	service := &DeploymentRedeployService{
		management:       management,
		resolveProject:   func(context.Context, Principal, FindMCPInput) (ResolvedProject, error) { return project, nil },
		organizations:    riskMutationOrganizationResolver{slug: "org-slug"},
		activeDeployment: func(context.Context, pgx.Tx, uuid.UUID) (string, error) { return sourceID, nil },
		receipts:         receipts,
		budget:           allowBudget(),
	}
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{UserID: principal.UserID, ActiveOrganizationID: principal.OrganizationID})
	return service, management, receipts, principal, project, ctx, sourceID
}

func TestRedeployDeploymentPreviewDefaultsToActiveWithoutMutation(t *testing.T) {
	t.Parallel()
	service, management, receipts, principal, project, ctx, sourceID := newRedeployServiceFixture(t)

	_, err := service.Redeploy(ctx, principal, RedeployDeploymentInput{ProjectID: project.ID.String(), IdempotencyKey: "stable", Confirmed: false})
	var refusal *deploymentRedeployError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "confirmation_required", refusal.Code)
	require.Equal(t, sourceID, refusal.Preview.SourceDeploymentID)
	require.Equal(t, sourceID, refusal.Preview.ActiveDeploymentID)
	require.Equal(t, "whole_project", refusal.Preview.Scope)
	require.Zero(t, management.prepareCalls)
	require.Zero(t, receipts.calls)
}

func TestRedeployDeploymentConfirmationRequiresExactPreviewIDs(t *testing.T) {
	t.Parallel()
	service, management, receipts, principal, project, ctx, _ := newRedeployServiceFixture(t)

	_, err := service.Redeploy(ctx, principal, RedeployDeploymentInput{ProjectID: project.ID.String(), IdempotencyKey: "stable", Confirmed: true})
	var refusal *deploymentRedeployError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "invalid_request", refusal.Code)
	require.Zero(t, management.prepareCalls)
	require.Zero(t, receipts.calls)
}

func TestRedeployDeploymentRejectsActorAndProjectMismatch(t *testing.T) {
	t.Parallel()
	service, management, _, principal, project, ctx, sourceID := newRedeployServiceFixture(t)
	mismatched := principal
	mismatched.UserID = "another-user"

	_, err := service.Redeploy(ctx, mismatched, RedeployDeploymentInput{ProjectID: project.ID.String(), DeploymentID: sourceID, IdempotencyKey: "stable"})
	require.ErrorIs(t, err, ErrForbidden)
	require.Zero(t, management.getCalls)

	service.resolveProject = func(context.Context, Principal, FindMCPInput) (ResolvedProject, error) {
		return ResolvedProject{ID: uuid.New()}, nil
	}
	_, err = service.Redeploy(ctx, principal, RedeployDeploymentInput{ProjectID: project.ID.String(), DeploymentID: sourceID, IdempotencyKey: "stable"})
	require.ErrorIs(t, err, ErrForbidden)
	require.Zero(t, management.getCalls)

	service.resolveProject = func(context.Context, Principal, FindMCPInput) (ResolvedProject, error) {
		return ResolvedProject{}, ErrForbidden
	}
	_, err = service.Redeploy(ctx, principal, RedeployDeploymentInput{ProjectID: project.ID.String(), DeploymentID: sourceID, IdempotencyKey: "stable"})
	require.ErrorIs(t, err, ErrForbidden)
	require.Zero(t, management.getCalls)
}

func TestRedeployDeploymentRejectsCrossProjectSource(t *testing.T) {
	t.Parallel()
	service, management, _, principal, project, ctx, sourceID := newRedeployServiceFixture(t)
	management.source.ProjectID = uuid.NewString()

	_, err := service.Redeploy(ctx, principal, RedeployDeploymentInput{ProjectID: project.ID.String(), DeploymentID: sourceID, IdempotencyKey: "stable"})
	require.ErrorIs(t, err, ErrForbidden)
	require.Zero(t, management.prepareCalls)
}

func TestRedeployDeploymentStaleActiveGuardRunsInsideMutation(t *testing.T) {
	t.Parallel()
	service, management, receipts, principal, project, ctx, sourceID := newRedeployServiceFixture(t)
	activeChecks := 0
	service.activeDeployment = func(context.Context, pgx.Tx, uuid.UUID) (string, error) {
		activeChecks++
		return uuid.NewString(), nil
	}

	_, err := service.Redeploy(ctx, principal, RedeployDeploymentInput{ProjectID: project.ID.String(), DeploymentID: sourceID, ExpectedActiveDeploymentID: sourceID, IdempotencyKey: "stable", Confirmed: true})
	var refusal *deploymentRedeployError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "conflict", refusal.Code)
	require.Equal(t, 1, receipts.calls)
	require.Equal(t, 1, activeChecks, "the active snapshot is checked by the receipt mutation callback")
	require.Zero(t, management.prepareCalls)
}

func TestRedeployDeploymentReplayDoesNotCloneAgain(t *testing.T) {
	t.Parallel()
	service, management, _, principal, project, ctx, sourceID := newRedeployServiceFixture(t)
	input := RedeployDeploymentInput{ProjectID: project.ID.String(), DeploymentID: sourceID, ExpectedActiveDeploymentID: sourceID, IdempotencyKey: "stable", Confirmed: true}

	first, err := service.Redeploy(ctx, principal, input)
	require.NoError(t, err)
	second, err := service.Redeploy(ctx, principal, input)
	require.NoError(t, err)
	require.Equal(t, first.DeploymentID, second.DeploymentID)
	require.True(t, second.Replayed)
	require.Equal(t, 1, management.prepareCalls)
}

func TestRedeployDeploymentRateLimitedReplayDoesNotProcess(t *testing.T) {
	t.Parallel()
	service, management, _, principal, project, ctx, sourceID := newRedeployServiceFixture(t)
	input := RedeployDeploymentInput{ProjectID: project.ID.String(), DeploymentID: sourceID, ExpectedActiveDeploymentID: sourceID, IdempotencyKey: "stable", Confirmed: true}

	_, err := service.Redeploy(ctx, principal, input)
	require.NoError(t, err)
	management.processCalls = 0
	service.budget = denyBudget()
	output, err := service.Redeploy(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, output.Replayed)
	require.Equal(t, "rate_limited", output.Processing)
	require.Zero(t, management.processCalls)
	require.Equal(t, 1, management.prepareCalls)
}

func TestRedeployDeploymentProcessFailureReturnsCommittedClone(t *testing.T) {
	t.Parallel()
	service, management, _, principal, project, ctx, sourceID := newRedeployServiceFixture(t)
	management.processErr = errors.New("processor offline")

	output, err := service.Redeploy(ctx, principal, RedeployDeploymentInput{ProjectID: project.ID.String(), DeploymentID: sourceID, ExpectedActiveDeploymentID: sourceID, IdempotencyKey: "stable", Confirmed: true})
	require.NoError(t, err)
	require.NotEmpty(t, output.DeploymentID)
	require.True(t, output.Verified)
	require.Equal(t, "created", output.Status)
	require.Equal(t, "unavailable", output.Processing)
}

func TestRedeployDeploymentFailedStatusIsObservedWithoutProcessing(t *testing.T) {
	t.Parallel()
	service, management, receipts, principal, project, ctx, sourceID := newRedeployServiceFixture(t)
	cloneID := uuid.NewString()
	clonedFrom := sourceID
	management.committed = &gendeployments.GetDeploymentResult{ID: cloneID, OrganizationID: principal.OrganizationID, ProjectID: project.ID.String(), Status: "failed", ClonedFrom: &clonedFrom}
	payload, err := json.Marshal(deploymentRedeployReceipt{ProjectID: project.ID.String(), SourceDeploymentID: sourceID, DeploymentID: cloneID})
	require.NoError(t, err)
	receipts.receipt = &OperationReceipt{ResultPayload: payload}

	output, err := service.Redeploy(ctx, principal, RedeployDeploymentInput{ProjectID: project.ID.String(), DeploymentID: sourceID, ExpectedActiveDeploymentID: sourceID, IdempotencyKey: "stable", Confirmed: true})
	require.NoError(t, err)
	require.Equal(t, "failed", output.Status)
	require.True(t, output.Verified)
	require.Equal(t, "observed", output.Processing)
	require.Zero(t, management.processCalls)
}

func TestRedeployDeploymentPostVerificationFailureIsBounded(t *testing.T) {
	t.Parallel()
	service, management, _, principal, project, ctx, sourceID := newRedeployServiceFixture(t)
	_, err := service.Redeploy(ctx, principal, RedeployDeploymentInput{ProjectID: project.ID.String(), DeploymentID: sourceID, ExpectedActiveDeploymentID: sourceID, IdempotencyKey: "stable", Confirmed: true})
	require.NoError(t, err)
	management.committed.ProjectID = uuid.NewString()
	output, err := service.Redeploy(ctx, principal, RedeployDeploymentInput{ProjectID: project.ID.String(), DeploymentID: sourceID, ExpectedActiveDeploymentID: sourceID, IdempotencyKey: "stable", Confirmed: true})
	require.NoError(t, err)
	require.NotEmpty(t, output.DeploymentID)
	require.False(t, output.Verified)
	require.Equal(t, "unknown", output.Status)
	require.Equal(t, "verification_unavailable", output.Processing)
}

func TestRedeployDeploymentDispatchCarriesActivationGuard(t *testing.T) {
	t.Parallel()
	service, management, _, principal, project, ctx, sourceID := newRedeployServiceFixture(t)
	_, err := service.Redeploy(ctx, principal, RedeployDeploymentInput{ProjectID: project.ID.String(), DeploymentID: sourceID, ExpectedActiveDeploymentID: sourceID, IdempotencyKey: "guarded", Confirmed: true})
	require.NoError(t, err)
	require.Equal(t, uuid.MustParse(sourceID), management.dispatchedActive)
	require.Equal(t, 1, management.processCalls)
}

func TestRedeployDeploymentDispatchPreservesExpectedNoActiveSnapshot(t *testing.T) {
	t.Parallel()
	service, management, _, principal, project, ctx, sourceID := newRedeployServiceFixture(t)
	service.activeDeployment = func(context.Context, pgx.Tx, uuid.UUID) (string, error) { return noActiveDeployment, nil }
	_, err := service.Redeploy(ctx, principal, RedeployDeploymentInput{ProjectID: project.ID.String(), DeploymentID: sourceID, ExpectedActiveDeploymentID: noActiveDeployment, IdempotencyKey: "no-active", Confirmed: true})
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, management.dispatchedActive)
	require.Equal(t, 1, management.processCalls)
}
