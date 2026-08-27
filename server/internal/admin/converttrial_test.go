package admin

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	orrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

func TestMarkEnterpriseTrialConvertedRequestBody_RequiresOrganizationIDOnly(t *testing.T) {
	t.Parallel()

	id := "org_convert_validate"
	require.NoError(t, srv.ValidateMarkEnterpriseTrialConvertedRequestBody(&srv.MarkEnterpriseTrialConvertedRequestBody{ID: &id}))
	require.Error(t, srv.ValidateMarkEnterpriseTrialConvertedRequestBody(&srv.MarkEnterpriseTrialConvertedRequestBody{}))
	require.Error(t, srv.ValidateMarkEnterpriseTrialConvertedRequestBody(&srv.MarkEnterpriseTrialConvertedRequestBody{ID: new(string)}))
}

func TestMarkEnterpriseTrialConverted_AcceptsEveryEligibleStateAndPreservesHistory(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	demotedAt := now.Add(-9 * 24 * time.Hour)
	disabledAt := now.Add(-8 * 24 * time.Hour)
	cases := []struct {
		name       string
		endsAt     time.Time
		demotedAt  *time.Time
		disabledAt *time.Time
	}{
		{name: "running", endsAt: now.Add(14 * 24 * time.Hour)},
		{name: "ending soon", endsAt: now.Add(2 * 24 * time.Hour)},
		{name: "expired", endsAt: now.Add(-2 * 24 * time.Hour)},
		{name: "demoted and disabled", endsAt: now.Add(-10 * 24 * time.Hour), demotedAt: &demotedAt, disabledAt: &disabledAt},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, svc, conn, _ := newRearmService(t)
			orgID := "org_convert_" + tc.name
			accountType, whitelisted := "enterprise", true
			if tc.demotedAt != nil {
				accountType, whitelisted = "free", false
			}
			seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID + " Name", slug: orgID + "-slug", accountType: accountType, whitelisted: whitelisted, disabledAt: tc.disabledAt})
			seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: tc.endsAt, demotedAt: tc.demotedAt})

			before := readTrial(t, ctx, conn, orgID)
			started := time.Now().UTC()
			res, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
			finished := time.Now().UTC()
			require.NoError(t, err)
			require.Equal(t, orgID, res.ID)

			after := readTrial(t, ctx, conn, orgID)
			require.True(t, after.ConvertedAt.Valid)
			require.False(t, after.ConvertedAt.Time.Before(started))
			require.False(t, after.ConvertedAt.Time.After(finished))
			require.Equal(t, before.EndsAt.Time, after.EndsAt.Time)
			require.Equal(t, before.DemotedAt, after.DemotedAt)

			state := readOrgState(t, ctx, conn, orgID)
			require.Equal(t, "enterprise", state.GramAccountType)
			require.True(t, state.Whitelisted)
			if tc.disabledAt != nil {
				require.True(t, state.DisabledAt.Valid)
				require.WithinDuration(t, *tc.disabledAt, state.DisabledAt.Time, 0)
			}
		})
	}
}

func TestMarkEnterpriseTrialConverted_RestoresDemotedFeaturesAndKeysWithoutClearingOtherCauses(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)
	orgID := "org_convert_demoted_resources"
	seedDemotedTrial(t, ctx, conn, orgID, "enterprise")
	seedDisabledTrialRuntimeFeatures(t, ctx, svc, conn, orgID)

	// Add independent causes to the trial cause seeded by seedDemotedTrial.
	for _, keyType := range openrouter.AllKeyTypes {
		for _, cause := range []openrouter.DisableCause{openrouter.DisableCauseAdminLock, openrouter.DisableCauseBillingInactive} {
			_, err := orrepo.New(conn).AddOpenRouterAPIKeyDisableCause(ctx, orrepo.AddOpenRouterAPIKeyDisableCauseParams{
				OrganizationID: orgID,
				KeyType:        string(keyType),
				DisableCause:   string(cause),
			})
			require.NoError(t, err)
		}
	}

	_, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	require.NoError(t, err)

	for _, keyType := range openrouter.AllKeyTypes {
		key := readOpenRouterKey(t, ctx, conn, orgID, keyType)
		require.EqualValues(t, 100, key.MonthlyCredits)
		require.NotContains(t, key.DisableCauses, string(openrouter.DisableCauseTrialDemotion))
		require.Contains(t, key.DisableCauses, string(openrouter.DisableCauseAdminLock))
		require.Contains(t, key.DisableCauses, string(openrouter.DisableCauseBillingInactive))
	}
	require.Len(t, provisioner.revivals, len(openrouter.AllKeyTypes))
	for _, call := range provisioner.revivals {
		require.NotNil(t, call.limit)
		require.Equal(t, 100, *call.limit, "conversion must send the explicit enterprise ceiling upstream before commit")
		require.Equal(t, "free", call.accountTypeSeen, "upstream work must precede admission commit")
		require.True(t, call.demotedSeen, "upstream work must precede trial conversion commit")
	}

	for _, feature := range productfeatures.TrialRuntimeFeatures {
		enabled, err := svc.productFeatures.IsFeatureEnabled(ctx, orgID, feature)
		require.NoError(t, err)
		require.True(t, enabled, "%s must be restored for a converted demoted trial", feature)
	}
}

