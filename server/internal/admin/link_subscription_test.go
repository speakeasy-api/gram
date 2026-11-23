package admin

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	adminrepo "github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func TestManualSubscriptionActivationLifecycleFree(t *testing.T) {
	t.Parallel()
	testManualSubscriptionActivationLifecycle(t, "free")
}

func TestManualSubscriptionActivationLifecycleRunningTrial(t *testing.T) {
	t.Parallel()
	testManualSubscriptionActivationLifecycle(t, "running_trial")
}

func TestManualSubscriptionActivationLifecycleDemotedTrial(t *testing.T) {
	t.Parallel()
	testManualSubscriptionActivationLifecycle(t, "demoted_trial")
}

func testManualSubscriptionActivationLifecycle(t *testing.T, scenario string) {
	t.Helper()
	ctx, svc, db := newTestAdminService(t)
	id := "org_manual_" + scenario
	now := time.Now().UTC()
	tier := "free"
	if scenario == "running_trial" {
		tier = "enterprise"
	}
	seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: id, accountType: tier, whitelisted: scenario == "running_trial", disabledAt: &now})
	if scenario != "free" {
		trial := trialFixture{orgID: id, endsAt: now.Add(time.Hour)}
		if scenario == "demoted_trial" {
			trial.demotedAt = &now
		}
		seedTrial(t, ctx, db, trial)
	}
	require.NoError(t, usagerepo.New(db).CreateStripeBillingMetadataFixture(ctx, usagerepo.CreateStripeBillingMetadataFixtureParams{OrganizationID: id, StripeCustomerID: conv.ToPGText("cus_" + scenario)}))
	fake := enableStripeCustomerLookup(svc)
	fake.subscriptionByID = subscriptionFor("cus_"+scenario, "sub_"+scenario, now)
	for _, kt := range openrouter.AllKeyTypes {
		seedOpenRouterKey(t, ctx, db, id, keyFixture{keyType: kt, monthlyCredits: 1})
		causes := []string{"admin_lock"}
		if kt == openrouter.KeyTypeChat {
			causes = append(causes, "billing_inactive")
		}
		if scenario == "demoted_trial" {
			causes = append(causes, "trial_demotion")
		}
		require.NoError(t, testrepo.New(db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: id, KeyType: string(kt), Disabled: true, DisableCauses: causes}))
	}
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, db, id))
	payload := &gen.SetStripeSubscriptionPayload{OrganizationID: id, StripeSubscriptionID: "sub_" + scenario}
	result, err := svc.SetStripeSubscription(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "payg", result.AccountType)
	require.True(t, result.Whitelisted)
	require.True(t, readOrgState(t, ctx, db, id).DisabledAt.Valid, "activation must preserve staff disable")
	if scenario != "free" {
		require.True(t, readTrial(t, ctx, db, id).ConvertedAt.Valid)
	}
	enabled, err := svc.productFeatures.IsFeatureEnabled(ctx, id, productfeatures.FeaturePlatformMCP)
	require.NoError(t, err)
	require.True(t, enabled)
	metadata, err := usagerepo.New(db).GetBillingMetadata(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "sub_"+scenario, metadata.StripeSubscriptionID.String)
	require.WithinDuration(t, now, metadata.StripeBillingCycleAnchor.Time, time.Microsecond)
	require.EqualValues(t, now.Day(), metadata.BillingCycleAnchorDay)
	for _, kt := range openrouter.AllKeyTypes {
		key := readOpenRouterKey(t, ctx, db, id, kt)
		require.True(t, key.Disabled)
		require.Equal(t, []string{"admin_lock"}, key.DisableCauses)
	}
	_, err = auditrepo.New(db).GetLatestOutboxPayloadByOrg(ctx, auditrepo.GetLatestOutboxPayloadByOrgParams{OrganizationID: id, EventType: string(events.OrganizationAccountTypeV1.EventType())})
	require.NoError(t, err, "provider policy reconciliation must be durable")
	count, err := audittest.AuditLogCount(ctx, db)
	require.NoError(t, err)
	// Retry preserves the original anchor even if Stripe's live anchor moves.
	fake.subscriptionByID.BillingCycleAnchor = now.Add(24 * time.Hour)
	_, err = svc.SetStripeSubscription(ctx, payload)
	require.NoError(t, err)
	after, err := usagerepo.New(db).GetBillingMetadata(ctx, id)
	require.NoError(t, err)
	require.Equal(t, metadata, after)
	afterCount, err := audittest.AuditLogCount(ctx, db)
	require.NoError(t, err)
	require.Equal(t, count, afterCount)
}

