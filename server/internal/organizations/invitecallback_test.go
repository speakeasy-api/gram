package organizations_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

func TestInviteCallback_FirstUserInvitedByPlatformAdminArmsTrial(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithRealFeatures(t)
	authCtx := requireAuthContext(t, ctx)
	_, err := ti.conn.Exec(ctx, "UPDATE users SET admin = true WHERE id = $1", authCtx.UserID)
	require.NoError(t, err)

	const organizationID = "org_platform_admin_invite"
	_, err = orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: "Invited Organization", Slug: "invited-organization",
		WorkosID: conv.ToPGText(organizationID), Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, ti.conn, organizationID))

	rawToken, invite := seedInviteCallbackInvite(t, ctx, ti, "platform-admin-first-user", organizationID, authCtx.UserID, authz.SystemRoleAdmin)
	for _, feature := range productfeatures.EnterpriseTrialBundle {
		enabled, featureErr := ti.features.IsFeatureEnabled(ctx, organizationID, feature)
		require.NoError(t, featureErr)
		require.Falsef(t, enabled, "feature %s should be cached as disabled before trial arming", feature)
	}
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", organizationID, authz.SystemRoleAdmin).Return("membership_01INVITE", nil).Once()
	seedInviteeUser(t, ctx, ti)
	ti.trial.trialStartedErr = errors.New("notify failed")
	ti.posthog.captureErr = errors.New("capture failed")
	ti.posthog.identifyErr = errors.New("identify failed")

	armedAt := time.Now().UTC()
	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)

	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, organizationID)
	require.NoError(t, err)
	require.Equal(t, "enterprise", org.GramAccountType)
	require.True(t, org.Whitelisted)
	roles, err := accessrepo.New(ti.conn).ListMemberRolePrincipalsByUser(ctx, accessrepo.ListMemberRolePrincipalsByUserParams{
		OrganizationID: organizationID,
		UserID:         "user_01INVITEE",
	})
	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.Equal(t, authz.SystemRoleAdmin, roles[0].RoleSlug)
	trial, err := trialsrepo.New(ti.conn).GetTrial(ctx, organizationID)
	require.NoError(t, err)
	require.Equal(t, "enterprise", trial.Tier)
	require.WithinDuration(t, armedAt.Add(14*24*time.Hour), trial.EndsAt.Time, time.Minute)
	for _, feature := range productfeatures.EnterpriseTrialBundle {
		enabled, featureErr := ti.features.IsFeatureEnabled(ctx, organizationID, feature)
		require.NoError(t, featureErr)
		require.Truef(t, enabled, "feature %s cache should refresh after trial arming", feature)
	}

	require.Equal(t, []string{organizationID}, ti.trial.trialStarted)
	require.Empty(t, ti.trial.adminAdded)
	require.Len(t, ti.posthog.events, 1)
	require.Equal(t, "onboarding_event", ti.posthog.events[0].eventName)
	require.Equal(t, "invitee@example.test", ti.posthog.events[0].distinctID)
	require.Equal(t, "platform_admin_invite", ti.posthog.events[0].properties["created_via"])
	require.Len(t, ti.posthog.identified, 1)
	require.Equal(t, "platform_admin_invite", ti.posthog.identified[0].properties["created_via"])

	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", storedInvite.State)
	audits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialArmed)
	require.NoError(t, err)
	require.EqualValues(t, 1, audits)

	serveInviteCallback(t, ti, rawToken)
	require.Len(t, ti.trial.trialStarted, 1)
	require.Len(t, ti.posthog.events, 1)
	audits, err = audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialArmed)
	require.NoError(t, err)
	require.EqualValues(t, 1, audits)
}

