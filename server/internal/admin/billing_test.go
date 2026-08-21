package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	orrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	"github.com/speakeasy-api/gram/server/internal/usage"
)

type fakeOpenRouterLimitUpdater struct {
	called          bool
	usedTransaction bool
	err             error
}

func (f *fakeOpenRouterLimitUpdater) RefreshAPIKeyLimitWithDB(ctx context.Context, db openrouter.DBTX, orgID string, keyType openrouter.KeyType, limit *int) (int, error) {
	f.called = true
	_, f.usedTransaction = db.(pgx.Tx)
	if f.err != nil {
		return 0, f.err
	}
	key, err := orrepo.New(db).GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(keyType)})
	if err != nil {
		return 0, fmt.Errorf("get fake OpenRouter key: %w", err)
	}
	_, err = orrepo.New(db).UpdateOpenRouterKey(ctx, orrepo.UpdateOpenRouterKeyParams{
		OrganizationID: orgID, KeyType: string(keyType), MonthlyCredits: int64(*limit), KeyHash: key.KeyHash, Reinstate: false,
	})
	if err != nil {
		return 0, fmt.Errorf("update fake OpenRouter key: %w", err)
	}
	return *limit, nil
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
	err = openRouterRepo.DisableOpenRouterAPIKey(ctx, orrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: "org_inference", KeyType: "internal",
	})
	require.NoError(t, err)

	result, err := svc.GetInferenceKeys(ctx, &gen.GetInferenceKeysPayload{OrganizationID: "org_inference"})
	require.NoError(t, err)
	require.Equal(t, []*gen.AdminInferenceKey{
		{KeyType: "chat", CreditsUsed: 42.75, MonthlyCredits: 100, Disabled: false},
		{KeyType: "internal", CreditsUsed: 12.5, MonthlyCredits: 50, Disabled: true},
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
		{KeyType: "chat", CreditsUsed: 7.25, MonthlyCredits: 100, Disabled: false},
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

func TestSetInferenceKeyMonthlyLimitUpdatesSpecificMaterializedKey(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_limit", name: "Inference Limit", slug: "inference-limit"})
	keys := orrepo.New(db)
	for _, keyType := range openrouter.AllKeyTypes {
		_, err := keys.CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
			OrganizationID: "org_limit", KeyType: string(keyType), KeyEncrypted: pgtype.Text{}, KeyHash: "hash-" + string(keyType), MonthlyCredits: 50,
		})
		require.NoError(t, err)
	}
	updater := &fakeOpenRouterLimitUpdater{}
	svc.openRouterLimit = updater

	result, err := svc.SetInferenceKeyMonthlyLimit(ctx, &gen.SetInferenceKeyMonthlyLimitPayload{
		OrganizationID: "org_limit", KeyType: "internal", MonthlyCredits: 275,
	})
	require.NoError(t, err)
	require.Equal(t, &gen.AdminInferenceKeyLimit{KeyType: "internal", MonthlyCredits: 275}, result)
	require.True(t, updater.called)
	require.True(t, updater.usedTransaction, "the refresh and audit must share a transaction on the billing-lock connection")

	internal, err := keys.GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{OrganizationID: "org_limit", KeyType: "internal"})
	require.NoError(t, err)
	require.EqualValues(t, 275, internal.MonthlyCredits)
	chat, err := keys.GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{OrganizationID: "org_limit", KeyType: "chat"})
	require.NoError(t, err)
	require.EqualValues(t, 50, chat.MonthlyCredits)
}

