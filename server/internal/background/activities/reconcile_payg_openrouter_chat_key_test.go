package activities_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type paygCauseCall struct {
	organizationID string
	keyType        openrouter.KeyType
	cause          openrouter.DisableCause
}

type mockPaygChatKeyProvisioner struct {
	mock.Mock
	addedCauses   []paygCauseCall
	removedCauses []paygCauseCall
	layeredAdd    bool
}

func (m *mockPaygChatKeyProvisioner) ProvisionAPIKey(ctx context.Context, organizationID string, keyType openrouter.KeyType) (string, error) {
	args := m.Called(ctx, organizationID, keyType)
	return args.String(0), args.Error(1)
}

func (m *mockPaygChatKeyProvisioner) RefreshAPIKeyLimit(ctx context.Context, organizationID string, keyType openrouter.KeyType, limit *int) (int, error) {
	args := m.Called(ctx, organizationID, keyType, limit)
	return args.Int(0), args.Error(1)
}

func (m *mockPaygChatKeyProvisioner) RefreshAPIKeyLimitWithDB(ctx context.Context, _ openrouter.DBTX, organizationID string, keyType openrouter.KeyType, limit *int) (int, error) {
	return m.RefreshAPIKeyLimit(ctx, organizationID, keyType, limit)
}

func (*mockPaygChatKeyProvisioner) AddAPIKeyDisableCause(context.Context, string, openrouter.KeyType, openrouter.DisableCause) (openrouter.DisableCauseChange, error) {
	return openrouter.DisableCauseChange{}, nil
}

func (m *mockPaygChatKeyProvisioner) AddAPIKeyDisableCauseWithDB(ctx context.Context, _ openrouter.DBTX, organizationID string, keyType openrouter.KeyType, cause openrouter.DisableCause) (openrouter.DisableCauseChange, error) {
	m.addedCauses = append(m.addedCauses, paygCauseCall{organizationID: organizationID, keyType: keyType, cause: cause})
	if m.layeredAdd {
		return openrouter.DisableCauseChange{CauseChanged: true, KeyAccessChanged: false}, nil
	}
	err := m.Called(ctx, organizationID, keyType).Error(0)
	return openrouter.DisableCauseChange{CauseChanged: err == nil, KeyAccessChanged: err == nil}, err
}

func (*mockPaygChatKeyProvisioner) RemoveAPIKeyDisableCause(context.Context, string, openrouter.KeyType, openrouter.DisableCause, *int) (int, openrouter.DisableCauseChange, error) {
	return 0, openrouter.DisableCauseChange{}, nil
}

func (m *mockPaygChatKeyProvisioner) RemoveAPIKeyDisableCauseWithDB(ctx context.Context, db openrouter.DBTX, organizationID string, keyType openrouter.KeyType, cause openrouter.DisableCause, limit *int) (int, openrouter.DisableCauseChange, error) {
	m.removedCauses = append(m.removedCauses, paygCauseCall{organizationID: organizationID, keyType: keyType, cause: cause})
	key, err := openrouterrepo.New(db).GetOpenRouterAPIKey(ctx, openrouterrepo.GetOpenRouterAPIKeyParams{OrganizationID: organizationID, KeyType: string(keyType)})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, openrouter.DisableCauseChange{}, nil
	}
	if err != nil {
		return 0, openrouter.DisableCauseChange{}, err
	}
	if limit != nil && key.MonthlyCredits == int64(*limit) && !slices.Contains(key.DisableCauses, string(cause)) {
		return *limit, openrouter.DisableCauseChange{}, nil
	}
	if cause == openrouter.DisableCauseTrialDemotion {
		if limit == nil {
			return 0, openrouter.DisableCauseChange{CauseChanged: true}, nil
		}
		return *limit, openrouter.DisableCauseChange{CauseChanged: true}, nil
	}
	refreshed, err := m.RefreshAPIKeyLimit(ctx, organizationID, keyType, limit)
	return refreshed, openrouter.DisableCauseChange{CauseChanged: err == nil, KeyAccessChanged: err == nil}, err
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