func TestManualSubscriptionActivationRollbackAudit(t *testing.T) {
	t.Parallel()
	testManualSubscriptionActivationRollback(t, "audit")
}

func TestManualSubscriptionActivationRollbackOutbox(t *testing.T) {
	t.Parallel()
	testManualSubscriptionActivationRollback(t, "outbox")
}

func TestManualSubscriptionActivationRollbackUnclassifiedKey(t *testing.T) {
	t.Parallel()
	testManualSubscriptionActivationRollback(t, "unclassified_key")
}

func testManualSubscriptionActivationRollback(t *testing.T, failure string) {
	t.Helper()
	ctx, svc, db := newTestAdminService(t)
	id := "org_manual_rollback_" + failure
	now := time.Now().UTC()
	seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: id, accountType: "free"})
	seedTrial(t, ctx, db, trialFixture{orgID: id, endsAt: now, demotedAt: &now})
	require.NoError(t, usagerepo.New(db).CreateStripeBillingMetadataFixture(ctx, usagerepo.CreateStripeBillingMetadataFixtureParams{OrganizationID: id, StripeCustomerID: conv.ToPGText("cus_" + failure)}))
	fake := enableStripeCustomerLookup(svc)
	fake.subscriptionByID = subscriptionFor("cus_"+failure, "sub_"+failure, now)
	seedOpenRouterKey(t, ctx, db, id, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 1})
	require.NoError(t, testrepo.New(db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: id, KeyType: string(openrouter.KeyTypeChat), Disabled: true, DisableCauses: []string{"trial_demotion"}}))
	switch failure {
	case "audit":
		require.NoError(t, audittest.RejectAction(ctx, db, audit.ActionOrganizationPaygActivated))
	case "outbox":
		err := testrepo.New(db).RejectPublishOutboxWritesFixture(ctx)
		require.NoError(t, err)
	case "unclassified_key":
		require.NoError(t, testrepo.New(db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: id, KeyType: string(openrouter.KeyTypeChat), Disabled: true, DisableCauses: nil}))
	}
	orgBefore, trialBefore, keyBefore := readOrgState(t, ctx, db, id), readTrial(t, ctx, db, id), readOpenRouterKey(t, ctx, db, id, openrouter.KeyTypeChat)
	_, err := svc.SetStripeSubscription(ctx, &gen.SetStripeSubscriptionPayload{OrganizationID: id, StripeSubscriptionID: "sub_" + failure})
	require.Error(t, err)
	require.Equal(t, orgBefore, readOrgState(t, ctx, db, id))
	require.Equal(t, trialBefore, readTrial(t, ctx, db, id))
	require.Equal(t, keyBefore, readOpenRouterKey(t, ctx, db, id, openrouter.KeyTypeChat))
	metadata, err := usagerepo.New(db).GetBillingMetadata(ctx, id)
	require.NoError(t, err)
	require.False(t, metadata.StripeSubscriptionID.Valid)
	count, err := audittest.AuditLogCount(ctx, db)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestManualSubscriptionActivationRejectsInactiveSubscription(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedPaygCustomer(t, ctx, db, "org_manual_inactive", "manual-inactive", "cus_inactive")
	fake := enableStripeCustomerLookup(svc)
	fake.subscriptionByID = subscriptionFor("cus_inactive", "sub_inactive", time.Now())
	fake.subscriptionByID.Status = "canceled"
	_, err := svc.SetStripeSubscription(ctx, &gen.SetStripeSubscriptionPayload{OrganizationID: "org_manual_inactive", StripeSubscriptionID: "sub_inactive"})
	requireOopsCode(t, err, oops.CodeConflict)
	metadata, err := usagerepo.New(db).GetBillingMetadata(ctx, "org_manual_inactive")
	require.NoError(t, err)
	require.False(t, metadata.StripeSubscriptionID.Valid)
}