func TestInviteCallback_ExistingInviteeRelationshipStillArmsTrial(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	_, err := ti.conn.Exec(ctx, "UPDATE users SET admin = true WHERE id = $1", authCtx.UserID)
	require.NoError(t, err)

	const organizationID = "org_existing_invitee_relationship"
	_, err = orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: "Existing Relationship Organization", Slug: "existing-relationship-organization",
		WorkosID: conv.ToPGText(organizationID), Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, ti.conn, organizationID))
	rawToken, invite := seedInviteCallbackInvite(t, ctx, ti, "existing-invitee-relationship", organizationID, authCtx.UserID, authz.SystemRoleAdmin)
	seedInviteeUser(t, ctx, ti)
	_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID, UserID: conv.ToPGText("user_01INVITEE"),
	})
	require.NoError(t, err)
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", organizationID, authz.SystemRoleAdmin).Return("membership_01INVITE", nil).Once()

	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	require.Equal(t, "http://localhost:5173", recorder.Header().Get("Location"))
	_, err = trialsrepo.New(ti.conn).GetTrial(ctx, organizationID)
	require.NoError(t, err)
	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", storedInvite.State)
}

func TestInviteCallback_RoleUpdateDuringAuthenticationUsesAuthoritativeRole(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAuth := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseAuth)

	ctx, ti := newTestOrganizationsServiceWithInviteIdentityProvider(t, blockingInviteIdentityProvider{started: started, release: release})
	authCtx := requireAuthContext(t, ctx)
	const organizationID = "org_invite_role_update"
	_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: "Role Update Organization", Slug: "role-update-organization",
		WorkosID: conv.ToPGText(organizationID), Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, ti.conn, organizationID))
	rawToken, invite := seedInviteCallbackInvite(t, ctx, ti, "role-update-during-authentication", organizationID, authCtx.UserID, authz.SystemRoleMember)
	seedInviteeUser(t, ctx, ti)
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", organizationID, authz.SystemRoleAdmin).Return("membership_01INVITE", nil).Once()

	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- serveInviteCallback(t, ti, rawToken) }()
	<-started

	updated, err := orgrepo.New(ti.conn).UpdateInvitationRole(ctx, orgrepo.UpdateInvitationRoleParams{
		ID: invite.ID, OrganizationID: organizationID, RoleSlug: conv.ToPGText(authz.SystemRoleAdmin),
	})
	require.NoError(t, err)
	require.Equal(t, authz.SystemRoleAdmin, updated.RoleSlug.String)
	releaseAuth()

	recorder := <-result
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", storedInvite.State)
	require.Equal(t, authz.SystemRoleAdmin, storedInvite.RoleSlug.String)
	roles, err := accessrepo.New(ti.conn).ListMemberRolePrincipalsByUser(ctx, accessrepo.ListMemberRolePrincipalsByUserParams{
		OrganizationID: organizationID, UserID: "user_01INVITEE",
	})
	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.Equal(t, authz.SystemRoleAdmin, roles[0].RoleSlug)
}

func TestInviteCallback_ExistingWorkOSMembershipRoleIsReconciled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	const organizationID = "org_existing_workos_membership"
	_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: "Existing Membership Organization", Slug: "existing-membership-organization",
		WorkosID: conv.ToPGText(organizationID), Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, ti.conn, organizationID))
	rawToken, invite := seedInviteCallbackInvite(t, ctx, ti, "existing-workos-membership", organizationID, authCtx.UserID, authz.SystemRoleAdmin)
	seedInviteeUser(t, ctx, ti)

	createErr := errors.New("membership already exists")
	member := &thirdpartyworkos.Member{ID: "membership_01EXISTING", UserID: "user_01INVITEE", OrganizationID: organizationID}
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", organizationID, authz.SystemRoleAdmin).Return("", createErr).Once()
	ti.orgs.On("GetOrgMembership", mock.Anything, "user_01INVITEE", organizationID).Return(member, nil).Once()
	ti.orgs.On("UpdateMemberRoles", mock.Anything, member.ID, []string{authz.SystemRoleAdmin}).Return(member, nil).Once()

	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", storedInvite.State)
	var membershipID string
	require.NoError(t, ti.conn.QueryRow(ctx, `
		SELECT workos_membership_id
		FROM organization_role_assignments
		WHERE organization_id = $1 AND user_id = $2 AND deleted_at IS NULL
	`, organizationID, "user_01INVITEE").Scan(&membershipID))
	require.Equal(t, member.ID, membershipID)
}

