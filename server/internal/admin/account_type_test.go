package admin

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
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
	"google.golang.org/protobuf/proto"
	"slices"
	"testing"
	"time"
)

//nolint:paralleltest // Cases reuse organization IDs in the shared Redis database and must remain sequential.
func TestAccountTypeTransitionMatrix(t *testing.T) {
	tiers := []string{"free", "pro", "payg", "enterprise"}
	for _, source := range tiers {
		for _, target := range tiers {
			for _, bulk := range []bool{false, true} {
				for _, state := range []struct {
					name   string
					causes []string
				}{
					{"enabled", []string{}},
					{"trial_demotion", []string{"trial_demotion"}},
					{"billing_inactive", []string{"billing_inactive"}},
					{"admin_lock", []string{"admin_lock"}},
					{"unknown", []string{"future_cause"}},
					{"lifecycle_overlap", []string{"trial_demotion", "billing_inactive"}},
					{"mixed", []string{"trial_demotion", "billing_inactive", "admin_lock", "future_cause"}},
					{"unclassified", nil},
				} {
					t.Run(source+"_to_"+target+map[bool]string{true: "/bulk/", false: "/single/"}[bulk]+state.name, func(t *testing.T) {
						ctx, svc, db := newTestAdminService(t)
						id := "org_tier_matrix"
						seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: id, accountType: source})
						sourceRuntime := source == "payg" || source == "enterprise"
						tx := testenv.BeginTx(t, ctx, db)
						require.NoError(t, productfeatures.SetTrialRuntimeFeaturesTx(ctx, tx, id, sourceRuntime))
						require.NoError(t, tx.Commit(ctx))
						if target == "payg" {
							require.NoError(t, usagerepo.New(db).CreateStripeBillingMetadataFixture(ctx, usagerepo.CreateStripeBillingMetadataFixtureParams{OrganizationID: id, StripeCustomerID: conv.ToPGText("cus_test")}))
							require.NoError(t, usagerepo.New(db).SetStripeSubscriptionFixture(ctx, usagerepo.SetStripeSubscriptionFixtureParams{OrganizationID: id, StripeSubscriptionID: conv.ToPGText("sub_test")}))
							svc.billing = &fakeBillingOperations{subscriptionByID: &stripeclient.SubscriptionState{ID: "sub_test", CustomerID: "cus_test", Status: "active", BillingCycleAnchor: time.Now()}}
						}
						for _, kt := range openrouter.AllKeyTypes {
							seedOpenRouterKey(t, ctx, db, id, keyFixture{keyType: kt, monthlyCredits: 1})
							require.NoError(t, testrepo.New(db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: id, KeyType: string(kt), Disabled: state.causes == nil || len(state.causes) > 0, DisableCauses: state.causes}))
						}
						var err error
						if bulk {
							_, err = svc.BulkUpdateAccountType(ctx, &gen.BulkUpdateAccountTypePayload{Ids: []string{id}, AccountType: target})
						} else {
							_, err = svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: id, AccountType: &target})
						}
						envelope, outboxErr := auditrepo.New(db).GetLatestOutboxPayloadByOrg(ctx, auditrepo.GetLatestOutboxPayloadByOrgParams{OrganizationID: id, EventType: string(events.OrganizationAccountTypeV1.EventType())})
						if state.causes == nil {
							require.ErrorIs(t, err, openrouter.ErrAPIKeyDisableCausesUnclassified)
							require.ErrorIs(t, outboxErr, pgx.ErrNoRows, "failed policy must not schedule reconciliation")
							for _, kt := range openrouter.AllKeyTypes {
								key := readOpenRouterKey(t, ctx, db, id, kt)
								require.EqualValues(t, 1, key.MonthlyCredits)
								require.Nil(t, key.DisableCauses)
								require.True(t, key.Disabled)
							}
							return
						}
						require.NoError(t, err)
						require.NoError(t, outboxErr, "every successful policy application must durably schedule reconciliation")
						var event webhooksv1.Event
						require.NoError(t, proto.Unmarshal(envelope, &event))
						require.NotEmpty(t, event.GetEventId())
						require.Equal(t, id, event.GetOrganizationId())
						require.Equal(t, string(events.OrganizationAccountTypeV1.EventType()), event.GetEventType())
						var payload events.AuditLogCreatedPayloadV1
						require.NoError(t, json.Unmarshal(event.GetPayload(), &payload))
						require.Equal(t, id, payload.OrganizationID)
						require.Equal(t, id, payload.SubjectID)
						require.Equal(t, "organization", payload.SubjectType)
						require.Equal(t, string(audit.ActionOrganizationAccountTypeChanged), payload.Action)
						require.JSONEq(t, `{"operation":"account_type_change"}`, string(payload.Metadata))
						require.JSONEq(t, `{"account_type":"`+source+`"}`, string(payload.BeforeSnapshot))
						require.JSONEq(t, `{"account_type":"`+target+`"}`, string(payload.AfterSnapshot))
						for _, feature := range productfeatures.TrialRuntimeFeatures {
							enabled, err := svc.productFeatures.IsFeatureEnabledUncached(ctx, id, feature)
							require.NoError(t, err)
							require.Equal(t, sourceRuntime || target == "payg" || target == "enterprise", enabled)
						}
						for _, kt := range openrouter.AllKeyTypes {
							key := readOpenRouterKey(t, ctx, db, id, kt)
							credits := int64(100)
							if target == "free" {
								credits = 5
							}
							require.Equal(t, credits, key.MonthlyCredits)
							wantCauses := slices.Clone(state.causes)
							if target == "payg" || target == "enterprise" {
								wantCauses = slices.DeleteFunc(wantCauses, func(cause string) bool {
									return cause == "trial_demotion" || (target == "payg" && kt == openrouter.KeyTypeChat && cause == "billing_inactive")
								})
							}
							// Free chat without a subscription gains a billing hold; other causes are preserved.
							if target == "free" && kt == openrouter.KeyTypeChat && !slices.Contains(wantCauses, "billing_inactive") {
								wantCauses = append(wantCauses, "billing_inactive")
							}
							require.ElementsMatch(t, wantCauses, key.DisableCauses)
							require.Equal(t, len(wantCauses) > 0, key.Disabled)
						}
					})
				}
			}
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
