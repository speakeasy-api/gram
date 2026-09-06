package usage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

type captureSpendCapScheduler struct {
	operationID    string
	organizationID string
	keyType        openrouter.KeyType
	limit          int
	actor          urn.Principal
	err            error
}

type captureInferenceCredits struct {
	openrouter.Provisioner
	mu    sync.Mutex
	calls []openrouter.KeyType
}

func (c *captureInferenceCredits) GetCreditsUsed(_ context.Context, _ string, keyType openrouter.KeyType) (float64, int, error) {
	c.mu.Lock()
	c.calls = append(c.calls, keyType)
	c.mu.Unlock()
	switch keyType {
	case openrouter.KeyTypeChat:
		return 25.5, 100, nil
	case openrouter.KeyTypeInternal:
		return 7.25, 50, nil
	default:
		return 0, 0, errors.New("unexpected key type")
	}
}

func (c *captureInferenceCredits) capturedCalls() []openrouter.KeyType {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]openrouter.KeyType(nil), c.calls...)
}

func (c *captureSpendCapScheduler) SetOpenRouterSpendCap(_ context.Context, operationID, organizationID string, keyType openrouter.KeyType, limit int, actor urn.Principal, _ *string) error {
	c.operationID = operationID
	c.organizationID = organizationID
	c.keyType = keyType
	c.limit = limit
	c.actor = actor
	return c.err
}

func configureSpendCapSubscription(t *testing.T, service *Service, db repo.DBTX, organizationID, status string) {
	t.Helper()
	configureSpendCapBillingSubscription(t, service, db, organizationID, status)
	createUsageInferenceKey(t, db, organizationID, openrouter.KeyTypeChat, 100)
}

func configureSpendCapBillingSubscription(t *testing.T, service *Service, db repo.DBTX, organizationID, status string) {
	t.Helper()

	require.NoError(t, repo.New(db).CreateStripeSubscriptionBillingMetadataFixture(t.Context(), repo.CreateStripeSubscriptionBillingMetadataFixtureParams{
		OrganizationID:       organizationID,
		StripeCustomerID:     pgtype.Text{String: "customer_placeholder", Valid: true},
		StripeSubscriptionID: pgtype.Text{String: "sub_spend_cap", Valid: true},
	}))
	service.stripeClient = &checkoutStripeClient{subscriptionState: &stripeclient.SubscriptionState{
		ID:                 "sub_spend_cap",
		CustomerID:         "customer_placeholder",
		Status:             status,
		CurrentPeriodStart: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		CurrentPeriodEnd:   time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
	}}
}

func createUsageInferenceKey(t *testing.T, db repo.DBTX, organizationID string, keyType openrouter.KeyType, monthlyCredits int64) {
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

func TestSetSpendCapWaitsForDedicatedOperation(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-success"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	configureSpendCapSubscription(t, service, db, organizationID, "active")
	scheduler := &captureSpendCapScheduler{}
	service.keyRefresher = scheduler

	result, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 600})
	require.NoError(t, err)
	require.Equal(t, 600, result.MonthlyCredits)
	require.NotEmpty(t, scheduler.operationID)
	require.Equal(t, organizationID, scheduler.organizationID)
	require.Equal(t, openrouter.KeyTypeChat, scheduler.keyType)
	require.Equal(t, 600, scheduler.limit)
	require.Equal(t, "user-billing-email-admin", scheduler.actor.ID)
}

func TestSetSpendCapTargetsSecurityInferenceKey(t *testing.T) {
	t.Parallel()

	organizationID := "org-security-inference-cap"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	configureSpendCapSubscription(t, service, db, organizationID, "active")
	createUsageInferenceKey(t, db, organizationID, openrouter.KeyTypeInternal, 50)
	scheduler := &captureSpendCapScheduler{}
	service.keyRefresher = scheduler
	keyType := string(openrouter.KeyTypeInternal)

	result, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{
		KeyType:        &keyType,
		MonthlyCredits: 75,
	})
	require.NoError(t, err)
	require.Equal(t, string(openrouter.KeyTypeInternal), result.KeyType)
	require.Equal(t, openrouter.KeyTypeInternal, scheduler.keyType)
	require.Equal(t, 75, scheduler.limit)
}

func TestSetSpendCapRejectsDisabledTargetWithoutScheduling(t *testing.T) {
	t.Parallel()

	organizationID := "org-disabled-inference-cap"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	configureSpendCapSubscription(t, service, db, organizationID, "active")
	require.NoError(t, openrouterrepo.New(db).DisableOpenRouterAPIKey(t.Context(), openrouterrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	}))
	scheduler := &captureSpendCapScheduler{}
	service.keyRefresher = scheduler

	_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 200})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Empty(t, scheduler.operationID)
}

