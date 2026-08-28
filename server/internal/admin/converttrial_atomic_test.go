package admin

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func TestMarkEnterpriseTrialConverted_AuditSnapshotsAreCompleteAndPrivate(t *testing.T) {
	t.Parallel()
	ctx, svc, conn, provisioner := newRearmService(t)
	orgID := "org_convert_audit"
	endsAt := time.Now().UTC().Add(7 * 24 * time.Hour)
	demotedAt := time.Now().UTC().Add(-time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Conversion Fixture", slug: "conversion-fixture", accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: endsAt, demotedAt: &demotedAt})
	for _, keyType := range openrouter.AllKeyTypes {
		seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: keyType, monthlyCredits: 7, disabled: true})
	}
	require.NoError(t, testrepo.New(conn).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat), Disabled: true, DisableCauses: []string{"trial_demotion", "billing_inactive", "admin_lock"}}))

	_, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	require.NoError(t, err)
	require.Equal(t, openrouter.AllKeyTypes, provisioner.reconcileAttempts)
	record, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(record.Metadata, &metadata))
	require.Equal(t, "platform_admin", metadata["conversion_source"])
	for _, payload := range [][]byte{record.BeforeSnapshot, record.AfterSnapshot} {
		var snapshot map[string]any
		require.NoError(t, json.Unmarshal(payload, &snapshot))
		require.Contains(t, snapshot, "organization")
		require.Contains(t, snapshot, "trial")
		require.Len(t, snapshot["keys"], len(openrouter.AllKeyTypes))
		text := string(payload)
		require.NotContains(t, text, "hash-")
		require.NotContains(t, text, "sk-test")
		require.NotContains(t, text, "cipher")
	}
}

func TestMarkEnterpriseTrialConverted_AuditFailureRollsBackEverything(t *testing.T) {
	t.Parallel()
	ctx, svc, conn, provisioner := newRearmService(t)
	orgID := "org_convert_audit_rollback"
	demotedAt := time.Now().UTC().Add(-time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: demotedAt, demotedAt: &demotedAt})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 7, disabled: true})
	beforeOrg, beforeTrial, beforeKey := readOrgState(t, ctx, conn, orgID), readTrial(t, ctx, conn, orgID), readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat)
	require.NoError(t, audittest.RejectAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted))

	_, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.Equal(t, beforeOrg, readOrgState(t, ctx, conn, orgID))
	require.Equal(t, beforeTrial, readTrial(t, ctx, conn, orgID))
	require.Equal(t, beforeKey, readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat))
	require.Empty(t, provisioner.reconcileAttempts)
}

func TestMarkEnterpriseTrialConverted_OutboxFailureRollsBackEverything(t *testing.T) {
	t.Parallel()
	ctx, svc, conn, provisioner := newRearmService(t)
	orgID := "org_convert_outbox_rollback"
	demotedAt := time.Now().UTC().Add(-time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: demotedAt, demotedAt: &demotedAt})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 7, disabled: true})
	beforeOrg, beforeTrial, beforeKey := readOrgState(t, ctx, conn, orgID), readTrial(t, ctx, conn, orgID), readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat)
	err := testrepo.New(conn).RejectPublishOutboxWritesFixture(ctx)
	require.NoError(t, err)

	_, err = svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.Equal(t, beforeOrg, readOrgState(t, ctx, conn, orgID))
	require.Equal(t, beforeTrial, readTrial(t, ctx, conn, orgID))
	require.Equal(t, beforeKey, readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat))
	count, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	require.Zero(t, count)
	require.Empty(t, provisioner.reconcileAttempts)
}

func TestMarkEnterpriseTrialConverted_UnclassifiedKeyRollsBack(t *testing.T) {
	t.Parallel()
	ctx, svc, conn, provisioner := newRearmService(t)
	orgID := "org_convert_null_key"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: time.Now().UTC().Add(time.Hour)})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 7})
	require.NoError(t, testrepo.New(conn).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat), Disabled: false, DisableCauses: nil}))

	_, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.False(t, readTrial(t, ctx, conn, orgID).ConvertedAt.Valid)
	require.Empty(t, provisioner.reconcileAttempts)
}

func TestMarkEnterpriseTrialConverted_PostCommitFailureRetryConverges(t *testing.T) {
	t.Parallel()
	ctx, svc, conn, provisioner := newRearmService(t)
	orgID := "org_convert_retry"
	demotedAt := time.Now().UTC().Add(-time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: demotedAt, demotedAt: &demotedAt})
	for _, keyType := range openrouter.AllKeyTypes {
		seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: keyType, monthlyCredits: 7, disabled: true})
	}
	provisioner.failOn, provisioner.failAfter, provisioner.failWith = openrouter.KeyTypeInternal, 1, errors.New("provider unavailable")

	_, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.True(t, readTrial(t, ctx, conn, orgID).ConvertedAt.Valid, "provider failure occurs after commit")
	count, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)

	provisioner.failWith = nil
	_, err = svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	require.NoError(t, err)
	count, err = audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}