func TestManualSubscriptionActivationRetriesPostCommitFailure(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	const id = "org_manual_cache_retry"
	seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: id, accountType: "free"})
	require.NoError(t, usagerepo.New(db).CreateStripeBillingMetadataFixture(ctx, usagerepo.CreateStripeBillingMetadataFixtureParams{OrganizationID: id, StripeCustomerID: conv.ToPGText("cus_cache_retry")}))
	fake := enableStripeCustomerLookup(svc)
	fake.subscriptionByID = subscriptionFor("cus_cache_retry", "sub_cache_retry", time.Now())
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	redisClient.AddHook(failAccountTypeCacheSetHook{})
	originalFeatures := svc.productFeatures
	svc.productFeatures = productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), db, redisClient)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, db, id))
	payload := &gen.SetStripeSubscriptionPayload{OrganizationID: id, StripeSubscriptionID: "sub_cache_retry"}
	_, err = svc.SetStripeSubscription(ctx, payload)
	require.ErrorContains(t, err, "subscription committed")
	require.Equal(t, "payg", readOrgState(t, ctx, db, id).GramAccountType)
	count, err := audittest.AuditLogCount(ctx, db)
	require.NoError(t, err)
	svc.productFeatures = originalFeatures
	_, err = svc.SetStripeSubscription(ctx, payload)
	require.NoError(t, err)
	afterCount, err := audittest.AuditLogCount(ctx, db)
	require.NoError(t, err)
	require.Equal(t, count, afterCount)
}

type subscriptionStatusChangesAfterPreview struct {
	*fakeBillingOperations
	calls int
}

func (f *subscriptionStatusChangesAfterPreview) GetStripeSubscriptionByID(ctx context.Context, id string, options ...stripeclient.GetSubscriptionOption) (*stripeclient.SubscriptionState, error) {
	state, err := f.fakeBillingOperations.GetStripeSubscriptionByID(ctx, id, options...)
	f.calls++
	if err == nil && f.calls > 1 {
		updated := *state
		updated.Status = "canceled"
		return &updated, nil
	}
	return state, err
}
func TestManualSubscriptionActivationRevalidatesLiveStatusUnderLock(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedPaygCustomer(t, ctx, db, "org_manual_status_race", "manual-status-race", "cus_status_race")
	fake := enableStripeCustomerLookup(svc)
	fake.subscriptionByID = subscriptionFor("cus_status_race", "sub_status_race", time.Now())
	svc.billing = &subscriptionStatusChangesAfterPreview{fakeBillingOperations: fake}
	_, err := svc.SetStripeSubscription(ctx, &gen.SetStripeSubscriptionPayload{OrganizationID: "org_manual_status_race", StripeSubscriptionID: "sub_status_race"})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Equal(t, [][]stripeclient.GetSubscriptionOption{{stripeclient.RequireAdoptionEligibility}, {stripeclient.RequireAdoptionEligibility}}, fake.subscriptionLookupOptions)
	metadata, err := usagerepo.New(db).GetBillingMetadata(ctx, "org_manual_status_race")
	require.NoError(t, err)
	require.False(t, metadata.StripeSubscriptionID.Valid)
}

func TestManualSubscriptionActivationRetriesConcurrentFirstTrial(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	const id = "org_manual_first_trial"
	seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: id, accountType: "free"})
	require.NoError(t, usagerepo.New(db).CreateStripeBillingMetadataFixture(ctx, usagerepo.CreateStripeBillingMetadataFixtureParams{OrganizationID: id, StripeCustomerID: conv.ToPGText("cus_first_trial")}))
	fake := enableStripeCustomerLookup(svc)
	fake.subscriptionByID = subscriptionFor("cus_first_trial", "sub_first_trial", time.Now())
	// Reproduce StartTrial's first-creation boundary: it owns the key locks
	// before inserting a previously absent lifecycle row.
	startTrialTx := testenv.BeginTx(t, ctx, db)
	require.NoError(t, openrouter.AcquireAPIKeyBillingTransactionLock(ctx, startTrialTx, id, openrouter.AllKeyTypes[0]))
	result := make(chan error, 1)
	payload := &gen.SetStripeSubscriptionPayload{OrganizationID: id, StripeSubscriptionID: "sub_first_trial"}
	go func() { _, err := svc.SetStripeSubscription(ctx, payload); result <- err }()
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	testenv.WaitForQueryBlockedBy(t, waitCtx, db, testenv.BackendPID(startTrialTx), "%AcquireOpenRouterBillingLock%")
	seedTrial(t, ctx, startTrialTx, trialFixture{orgID: id, endsAt: time.Now().Add(time.Hour)})
	require.NoError(t, adminrepo.New(startTrialTx).AdminUpdateOrganization(ctx, adminrepo.AdminUpdateOrganizationParams{ID: id, AccountType: conv.ToPGText("enterprise"), Whitelisted: pgtype.Bool{Bool: true, Valid: true}}))
	require.NoError(t, startTrialTx.Commit(ctx))
	select {
	case err := <-result:
		requireOopsCode(t, err, oops.CodeConflict)
		require.ErrorContains(t, err, "trial lifecycle changed")
	case <-waitCtx.Done():
		t.Fatal("subscription activation did not finish after trial commit")
	}
	require.Equal(t, "enterprise", readOrgState(t, ctx, db, id).GramAccountType)
	require.False(t, readTrial(t, ctx, db, id).ConvertedAt.Valid)
	metadata, err := usagerepo.New(db).GetBillingMetadata(ctx, id)
	require.NoError(t, err)
	require.False(t, metadata.StripeSubscriptionID.Valid)
	_, err = svc.SetStripeSubscription(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "payg", readOrgState(t, ctx, db, id).GramAccountType)
	require.True(t, readTrial(t, ctx, db, id).ConvertedAt.Valid)
}

