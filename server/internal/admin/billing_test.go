package admin

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	"github.com/speakeasy-api/gram/server/internal/usage"
)

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
	openRouterRepo := orrepo.New(db)
	_, err := openRouterRepo.CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: "org_inference", KeyType: "chat", KeyEncrypted: pgtype.Text{}, KeyHash: "hash-chat", MonthlyCredits: 100,
	})
	require.NoError(t, err)
	_, err = openRouterRepo.CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: "org_inference", KeyType: "internal", KeyEncrypted: pgtype.Text{}, KeyHash: "hash-internal", MonthlyCredits: 50,
	})
	require.NoError(t, err)
	err = openRouterRepo.DisableOpenRouterAPIKey(ctx, orrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: "org_inference", KeyType: "internal",
	})
	require.NoError(t, err)

	result, err := svc.GetInferenceKeys(ctx, &gen.GetInferenceKeysPayload{OrganizationID: "org_inference"})
	require.NoError(t, err)
	require.Equal(t, []*gen.AdminInferenceKey{
		{KeyType: "chat", MonthlyCredits: 100, Disabled: false},
		{KeyType: "internal", MonthlyCredits: 50, Disabled: true},
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
		{KeyType: "chat", MonthlyCredits: 100, Disabled: false},
	}, result)

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
	require.Equal(t, audit.SpeakeasyTeamActorLabel, *fake.actor.DisplayName)
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
