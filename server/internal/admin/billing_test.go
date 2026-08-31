package admin

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	orrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usage"
)

type fakeOpenRouterSpendCapScheduler struct {
	operationID      string
	organizationID   string
	keyType          openrouter.KeyType
	limit            int
	actor            urn.Principal
	actorDisplayName *string
	effectiveLimit   int
	err              error
}

func (f *fakeOpenRouterSpendCapScheduler) SetAdminOpenRouterSpendCap(_ context.Context, operationID, organizationID string, keyType openrouter.KeyType, limit int, actor urn.Principal, actorDisplayName *string) (int, error) {
	f.operationID = operationID
	f.organizationID = organizationID
	f.keyType = keyType
	f.limit = limit
	f.actor = actor
	f.actorDisplayName = actorDisplayName
	if f.err != nil {
		return 0, f.err
	}
	if f.effectiveLimit != 0 {
		return f.effectiveLimit, nil
	}
	return limit, nil
}

type fakeOpenRouterUsage struct {
	creditsByKeyType map[openrouter.KeyType]float64
	limitsByKeyType  map[openrouter.KeyType]int
	err              error
}

func (f *fakeOpenRouterUsage) GetCreditsUsed(_ context.Context, _ string, keyType openrouter.KeyType) (float64, int, error) {
	if f.err != nil {
		return 0, 0, f.err
	}
	return f.creditsByKeyType[keyType], f.limitsByKeyType[keyType], nil
}

type fakeBillingOperations struct {
	organizationID string
	cancel         *bool
	actor          usage.BillingActor
	subscription   *usage.StripeSubscription
}

func (f *fakeBillingOperations) GetPaygBillingSummaryForOrganization(_ context.Context, organizationID string) (*usage.PaygBillingSummary, error) {
	f.organizationID = organizationID
	return &usage.PaygBillingSummary{PeriodStart: "2026-08-01T00:00:00Z", PeriodEnd: "2026-09-01T00:00:00Z", TumTokens: 42, TumUnitPriceUsd: "0.1", TumCostUsd: "4.2", OtherInferenceSpendUsd: "1.0", EstimatedTotalUsd: "5.2"}, nil
}

func (f *fakeBillingOperations) GetStripeSubscriptionForOrganization(_ context.Context, organizationID string) (*usage.StripeSubscription, error) {
	f.organizationID = organizationID
	if f.subscription != nil {
		return f.subscription, nil
	}
	return &usage.StripeSubscription{Status: "active", CurrentPeriodStart: "2026-08-01T00:00:00Z", CurrentPeriodEnd: "2026-09-01T00:00:00Z"}, nil
}

func (f *fakeBillingOperations) SetStripeSubscriptionCancelAtPeriodEndForOrganization(_ context.Context, organizationID string, actor usage.BillingActor, cancel bool) (*usage.StripeSubscription, error) {
	f.organizationID = organizationID
	f.cancel = &cancel
	f.actor = actor
	return &usage.StripeSubscription{Status: "active", CurrentPeriodStart: "2026-08-01T00:00:00Z", CurrentPeriodEnd: "2026-09-01T00:00:00Z", CancelAtPeriodEnd: cancel}, nil
}