func TestInviteCallback_WorkOSFailureDoesNotRollBackAcceptance(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	rawToken, invite := seedInviteCallbackInvite(t, ctx, ti, "workos-membership-failure", authCtx.ActiveOrganizationID, authCtx.UserID, authz.SystemRoleAdmin)
	seedInviteeUser(t, ctx, ti)
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", authCtx.ActiveOrganizationID, authz.SystemRoleAdmin).Return("", errors.New("create failed")).Once()
	ti.orgs.On("GetOrgMembership", mock.Anything, "user_01INVITEE", authCtx.ActiveOrganizationID).Return(nil, errors.New("lookup failed")).Once()

	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	require.Equal(t, "http://localhost:5173", recorder.Header().Get("Location"))
	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", storedInvite.State)
}

func TestInviteCallback_TrialBundleFailureRollsBackAcceptance(t *testing.T) {
	t.Parallel()

	seederErr := errors.New("seed trial bundle")
	ctx, ti := newTestOrganizationsServiceWithTrialBundleSeeder(t, func(ctx context.Context, tx pgx.Tx, organizationID string) error {
		if err := productfeatures.SeedEnterpriseTrialBundleTx(ctx, tx, organizationID); err != nil {
			return err
		}
		return seederErr
	})
	authCtx := requireAuthContext(t, ctx)
	_, err := ti.conn.Exec(ctx, "UPDATE users SET admin = true WHERE id = $1", authCtx.UserID)
	require.NoError(t, err)

	const organizationID = "org_failed_platform_invite"
	_, err = orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: "Failed Invite Organization", Slug: "failed-invite-organization",
		WorkosID: conv.ToPGText(organizationID), Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, ti.conn, organizationID))
	rawToken, invite := seedInviteCallbackInvite(t, ctx, ti, "trial-bundle-failure", organizationID, authCtx.UserID, authz.SystemRoleAdmin)
	auditsBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialArmed)
	require.NoError(t, err)
	seedInviteeUser(t, ctx, ti)

	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)

	active, err := orgrepo.New(ti.conn).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{
		UserID: "user_01INVITEE", OrganizationID: organizationID,
	})
	require.NoError(t, err)
	require.False(t, active)
	roles, err := accessrepo.New(ti.conn).ListMemberRolePrincipalsByUser(ctx, accessrepo.ListMemberRolePrincipalsByUserParams{
		OrganizationID: organizationID, UserID: "user_01INVITEE",
	})
	require.NoError(t, err)
	require.Empty(t, roles)
	_, err = trialsrepo.New(ti.conn).GetTrial(ctx, organizationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", storedInvite.State)
	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, organizationID)
	require.NoError(t, err)
	require.Equal(t, "free", org.GramAccountType)
	require.True(t, org.Whitelisted)
	var entitlementRows int
	require.NoError(t, ti.conn.QueryRow(ctx, "SELECT count(*) FROM organization_features WHERE organization_id = $1", organizationID).Scan(&entitlementRows))
	require.Zero(t, entitlementRows)
	auditsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialArmed)
	require.NoError(t, err)
	require.Equal(t, auditsBefore, auditsAfter)
	require.Empty(t, ti.trial.trialStarted)
	require.Empty(t, ti.posthog.events)
}

func TestInviteCallback_DeletedPlatformAdminInviterDoesNotArmTrial(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	_, err := ti.conn.Exec(ctx, "UPDATE users SET admin = true WHERE id = $1", authCtx.UserID)
	require.NoError(t, err)

	const organizationID = "org_deleted_platform_admin_inviter"
	_, err = orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: "Deleted Inviter Organization", Slug: "deleted-inviter-organization",
		WorkosID: conv.ToPGText(organizationID), Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, ti.conn, organizationID))
	rawToken, invite := seedInviteCallbackInvite(t, ctx, ti, "deleted-platform-admin-inviter", organizationID, authCtx.UserID, authz.SystemRoleAdmin)
	require.NoError(t, testrepo.New(ti.conn).ForceSoftDeleteUser(ctx, authCtx.UserID))
	seedInviteeUser(t, ctx, ti)
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", organizationID, authz.SystemRoleAdmin).Return("membership_01INVITE", nil).Once()
	auditsBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialArmed)
	require.NoError(t, err)

	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", storedInvite.State)
	_, err = trialsrepo.New(ti.conn).GetTrial(ctx, organizationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, organizationID)
	require.NoError(t, err)
	require.Equal(t, "free", org.GramAccountType)
	auditsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialArmed)
	require.NoError(t, err)
	require.Equal(t, auditsBefore, auditsAfter)
}

