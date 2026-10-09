package admin

import (
	"context"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

//nolint:paralleltest // Cases reuse organization IDs in the shared Redis database and must remain sequential.
func TestAccountTypeTransitionMatrix(t *testing.T) {
	for _, target := range []string{"free", "pro", "payg", "enterprise"} {
		for _, bulk := range []bool{false, true} {
			t.Run(target+map[bool]string{true: "/bulk", false: "/single"}[bulk], func(t *testing.T) {
				ctx, svc, db := newTestAdminService(t)
				id := "org_tier_matrix"
				seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: id, accountType: target})
				if target == "payg" {
					require.NoError(t, usagerepo.New(db).CreateStripeBillingMetadataFixture(ctx, usagerepo.CreateStripeBillingMetadataFixtureParams{OrganizationID: id, StripeCustomerID: conv.ToPGText("cus_test")}))
					require.NoError(t, usagerepo.New(db).SetStripeSubscriptionFixture(ctx, usagerepo.SetStripeSubscriptionFixtureParams{OrganizationID: id, StripeSubscriptionID: conv.ToPGText("sub_test")}))
					svc.billing = &fakeBillingOperations{subscriptionByID: &stripeclient.SubscriptionState{ID: "sub_test", CustomerID: "cus_test", Status: "active", BillingCycleAnchor: time.Now()}}
				}
				for _, kt := range openrouter.AllKeyTypes {
					seedOpenRouterKey(t, ctx, db, id, keyFixture{keyType: kt, monthlyCredits: 1})
					require.NoError(t, testrepo.New(db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: id, KeyType: string(kt), Disabled: true, DisableCauses: []string{"trial_demotion", "billing_inactive", "admin_lock", "future_cause"}}))
				}
				if bulk {
					_, err := svc.BulkUpdateAccountType(ctx, &gen.BulkUpdateAccountTypePayload{Ids: []string{id}, AccountType: target})
					require.NoError(t, err)
				} else {
					_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: id, AccountType: &target})
					require.NoError(t, err)
				}
				for _, feature := range productfeatures.TrialRuntimeFeatures {
					enabled, err := svc.productFeatures.IsFeatureEnabledUncached(ctx, id, feature)
					require.NoError(t, err)
					require.Equal(t, target == "payg" || target == "enterprise", enabled)
				}
				for _, kt := range openrouter.AllKeyTypes {
					key := readOpenRouterKey(t, ctx, db, id, kt)
					credits := int64(100)
					if target == "free" {
						credits = 5
					}
					require.Equal(t, credits, key.MonthlyCredits)
					require.Contains(t, key.DisableCauses, "admin_lock")
					require.Contains(t, key.DisableCauses, "future_cause")
					require.True(t, key.Disabled)
					if target == "payg" || target == "enterprise" {
						require.NotContains(t, key.DisableCauses, "trial_demotion")
						if target == "payg" && kt == openrouter.KeyTypeChat {
							require.NotContains(t, key.DisableCauses, "billing_inactive")
						}
					} else {
						require.Contains(t, key.DisableCauses, "trial_demotion")
					}
				}
			})
		}
	}
}

func TestAccountTypeUnsafeBillingRollsBack(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	for _, id := range []string{"org_tier_a", "org_tier_b"} {
		seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: id, accountType: "free"})
	}
	seedPaygCustomer(t, ctx, db, "org_tier_customer", "tier-customer", "cus_only")
	require.NoError(t, usagerepo.New(db).CreateStripeBillingMetadataFixture(ctx, usagerepo.CreateStripeBillingMetadataFixtureParams{OrganizationID: "org_tier_a", StripeCustomerID: conv.ToPGText("cus_test")}))
	require.NoError(t, usagerepo.New(db).SetStripeSubscriptionFixture(ctx, usagerepo.SetStripeSubscriptionFixtureParams{OrganizationID: "org_tier_a", StripeSubscriptionID: conv.ToPGText("sub_test")}))
	svc.billing = &fakeBillingOperations{subscriptionByID: &stripeclient.SubscriptionState{ID: "sub_test", CustomerID: "cus_test", Status: "active", BillingCycleAnchor: time.Now()}}
	target := "payg"
	_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: "org_tier_customer", AccountType: &target})
	require.Error(t, err)
	_, err = svc.BulkUpdateAccountType(ctx, &gen.BulkUpdateAccountTypePayload{Ids: []string{"org_tier_a", "org_tier_b"}, AccountType: target})
	require.Error(t, err)
	require.Equal(t, "free", readOrgState(t, ctx, db, "org_tier_a").GramAccountType)
	require.Equal(t, "free", readOrgState(t, ctx, db, "org_tier_b").GramAccountType)
}

func TestAccountTypeAuditOutboxRollback(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"audit", "outbox"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			ctx, svc, db := newTestAdminService(t)
			id := "org_tier_rollback"
			seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: id, accountType: "free"})
			seedOpenRouterKey(t, ctx, db, id, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 5})
			before := readOrgState(t, ctx, db, id)
			key := readOpenRouterKey(t, ctx, db, id, openrouter.KeyTypeChat)
			if failure == "audit" {
				require.NoError(t, audittest.RejectAction(ctx, db, audit.ActionOrganizationAccountTypeChanged))
			} else {
				require.NoError(t, testrepo.New(db).RejectPublishOutboxWritesFixture(ctx))
			}
			target := "enterprise"
			whitelist := true
			_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: id, AccountType: &target, Whitelisted: &whitelist})
			require.Error(t, err)
			require.Equal(t, before, readOrgState(t, ctx, db, id))
			require.Equal(t, key, readOpenRouterKey(t, ctx, db, id, openrouter.KeyTypeChat))
			count, err := audittest.AuditLogCountByAction(ctx, db, audit.ActionOrganizationAccountTypeChanged)
			require.NoError(t, err)
			require.Zero(t, count)
		})
	}
}

