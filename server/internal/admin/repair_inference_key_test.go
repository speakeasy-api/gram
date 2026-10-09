package admin

import (
	"errors"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

//nolint:tparallel // Validation cases must finish before the parent mutates their shared key fixture.
func TestRepairInferenceKey(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	id := "org_repair_test"
	seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: "repair-slug", accountType: "enterprise"})
	seedOpenRouterKey(t, ctx, db, id, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 17})
	require.NoError(t, testrepo.New(db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: id, KeyType: "chat", Disabled: true, DisableCauses: []string{"admin_lock", "billing_inactive", "future_cause"}}))
	p := &gen.RepairInferenceKeyPayload{OrganizationID: id, KeyType: "chat", RemoveCauses: []string{"admin_lock"}, Confirmation: "I know what I'm doing", Reason: "BUG-123"}
	_, err := svc.RepairInferenceKey(ctx, p)
	require.Error(t, err)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	for _, bad := range []string{"confirmation", "reason", "selection", "duplicate", "unknown", "billing", "slug", "key"} {
		t.Run(bad, func(t *testing.T) {
			q := *p
			switch bad {
			case "confirmation":
				q.Confirmation = "I know what I’m doing"
			case "reason":
				q.Reason = " "
			case "selection":
				q.RemoveCauses = nil
			case "duplicate":
				q.RemoveCauses = []string{"admin_lock", "admin_lock"}
			case "unknown":
				q.RemoveCauses = []string{"future_cause"}
			case "billing":
				q.RemoveCauses = []string{"billing_inactive"}
			case "slug":
				q.OrganizationID = "repair-slug"
			case "key":
				q.KeyType = ""
			}
			_, err := svc.RepairInferenceKey(ctx, &q)
			require.Error(t, err)
		})
	}
	for range 2 { //nolint:paralleltest // Repeated repairs intentionally mutate the same key sequentially.
		result, err := svc.RepairInferenceKey(ctx, p)
		require.NoError(t, err)
		require.True(t, result.ReconciliationPending)
		require.ElementsMatch(t, []string{"billing_inactive", "future_cause"}, result.Key.DisableCauses)
		require.Equal(t, int64(17), result.Key.MonthlyCredits)
		require.True(t, result.Key.Disabled)
	}
	require.Equal(t, "enterprise", readOrgState(t, ctx, db, id).GramAccountType)
}

func TestRepairInferenceKeyRollback(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"audit", "outbox"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			ctx, svc, db := newTestAdminService(t)
			ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
			id := "org_repair_rollback"
			seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: id, accountType: "free"})
			seedOpenRouterKey(t, ctx, db, id, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 7})
			require.NoError(t, testrepo.New(db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: id, KeyType: "chat", Disabled: true, DisableCauses: []string{"admin_lock"}}))
			before := readOpenRouterKey(t, ctx, db, id, openrouter.KeyTypeChat)
			if failure == "audit" {
				require.NoError(t, audittest.RejectAction(ctx, db, audit.ActionOrganizationInferenceKeyRepaired))
			} else {
				require.NoError(t, testrepo.New(db).RejectPublishOutboxWritesFixture(ctx))
			}
			_, err := svc.RepairInferenceKey(ctx, &gen.RepairInferenceKeyPayload{OrganizationID: id, KeyType: "chat", RemoveCauses: []string{"admin_lock"}, Confirmation: "I know what I'm doing", Reason: "BUG-123"})
			require.Error(t, err)
			require.Equal(t, before, readOpenRouterKey(t, ctx, db, id, openrouter.KeyTypeChat))
			count, err := audittest.AuditLogCountByAction(ctx, db, audit.ActionOrganizationInferenceKeyRepaired)
			require.NoError(t, err)
			require.Zero(t, count)
		})
	}
}