func TestGetInferenceKeysUsesCanonicalOrganizationIDAndReturnsConfiguredState(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_inference", name: "Inference", slug: "inference"})
	svc.openRouterUsage = &fakeOpenRouterUsage{
		creditsByKeyType: map[openrouter.KeyType]float64{openrouter.KeyTypeChat: 42.75, openrouter.KeyTypeInternal: 12.5},
		limitsByKeyType:  map[openrouter.KeyType]int{openrouter.KeyTypeChat: 999, openrouter.KeyTypeInternal: 999},
	}
	openRouterRepo := orrepo.New(db)
	_, err := openRouterRepo.CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: "org_inference", KeyType: "chat", KeyEncrypted: pgtype.Text{}, KeyHash: "hash-chat", MonthlyCredits: 100,
	})
	require.NoError(t, err)
	_, err = openRouterRepo.CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: "org_inference", KeyType: "internal", KeyEncrypted: pgtype.Text{}, KeyHash: "hash-internal", MonthlyCredits: 50,
	})
	require.NoError(t, err)
	_, err = openRouterRepo.AddOpenRouterAPIKeyDisableCause(ctx, orrepo.AddOpenRouterAPIKeyDisableCauseParams{
		OrganizationID: "org_inference", KeyType: "chat", KeyHash: "hash-chat", DisableCause: "admin_lock",
	})
	require.NoError(t, err)
	_, err = openRouterRepo.AddOpenRouterAPIKeyDisableCause(ctx, orrepo.AddOpenRouterAPIKeyDisableCauseParams{
		OrganizationID: "org_inference", KeyType: "chat", KeyHash: "hash-chat", DisableCause: "future_policy",
	})
	require.NoError(t, err)
	err = openRouterRepo.DisableOpenRouterAPIKey(ctx, orrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: "org_inference", KeyType: "internal",
	})
	require.NoError(t, err)

	result, err := svc.GetInferenceKeys(ctx, &gen.GetInferenceKeysPayload{OrganizationID: "org_inference"})
	require.NoError(t, err)
	require.Equal(t, []*gen.AdminInferenceKey{
		{KeyType: "chat", CreditsUsed: 42.75, MonthlyCredits: 100, Disabled: true, DisableCauses: []string{"admin_lock", "future_policy"}, DisableCausesClassified: true},
		{KeyType: "internal", CreditsUsed: 12.5, MonthlyCredits: 50, Disabled: true, DisableCauses: nil, DisableCausesClassified: false},
	}, result)
}

func TestGetInferenceKeysRejectsOrganizationSlug(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_inference_slug", name: "Inference Slug", slug: "inference-slug"})

	_, err := svc.GetInferenceKeys(ctx, &gen.GetInferenceKeysPayload{OrganizationID: "inference-slug"})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetInferenceKeysOmitsUnsupportedAndAbsentKeys(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_inference_filtered", name: "Inference Filtered", slug: "inference-filtered"})
	openRouterUsage := &fakeOpenRouterUsage{
		creditsByKeyType: make(map[openrouter.KeyType]float64),
		limitsByKeyType:  make(map[openrouter.KeyType]int),
	}
	openRouterUsage.creditsByKeyType[openrouter.KeyTypeChat] = 7.25
	openRouterUsage.limitsByKeyType[openrouter.KeyTypeChat] = 999
	svc.openRouterUsage = openRouterUsage
	openRouterRepo := orrepo.New(db)
	_, err := openRouterRepo.CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: "org_inference_filtered", KeyType: "chat", KeyEncrypted: pgtype.Text{}, KeyHash: "hash-chat", MonthlyCredits: 100,
	})
	require.NoError(t, err)
	_, err = openRouterRepo.CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: "org_inference_filtered", KeyType: "unsupported", KeyEncrypted: pgtype.Text{}, KeyHash: "hash-unsupported", MonthlyCredits: 25,
	})
	require.NoError(t, err)

	result, err := svc.GetInferenceKeys(ctx, &gen.GetInferenceKeysPayload{OrganizationID: "org_inference_filtered"})
	require.NoError(t, err)
	require.Equal(t, []*gen.AdminInferenceKey{
		{KeyType: "chat", CreditsUsed: 7.25, MonthlyCredits: 100, Disabled: false, DisableCauses: []string{}, DisableCausesClassified: true},
	}, result)

}

func TestGetInferenceKeysReportsUnavailableWhenOpenRouterIsNotConfigured(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_inference_usage_unavailable", name: "Inference Usage Unavailable", slug: "inference-usage-unavailable"})
	svc.openRouterUsage = TrialKeysUnavailable{}
	_, err := orrepo.New(db).CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: "org_inference_usage_unavailable", KeyType: "chat", KeyEncrypted: pgtype.Text{}, KeyHash: "hash-chat", MonthlyCredits: 100,
	})
	require.NoError(t, err)

	_, err = svc.GetInferenceKeys(ctx, &gen.GetInferenceKeysPayload{OrganizationID: "org_inference_usage_unavailable"})
	requireOopsCode(t, err, oops.CodeUnavailable)
}