func TestAccountTypeFreeAddsBillingCauseOnlyToChat(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	id := "org_tier_free"
	seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: id, accountType: "pro"})
	for _, kt := range openrouter.AllKeyTypes {
		seedOpenRouterKey(t, ctx, db, id, keyFixture{keyType: kt, monthlyCredits: 100})
	}
	target := "free"
	_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: id, AccountType: &target})
	require.NoError(t, err)
	for _, kt := range openrouter.AllKeyTypes {
		key := readOpenRouterKey(t, ctx, db, id, kt)
		require.Equal(t, int64(5), key.MonthlyCredits)
		require.Equal(t, kt == openrouter.KeyTypeChat, key.Disabled)
	}
}

func TestAccountTypeCacheRefreshSurvivesCancellationAfterCommit(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	id := "org_tier_cache"
	seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: id, accountType: "free"})
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	redisClient.AddHook(&cancelOnRedisSetHook{cancel: cancel, called: make(chan struct{})})
	svc.productFeatures = productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), db, redisClient)
	target := "enterprise"
	_, _ = svc.UpdateOrganization(requestCtx, &gen.UpdateOrganizationPayload{ID: id, AccountType: &target})
	require.Equal(t, "enterprise", readOrgState(t, ctx, db, id).GramAccountType)
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		enabled, err := svc.productFeatures.IsFeatureEnabled(ctx, id, feature)
		require.NoError(t, err)
		require.True(t, enabled)
	}
	count, err := audittest.AuditLogCountByAction(ctx, db, audit.ActionOrganizationAccountTypeChanged)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestAccountTypeBillingFirstWriteLockOrder(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	id := "org_tier_billing_lock"
	seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: id, accountType: "free"})
	billingConn, err := db.Acquire(ctx)
	require.NoError(t, err)
	defer billingConn.Release()
	tx, err := billingConn.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction holds the billing lock across the competing request.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	require.NoError(t, usagerepo.New(tx).LockBillingMetadataOrganization(ctx, id))
	done := make(chan error, 1)
	go func() {
		target := "pro"
		_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: id, AccountType: &target})
		done <- err
	}()
	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	testenv.WaitForQueryBlockedBy(t, waitCtx, db, testenv.BackendPID(tx), "%billing-email:%")
	_, err = usagerepo.New(tx).UpsertBillingEmail(ctx, usagerepo.UpsertBillingEmailParams{OrganizationID: id, AlertEmail: conv.ToPGText("billing@example.test")})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("tier update remained blocked")
	}
}

//nolint:paralleltest // Cases reuse organization IDs in the shared Redis database and must remain sequential.
func TestAccountTypeDemotedProTrialToEnterprise(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		for _, remaining := range []string{"", "admin_lock", "future_cause", "billing_inactive"} {
			t.Run(map[bool]string{false: "single", true: "bulk"}[bulk]+"/"+remaining, func(t *testing.T) {
				ctx, svc, db := newTestAdminService(t)
				id := "org_demoted_pro_enterprise"
				seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: id, accountType: "free"})
				demotedAt := time.Now().UTC().Add(-time.Hour)
				seedTrial(t, ctx, db, trialFixture{orgID: id, tier: "pro", endsAt: demotedAt, demotedAt: &demotedAt})
				for _, kt := range openrouter.AllKeyTypes {
					seedOpenRouterKey(t, ctx, db, id, keyFixture{keyType: kt, monthlyCredits: 5})
					causes := []string{"trial_demotion"}
					if remaining != "" {
						causes = append(causes, remaining)
					}
					require.NoError(t, testrepo.New(db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: id, KeyType: string(kt), Disabled: true, DisableCauses: causes}))
				}
				target := "enterprise"
				if bulk {
					_, err := svc.BulkUpdateAccountType(ctx, &gen.BulkUpdateAccountTypePayload{Ids: []string{id}, AccountType: target})
					require.NoError(t, err)
				} else {
					_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: id, AccountType: &target})
					require.NoError(t, err)
				}
				require.Equal(t, target, readOrgState(t, ctx, db, id).GramAccountType)
				for _, kt := range openrouter.AllKeyTypes {
					key := readOpenRouterKey(t, ctx, db, id, kt)
					require.NotContains(t, key.DisableCauses, "trial_demotion")
					require.Equal(t, int64(100), key.MonthlyCredits)
					require.Equal(t, remaining != "", key.Disabled)
					if remaining == "" {
						require.Empty(t, key.DisableCauses)
					} else {
						require.Equal(t, []string{remaining}, key.DisableCauses)
					}
				}
				for _, feature := range productfeatures.TrialRuntimeFeatures {
					enabled, err := svc.productFeatures.IsFeatureEnabledUncached(ctx, id, feature)
					require.NoError(t, err)
					require.True(t, enabled)
				}
				record, err := audittest.LatestAuditLogByAction(ctx, db, audit.ActionOrganizationAccountTypeChanged)
				require.NoError(t, err)
				require.JSONEq(t, `{"operation":"account_type_change"}`, string(record.Metadata))
				envelope, err := auditrepo.New(db).GetLatestOutboxPayloadByOrg(ctx, auditrepo.GetLatestOutboxPayloadByOrgParams{OrganizationID: id, EventType: string(events.OrganizationAccountTypeV1.EventType())})
				require.NoError(t, err)
				require.NotEmpty(t, envelope)
			})
		}
	}
}
