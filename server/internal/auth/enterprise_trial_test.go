package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	orgid "github.com/speakeasy-api/gram/server/internal/organizations/id"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featureRepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

// signUpWithoutOrganization drives a user with no organizations through
// Callback and Authenticate, leaving them on a session with no active
// organization: the state a public self-signup reaches just before Register.
func signUpWithoutOrganization(t *testing.T, workosUserID, email string) (context.Context, *e2eInstance, string) {
	t.Helper()

	fetcher := &mockWorkOSFetcher{
		members: map[string][]workos.Member{},
		orgs:    map[string]*workos.Organization{},
	}
	userInfo := &MockUserInfo{
		UserID:        workosUserID,
		Email:         email,
		Organizations: []MockOrganizationEntry{},
	}

	ctx, inst := newE2EAuthService(t, userInfo, fetcher)

	callbackResult, err := inst.callbackWithNonce(ctx, t)
	require.NoError(t, err)
	require.NotEmpty(t, callbackResult.SessionToken)

	ctx, err = inst.sessionManager.Authenticate(ctx, callbackResult.SessionToken)
	require.NoError(t, err)

	return ctx, inst, callbackResult.SessionToken
}

func TestRegister_ArmsEnterpriseTrial(t *testing.T) {
	t.Parallel()

	const email = "trial-armed@example.com"
	ctx, inst, _ := signUpWithoutOrganization(t, "user_01TRIAL_ARMED", email)

	auditsBefore, err := audittest.AuditLogCountByAction(ctx, inst.conn, audit.ActionOrganizationEnterpriseTrialArmed)
	require.NoError(t, err)

	const orgName = "Enterprise Trial Org"
	organizationID := orgid.FromWorkOSID("workos_org_" + orgName)

	armedAt := time.Now().UTC()
	require.NoError(t, inst.service.Register(ctx, &gen.RegisterPayload{OrgName: orgName}))

	org, err := orgRepo.New(inst.conn).GetOrganizationMetadata(ctx, organizationID)
	require.NoError(t, err)
	require.Equal(t, "enterprise", org.GramAccountType)
	require.True(t, org.Whitelisted, "a trial organization must clear the whitelist gate")

	trial, err := trialsRepo.New(inst.conn).GetTrial(ctx, organizationID)
	require.NoError(t, err)
	require.Equal(t, "enterprise", trial.Tier)
	require.WithinDuration(t, armedAt.Add(14*24*time.Hour), trial.EndsAt.Time, time.Minute)

	// One canary feature is enough here. This test only has to prove the signup
	// path reached the seeder, not what the seeder enables.
	seeded, err := featureRepo.New(inst.conn).IsFeatureEnabled(ctx, featureRepo.IsFeatureEnabledParams{
		OrganizationID: organizationID,
		FeatureName:    string(productfeatures.FeatureSSO),
	})
	require.NoError(t, err)
	require.True(t, seeded, "the signup path seeds the entitlement bundle")

	auditsAfter, err := audittest.AuditLogCountByAction(ctx, inst.conn, audit.ActionOrganizationEnterpriseTrialArmed)
	require.NoError(t, err)
	require.Equal(t, auditsBefore+1, auditsAfter)

	entry, err := audittest.LatestAuditLogByAction(ctx, inst.conn, audit.ActionOrganizationEnterpriseTrialArmed)
	require.NoError(t, err)
	require.NotEmpty(t, entry.ActorDisplay, "org-less session must still resolve the actor email")
	require.Equal(t, email, entry.ActorDisplay)
}

// TestRegister_EnterpriseTrialResolvesThroughInfo checks the tier survives the
// Info round trip, so the enterprise surfaces appear straight after signup
// without a re-login.
func TestRegister_EnterpriseTrialResolvesThroughInfo(t *testing.T) {
	t.Parallel()

	ctx, inst, sessionToken := signUpWithoutOrganization(t, "user_01TRIAL_RESOLVE", "trial-resolve@example.com")

	const orgName = "Enterprise Resolve Org"
	organizationID := orgid.FromWorkOSID("workos_org_" + orgName)

	armedAt := time.Now().UTC()
	require.NoError(t, inst.service.Register(ctx, &gen.RegisterPayload{OrgName: orgName}))

	// Pick up the organization Register just created, as the dashboard does on
	// its next request.
	ctx, err := inst.sessionManager.Authenticate(ctx, sessionToken)
	require.NoError(t, err)

	infoResult, err := inst.service.Info(ctx, &gen.InfoPayload{})
	require.NoError(t, err)
	require.Equal(t, organizationID, infoResult.ActiveOrganizationID)
	require.Equal(t, "enterprise", infoResult.GramAccountType)
	require.True(t, infoResult.HasActiveSubscription, "an enterprise organization short-circuits the billing lookup")
	require.True(t, infoResult.Whitelisted)
	require.Len(t, infoResult.Organizations, 1)
	require.Equal(t, orgName, infoResult.Organizations[0].Name)

	// Info reads the trials table, so the row Register wrote is what feeds the
	// dashboard countdown.
	require.NotNil(t, infoResult.Trial)
	endsAt, err := time.Parse(time.RFC3339, infoResult.Trial.EndsAt)
	require.NoError(t, err)
	require.WithinDuration(t, armedAt.Add(14*24*time.Hour), endsAt, time.Minute)
}