func TestMarkEnterpriseTrialConverted_IsIdempotentAndAlwaysNotifiesInactive(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	orgID := "org_convert_idempotent"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Idempotent Co", slug: "idempotent-co", accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: time.Now().UTC().Add(10 * 24 * time.Hour)})
	notifier := &fakeTrialNotifier{inactiveErr: assertiveNotifierError{}}
	svc.trial = notifier
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "operator-placeholder", Name: "Test Operator", Email: "operator@example.test"})

	before, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	_, err = svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	require.NoError(t, err)
	first := readTrial(t, ctx, conn, orgID).ConvertedAt.Time
	_, err = svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	require.NoError(t, err)
	require.Equal(t, first, readTrial(t, ctx, conn, orgID).ConvertedAt.Time)
	require.Equal(t, []string{orgID, orgID}, notifier.inactive)

	after, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	var metadata map[string]string
	require.NoError(t, json.Unmarshal(entry.Metadata, &metadata))
	require.Equal(t, "admin", metadata["conversion_source"])
	require.Equal(t, "Test Operator", *entry.ActorDisplayName)
}

func TestMarkEnterpriseTrialConverted_RejectsMissingNoTrialAndPayg(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_convert_no_trial", name: "No Trial", slug: "no-trial", accountType: "enterprise", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_convert_payg", name: "PAYG", slug: "payg", accountType: "payg", whitelisted: true})
	convertedAt := time.Now().UTC().Add(-time.Hour)
	seedTrial(t, ctx, conn, trialFixture{orgID: "org_convert_payg", endsAt: time.Now().UTC().Add(10 * 24 * time.Hour), convertedAt: &convertedAt})

	_, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: "org_convert_missing"})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: "org_convert_no_trial"})
	requireOopsCode(t, err, oops.CodeConflict)
	_, err = svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: "org_convert_payg"})
	requireOopsCode(t, err, oops.CodeConflict)
	require.True(t, convertedAt.Equal(readTrial(t, ctx, conn, "org_convert_payg").ConvertedAt.Time))
}

type assertiveNotifierError struct{}

func (assertiveNotifierError) Error() string { return "notifier unavailable" }

func TestMarkEnterpriseTrialConverted_AuditFailureRollsBackConversionAndRestoration(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	orgID := "org_convert_audit_atomic"
	seedDemotedTrial(t, ctx, conn, orgID, "enterprise")
	before := readTrial(t, ctx, conn, orgID)
	require.NoError(t, audittest.RejectAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted))

	_, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	requireOopsCode(t, err, oops.CodeUnexpected)

	after := readTrial(t, ctx, conn, orgID)
	require.False(t, after.ConvertedAt.Valid)
	require.Equal(t, before.DemotedAt, after.DemotedAt)
	state := readOrgState(t, ctx, conn, orgID)
	require.Equal(t, "free", state.GramAccountType)
	require.False(t, state.Whitelisted)
}

func TestUpdateOrganization_DoesNotInferEnterpriseTrialConversion(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	orgID := "org_update_not_conversion"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Update Only", slug: "update-only", accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: time.Now().UTC().Add(10 * 24 * time.Hour)})
	enterprise := "enterprise"

	_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: orgID, AccountType: &enterprise})
	require.NoError(t, err)
	require.False(t, readTrial(t, ctx, conn, orgID).ConvertedAt.Valid)
}