func TestSetSpendCapRejectsAbsentTargetWithoutScheduling(t *testing.T) {
	t.Parallel()

	organizationID := "org-absent-inference-cap"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	configureSpendCapSubscription(t, service, db, organizationID, "active")
	scheduler := &captureSpendCapScheduler{}
	service.keyRefresher = scheduler
	keyType := string(openrouter.KeyTypeInternal)

	_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{
		KeyType:        &keyType,
		MonthlyCredits: 200,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Empty(t, scheduler.operationID)
}

func TestGetInferenceSpendCapsListsOnlyMaterializedPlatformKeys(t *testing.T) {
	t.Parallel()

	organizationID := "org-inference-cap-list"
	service, db, _, _ := newTUMTestService(t, organizationID)
	createUsageInferenceKey(t, db, organizationID, openrouter.KeyTypeChat, 100)
	createUsageInferenceKey(t, db, organizationID, openrouter.KeyTypeInternal, 50)
	credits := &captureInferenceCredits{Provisioner: openrouter.NewDevelopment("key_placeholder")}
	service.openRouter = credits

	result, err := service.GetInferenceSpendCaps(
		authztest.WithExactGrants(t, billingEmailAdminContext(t, organizationID), authz.NewGrant(authz.ScopeOrgRead, organizationID)),
		&gen.GetInferenceSpendCapsPayload{},
	)
	require.NoError(t, err)
	require.ElementsMatch(t, []openrouter.KeyType{openrouter.KeyTypeChat, openrouter.KeyTypeInternal}, credits.capturedCalls())
	require.Equal(t, []*gen.InferenceSpendCap{
		{KeyType: "chat", CreditsUsed: 25.5, MonthlyCredits: 100, Disabled: false},
		{KeyType: "internal", CreditsUsed: 7.25, MonthlyCredits: 50, Disabled: false},
	}, result)
	credits.mu.Lock()
	defer credits.mu.Unlock()
	require.ElementsMatch(t, []openrouter.KeyType{openrouter.KeyTypeChat, openrouter.KeyTypeInternal}, credits.calls)
}

func TestDisablePaygOpenRouterChatKeyPreservesClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		disableCauses []string
		wantCauses    []string
	}{
		{name: "legacy unclassified", disableCauses: nil, wantCauses: nil},
		{name: "classified empty", disableCauses: []string{}, wantCauses: []string{"billing_inactive"}},
		{name: "classified admin lock", disableCauses: []string{"admin_lock"}, wantCauses: []string{"admin_lock", "billing_inactive"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			organizationID := "org-billing-loss-" + uuid.NewString()[:8]
			_, db, _, _ := newTUMTestService(t, organizationID)
			createUsageInferenceKey(t, db, organizationID, openrouter.KeyTypeChat, 100)
			require.NoError(t, testrepo.New(db).SetOpenRouterAPIKeyClassificationFixture(t.Context(), testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
				OrganizationID: organizationID,
				KeyType:        string(openrouter.KeyTypeChat),
				Disabled:       false,
				DisableCauses:  tt.disableCauses,
			}))

			queries := repo.New(db)
			require.NoError(t, queries.DisablePaygOpenRouterChatKey(t.Context(), organizationID))
			require.NoError(t, queries.DisablePaygOpenRouterChatKey(t.Context(), organizationID), "billing loss must be idempotent")

			row, err := openrouterrepo.New(db).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
				OrganizationID: organizationID,
				KeyType:        string(openrouter.KeyTypeChat),
			})
			require.NoError(t, err)
			require.True(t, row.Disabled)
			require.Equal(t, tt.wantCauses, row.DisableCauses)
		})
	}
}

func TestMaterializedInferenceKeyReadsUseEffectiveDisabledCompatibility(t *testing.T) {
	t.Parallel()

	organizationID := "org-inference-cap-compat"
	_, db, _, _ := newTUMTestService(t, organizationID)
	createUsageInferenceKey(t, db, organizationID, openrouter.KeyTypeChat, 100)
	err := testrepo.New(db).SetOpenRouterAPIKeyClassificationFixture(t.Context(), testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: organizationID, KeyType: string(openrouter.KeyTypeChat), Disabled: true, DisableCauses: []string{}})
	require.NoError(t, err)

	key, err := repo.New(db).GetMaterializedOpenRouterInferenceKey(t.Context(), repo.GetMaterializedOpenRouterInferenceKeyParams{OrganizationID: organizationID, KeyType: "chat"})
	require.NoError(t, err)
	require.False(t, key.Disabled)

	err = testrepo.New(db).SetOpenRouterAPIKeyClassificationFixture(t.Context(), testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: organizationID, KeyType: string(openrouter.KeyTypeChat), Disabled: false, DisableCauses: []string{"billing_inactive"}})
	require.NoError(t, err)
	key, err = repo.New(db).GetMaterializedOpenRouterInferenceKey(t.Context(), repo.GetMaterializedOpenRouterInferenceKeyParams{OrganizationID: organizationID, KeyType: "chat"})
	require.NoError(t, err)
	require.True(t, key.Disabled)
}