func (m *mockPaygChatKeyProvisioner) ReconcileMonthlyCredits(ctx context.Context, organizationID string, keyType openrouter.KeyType, currentLimit int64, currentGeneration int64, upstreamLimit *int64) (int64, error) {
	args := m.Called(ctx, organizationID, keyType, currentLimit, currentGeneration, upstreamLimit)
	credits, ok := args.Get(0).(int64)
	if !ok {
		panic("mock monthly credits value is not an int64")
	}
	return credits, args.Error(1)
}

func (m *mockPaygChatKeyProvisioner) ReconcileMonthlyCreditsWithDB(ctx context.Context, _ openrouter.DBTX, organizationID string, keyType openrouter.KeyType, currentLimit int64, currentGeneration int64, upstreamLimit *int64) (int64, error) {
	return m.ReconcileMonthlyCredits(ctx, organizationID, keyType, currentLimit, currentGeneration, upstreamLimit)
}

func (m *mockPaygChatKeyProvisioner) GetModelUsage(ctx context.Context, generationID string, organizationID string, keyType openrouter.KeyType) (*openrouter.ModelUsage, error) {
	args := m.Called(ctx, generationID, organizationID, keyType)
	usage, _ := args.Get(0).(*openrouter.ModelUsage)
	return usage, args.Error(1)
}

func setupPaygChatKeyReconciler(t *testing.T, accountType string, subscriptionID pgtype.Text) (*activities.ReconcilePaygOpenRouterChatKey, *mockPaygChatKeyProvisioner, *pgxpool.Pool, string) {
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
	return reconciler, provisioner, db, organizationID
}

func createPaygReconcilerKey(t *testing.T, db openrouterrepo.DBTX, organizationID string, keyType openrouter.KeyType, monthlyCredits int64) {
	t.Helper()
	_, err := openrouterrepo.New(db).CreateOpenRouterAPIKey(t.Context(), openrouterrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(keyType),
		KeyEncrypted:   pgtype.Text{},
		KeyHash:        "hash_placeholder_" + string(keyType),
		MonthlyCredits: monthlyCredits,
	})
	require.NoError(t, err)
}