func TestGetInferenceKeysFailsWhenOpenRouterUsageCannotBeRead(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_inference_usage_error", name: "Inference Usage Error", slug: "inference-usage-error"})
	svc.openRouterUsage = &fakeOpenRouterUsage{err: errors.New("provider unavailable")}
	_, err := orrepo.New(db).CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: "org_inference_usage_error", KeyType: "chat", KeyEncrypted: pgtype.Text{}, KeyHash: "hash-chat", MonthlyCredits: 100,
	})
	require.NoError(t, err)

	_, err = svc.GetInferenceKeys(ctx, &gen.GetInferenceKeysPayload{OrganizationID: "org_inference_usage_error"})
	requireOopsCode(t, err, oops.CodeUnexpected)
}

func TestSetInferenceKeyMonthlyLimitSchedulesDurableAdminOperation(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_limit", name: "Inference Limit", slug: "inference-limit"})
	_, err := orrepo.New(db).CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: "org_limit", KeyType: "internal", KeyEncrypted: pgtype.Text{}, KeyHash: "hash-internal", MonthlyCredits: 50,
	})
	require.NoError(t, err)
	scheduler := &fakeOpenRouterSpendCapScheduler{effectiveLimit: 274}
	svc.openRouterSpendCap = scheduler
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID: "session-limit", Email: "operator@example.test", OIDCSubject: "oidc-subject-limit", Name: "Test Operator", HD: "example.test",
	})

	result, err := svc.SetInferenceKeyMonthlyLimit(ctx, &gen.SetInferenceKeyMonthlyLimitPayload{
		OrganizationID: "org_limit", KeyType: "internal", MonthlyCredits: 275,
	})
	require.NoError(t, err)
	require.Equal(t, &gen.AdminInferenceKeyLimit{KeyType: "internal", MonthlyCredits: 274}, result)
	require.NotEmpty(t, scheduler.operationID)
	require.Equal(t, "org_limit", scheduler.organizationID)
	require.Equal(t, openrouter.KeyTypeInternal, scheduler.keyType)
	require.Equal(t, 275, scheduler.limit)
	require.Equal(t, "oidc-subject-limit", scheduler.actor.ID)
	require.Equal(t, urn.PrincipalTypeUser, scheduler.actor.Type)
	require.NotNil(t, scheduler.actorDisplayName)
	require.Equal(t, "Test Operator", *scheduler.actorDisplayName)
}

func TestSetInferenceKeyMonthlyLimitReportsSchedulerFailure(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_limit_failure", name: "Inference Limit Failure", slug: "inference-limit-failure"})
	_, err := orrepo.New(db).CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: "org_limit_failure", KeyType: "chat", KeyEncrypted: pgtype.Text{}, KeyHash: "hash-chat-failure", MonthlyCredits: 50,
	})
	require.NoError(t, err)
	svc.openRouterSpendCap = &fakeOpenRouterSpendCapScheduler{err: errors.New("workflow failed")}

	_, err = svc.SetInferenceKeyMonthlyLimit(ctx, &gen.SetInferenceKeyMonthlyLimitPayload{
		OrganizationID: "org_limit_failure", KeyType: "chat", MonthlyCredits: 275,
	})
	requireOopsCode(t, err, oops.CodeUnexpected)
}

func TestSetInferenceKeyMonthlyLimitValidatesExplicitKeyAndBounds(t *testing.T) {
	t.Parallel()
	_, svc, _ := newTestAdminService(t)

	for _, payload := range []*gen.SetInferenceKeyMonthlyLimitPayload{
		{OrganizationID: "unused", KeyType: "", MonthlyCredits: 100},
		{OrganizationID: "unused", KeyType: "unsupported", MonthlyCredits: 100},
		{OrganizationID: "unused", KeyType: "chat", MonthlyCredits: 0},
		{OrganizationID: "unused", KeyType: "chat", MonthlyCredits: 10001},
	} {
		_, err := svc.SetInferenceKeyMonthlyLimit(t.Context(), payload)
		requireOopsCode(t, err, oops.CodeInvalid)
	}
}