func TestInviteCallback_ConcurrentFirstUserAcceptancesArmOneTrial(t *testing.T) {
	t.Parallel()

	identities := map[string]inviteTestIdentity{
		"invitee-one@example.test": {gramUserID: "gram_user_01INVITEE", workosUserID: "workos_user_01INVITEE"},
		"invitee-two@example.test": {gramUserID: "gram_user_02INVITEE", workosUserID: "workos_user_02INVITEE"},
	}
	ctx, ti := newTestOrganizationsServiceWithInviteIdentityProvider(t, testInviteIdentityProvider{identities: identities})
	authCtx := requireAuthContext(t, ctx)
	_, err := ti.conn.Exec(ctx, "UPDATE users SET admin = true WHERE id = $1", authCtx.UserID)
	require.NoError(t, err)

	const organizationID = "org_concurrent_platform_invites"
	_, err = orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: "Concurrent Invite Organization", Slug: "concurrent-invite-organization",
		WorkosID: conv.ToPGText(organizationID), Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, ti.conn, organizationID))

	rawTokenOne, inviteOne := seedInviteCallbackInviteForEmail(t, ctx, ti, "concurrent-one", organizationID, authCtx.UserID, authz.SystemRoleAdmin, "invitee-one@example.test")
	rawTokenTwo, inviteTwo := seedInviteCallbackInviteForEmail(t, ctx, ti, "concurrent-two", organizationID, authCtx.UserID, authz.SystemRoleAdmin, "invitee-two@example.test")
	seedInviteeUserIdentity(t, ctx, ti, "gram_user_01INVITEE", "invitee-one@example.test")
	seedInviteeUserIdentity(t, ctx, ti, "gram_user_02INVITEE", "invitee-two@example.test")
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "workos_user_01INVITEE", organizationID, authz.SystemRoleAdmin).Return("membership_01INVITEE", nil).Once()
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "workos_user_02INVITEE", organizationID, authz.SystemRoleAdmin).Return("membership_02INVITEE", nil).Once()

	tokens := []string{rawTokenOne, rawTokenTwo}
	recorders := make([]*httptest.ResponseRecorder, len(tokens))
	var wg sync.WaitGroup
	for i, rawToken := range tokens {
		wg.Go(func() { recorders[i] = serveInviteCallback(t, ti, rawToken) })
	}
	wg.Wait()

	for _, recorder := range recorders {
		require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
		require.Equal(t, "http://localhost:5173", recorder.Header().Get("Location"))
	}
	for _, invite := range []orgrepo.OrganizationInvitation{inviteOne, inviteTwo} {
		storedInvite, getErr := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
		require.NoError(t, getErr)
		require.Equal(t, "accepted", storedInvite.State)
	}
	for _, invitee := range identities {
		active, activeErr := orgrepo.New(ti.conn).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{UserID: invitee.gramUserID, OrganizationID: organizationID})
		require.NoError(t, activeErr)
		require.True(t, active)
		roles, rolesErr := accessrepo.New(ti.conn).ListMemberRolePrincipalsByUser(ctx, accessrepo.ListMemberRolePrincipalsByUserParams{OrganizationID: organizationID, UserID: invitee.gramUserID})
		require.NoError(t, rolesErr)
		require.Len(t, roles, 1)
		require.Equal(t, authz.SystemRoleAdmin, roles[0].RoleSlug)
	}
	var trials int
	require.NoError(t, ti.conn.QueryRow(ctx, "SELECT count(*) FROM trials WHERE organization_id = $1", organizationID).Scan(&trials))
	require.Equal(t, 1, trials)
	audits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationEnterpriseTrialArmed)
	require.NoError(t, err)
	require.EqualValues(t, 1, audits)
	require.Len(t, ti.trial.trialStarted, 1)
	require.Len(t, ti.posthog.events, 1)
}