func TestReconcilePaygOpenRouterChatKeyCurrentPaidStateEnablesOtherInferenceWhenSecurityIsAbsent(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, db, organizationID := setupPaygChatKeyReconciler(t, "payg", pgtype.Text{String: "subscription_placeholder", Valid: true})
	createPaygReconcilerKey(t, db, organizationID, openrouter.KeyTypeChat, 50)
	provisioner.On("RefreshAPIKeyLimit", mock.Anything, organizationID, openrouter.KeyTypeChat, mock.MatchedBy(func(limit *int) bool {
		return limit != nil && *limit == 100
	})).Return(100, nil).Once()

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateEnabled}))
	provisioner.AssertNotCalled(t, "AddAPIKeyDisableCauseWithDB", mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyCurrentPaidStateLeavesEnabledSecurityUntouched(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, db, organizationID := setupPaygChatKeyReconciler(t, "payg", pgtype.Text{String: "subscription_placeholder", Valid: true})
	createPaygReconcilerKey(t, db, organizationID, openrouter.KeyTypeChat, 50)
	_, err := openrouterrepo.New(db).CreateOpenRouterAPIKey(t.Context(), openrouterrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeInternal),
		KeyEncrypted:   pgtype.Text{},
		KeyHash:        "hash_placeholder_internal",
		MonthlyCredits: 37,
	})
	require.NoError(t, err)
	provisioner.On("RefreshAPIKeyLimit", mock.Anything, organizationID, openrouter.KeyTypeChat, mock.AnythingOfType("*int")).Return(100, nil).Once()

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateEnabled}))
	provisioner.AssertNotCalled(t, "RefreshAPIKeyLimit", mock.Anything, organizationID, openrouter.KeyTypeInternal, mock.Anything)
	key, err := openrouterrepo.New(db).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeInternal),
	})
	require.NoError(t, err)
	require.EqualValues(t, 37, key.MonthlyCredits)
	require.False(t, key.Disabled)
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyActivationRetryPreservesSelectedOtherCap(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, db, organizationID := setupPaygChatKeyReconciler(t, "payg", pgtype.Text{String: "subscription_placeholder", Valid: true})
	createPaygReconcilerKey(t, db, organizationID, openrouter.KeyTypeChat, 75)
	require.NoError(t, audit.NewLogger().LogOpenRouterAPIKeySetSpendCap(t.Context(), db, audit.LogOpenRouterAPIKeySetSpendCapEvent{
		OrganizationID:      organizationID,
		Actor:               urn.NewPrincipal(urn.PrincipalTypeUser, "user_placeholder"),
		ActorDisplayName:    nil,
		ActorSlug:           nil,
		OpenRouterAPIKeyURN: urn.NewOpenRouterAPIKey(organizationID, string(openrouter.KeyTypeChat)),
		KeyType:             string(openrouter.KeyTypeChat),
		OperationIdentifier: "operation_other_placeholder",
		OpenRouterAPIKeySnapshotBefore: &audit.OpenRouterAPIKeySpendCapSnapshot{
			MonthlyCredits: 100,
		},
		OpenRouterAPIKeySnapshotAfter: &audit.OpenRouterAPIKeySpendCapSnapshot{
			MonthlyCredits: 75,
		},
	}))

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateEnabled}))
	provisioner.AssertNotCalled(t, "RefreshAPIKeyLimit", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestReconcilePaygOpenRouterChatKeyOrdinaryRecoveryPreservesAdminLockedSecurityAtRecordedCap(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, db, organizationID := setupPaygChatKeyReconciler(t, "payg", pgtype.Text{String: "subscription_placeholder", Valid: true})
	createPaygReconcilerKey(t, db, organizationID, openrouter.KeyTypeChat, 50)
	_, err := openrouterrepo.New(db).CreateOpenRouterAPIKey(t.Context(), openrouterrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeInternal),
		KeyEncrypted:   pgtype.Text{},
		KeyHash:        "hash_placeholder_internal",
		MonthlyCredits: 37,
	})
	require.NoError(t, err)
	require.NoError(t, openrouterrepo.New(db).DisableOpenRouterAPIKey(t.Context(), openrouterrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeInternal),
	}))
	require.NoError(t, audit.NewLogger().LogOpenRouterAPIKeySetSpendCap(t.Context(), db, audit.LogOpenRouterAPIKeySetSpendCapEvent{
		OrganizationID:      organizationID,
		Actor:               urn.NewPrincipal(urn.PrincipalTypeUser, "user_placeholder"),
		ActorDisplayName:    nil,
		ActorSlug:           nil,
		OpenRouterAPIKeyURN: urn.NewOpenRouterAPIKey(organizationID, string(openrouter.KeyTypeInternal)),
		KeyType:             string(openrouter.KeyTypeInternal),
		OperationIdentifier: "operation_placeholder",
		OpenRouterAPIKeySnapshotBefore: &audit.OpenRouterAPIKeySpendCapSnapshot{
			MonthlyCredits: 50,
		},
		OpenRouterAPIKeySnapshotAfter: &audit.OpenRouterAPIKeySpendCapSnapshot{
			MonthlyCredits: 37,
		},
	}))
	provisioner.On("RefreshAPIKeyLimit", mock.Anything, organizationID, openrouter.KeyTypeChat, mock.AnythingOfType("*int")).Return(100, nil).Once()

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateEnabled}))
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyOrdinaryRecoveryPreservesUnconvertedTrialSecurity(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, db, organizationID := setupPaygChatKeyReconciler(t, "payg", pgtype.Text{String: "subscription_placeholder", Valid: true})
	createPaygReconcilerKey(t, db, organizationID, openrouter.KeyTypeChat, 50)
	_, err := openrouterrepo.New(db).CreateOpenRouterAPIKey(t.Context(), openrouterrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeInternal),
		KeyEncrypted:   pgtype.Text{},
		KeyHash:        "hash_placeholder_internal",
		MonthlyCredits: 50,
	})
	require.NoError(t, err)
	require.NoError(t, openrouterrepo.New(db).DisableOpenRouterAPIKey(t.Context(), openrouterrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeInternal),
	}))
	provisioner.On("RefreshAPIKeyLimit", mock.Anything, organizationID, openrouter.KeyTypeChat, mock.AnythingOfType("*int")).Return(100, nil).Once()

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateEnabled}))
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyStripeConversionRemovesTrialDemotionFromBothKeys(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, db, organizationID := setupPaygChatKeyReconciler(t, "payg", pgtype.Text{String: "subscription_placeholder", Valid: true})
	createPaygReconcilerKey(t, db, organizationID, openrouter.KeyTypeChat, 50)
	createPaygReconcilerKey(t, db, organizationID, openrouter.KeyTypeInternal, 37)
	_, err := db.Exec(t.Context(), `INSERT INTO trials (organization_id, tier, ends_at, demoted_at, converted_at) VALUES ($1, 'enterprise', clock_timestamp() - interval '2 days', clock_timestamp() - interval '1 day', clock_timestamp())`, organizationID)
	require.NoError(t, err)
	for _, keyType := range openrouter.AllKeyTypes {
		_, err = openrouterrepo.New(db).AddOpenRouterAPIKeyDisableCause(t.Context(), openrouterrepo.AddOpenRouterAPIKeyDisableCauseParams{OrganizationID: organizationID, KeyType: string(keyType), DisableCause: string(openrouter.DisableCauseTrialDemotion)})
		require.NoError(t, err)
	}
	provisioner.On("RefreshAPIKeyLimit", mock.Anything, organizationID, openrouter.KeyTypeChat, mock.MatchedBy(func(limit *int) bool { return limit != nil && *limit == 100 })).Return(100, nil).Once()
	provisioner.On("RefreshAPIKeyLimit", mock.Anything, organizationID, openrouter.KeyTypeInternal, mock.Anything).Return(100, nil).Maybe()

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateEnabled}))
	require.ElementsMatch(t, []paygCauseCall{
		{organizationID: organizationID, keyType: openrouter.KeyTypeChat, cause: openrouter.DisableCauseBillingInactive},
		{organizationID: organizationID, keyType: openrouter.KeyTypeChat, cause: openrouter.DisableCauseTrialDemotion},
		{organizationID: organizationID, keyType: openrouter.KeyTypeInternal, cause: openrouter.DisableCauseTrialDemotion},
	}, provisioner.removedCauses)
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyCurrentPaidStateWithoutKeyIsNoop(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, _, organizationID := setupPaygChatKeyReconciler(t, "payg", pgtype.Text{String: "subscription_placeholder", Valid: true})

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateEnabled}))
	provisioner.AssertNotCalled(t, "AddAPIKeyDisableCauseWithDB", mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyCurrentFreeStateDisablesOtherOnly(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, _, organizationID := setupPaygChatKeyReconciler(t, "free", pgtype.Text{})
	provisioner.On("AddAPIKeyDisableCauseWithDB", mock.Anything, organizationID, openrouter.KeyTypeChat).Return(nil).Once()

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateDisabled}))
	provisioner.AssertNotCalled(t, "RefreshAPIKeyLimit", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyBillingLossOwnsOnlyBillingInactive(t *testing.T) {
	t.Parallel()

	t.Run("enabled chat key changes effective access", func(t *testing.T) {
		reconciler, provisioner, _, organizationID := setupPaygChatKeyReconciler(t, "free", pgtype.Text{})
		provisioner.On("AddAPIKeyDisableCauseWithDB", mock.Anything, organizationID, openrouter.KeyTypeChat).Return(nil).Once()

		require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateDisabled}))
		require.Equal(t, []paygCauseCall{{organizationID: organizationID, keyType: openrouter.KeyTypeChat, cause: openrouter.DisableCauseBillingInactive}}, provisioner.addedCauses)
		provisioner.AssertExpectations(t)
	})

	t.Run("admin locked chat key stays disabled without another state patch", func(t *testing.T) {
		reconciler, provisioner, _, organizationID := setupPaygChatKeyReconciler(t, "free", pgtype.Text{})
		provisioner.layeredAdd = true

		require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateDisabled}))
		require.Equal(t, []paygCauseCall{{organizationID: organizationID, keyType: openrouter.KeyTypeChat, cause: openrouter.DisableCauseBillingInactive}}, provisioner.addedCauses)
		provisioner.AssertNotCalled(t, "AddAPIKeyDisableCauseWithDB", mock.Anything, mock.Anything, mock.Anything)
	})
}

