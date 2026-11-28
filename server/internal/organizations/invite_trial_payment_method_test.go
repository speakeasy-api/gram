package organizations_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/trials"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

func TestInviteCallback_PlatformAdminInviteWaitsForPaymentMethod(t *testing.T) {
	t.Parallel()

	for _, whitelisted := range []bool{false, true} {
		ctx, ti := newTestOrganizationsServiceWithRealFeatures(t)
		flags := &feature.InMemory{}
		flags.SetFlag(feature.FlagSelfServeTrialRequiresPaymentMethod, feature.AnyDistinctID, true)
		ti.sessionManager.SetFlags(flags)
		authCtx := requireAuthContext(t, ctx)
		require.NoError(t, testrepo.New(ti.conn).SetUserPlatformAdminFixture(ctx, testrepo.SetUserPlatformAdminFixtureParams{Admin: true, ID: authCtx.UserID}))

		organizationID := "org_card_invite_" + uuid.NewString()
		_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
			ID: organizationID, Name: "Card Invite Organization", Slug: "card-invite-" + uuid.NewString()[:8],
			WorkosID: conv.ToPGText(organizationID), Whitelisted: pgtype.Bool{Bool: whitelisted, Valid: true},
			CreationSource: conv.ToPGText(orgprovision.SourcePlatformAdmin),
		})
		require.NoError(t, err)
		require.NoError(t, authz.SeedSystemRoleGrants(ctx, ti.conn, organizationID))

		rawToken, _ := seedInviteCallbackInvite(t, ctx, ti, "card-invite", organizationID, authCtx.UserID, authz.SystemRoleAdmin)
		ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", organizationID, authz.SystemRoleAdmin).Return(&thirdpartyworkos.Member{ID: "membership_01INVITE", RoleSlugs: []string{authz.SystemRoleAdmin}}, nil).Once()
		seedInviteeUser(t, ctx, ti)

		recorder := serveInviteCallback(t, ti, rawToken)
		require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)

		org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, organizationID)
		require.NoError(t, err)
		_, err = trialsrepo.New(ti.conn).GetTrial(ctx, organizationID)
		if whitelisted {
			require.NoError(t, err, "an organization already inside the product has no card gate, so the invite arms the trial")
			continue
		}
		require.ErrorIs(t, err, pgx.ErrNoRows, "the invite offers the trial; a card starts it")
		require.False(t, org.Whitelisted)
		require.Empty(t, ti.trial.trialStarted)
		awaiting, err := trials.AwaitingPaymentMethod(ctx, ti.conn, org)
		require.NoError(t, err)
		require.True(t, awaiting, "a staff-invited prospect can start the trial with a card")
	}
}