func TestGetInferenceSpendCapsReturnsEmptyBeforeKeyMaterialization(t *testing.T) {
	t.Parallel()

	organizationID := "org-inference-cap-empty"
	service, _, _, _ := newTUMTestService(t, organizationID)
	credits := &captureInferenceCredits{Provisioner: openrouter.NewDevelopment("key_placeholder")}
	service.openRouter = credits

	result, err := service.GetInferenceSpendCaps(
		authztest.WithExactGrants(t, billingEmailAdminContext(t, organizationID), authz.NewGrant(authz.ScopeOrgRead, organizationID)),
		&gen.GetInferenceSpendCapsPayload{},
	)
	require.NoError(t, err)
	require.Empty(t, result)
	require.Empty(t, credits.capturedCalls())
}

func TestGetInferenceSpendCapsRequiresOrganizationRead(t *testing.T) {
	t.Parallel()

	organizationID := "org-inference-cap-forbidden"
	service, db, _, _ := newTUMTestService(t, organizationID)
	createUsageInferenceKey(t, db, organizationID, openrouter.KeyTypeChat, 100)
	credits := &captureInferenceCredits{Provisioner: openrouter.NewDevelopment("key_placeholder")}
	service.openRouter = credits
	ctx := authztest.WithExactGrants(t, billingEmailAdminContext(t, organizationID))

	_, err := service.GetInferenceSpendCaps(ctx, &gen.GetInferenceSpendCapsPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Empty(t, credits.capturedCalls())
}

func TestSetSpendCapRejectsStripeTrial(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-stripe-trial"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	configureSpendCapSubscription(t, service, db, organizationID, "trialing")
	service.keyRefresher = &captureSpendCapScheduler{}

	_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 200})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestSetSpendCapRejectsInactiveStripeSubscription(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"canceled", "unpaid", "incomplete", "incomplete_expired", "paused"} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()

			organizationID := "org-spend-cap-" + status
			service, db, _, _ := newTUMTestService(t, organizationID)
			setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
			configureSpendCapSubscription(t, service, db, organizationID, status)
			scheduler := &captureSpendCapScheduler{}
			service.keyRefresher = scheduler

			_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 200})
			requireOopsCode(t, err, oops.CodeConflict)
			require.Empty(t, scheduler.operationID)
		})
	}
}

func TestSetSpendCapAllowsPastDueStripeSubscription(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-past-due"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	configureSpendCapSubscription(t, service, db, organizationID, "past_due")
	scheduler := &captureSpendCapScheduler{}
	service.keyRefresher = scheduler

	_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 200})
	require.NoError(t, err)
	require.Equal(t, organizationID, scheduler.organizationID)
}

func TestSetSpendCapRejectsActiveTrial(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-trial"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	require.NoError(t, trialsrepo.New(db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
		OrganizationID: organizationID,
		Tier:           string(billing.TierEnterprise),
		EndsAt:         pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	}))
	service.keyRefresher = &captureSpendCapScheduler{}

	_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 200})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestSetSpendCapRejectsNonPaygOrganization(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-enterprise"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierEnterprise)
	service.keyRefresher = &captureSpendCapScheduler{}

	_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 200})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSetSpendCapRejectsNonAdmin(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-member"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	service.keyRefresher = &captureSpendCapScheduler{}
	ctx := authztest.WithExactGrants(t, billingEmailAdminContext(t, organizationID), authz.NewGrant(authz.ScopeOrgRead, organizationID))

	_, err := service.SetSpendCap(ctx, &gen.SetSpendCapPayload{MonthlyCredits: 200})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSetSpendCapRejectsOutOfRangeValues(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-range"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	service.keyRefresher = &captureSpendCapScheduler{}

	for _, value := range []int{0, 10001} {
		_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: value})
		requireOopsCode(t, err, oops.CodeInvalid)
	}
}

func TestSetSpendCapRejectsAbsentTargetKey(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-absent"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	configureSpendCapBillingSubscription(t, service, db, organizationID, "active")
	scheduler := &captureSpendCapScheduler{}
	service.keyRefresher = scheduler

	_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 200})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Empty(t, scheduler.operationID)
}

func TestSetSpendCapRejectsDisabledTargetKey(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-disabled"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	configureSpendCapSubscription(t, service, db, organizationID, "active")
	require.NoError(t, openrouterrepo.New(db).DisableOpenRouterAPIKey(t.Context(), openrouterrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	}))
	scheduler := &captureSpendCapScheduler{}
	service.keyRefresher = scheduler

	_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 200})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Empty(t, scheduler.operationID)
}

func TestSetSpendCapDoesNotAuditFailedScheduling(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-schedule-failure"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	configureSpendCapSubscription(t, service, db, organizationID, "active")
	service.keyRefresher = &captureSpendCapScheduler{err: errors.New("scheduler unavailable")}

	_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 200})
	requireOopsCode(t, err, oops.CodeUnexpected)
}