func TestSetInferenceKeyMonthlyLimitMapsRefreshFailureToGatewayError(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_limit_gateway", name: "Inference Limit Gateway", slug: "inference-limit-gateway"})
	keys := orrepo.New(db)
	_, err := keys.CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: "org_limit_gateway", KeyType: "chat", KeyEncrypted: pgtype.Text{}, KeyHash: "hash-chat-gateway", MonthlyCredits: 50,
	})
	require.NoError(t, err)
	svc.openRouterLimit = &fakeOpenRouterLimitUpdater{err: errors.New("upstream patch failed")}

	_, err = svc.SetInferenceKeyMonthlyLimit(ctx, &gen.SetInferenceKeyMonthlyLimitPayload{
		OrganizationID: "org_limit_gateway", KeyType: "chat", MonthlyCredits: 275,
	})
	requireOopsCode(t, err, oops.CodeGatewayError)

	key, err := keys.GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{OrganizationID: "org_limit_gateway", KeyType: "chat"})
	require.NoError(t, err)
	require.EqualValues(t, 50, key.MonthlyCredits)
	count, err := audittest.AuditLogCountByAction(ctx, db, audit.ActionOpenRouterAPIKeySetSpendCap)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestSetInferenceKeyMonthlyLimitWritesAdminAuditEntry(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_limit_audit", name: "Inference Limit Audit", slug: "inference-limit-audit"})
	_, err := orrepo.New(db).CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: "org_limit_audit", KeyType: "internal", KeyEncrypted: pgtype.Text{}, KeyHash: "hash-internal-audit", MonthlyCredits: 50,
	})
	require.NoError(t, err)
	svc.openRouterLimit = &fakeOpenRouterLimitUpdater{}
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID: "session-limit-audit", Email: "operator@example.test", OIDCSubject: "oidc-subject-limit-audit", Name: "Test Operator", HD: "example.test",
	})

	_, err = svc.SetInferenceKeyMonthlyLimit(ctx, &gen.SetInferenceKeyMonthlyLimitPayload{
		OrganizationID: "org_limit_audit", KeyType: "internal", MonthlyCredits: 275,
	})
	require.NoError(t, err)

	entry, err := audittest.LatestAuditLogByAction(ctx, db, audit.ActionOpenRouterAPIKeySetSpendCap)
	require.NoError(t, err)
	require.Equal(t, "org_limit_audit", entry.OrganizationID)
	require.Equal(t, "oidc-subject-limit-audit", entry.ActorID)
	require.Equal(t, "user", entry.ActorType)
	require.NotNil(t, entry.ActorDisplayName)
	require.Equal(t, audit.SpeakeasyTeamActorLabel, *entry.ActorDisplayName)
	require.Equal(t, "openrouter_api_key", entry.SubjectType)
	require.Equal(t, "Security inference cap", entry.SubjectDisplay)

	var metadata struct {
		KeyType string `json:"key_type"`
	}
	require.NoError(t, json.Unmarshal(entry.Metadata, &metadata))
	require.Equal(t, "internal", metadata.KeyType)
	before, err := audittest.DecodeAuditData(entry.BeforeSnapshot)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.InDelta(t, 50, before["monthly_credits"], 0)
	require.InDelta(t, 275, after["monthly_credits"], 0)
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

func TestSetInferenceKeyMonthlyLimitRejectsAbsentAndDisabledKeys(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedOrg(t, ctx, db, orgFixture{id: "org_limit_reject", name: "Inference Limit Reject", slug: "inference-limit-reject"})
	updater := &fakeOpenRouterLimitUpdater{}
	svc.openRouterLimit = updater

	_, err := svc.SetInferenceKeyMonthlyLimit(ctx, &gen.SetInferenceKeyMonthlyLimitPayload{
		OrganizationID: "org_limit_reject", KeyType: "internal", MonthlyCredits: 100,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.False(t, updater.called)

	_, err = orrepo.New(db).CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: "org_limit_reject", KeyType: "internal", KeyEncrypted: pgtype.Text{}, KeyHash: "hash-disabled", MonthlyCredits: 50,
	})
	require.NoError(t, err)
	require.NoError(t, orrepo.New(db).DisableOpenRouterAPIKey(ctx, orrepo.DisableOpenRouterAPIKeyParams{OrganizationID: "org_limit_reject", KeyType: "internal"}))

	_, err = svc.SetInferenceKeyMonthlyLimit(ctx, &gen.SetInferenceKeyMonthlyLimitPayload{
		OrganizationID: "org_limit_reject", KeyType: "internal", MonthlyCredits: 100,
	})
	requireOopsCode(t, err, oops.CodeConflict)
	require.False(t, updater.called)
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
