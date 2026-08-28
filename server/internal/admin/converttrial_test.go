package admin

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestMarkEnterpriseTrialConvertedRequestBody_RequiresOrganizationIDOnly(t *testing.T) {
	t.Parallel()

	id := "org_convert_validate"
	require.NoError(t, srv.ValidateMarkEnterpriseTrialConvertedRequestBody(&srv.MarkEnterpriseTrialConvertedRequestBody{ID: &id}))
	require.Error(t, srv.ValidateMarkEnterpriseTrialConvertedRequestBody(&srv.MarkEnterpriseTrialConvertedRequestBody{}))
	require.Error(t, srv.ValidateMarkEnterpriseTrialConvertedRequestBody(&srv.MarkEnterpriseTrialConvertedRequestBody{ID: new(string)}))
}

func TestMarkEnterpriseTrialConverted_EligibilityAndIdempotencyBoundary(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	demotedAt := now.Add(-24 * time.Hour)
	convertedAt := now.Add(-time.Hour)

	tests := []struct {
		name           string
		seedOrg        bool
		accountType    string
		seedTrial      bool
		trialTier      string
		convertedAt    *time.Time
		demotedAt      *time.Time
		wantCode       oops.Code
		wantNotImpl    bool
		wantValidRetry bool
	}{
		{name: "organization without trial", seedOrg: true, accountType: "enterprise", wantCode: oops.CodeConflict},
		{name: "stored trial tier is not enterprise", seedOrg: true, accountType: "enterprise", seedTrial: true, trialTier: "free", wantCode: oops.CodeConflict},
		{name: "running unconverted enterprise trial is eligible", seedOrg: true, accountType: "enterprise", seedTrial: true, trialTier: "enterprise", wantCode: oops.CodeUnexpected, wantNotImpl: true},
		{name: "demoted unconverted enterprise trial is eligible", seedOrg: true, accountType: "free", seedTrial: true, trialTier: "enterprise", demotedAt: &demotedAt, wantCode: oops.CodeUnexpected, wantNotImpl: true},
		{name: "converted enterprise organization is a valid retry", seedOrg: true, accountType: "enterprise", seedTrial: true, trialTier: "enterprise", convertedAt: &convertedAt, wantValidRetry: true},
		{name: "converted trial with free organization is incompatible", seedOrg: true, accountType: "free", seedTrial: true, trialTier: "enterprise", convertedAt: &convertedAt, wantCode: oops.CodeConflict},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, svc, conn, provisioner := newRearmService(t)
			orgID := "org_convert_boundary_" + string(rune('a'+i))
			if tc.seedOrg {
				seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: tc.accountType, whitelisted: tc.accountType == "enterprise"})
			}
			if tc.seedTrial {
				seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: tc.trialTier, endsAt: now.Add(7 * 24 * time.Hour), convertedAt: tc.convertedAt, demotedAt: tc.demotedAt})
			}

			beforeAudit, err := audittest.AuditLogCount(ctx, conn)
			require.NoError(t, err)
			beforeOutbox, err := testrepo.New(conn).CountPublishOutboxRows(ctx)
			require.NoError(t, err)
			beforeOrg := readOrgState(t, ctx, conn, orgID)
			var beforeTrial any
			if tc.seedTrial {
				beforeTrial = readTrial(t, ctx, conn, orgID)
			}

			result, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
			if tc.wantValidRetry {
				require.NoError(t, err)
				require.Equal(t, orgID, result.ID)
			} else {
				requireOopsCode(t, err, tc.wantCode)
				if tc.wantNotImpl {
					require.ErrorContains(t, err, "not implemented")
				}
			}

			require.Equal(t, beforeOrg, readOrgState(t, ctx, conn, orgID))
			if tc.seedTrial {
				require.Equal(t, beforeTrial, readTrial(t, ctx, conn, orgID))
			}
			afterAudit, err := audittest.AuditLogCount(ctx, conn)
			require.NoError(t, err)
			require.Equal(t, beforeAudit, afterAudit)
			afterOutbox, err := testrepo.New(conn).CountPublishOutboxRows(ctx)
			require.NoError(t, err)
			require.Equal(t, beforeOutbox, afterOutbox)
			require.Empty(t, provisioner.revivals, "eligibility boundary must not perform provider HTTP")
		})
	}
}
