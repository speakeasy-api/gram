package auth_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	orgid "github.com/speakeasy-api/gram/server/internal/organizations/id"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/trials"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

func requirePaymentMethodForTrials(enabled bool) *feature.InMemory {
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagSelfServeTrialRequiresPaymentMethod, feature.AnyDistinctID, enabled)
	return flags
}

func TestRegister_TrialWaitsForPaymentMethod(t *testing.T) {
	t.Parallel()

	ctx, inst, sessionToken := signUpWithoutOrganization(t, "user_01TRIAL_CARD", "trial-card@example.com")
	flags := requirePaymentMethodForTrials(true)
	inst.sessionManager.SetFlags(flags)

	const orgName = "Card Gated Org"
	organizationID := orgid.FromWorkOSID("workos_org_" + orgName)
	require.NoError(t, inst.service.Register(ctx, &gen.RegisterPayload{OrgName: orgName}))

	org, err := orgRepo.New(inst.conn).GetOrganizationMetadata(ctx, organizationID)
	require.NoError(t, err)
	require.Equal(t, "free", org.GramAccountType)
	require.False(t, org.Whitelisted, "the organization stays behind the dashboard gate until a card starts the trial")

	_, err = trialsRepo.New(inst.conn).GetTrial(ctx, organizationID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "no trial starts without a card")

	ctx, err = inst.sessionManager.Authenticate(ctx, sessionToken)
	require.NoError(t, err)
	info, err := inst.service.Info(ctx, &gen.InfoPayload{})
	require.NoError(t, err)
	require.True(t, info.TrialPendingPaymentMethod)
	require.Nil(t, info.Trial)
	require.Nil(t, info.TrialCardRejection)

	// A rejected card is reported so the admin can add another.
	_, err = trialsRepo.New(inst.conn).RecordTrialPaymentMethod(ctx, trialsRepo.RecordTrialPaymentMethodParams{
		OrganizationID:          organizationID,
		StripeCustomerID:        "cus_placeholder",
		StripePaymentMethodID:   "pm_placeholder",
		CardFingerprint:         "fingerprint_placeholder",
		CardFunding:             "prepaid",
		StripeCheckoutSessionID: "cs_placeholder",
		RejectionReason:         pgtype.Text{String: trials.CardRejectionPrepaid, Valid: true},
	})
	require.NoError(t, err)
	info, err = inst.service.Info(ctx, &gen.InfoPayload{})
	require.NoError(t, err)
	require.True(t, info.TrialPendingPaymentMethod)
	require.NotNil(t, info.TrialCardRejection)
	require.Equal(t, trials.CardRejectionPrepaid, info.TrialCardRejection.Reason)

	// The kill switch hides the card gate again.
	flags.SetFlag(feature.FlagSelfServeTrialRequiresPaymentMethod, feature.AnyDistinctID, false)
	info, err = inst.service.Info(ctx, &gen.InfoPayload{})
	require.NoError(t, err)
	require.False(t, info.TrialPendingPaymentMethod)
}

func TestRegister_TrialStartsWithoutPaymentMethodWhenFlagOff(t *testing.T) {
	t.Parallel()

	ctx, inst, sessionToken := signUpWithoutOrganization(t, "user_01TRIAL_NOCARD", "trial-nocard@example.com")
	inst.sessionManager.SetFlags(requirePaymentMethodForTrials(false))

	const orgName = "Card Free Org"
	organizationID := orgid.FromWorkOSID("workos_org_" + orgName)
	require.NoError(t, inst.service.Register(ctx, &gen.RegisterPayload{OrgName: orgName}))

	_, err := trialsRepo.New(inst.conn).GetTrial(ctx, organizationID)
	require.NoError(t, err)

	ctx, err = inst.sessionManager.Authenticate(ctx, sessionToken)
	require.NoError(t, err)
	info, err := inst.service.Info(ctx, &gen.InfoPayload{})
	require.NoError(t, err)
	require.False(t, info.TrialPendingPaymentMethod)
	require.True(t, info.Whitelisted)
}

func TestCallback_LoginDefersTrialUntilPaymentMethod(t *testing.T) {
	t.Parallel()

	const (
		email          = "card-login@example.com"
		organizationID = "org-card-login"
		workosOrgID    = "org_01CARD_LOGIN"
	)
	userInfo := &MockUserInfo{
		UserID:        "user_01CARD_LOGIN",
		Email:         email,
		Organizations: nil,
	}
	ctx, instance := newTestAuthServiceWithWorkOSClient(t, userInfo, &mockWorkOSFetcher{
		members: map[string][]workos.Member{
			userInfo.UserID: {{
				ID:             "om_card_login",
				UserID:         userInfo.UserID,
				OrganizationID: workosOrgID,
				Organization:   "Card Login Org",
				RoleSlugs:      []string{"admin"},
			}},
		},
		orgs: map[string]*workos.Organization{
			workosOrgID: {ID: workosOrgID, Name: "Card Login Org"},
		},
	})
	flags := requirePaymentMethodForTrials(true)
	instance.sessionManager.SetFlags(flags)
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	require.NoError(t, instance.createTestOrganization(ctx, MockOrganizationEntry{
		ID:                 organizationID,
		Name:               "Card Login Org",
		Slug:               organizationID,
		WorkosID:           new(workosOrgID),
		UserWorkspaceSlugs: nil,
	}, userInfo.UserID))

	result, err := instance.callbackWithNonce(ctx, t)
	require.NoError(t, err)
	require.NotContains(t, result.Location, "signin_error=")

	authed, err := instance.sessionManager.Authenticate(ctx, result.SessionToken)
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(authed)
	require.True(t, ok)
	require.Equal(t, organizationID, authCtx.ActiveOrganizationID)
	require.False(t, authCtx.Whitelisted)
	require.Equal(t, "free", authCtx.AccountType)
	require.Empty(t, instance.trialNotifier.trialStarted)

	_, err = trialsRepo.New(instance.conn).GetTrial(ctx, organizationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	info, err := instance.service.Info(authed, &gen.InfoPayload{})
	require.NoError(t, err)
	require.True(t, info.TrialPendingPaymentMethod, "an admin can start the trial by adding a card")

	// With the kill switch off, the next admin login starts the trial as before.
	flags.SetFlag(feature.FlagSelfServeTrialRequiresPaymentMethod, feature.AnyDistinctID, false)
	result, err = instance.callbackWithNonce(ctx, t)
	require.NoError(t, err)
	require.NotContains(t, result.Location, "signin_error=")
	authed, err = instance.sessionManager.Authenticate(ctx, result.SessionToken)
	require.NoError(t, err)
	authCtx, ok = contextvalues.GetAuthContext(authed)
	require.True(t, ok)
	require.True(t, authCtx.Whitelisted)
	require.Equal(t, "enterprise", authCtx.AccountType)
	require.Equal(t, []string{organizationID}, instance.trialNotifier.trialStarted)
}