func TestReconcilePaygOpenRouterChatKeyBillingRecoveryRemovesOnlyBillingInactive(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, db, organizationID := setupPaygChatKeyReconciler(t, "payg", pgtype.Text{String: "subscription_placeholder", Valid: true})
	createPaygReconcilerKey(t, db, organizationID, openrouter.KeyTypeChat, 50)
	provisioner.On("RefreshAPIKeyLimit", mock.Anything, organizationID, openrouter.KeyTypeChat, mock.MatchedBy(func(limit *int) bool { return limit != nil && *limit == 100 })).Return(100, nil).Once()

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateEnabled}))
	require.Equal(t, []paygCauseCall{{organizationID: organizationID, keyType: openrouter.KeyTypeChat, cause: openrouter.DisableCauseBillingInactive}}, provisioner.removedCauses)
	for _, call := range provisioner.removedCauses {
		require.NotEqual(t, openrouter.DisableCauseTrialDemotion, call.cause, "ordinary billing recovery must preserve trial demotion")
	}
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyStaleActivationWakeupCannotOverrideCurrentFreeState(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, _, organizationID := setupPaygChatKeyReconciler(t, "free", pgtype.Text{})
	provisioner.On("AddAPIKeyDisableCauseWithDB", mock.Anything, organizationID, openrouter.KeyTypeChat).Return(nil).Once()

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateDisabled}), "newer deactivation wake-up")
	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateEnabled}), "stale activation delivered last")

	provisioner.AssertNotCalled(t, "RefreshAPIKeyLimit", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyStaleDeactivationWakeupCannotOverrideCurrentPaidState(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, db, organizationID := setupPaygChatKeyReconciler(t, "payg", pgtype.Text{String: "subscription_placeholder", Valid: true})
	createPaygReconcilerKey(t, db, organizationID, openrouter.KeyTypeChat, 50)
	provisioner.On("RefreshAPIKeyLimit", mock.Anything, organizationID, openrouter.KeyTypeChat, mock.AnythingOfType("*int")).Return(100, nil).Once()

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateEnabled}), "newer activation wake-up")
	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateDisabled}), "stale deactivation delivered last")

	provisioner.AssertNotCalled(t, "AddAPIKeyDisableCauseWithDB", mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyExternalFailureIsRetryableAndIdempotent(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, _, organizationID := setupPaygChatKeyReconciler(t, "free", pgtype.Text{})
	provisioner.On("AddAPIKeyDisableCauseWithDB", mock.Anything, organizationID, openrouter.KeyTypeChat).Return(errors.New("upstream unavailable")).Once()
	provisioner.On("AddAPIKeyDisableCauseWithDB", mock.Anything, organizationID, openrouter.KeyTypeChat).Return(nil).Once()

	args := activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateDisabled}
	require.ErrorContains(t, reconciler.Do(t.Context(), args), "disable PAYG Other inference key")
	require.NoError(t, reconciler.Do(t.Context(), args))
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyRejectsMixedProjection(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, _, organizationID := setupPaygChatKeyReconciler(t, "payg", pgtype.Text{})

	err := reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateEnabled})
	require.ErrorContains(t, err, "inconsistent PAYG Other inference key billing projection")
	provisioner.AssertNotCalled(t, "RefreshAPIKeyLimit", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertNotCalled(t, "AddAPIKeyDisableCauseWithDB", mock.Anything, mock.Anything, mock.Anything)
}

