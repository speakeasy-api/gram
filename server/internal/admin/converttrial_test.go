package admin

import (
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	orrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
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
		seedKey        bool
		wantCode       oops.Code
		wantHTTPStatus int
		wantNotImpl    bool
		wantValidRetry bool
	}{
		{name: "absent organization is private not found", wantCode: oops.CodeNotFound, wantHTTPStatus: http.StatusNotFound},
		{name: "existing organization without trial is a conflict", seedOrg: true, accountType: "enterprise", seedKey: true, wantCode: oops.CodeConflict},
		{name: "stored trial tier is not enterprise", seedOrg: true, accountType: "enterprise", seedTrial: true, trialTier: "free", seedKey: true, wantCode: oops.CodeConflict},
		{name: "running unconverted enterprise trial is eligible", seedOrg: true, accountType: "enterprise", seedTrial: true, trialTier: "enterprise", seedKey: true, wantCode: oops.CodeUnexpected, wantNotImpl: true},
		{name: "demoted unconverted enterprise trial is eligible", seedOrg: true, accountType: "free", seedTrial: true, trialTier: "enterprise", demotedAt: &demotedAt, seedKey: true, wantCode: oops.CodeUnexpected, wantNotImpl: true},
		{name: "converted enterprise organization is a valid retry", seedOrg: true, accountType: "enterprise", seedTrial: true, trialTier: "enterprise", convertedAt: &convertedAt, seedKey: true, wantValidRetry: true},
		{name: "converted trial with free organization is incompatible", seedOrg: true, accountType: "free", seedTrial: true, trialTier: "enterprise", convertedAt: &convertedAt, seedKey: true, wantCode: oops.CodeConflict},
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
			if tc.seedKey {
				require.True(t, tc.seedOrg, "an OpenRouter key requires an organization")
				seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 73, disabled: true})
			}

			beforeAudit, err := audittest.AuditLogCount(ctx, conn)
			require.NoError(t, err)
			beforeOutbox, err := testrepo.New(conn).CountPublishOutboxRows(ctx)
			require.NoError(t, err)
			var beforeOrg any
			if tc.seedOrg {
				beforeOrg = readOrgState(t, ctx, conn, orgID)
			}
			var beforeTrial any
			if tc.seedTrial {
				beforeTrial = readTrial(t, ctx, conn, orgID)
			}
			var beforeKey any
			if tc.seedKey {
				beforeKey = readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat)
			}

			result, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
			if tc.wantValidRetry {
				require.NoError(t, err)
				require.Equal(t, orgID, result.ID)
			} else {
				requireOopsCode(t, err, tc.wantCode)
				if tc.wantHTTPStatus != 0 {
					var publicErr *oops.ShareableError
					require.ErrorAs(t, err, &publicErr)
					require.Equal(t, tc.wantHTTPStatus, publicErr.HTTPStatus(ctx))
				}
				if tc.wantNotImpl {
					require.ErrorContains(t, err, "not implemented")
				}
			}

			if tc.seedOrg {
				require.Equal(t, beforeOrg, readOrgState(t, ctx, conn, orgID))
			} else {
				_, orgErr := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: orgID})
				requireOopsCode(t, orgErr, oops.CodeNotFound)
				_, trialErr := trialsRepo.New(conn).GetTrial(ctx, orgID)
				require.ErrorIs(t, trialErr, pgx.ErrNoRows)
				_, keyErr := orrepo.New(conn).GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat)})
				require.ErrorIs(t, keyErr, pgx.ErrNoRows)
			}
			if tc.seedTrial {
				require.Equal(t, beforeTrial, readTrial(t, ctx, conn, orgID))
			}
			if tc.seedKey {
				require.Equal(t, beforeKey, readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat), "complete persisted key state must remain unchanged")
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