func TestInviteCallback_FirstUserWithoutPlatformAdminInviterGetsNoTrial(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)

	const organizationID = "org_ordinary_first_invite"
	_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: "Ordinary Invite Organization", Slug: "ordinary-invite-organization",
		WorkosID: conv.ToPGText(organizationID), Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, ti.conn, organizationID))
	rawToken, _ := seedInviteCallbackInvite(t, ctx, ti, "ordinary-first-user", organizationID, authCtx.UserID, authz.SystemRoleAdmin)
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", organizationID, authz.SystemRoleAdmin).Return("membership_01INVITE", nil).Once()
	seedInviteeUser(t, ctx, ti)

	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	_, err = trialsrepo.New(ti.conn).GetTrial(ctx, organizationID)
	require.Error(t, err)
	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, organizationID)
	require.NoError(t, err)
	require.Equal(t, "free", org.GramAccountType)
	require.True(t, org.Whitelisted)
	require.Empty(t, ti.trial.trialStarted)
	require.Len(t, ti.trial.adminAdded, 1)
	require.Empty(t, ti.posthog.events)
}

func TestInviteCallback_ExistingTrialLifecycleIsUnchanged(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	_, err := ti.conn.Exec(ctx, "UPDATE users SET admin = true WHERE id = $1", authCtx.UserID)
	require.NoError(t, err)

	const organizationID = "org_existing_invite_trial"
	_, err = orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: "Existing Trial Organization", Slug: "existing-trial-organization",
		WorkosID: conv.ToPGText(organizationID), Whitelisted: pgtype.Bool{Bool: false, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, ti.conn, organizationID))
	require.NoError(t, trialsrepo.New(ti.conn).CreateTrial(ctx, trialsrepo.CreateTrialParams{
		OrganizationID: organizationID, Tier: "enterprise", EndsAt: pgtype.Timestamptz{Time: time.Now().Add(-24 * time.Hour), Valid: true},
	}))
	before, err := trialsrepo.New(ti.conn).GetTrial(ctx, organizationID)
	require.NoError(t, err)

	rawToken, _ := seedInviteCallbackInvite(t, ctx, ti, "existing-lifecycle", organizationID, authCtx.UserID, authz.SystemRoleAdmin)
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", organizationID, authz.SystemRoleAdmin).Return("membership_01INVITE", nil).Once()
	seedInviteeUser(t, ctx, ti)

	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	after, err := trialsrepo.New(ti.conn).GetTrial(ctx, organizationID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, organizationID)
	require.NoError(t, err)
	require.Equal(t, "free", org.GramAccountType)
	require.False(t, org.Whitelisted)
	require.Empty(t, ti.trial.trialStarted)
	require.Len(t, ti.trial.adminAdded, 1)
	require.Empty(t, ti.posthog.events)
}

func TestInviteCallback_AdminInviteNotifiesTrialAdminAdded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	rawToken, invite := seedInviteCallbackInvite(t, ctx, ti, "admin-accepted", authCtx.ActiveOrganizationID, authCtx.UserID, authz.SystemRoleAdmin)
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", authCtx.ActiveOrganizationID, authz.SystemRoleAdmin).Return("membership_01INVITE", nil).Once()
	seedInviteeUser(t, ctx, ti)

	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	var sessionCookie *http.Cookie
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == constants.SessionCookie {
			sessionCookie = cookie
		}
	}
	require.NotNil(t, sessionCookie)
	require.Equal(t, constants.SessionCookieMaxAgeSeconds, sessionCookie.MaxAge)
	require.Equal(t, []adminAddedNotification{{
		organizationID: authCtx.ActiveOrganizationID,
		userID:         "user_01INVITEE",
	}}, ti.trial.adminAdded)

	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", storedInvite.State)
}