func TestReconcilePaygOpenRouterChatKeyIgnoresEnterpriseOrganization(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, _, organizationID := setupPaygChatKeyReconciler(t, "enterprise", pgtype.Text{})
	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateDisabled}))
	provisioner.AssertNotCalled(t, "RefreshAPIKeyLimit", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertNotCalled(t, "AddAPIKeyDisableCauseWithDB", mock.Anything, mock.Anything, mock.Anything)
}

func TestReconcilePaygOpenRouterChatKeyIgnoresPolarOrganization(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, _, organizationID := setupPaygChatKeyReconciler(t, "pro", pgtype.Text{})
	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateDisabled}))
	provisioner.AssertNotCalled(t, "RefreshAPIKeyLimit", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertNotCalled(t, "AddAPIKeyDisableCauseWithDB", mock.Anything, mock.Anything, mock.Anything)
}

func TestRefreshOpenRouterChatKeyDoesNotReinstateCommittedSubscriptionLoss(t *testing.T) {
	t.Parallel()

	_, provisioner, db, organizationID := setupPaygChatKeyReconciler(t, "free", pgtype.Text{})
	queries := openrouterrepo.New(db)
	_, err := queries.CreateOpenRouterAPIKey(t.Context(), openrouterrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeChat),
		KeyEncrypted:   pgtype.Text{},
		KeyHash:        "hash_placeholder",
		MonthlyCredits: 100,
	})
	require.NoError(t, err)
	require.NoError(t, queries.DisableOpenRouterAPIKey(t.Context(), openrouterrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	}))

	refresh := activities.NewRefreshOpenRouterKey(testenv.NewLogger(t), db, provisioner)
	require.NoError(t, refresh.Do(t.Context(), activities.RefreshOpenRouterKeyArgs{
		OrgID:   organizationID,
		Limit:   nil,
		KeyType: string(openrouter.KeyTypeChat),
	}))
	provisioner.AssertNotCalled(t, "RefreshAPIKeyLimit", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestRefreshOpenRouterInternalKeyHoldsBillingLockAcrossUpstreamPatch(t *testing.T) {
	t.Parallel()

	_, provisioner, db, organizationID := setupPaygChatKeyReconciler(t, "payg", pgtype.Text{String: "subscription_placeholder", Valid: true})
	patchStarted := make(chan struct{})
	releasePatch := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releasePatch) }) }
	t.Cleanup(release)

	provisioner.On("RefreshAPIKeyLimit", mock.Anything, organizationID, openrouter.KeyTypeInternal, mock.Anything).Run(func(mock.Arguments) {
		close(patchStarted)
		<-releasePatch
	}).Return(100, nil).Once()

	refresh := activities.NewRefreshOpenRouterKey(testenv.NewLogger(t), db, provisioner)
	refreshDone := make(chan error, 1)
	go func() {
		refreshDone <- refresh.Do(t.Context(), activities.RefreshOpenRouterKeyArgs{
			OrgID:   organizationID,
			Limit:   nil,
			KeyType: string(openrouter.KeyTypeInternal),
		})
	}()

	select {
	case <-patchStarted:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "legacy Security inference key refresh did not reach upstream PATCH")
	}

	contender, err := db.Acquire(t.Context())
	require.NoError(t, err)
	defer contender.Release()
	lockDone := make(chan error, 1)
	go func() {
		queries := activitiesrepo.New(contender)
		lockErr := queries.AcquireOpenRouterKeyBillingLock(t.Context(), activitiesrepo.AcquireOpenRouterKeyBillingLockParams{
			KeyType:        string(openrouter.KeyTypeInternal),
			OrganizationID: organizationID,
		})
		if lockErr == nil {
			_, lockErr = queries.ReleaseOpenRouterKeyBillingLock(t.Context(), activitiesrepo.ReleaseOpenRouterKeyBillingLockParams{
				KeyType:        string(openrouter.KeyTypeInternal),
				OrganizationID: organizationID,
			})
		}
		lockDone <- lockErr
	}()

	select {
	case lockErr := <-lockDone:
		require.NoError(t, lockErr)
		require.FailNow(t, "billing lock was released before upstream PATCH completed")
	case <-time.After(150 * time.Millisecond):
	}

	release()
	select {
	case refreshErr := <-refreshDone:
		require.NoError(t, refreshErr)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "legacy Security inference key refresh did not finish")
	}
	select {
	case lockErr := <-lockDone:
		require.NoError(t, lockErr)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "billing writer did not acquire released lock")
	}
	provisioner.AssertExpectations(t)
}