func TestSetInferenceKeyMonthlyLimitReportsUnavailableWithoutScheduler(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_limit_unavailable", name: "Inference Limit Unavailable", slug: "inference-limit-unavailable"})

	_, err := svc.SetInferenceKeyMonthlyLimit(ctx, &gen.SetInferenceKeyMonthlyLimitPayload{
		OrganizationID: "org_limit_unavailable", KeyType: "chat", MonthlyCredits: 100,
	})
	requireOopsCode(t, err, oops.CodeUnavailable)
}

func TestSetInferenceKeyMonthlyLimitRejectsAbsentAndDisabledKeys(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_limit_reject", name: "Inference Limit Reject", slug: "inference-limit-reject"})
	scheduler := &fakeOpenRouterSpendCapScheduler{}
	svc.openRouterSpendCap = scheduler

	_, err := svc.SetInferenceKeyMonthlyLimit(ctx, &gen.SetInferenceKeyMonthlyLimitPayload{
		OrganizationID: "org_limit_reject", KeyType: "internal", MonthlyCredits: 100,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Empty(t, scheduler.operationID)

	_, err = orrepo.New(db).CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: "org_limit_reject", KeyType: "internal", KeyEncrypted: pgtype.Text{}, KeyHash: "hash-disabled", MonthlyCredits: 50,
	})
	require.NoError(t, err)
	require.NoError(t, orrepo.New(db).DisableOpenRouterAPIKey(ctx, orrepo.DisableOpenRouterAPIKeyParams{OrganizationID: "org_limit_reject", KeyType: "internal"}))

	_, err = svc.SetInferenceKeyMonthlyLimit(ctx, &gen.SetInferenceKeyMonthlyLimitPayload{
		OrganizationID: "org_limit_reject", KeyType: "internal", MonthlyCredits: 100,
	})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Empty(t, scheduler.operationID)
}

func TestGetInferenceSpendHistoryReturnsCompleteMonths(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	const organizationID = "org_inference_history"
	seedOrg(t, ctx, db, orgFixture{id: organizationID, name: "Inference History", slug: "inference-history"})

	now := time.Now().UTC()
	currentMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	firstMonth := currentMonth.AddDate(0, -2, 0)
	openRouterRepo := orrepo.New(db)
	for _, keyType := range []string{"chat", "internal"} {
		_, err := openRouterRepo.CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
			OrganizationID: organizationID, KeyType: keyType, KeyEncrypted: pgtype.Text{}, KeyHash: "hash-" + keyType, MonthlyCredits: 100,
		})
		require.NoError(t, err)
	}
	fixtures := testrepo.New(db)
	err := fixtures.SetOpenRouterAPIKeyCreatedAtFixture(ctx, testrepo.SetOpenRouterAPIKeyCreatedAtFixtureParams{
		CreatedAt:      pgtype.Timestamptz{Time: firstMonth, Valid: true},
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	for keyType, spendUSD := range map[string]string{"chat": "1.0", "internal": "0.5"} {
		err = fixtures.SeedOpenRouterSpendRangeFixture(ctx, testrepo.SeedOpenRouterSpendRangeFixtureParams{
			OrganizationID: organizationID,
			KeyType:        keyType,
			SpendUsd:       spendUSD,
			StartDay:       pgtype.Date{Time: firstMonth, Valid: true},
			EndDay:         pgtype.Date{Time: currentMonth.AddDate(0, 0, -1), Valid: true},
		})
		require.NoError(t, err)
	}
	err = fixtures.SeedOpenRouterSpendRangeFixture(ctx, testrepo.SeedOpenRouterSpendRangeFixtureParams{
		OrganizationID: organizationID,
		KeyType:        "unsupported",
		SpendUsd:       "999.0",
		StartDay:       pgtype.Date{Time: firstMonth, Valid: true},
		EndDay:         pgtype.Date{Time: firstMonth, Valid: true},
	})
	require.NoError(t, err)

	result, err := svc.GetInferenceSpendHistory(ctx, &gen.GetInferenceSpendHistoryPayload{OrganizationID: organizationID})
	require.NoError(t, err)
	require.Len(t, result, 2)
	for index, month := range result {
		periodStart := firstMonth.AddDate(0, index, 0)
		periodEnd := periodStart.AddDate(0, 1, 0)
		days := periodEnd.Sub(periodStart).Hours() / 24
		require.Equal(t, periodStart.Format(time.DateOnly), month.PeriodStart)
		require.Equal(t, periodEnd.Format(time.DateOnly), month.PeriodEnd)
		require.Equal(t, fmt.Sprintf("%.6f", days*1.5), month.SpendUsd)
	}

	err = fixtures.DeleteOpenRouterSpendDayFixture(ctx, testrepo.DeleteOpenRouterSpendDayFixtureParams{
		OrganizationID: organizationID,
		KeyType:        "internal",
		Day:            pgtype.Date{Time: firstMonth, Valid: true},
	})
	require.NoError(t, err)
	result, err = svc.GetInferenceSpendHistory(ctx, &gen.GetInferenceSpendHistoryPayload{OrganizationID: organizationID})
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, firstMonth.AddDate(0, 1, 0).Format(time.DateOnly), result[0].PeriodStart)
}

