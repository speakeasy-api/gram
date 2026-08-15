package activities_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type mockPaygChatKeyProvisioner struct {
	mock.Mock
}

func (m *mockPaygChatKeyProvisioner) ProvisionAPIKey(ctx context.Context, organizationID string, keyType openrouter.KeyType) (string, error) {
	args := m.Called(ctx, organizationID, keyType)
	return args.String(0), args.Error(1)
}

func (m *mockPaygChatKeyProvisioner) RefreshAPIKeyLimit(ctx context.Context, organizationID string, keyType openrouter.KeyType, limit *int) (int, error) {
	args := m.Called(ctx, organizationID, keyType, limit)
	return args.Int(0), args.Error(1)
}

func (m *mockPaygChatKeyProvisioner) DisableAPIKey(ctx context.Context, organizationID string, keyType openrouter.KeyType) error {
	return m.Called(ctx, organizationID, keyType).Error(0)
}

func (m *mockPaygChatKeyProvisioner) GetCreditsUsed(ctx context.Context, organizationID string, keyType openrouter.KeyType) (float64, int, error) {
	args := m.Called(ctx, organizationID, keyType)
	credits, ok := args.Get(0).(float64)
	if !ok {
		panic("mock credits used value is not a float64")
	}
	return credits, args.Int(1), args.Error(2)
}

func (m *mockPaygChatKeyProvisioner) GetKeyUsage(ctx context.Context, apiKey string) (float64, *int64, error) {
	args := m.Called(ctx, apiKey)
	used, ok := args.Get(0).(float64)
	if !ok {
		panic("mock key usage value is not a float64")
	}
	limit, _ := args.Get(1).(*int64)
	return used, limit, args.Error(2)
}

func (m *mockPaygChatKeyProvisioner) ReconcileMonthlyCredits(ctx context.Context, organizationID string, keyType openrouter.KeyType, currentLimit int64, upstreamLimit *int64) (int64, error) {
	args := m.Called(ctx, organizationID, keyType, currentLimit, upstreamLimit)
	credits, ok := args.Get(0).(int64)
	if !ok {
		panic("mock monthly credits value is not an int64")
	}
	return credits, args.Error(1)
}

func (m *mockPaygChatKeyProvisioner) GetModelUsage(ctx context.Context, generationID string, organizationID string, keyType openrouter.KeyType) (*openrouter.ModelUsage, error) {
	args := m.Called(ctx, generationID, organizationID, keyType)
	usage, _ := args.Get(0).(*openrouter.ModelUsage)
	return usage, args.Error(1)
}

func setupPaygChatKeyReconciler(t *testing.T, accountType string, subscriptionID pgtype.Text) (*activities.ReconcilePaygOpenRouterChatKey, *mockPaygChatKeyProvisioner, string) {
	t.Helper()

	db, err := infra.CloneTestDatabase(t, "payg_chat_key_"+uuid.NewString()[:8])
	require.NoError(t, err)

	organizationID := "org-" + uuid.NewString()[:8]
	_, err = organizationsrepo.New(db).UpsertOrganizationMetadata(t.Context(), organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Test Organization",
		Slug:        organizationID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{Bool: accountType == "payg", Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, activitiesrepo.New(db).SetPaygOpenRouterChatKeyProjectionFixture(t.Context(), activitiesrepo.SetPaygOpenRouterChatKeyProjectionFixtureParams{
		StripeSubscriptionID: subscriptionID,
		GramAccountType:      accountType,
		OrganizationID:       organizationID,
	}))

	provisioner := &mockPaygChatKeyProvisioner{Mock: mock.Mock{}}
	reconciler := activities.NewReconcilePaygOpenRouterChatKey(testenv.NewLogger(t), db, provisioner)
	return reconciler, provisioner, organizationID
}

func TestReconcilePaygOpenRouterChatKeyCurrentPaidStateEnablesChatOnly(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, organizationID := setupPaygChatKeyReconciler(t, "payg", pgtype.Text{String: "subscription_placeholder", Valid: true})
	provisioner.On("RefreshAPIKeyLimit", mock.Anything, organizationID, openrouter.KeyTypeChat, mock.MatchedBy(func(limit *int) bool {
		return limit != nil && *limit == 100
	})).Return(100, nil).Once()

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID}))
	provisioner.AssertNotCalled(t, "DisableAPIKey", mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyCurrentPaidStateWithoutKeyIsNoop(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, organizationID := setupPaygChatKeyReconciler(t, "payg", pgtype.Text{String: "subscription_placeholder", Valid: true})
	provisioner.On("RefreshAPIKeyLimit", mock.Anything, organizationID, openrouter.KeyTypeChat, mock.AnythingOfType("*int")).Return(0, pgx.ErrNoRows).Once()

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID}))
	provisioner.AssertNotCalled(t, "DisableAPIKey", mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyCurrentFreeStateDisablesChatOnly(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, organizationID := setupPaygChatKeyReconciler(t, "free", pgtype.Text{})
	provisioner.On("DisableAPIKey", mock.Anything, organizationID, openrouter.KeyTypeChat).Return(nil).Once()

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID}))
	provisioner.AssertNotCalled(t, "RefreshAPIKeyLimit", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyStaleActivationWakeupCannotOverrideCurrentFreeState(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, organizationID := setupPaygChatKeyReconciler(t, "free", pgtype.Text{})
	provisioner.On("DisableAPIKey", mock.Anything, organizationID, openrouter.KeyTypeChat).Return(nil).Twice()

	args := activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID}
	require.NoError(t, reconciler.Do(t.Context(), args), "newer deactivation wake-up")
	require.NoError(t, reconciler.Do(t.Context(), args), "stale activation delivered last")

	provisioner.AssertNotCalled(t, "RefreshAPIKeyLimit", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyStaleDeactivationWakeupCannotOverrideCurrentPaidState(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, organizationID := setupPaygChatKeyReconciler(t, "payg", pgtype.Text{String: "subscription_placeholder", Valid: true})
	provisioner.On("RefreshAPIKeyLimit", mock.Anything, organizationID, openrouter.KeyTypeChat, mock.AnythingOfType("*int")).Return(100, nil).Twice()

	args := activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID}
	require.NoError(t, reconciler.Do(t.Context(), args), "newer activation wake-up")
	require.NoError(t, reconciler.Do(t.Context(), args), "stale deactivation delivered last")

	provisioner.AssertNotCalled(t, "DisableAPIKey", mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyExternalFailureIsRetryableAndIdempotent(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, organizationID := setupPaygChatKeyReconciler(t, "free", pgtype.Text{})
	provisioner.On("DisableAPIKey", mock.Anything, organizationID, openrouter.KeyTypeChat).Return(errors.New("upstream unavailable")).Once()
	provisioner.On("DisableAPIKey", mock.Anything, organizationID, openrouter.KeyTypeChat).Return(nil).Once()

	args := activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID}
	require.ErrorContains(t, reconciler.Do(t.Context(), args), "disable PAYG OpenRouter chat key")
	require.NoError(t, reconciler.Do(t.Context(), args))
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyRejectsMixedProjection(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, organizationID := setupPaygChatKeyReconciler(t, "payg", pgtype.Text{})

	err := reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID})
	require.ErrorContains(t, err, "inconsistent PAYG chat key billing projection")
	provisioner.AssertNotCalled(t, "RefreshAPIKeyLimit", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertNotCalled(t, "DisableAPIKey", mock.Anything, mock.Anything, mock.Anything)
}