func TestInviteCallback_MemberInviteDoesNotNotifyTrialAdminAdded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	rawToken, invite := seedInviteCallbackInvite(t, ctx, ti, "member-accepted", authCtx.ActiveOrganizationID, authCtx.UserID, authz.SystemRoleMember)
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", authCtx.ActiveOrganizationID, authz.SystemRoleMember).Return("membership_01INVITE", nil).Once()
	seedInviteeUser(t, ctx, ti)

	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	require.Empty(t, ti.trial.adminAdded)

	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", storedInvite.State)
}

func TestInviteCallback_TrialNotifierFailurePreservesInviteAcceptance(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	rawToken, invite := seedInviteCallbackInvite(t, ctx, ti, "admin-notifier-failure", authCtx.ActiveOrganizationID, authCtx.UserID, authz.SystemRoleAdmin)
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", authCtx.ActiveOrganizationID, authz.SystemRoleAdmin).Return("membership_01INVITE", nil).Once()
	ti.trial.adminAddedErr = errors.New("notify failed")
	seedInviteeUser(t, ctx, ti)

	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	require.Equal(t, []adminAddedNotification{{
		organizationID: authCtx.ActiveOrganizationID,
		userID:         "user_01INVITEE",
	}}, ti.trial.adminAdded)

	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", storedInvite.State)
}

func TestInviteCallback_RevokedInviteDoesNotNotifyTrialAdminAdded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	rawToken, invite := seedInviteCallbackInvite(t, ctx, ti, "admin-revoked", authCtx.ActiveOrganizationID, authCtx.UserID, authz.SystemRoleAdmin)
	require.NoError(t, orgrepo.New(ti.conn).RevokeInvitation(ctx, invite.ID))

	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	require.Empty(t, ti.trial.adminAdded)

	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "revoked", storedInvite.State)
}

func requireAuthContext(t *testing.T, ctx context.Context) *contextvalues.AuthContext {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	return authCtx
}

func seedInviteCallbackInvite(t *testing.T, ctx context.Context, ti *testInstance, name, organizationID, inviterUserID, roleSlug string) (string, orgrepo.OrganizationInvitation) {
	t.Helper()
	return seedInviteCallbackInviteForEmail(t, ctx, ti, name, organizationID, inviterUserID, roleSlug, "invitee@example.test")
}

func seedInviteCallbackInviteForEmail(t *testing.T, ctx context.Context, ti *testInstance, name, organizationID, inviterUserID, roleSlug, email string) (string, orgrepo.OrganizationInvitation) {
	t.Helper()

	rawToken := "token-" + name
	invite, err := orgrepo.New(ti.conn).CreateInvitation(ctx, orgrepo.CreateInvitationParams{
		OrganizationID: organizationID,
		Email:          email,
		TokenHash:      inviteTokenHash(rawToken),
		InviterUserID:  conv.ToPGText(inviterUserID),
		RoleSlug:       pgtype.Text{String: roleSlug, Valid: roleSlug != ""},
		ExpiresInDays:  7,
	})
	require.NoError(t, err)
	return rawToken, invite
}

func seedInviteeUser(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()
	seedInviteeUserIdentity(t, ctx, ti, "user_01INVITEE", "invitee@example.test")
}

func seedInviteeUserIdentity(t *testing.T, ctx context.Context, ti *testInstance, userID, email string) {
	t.Helper()

	require.NoError(t, testrepo.New(ti.conn).InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{
		ID:          userID,
		Email:       email,
		DisplayName: "Invitee",
	}))
}

func serveInviteCallback(t *testing.T, ti *testInstance, rawToken string) *httptest.ResponseRecorder {
	t.Helper()

	mux := goahttp.NewMuxer()
	organizations.Attach(mux, ti.service)
	req := httptest.NewRequest(http.MethodGet, "/rpc/organizations.inviteCallback?invite_token="+rawToken, nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	return recorder
}

func inviteTokenHash(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}
