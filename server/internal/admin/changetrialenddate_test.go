package admin

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestChangeTrialEndDate(t *testing.T) {
	t.Parallel()
	for _, days := range []int{1, 30, 400} {
		t.Run(fmt.Sprintf("%d_days", days), func(t *testing.T) {
			t.Parallel()
			ctx, svc, conn := newTestAdminService(t)
			const orgID = "org_change_trial_end"
			seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Trial Example", slug: "trial-example", accountType: "enterprise"})
			seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: time.Now().UTC().Add(14 * 24 * time.Hour)})
			before := readTrial(t, ctx, conn, orgID)
			endsAt := time.Now().UTC().AddDate(0, 0, days).Truncate(24 * time.Hour)
			result, err := svc.ChangeTrialEndDate(ctx, &gen.ChangeTrialEndDatePayload{ID: orgID, EndsAt: endsAt.Format(time.RFC3339)})
			require.NoError(t, err)
			after := readTrial(t, ctx, conn, orgID)
			require.True(t, endsAt.Equal(after.EndsAt.Time))
			require.Equal(t, before.CreatedAt, after.CreatedAt)
			require.Equal(t, before.ConvertedAt, after.ConvertedAt)
			require.Equal(t, before.DemotedAt, after.DemotedAt)
			require.NotNil(t, result.TrialEndsAt)
			entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialEndChanged)
			require.NoError(t, err)
			var metadata struct {
				Previous time.Time `json:"previous_trial_ends_at"`
				Next     time.Time `json:"trial_ends_at"`
			}
			require.NoError(t, json.Unmarshal(entry.Metadata, &metadata))
			require.True(t, before.EndsAt.Time.Equal(metadata.Previous))
			require.True(t, endsAt.Equal(metadata.Next))
		})
	}
}

func TestChangeTrialEndDateRejectsInvalidDates(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	for _, value := range []string{"", "invalid", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)} {
		_, err := svc.ChangeTrialEndDate(ctx, &gen.ChangeTrialEndDatePayload{ID: "org_change_trial_end", EndsAt: value})
		requireOopsCode(t, err, oops.CodeInvalid)
	}
}

func TestChangeTrialEndDateRejectsInactiveTrials(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"expired", "converted", "demoted", "missing"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			ctx, svc, conn := newTestAdminService(t)
			const orgID = "org_change_inactive_trial"
			seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Inactive Trial", slug: "inactive-trial", accountType: "enterprise"})
			now := time.Now().UTC()
			fixture := trialFixture{orgID: orgID, endsAt: now.Add(24 * time.Hour)}
			switch state {
			case "expired":
				fixture.endsAt = now.Add(-time.Hour)
			case "converted":
				fixture.convertedAt = &now
			case "demoted":
				fixture.demotedAt = &now
			}
			if state != "missing" {
				seedTrial(t, ctx, conn, fixture)
			}
			_, err := svc.ChangeTrialEndDate(ctx, &gen.ChangeTrialEndDatePayload{ID: orgID, EndsAt: now.Add(48 * time.Hour).Format(time.RFC3339)})
			requireOopsCode(t, err, oops.CodeConflict)
		})
	}
}

func TestChangeTrialEndDate_AFailedAuditEntryRollsBackTheChange(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seededEndsAt := time.Now().UTC().Add(10 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: "org_ext_atomic", name: "Atomic Co", slug: "ext-atomic", accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: "org_ext_atomic", endsAt: seededEndsAt})
	before := readTrial(t, ctx, conn, "org_ext_atomic")

	// Failing the audit insert deterministically, from outside the handler. The
	// test owns its database, so the constraint reaches no other test.
	require.NoError(t, audittest.RejectAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialEndChanged))

	_, err := svc.ChangeTrialEndDate(ctx, &gen.ChangeTrialEndDatePayload{ID: "org_ext_atomic", EndsAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339)})
	requireOopsCode(t, err, oops.CodeUnexpected)

	after := readTrial(t, ctx, conn, "org_ext_atomic")
	require.Equal(t, before.EndsAt.Time, after.EndsAt.Time,
		"an extension whose audit entry failed must not survive: was %s, now %s", before.EndsAt.Time, after.EndsAt.Time)
	require.Equal(t, before.UpdatedAt.Time, after.UpdatedAt.Time)

	count, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialEndChanged)
	require.NoError(t, err)
	require.Zero(t, count)
}
