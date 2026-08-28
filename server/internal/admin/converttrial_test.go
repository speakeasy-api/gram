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

func TestUpdateOrganization_EnterpriseTrialDelegatesToAtomicConversion(t *testing.T) {
	t.Parallel()
	ctx, svc, conn, _ := newRearmService(t)
	orgID := "org_update_conversion_delegate"
	demotedAt := time.Now().UTC().Add(-time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: demotedAt, demotedAt: &demotedAt})
	for _, keyType := range openrouter.AllKeyTypes {
		seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: keyType, monthlyCredits: 7, disabled: true})
	}
	enterprise, whitelisted := "enterprise", true
	result, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: orgID, AccountType: &enterprise, Whitelisted: &whitelisted})
	require.NoError(t, err)
	require.Equal(t, "enterprise", result.AccountType)
	require.True(t, result.Whitelisted)
	require.True(t, readTrial(t, ctx, conn, orgID).ConvertedAt.Valid)
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
		wantConverted  bool
		wantValidRetry bool
	}{
		{name: "absent organization is private not found", wantCode: oops.CodeNotFound, wantHTTPStatus: http.StatusNotFound},
		{name: "existing organization without trial is a conflict", seedOrg: true, accountType: "enterprise", seedKey: true, wantCode: oops.CodeConflict},
		{name: "stored trial tier is not enterprise", seedOrg: true, accountType: "enterprise", seedTrial: true, trialTier: "free", seedKey: true, wantCode: oops.CodeConflict},
		{name: "running unconverted enterprise trial converts", seedOrg: true, accountType: "enterprise", seedTrial: true, trialTier: "enterprise", seedKey: true, wantConverted: true},
		{name: "running undemoted trial normalizes drifted free access", seedOrg: true, accountType: "free", seedTrial: true, trialTier: "enterprise", seedKey: true, wantConverted: true},
		{name: "demoted unconverted enterprise trial converts", seedOrg: true, accountType: "free", seedTrial: true, trialTier: "enterprise", demotedAt: &demotedAt, seedKey: true, wantConverted: true},
		{name: "demoted trial preserves already restored enterprise access", seedOrg: true, accountType: "enterprise", seedTrial: true, trialTier: "enterprise", demotedAt: &demotedAt, seedKey: true, wantConverted: true},
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
			var beforeTrial *trialsRepo.Trial
			if tc.seedTrial {
				trial := readTrial(t, ctx, conn, orgID)
				beforeTrial = &trial
			}
			var beforeKey *orrepo.OpenrouterApiKey
			if tc.seedKey {
				key := readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat)
				beforeKey = &key
			}

			result, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
			if tc.wantValidRetry || tc.wantConverted {
				require.NoError(t, err)
				require.Equal(t, orgID, result.OrganizationID)
				require.NotEmpty(t, result.ConvertedAt)
			} else {
				requireOopsCode(t, err, tc.wantCode)
				if tc.wantHTTPStatus != 0 {
					var publicErr *oops.ShareableError
					require.ErrorAs(t, err, &publicErr)
					require.Equal(t, tc.wantHTTPStatus, publicErr.HTTPStatus(ctx))
				}
			}

			if tc.seedOrg {
				if tc.wantConverted {
					org := readOrgState(t, ctx, conn, orgID)
					require.Equal(t, "enterprise", org.GramAccountType)
					require.True(t, org.Whitelisted)
				} else {
					require.Equal(t, beforeOrg, readOrgState(t, ctx, conn, orgID))
				}
			} else {
				_, orgErr := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: orgID})
				requireOopsCode(t, orgErr, oops.CodeNotFound)
				_, trialErr := trialsRepo.New(conn).GetTrial(ctx, orgID)
				require.ErrorIs(t, trialErr, pgx.ErrNoRows)
				_, keyErr := orrepo.New(conn).GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat)})
				require.ErrorIs(t, keyErr, pgx.ErrNoRows)
			}
			if tc.seedTrial {
				if tc.wantConverted {
					trial := readTrial(t, ctx, conn, orgID)
					require.True(t, trial.ConvertedAt.Valid)
					require.Equal(t, beforeTrial.EndsAt, trial.EndsAt)
					require.Equal(t, beforeTrial.DemotedAt, trial.DemotedAt)
				} else {
					require.Equal(t, *beforeTrial, readTrial(t, ctx, conn, orgID))
				}
			}
			if tc.seedKey {
				if tc.wantConverted {
					key := readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat)
					floor, ok := openrouter.DefaultCreditLimit(orgID, "enterprise", false)
					require.True(t, ok)
					require.GreaterOrEqual(t, key.MonthlyCredits, int64(floor))
					require.Empty(t, key.DisableCauses)
					require.False(t, key.Disabled)
				} else {
					afterKey := readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat)
					afterKey.UpdatedAt = beforeKey.UpdatedAt
					require.Equal(t, *beforeKey, afterKey, "retry reconciliation may only refresh bookkeeping timestamps")
				}
			}
			afterAudit, err := audittest.AuditLogCount(ctx, conn)
			require.NoError(t, err)
			if tc.wantConverted {
				require.Equal(t, beforeAudit+1, afterAudit)
			} else {
				require.Equal(t, beforeAudit, afterAudit)
			}
			afterOutbox, err := testrepo.New(conn).CountPublishOutboxRows(ctx)
			require.NoError(t, err)
			if tc.wantConverted {
				require.Equal(t, beforeOutbox+1, afterOutbox)
			} else {
				require.Equal(t, beforeOutbox, afterOutbox)
			}
			if tc.wantConverted || tc.wantValidRetry {
				require.Equal(t, openrouter.AllKeyTypes, provisioner.reconcileAttempts)
			} else {
				require.Empty(t, provisioner.revivals, "rejected conversion must not perform provider HTTP")
			}
		})
	}
}