func TestManualSubscriptionAssignmentEligibilityFree(t *testing.T) {
	t.Parallel()
	testManualSubscriptionAssignmentEligibility(t, subscriptionAssignmentCase{tier: "free", allowed: true})
}

func TestManualSubscriptionAssignmentEligibilityPaygRepair(t *testing.T) {
	t.Parallel()
	testManualSubscriptionAssignmentEligibility(t, subscriptionAssignmentCase{tier: "payg", allowed: true})
}

func TestManualSubscriptionAssignmentEligibilityPaidEnterprise(t *testing.T) {
	t.Parallel()
	testManualSubscriptionAssignmentEligibility(t, subscriptionAssignmentCase{tier: "enterprise"})
}

func TestManualSubscriptionAssignmentEligibilityConvertedEnterprise(t *testing.T) {
	t.Parallel()
	testManualSubscriptionAssignmentEligibility(t, subscriptionAssignmentCase{tier: "enterprise", trial: true, converted: true, whitelisted: true})
}

func TestManualSubscriptionAssignmentEligibilityRunningEnterpriseTrial(t *testing.T) {
	t.Parallel()
	testManualSubscriptionAssignmentEligibility(t, subscriptionAssignmentCase{tier: "enterprise", trial: true, whitelisted: true, allowed: true})
}

func TestManualSubscriptionAssignmentEligibilityDemotedTrial(t *testing.T) {
	t.Parallel()
	testManualSubscriptionAssignmentEligibility(t, subscriptionAssignmentCase{tier: "free", trial: true, allowed: true})
}

func TestManualSubscriptionAssignmentEligibilityInconsistentEnterpriseTrial(t *testing.T) {
	t.Parallel()
	testManualSubscriptionAssignmentEligibility(t, subscriptionAssignmentCase{tier: "enterprise", trial: true})
}

func TestManualSubscriptionAssignmentEligibilityInconsistentFreeTrial(t *testing.T) {
	t.Parallel()
	testManualSubscriptionAssignmentEligibility(t, subscriptionAssignmentCase{tier: "free", trial: true, whitelisted: true})
}

func TestManualSubscriptionAssignmentEligibilityInconsistentPAYGTrial(t *testing.T) {
	t.Parallel()
	testManualSubscriptionAssignmentEligibility(t, subscriptionAssignmentCase{tier: "payg", trial: true})
}

func TestManualSubscriptionAssignmentEligibilityConvertedPAYGTrial(t *testing.T) {
	t.Parallel()
	testManualSubscriptionAssignmentEligibility(t, subscriptionAssignmentCase{tier: "payg", trial: true, converted: true, allowed: true})
}

func TestManualSubscriptionAssignmentEligibilityPro(t *testing.T) {
	t.Parallel()
	testManualSubscriptionAssignmentEligibility(t, subscriptionAssignmentCase{tier: "pro"})
}

func testManualSubscriptionAssignmentEligibility(t *testing.T, tc subscriptionAssignmentCase) {
	t.Helper()
	org := adminrepo.AdminGetOrganizationRow{AccountType: tc.tier, Whitelisted: tc.whitelisted, StripeCustomerID: conv.ToPGText("cus_eligibility")}
	if tc.trial {
		org.TrialTier = conv.ToPGText("enterprise")
	}
	org.TrialConvertedAt.Valid = tc.converted
	err := requireStripeSubscriptionAssignment(org, "sub_eligibility")
	if tc.allowed {
		require.NoError(t, err)
	} else {
		requireOopsCode(t, err, oops.CodeConflict)
	}
}

type subscriptionAssignmentCase struct {
	tier                                   string
	trial, converted, whitelisted, allowed bool
}
