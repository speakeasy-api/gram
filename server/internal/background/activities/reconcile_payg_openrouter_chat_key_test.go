package activities_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
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

func (m *mockPaygChatKeyProvisioner) RefreshAPIKeyLimitWithDB(ctx context.Context, _ openrouter.DBTX, organizationID string, keyType openrouter.KeyType, limit *int) (int, error) {
	return m.RefreshAPIKeyLimit(ctx, organizationID, keyType, limit)
}

func (m *mockPaygChatKeyProvisioner) ReinstateAPIKeyLimitWithDB(ctx context.Context, _ openrouter.DBTX, organizationID string, keyType openrouter.KeyType, limit *int) (int, error) {
	return m.RefreshAPIKeyLimit(ctx, organizationID, keyType, limit)
}

func (m *mockPaygChatKeyProvisioner) DisableAPIKey(ctx context.Context, organizationID string, keyType openrouter.KeyType) error {
	return m.Called(ctx, organizationID, keyType).Error(0)
}

func (m *mockPaygChatKeyProvisioner) DisableAPIKeyWithDB(ctx context.Context, _ openrouter.DBTX, organizationID string, keyType openrouter.KeyType) error {
	return m.DisableAPIKey(ctx, organizationID, keyType)
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
	provisioner.AssertNotCalled(t, "DisableAPIKey", mock.Anything, mock.Anything, mock.Anything)
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

func TestReconcilePaygOpenRouterChatKeyReenablesDisabledSecurityAtRecordedCap(t *testing.T) {
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
	provisioner.On("RefreshAPIKeyLimit", mock.Anything, organizationID, openrouter.KeyTypeInternal, mock.MatchedBy(func(limit *int) bool {
		return limit != nil && *limit == 37
	})).Return(37, nil).Once()

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateEnabled}))
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyReenablesDisabledTrialSecurityAtPaygDefault(t *testing.T) {
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
	provisioner.On("RefreshAPIKeyLimit", mock.Anything, organizationID, openrouter.KeyTypeInternal, mock.MatchedBy(func(limit *int) bool {
		return limit != nil && *limit == 100
	})).Return(100, nil).Once()

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateEnabled}))
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyCurrentPaidStateWithoutKeyIsNoop(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, _, organizationID := setupPaygChatKeyReconciler(t, "payg", pgtype.Text{String: "subscription_placeholder", Valid: true})

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateEnabled}))
	provisioner.AssertNotCalled(t, "DisableAPIKey", mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyCurrentFreeStateDisablesOtherOnly(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, _, organizationID := setupPaygChatKeyReconciler(t, "free", pgtype.Text{})
	provisioner.On("DisableAPIKey", mock.Anything, organizationID, openrouter.KeyTypeChat).Return(nil).Once()

	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateDisabled}))
	provisioner.AssertNotCalled(t, "RefreshAPIKeyLimit", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyStaleActivationWakeupCannotOverrideCurrentFreeState(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, _, organizationID := setupPaygChatKeyReconciler(t, "free", pgtype.Text{})
	provisioner.On("DisableAPIKey", mock.Anything, organizationID, openrouter.KeyTypeChat).Return(nil).Once()

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

	provisioner.AssertNotCalled(t, "DisableAPIKey", mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertExpectations(t)
}

func TestReconcilePaygOpenRouterChatKeyExternalFailureIsRetryableAndIdempotent(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, _, organizationID := setupPaygChatKeyReconciler(t, "free", pgtype.Text{})
	provisioner.On("DisableAPIKey", mock.Anything, organizationID, openrouter.KeyTypeChat).Return(errors.New("upstream unavailable")).Once()
	provisioner.On("DisableAPIKey", mock.Anything, organizationID, openrouter.KeyTypeChat).Return(nil).Once()

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
	provisioner.AssertNotCalled(t, "DisableAPIKey", mock.Anything, mock.Anything, mock.Anything)
}

func TestReconcilePaygOpenRouterChatKeyIgnoresEnterpriseOrganization(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, _, organizationID := setupPaygChatKeyReconciler(t, "enterprise", pgtype.Text{})
	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateDisabled}))
	provisioner.AssertNotCalled(t, "RefreshAPIKeyLimit", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertNotCalled(t, "DisableAPIKey", mock.Anything, mock.Anything, mock.Anything)
}

func TestReconcilePaygOpenRouterChatKeyIgnoresPolarOrganization(t *testing.T) {
	t.Parallel()

	reconciler, provisioner, _, organizationID := setupPaygChatKeyReconciler(t, "pro", pgtype.Text{})
	require.NoError(t, reconciler.Do(t.Context(), activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: organizationID, DesiredState: openrouter.KeyDesiredStateDisabled}))
	provisioner.AssertNotCalled(t, "RefreshAPIKeyLimit", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	provisioner.AssertNotCalled(t, "DisableAPIKey", mock.Anything, mock.Anything, mock.Anything)
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