func TestRepairInferenceKeyLiveBillingAndDiagnostics(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	id := "org_repair_billing"
	seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: id, accountType: "enterprise"})
	seedOpenRouterKey(t, ctx, db, id, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 19})
	require.NoError(t, testrepo.New(db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: id, KeyType: "chat", Disabled: true, DisableCauses: []string{"billing_inactive"}}))
	require.NoError(t, usagerepo.New(db).CreateStripeBillingMetadataFixture(ctx, usagerepo.CreateStripeBillingMetadataFixtureParams{OrganizationID: id, StripeCustomerID: conv.ToPGText("cus_test")}))
	require.NoError(t, usagerepo.New(db).SetStripeSubscriptionFixture(ctx, usagerepo.SetStripeSubscriptionFixtureParams{OrganizationID: id, StripeSubscriptionID: conv.ToPGText("sub_test")}))
	state := &stripeclient.SubscriptionState{ID: "sub_test", CustomerID: "cus_test", Status: "active", BillingCycleAnchor: time.Now()}
	fake := &fakeBillingOperations{subscriptionByID: state}
	svc.billing = fake
	org, err := repo.New(db).AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{ID: id, AllowSlug: false})
	require.NoError(t, err)
	require.True(t, svc.inferenceKeyCauseDiagnostics(ctx, org, []string{"billing_inactive"}, true)[0].Removable)
	p := &gen.RepairInferenceKeyPayload{OrganizationID: id, KeyType: "chat", RemoveCauses: []string{"billing_inactive"}, Confirmation: "I know what I'm doing", Reason: "BUG-123"}
	state.Status = "canceled" // Dialog was eligible; mutation must revalidate.
	_, err = svc.RepairInferenceKey(ctx, p)
	require.Error(t, err)
	fake.subscriptionErr = errors.New("provider unavailable secret")
	diagnostics := svc.inferenceKeyCauseDiagnostics(ctx, org, []string{"billing_inactive", "future_cause"}, true)
	for _, d := range diagnostics {
		require.False(t, d.Removable)
		require.NotNil(t, d.BlockedReason)
		require.NotContains(t, *d.BlockedReason, "secret")
	}
	svc.openRouterUsage = &fakeOpenRouterUsage{creditsByKeyType: map[openrouter.KeyType]float64{openrouter.KeyTypeChat: 1, openrouter.KeyTypeInternal: 0}}
	keys, readErr := svc.GetInferenceKeys(ctx, &gen.GetInferenceKeysPayload{OrganizationID: id})
	require.NoError(t, readErr)
	require.Len(t, keys, 1)
	require.False(t, keys[0].CauseDiagnostics[0].Removable)
	fake.subscriptionErr = nil
	state.Status = "active"
	before := map[string]bool{}
	for _, f := range productfeatures.TrialRuntimeFeatures {
		v, e := svc.productFeatures.IsFeatureEnabledUncached(ctx, id, f)
		require.NoError(t, e)
		before[string(f)] = v
	}
	result, err := svc.RepairInferenceKey(ctx, p)
	require.NoError(t, err)
	require.False(t, result.Key.Disabled)
	require.Equal(t, int64(19), result.Key.MonthlyCredits)
	for _, f := range productfeatures.TrialRuntimeFeatures {
		v, e := svc.productFeatures.IsFeatureEnabledUncached(ctx, id, f)
		require.NoError(t, e)
		require.Equal(t, before[string(f)], v)
	}
	record, err := audittest.LatestAuditLogByAction(ctx, db, audit.ActionOrganizationInferenceKeyRepaired)
	require.NoError(t, err)
	require.NotNil(t, record)
	require.JSONEq(t, `{"operation":"inference_key_repair","reason":"BUG-123","key_type":"chat","remove_causes":["billing_inactive"]}`, string(record.Metadata))
	require.Equal(t, "staff-test", record.ActorID)
	require.JSONEq(t, `{"key_type":"chat","monthly_credits":19,"disabled":true,"disable_causes":["billing_inactive"]}`, string(record.BeforeSnapshot))
	require.JSONEq(t, `{"key_type":"chat","monthly_credits":19,"disabled":false,"disable_causes":[]}`, string(record.AfterSnapshot))
}

func TestRepairInferenceKeyUnclassified(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	id := "org_repair_legacy"
	seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: id, accountType: "free"})
	seedOpenRouterKey(t, ctx, db, id, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 7})
	require.NoError(t, testrepo.New(db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: id, KeyType: "chat", Disabled: true, DisableCauses: nil}))
	before := readOpenRouterKey(t, ctx, db, id, openrouter.KeyTypeChat)
	_, err := svc.RepairInferenceKey(ctx, &gen.RepairInferenceKeyPayload{OrganizationID: id, KeyType: "chat", RemoveCauses: []string{"admin_lock"}, Confirmation: "I know what I'm doing", Reason: "BUG-123"})
	require.ErrorContains(t, err, "Unclassified")
	require.Equal(t, before, readOpenRouterKey(t, ctx, db, id, openrouter.KeyTypeChat))
}