func TestGetPaygBillingSummaryUsesCanonicalOrganizationID(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_billing", name: "Billing", slug: "billing"})
	fake := &fakeBillingOperations{}
	svc.billing = fake

	result, err := svc.GetPaygBillingSummary(ctx, &gen.GetPaygBillingSummaryPayload{OrganizationID: "org_billing"})
	require.NoError(t, err)
	require.Equal(t, "org_billing", fake.organizationID)
	require.Equal(t, int64(42), result.TumTokens)
}

func TestCancelStripeSubscriptionUsesExplicitOrganization(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_billing", name: "Billing", slug: "billing"})
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID: "session-billing", Email: "operator@example.test", OIDCSubject: "oidc-subject-billing",
		Name: "Test Operator", HD: "example.test",
	})
	fake := &fakeBillingOperations{}
	svc.billing = fake

	result, err := svc.CancelStripeSubscription(ctx, &gen.CancelStripeSubscriptionPayload{OrganizationID: "org_billing"})
	require.NoError(t, err)
	require.NotNil(t, fake.cancel)
	require.True(t, *fake.cancel)
	require.True(t, result.CancelAtPeriodEnd)
	require.Equal(t, "oidc-subject-billing", fake.actor.Principal.ID)
	require.NotNil(t, fake.actor.DisplayName)
	require.Equal(t, "Test Operator", *fake.actor.DisplayName)
	require.NotEqual(t, "operator@example.test", *fake.actor.DisplayName)
}

func TestBillingRejectsUnknownOrganizationBeforeProviderCall(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	fake := &fakeBillingOperations{}
	svc.billing = fake

	_, err := svc.GetStripeSubscription(ctx, &gen.GetStripeSubscriptionPayload{OrganizationID: "org_missing"})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Empty(t, fake.organizationID)
}

func TestGetStripeSubscriptionMapsPaymentAndOptionalDates(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_subscription", name: "Subscription", slug: "subscription"})
	cancelAt := "2026-09-01T00:00:00Z"
	fake := &fakeBillingOperations{subscription: &usage.StripeSubscription{
		Status: "past_due", CurrentPeriodStart: "2026-08-01T00:00:00Z", CurrentPeriodEnd: "2026-09-01T00:00:00Z",
		CancelAtPeriodEnd: true, CancelAt: &cancelAt, PaymentFailed: true,
	}}
	svc.billing = fake

	result, err := svc.GetStripeSubscription(ctx, &gen.GetStripeSubscriptionPayload{OrganizationID: "org_subscription"})
	require.NoError(t, err)
	require.Equal(t, "past_due", result.Status)
	require.True(t, result.PaymentFailed)
	require.Equal(t, &cancelAt, result.CancelAt)
}

func TestResumeStripeSubscriptionUsesExplicitOrganization(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_resume", name: "Resume", slug: "resume"})
	fake := &fakeBillingOperations{}
	svc.billing = fake

	result, err := svc.ResumeStripeSubscription(ctx, &gen.ResumeStripeSubscriptionPayload{OrganizationID: "org_resume"})
	require.NoError(t, err)
	require.Equal(t, "org_resume", fake.organizationID)
	require.NotNil(t, fake.cancel)
	require.False(t, *fake.cancel)
	require.False(t, result.CancelAtPeriodEnd)
}
