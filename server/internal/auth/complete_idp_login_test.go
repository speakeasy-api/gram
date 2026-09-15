package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const (
	idpLoginWorkosUserID = "user_01IDPLOGIN"
	idpLoginWorkosOrgID  = "org_01IDPLOGIN"
	idpLoginGramOrgID    = "idp-login-org"
)

// newIDPLoginInstance seeds a user and a WorkOS-linked org with no membership
// row between them. The fetcher decides what WorkOS reports.
func newIDPLoginInstance(t *testing.T, fetcher *mockWorkOSFetcher) (context.Context, *e2eInstance, *identity.IDPUserInfo, string) {
	t.Helper()

	suffix := uuid.New().String()[:8]
	gramUserID := "user-idp-login-" + suffix
	email := "idp-login-" + suffix + "@example.com"
	userInfo := &MockUserInfo{
		UserID:        gramUserID,
		Email:         email,
		Organizations: []MockOrganizationEntry{},
	}
	ctx, inst := newE2EAuthService(t, userInfo, fetcher)
	require.NoError(t, inst.createTestUser(ctx, userInfo))
	workosOrgID := idpLoginWorkosOrgID
	require.NoError(t, inst.createTestOrganization(ctx, MockOrganizationEntry{
		ID:                 idpLoginGramOrgID,
		Name:               "IDP Login Org",
		Slug:               "idp-login-org",
		WorkosID:           &workosOrgID,
		UserWorkspaceSlugs: []string{"idp-login-org"},
	}, ""))

	return ctx, inst, &identity.IDPUserInfo{Sub: idpLoginWorkosUserID, Email: email, Name: "IDP Login"}, gramUserID
}

func memberFetcher() *mockWorkOSFetcher {
	return &mockWorkOSFetcher{
		members: map[string][]workos.Member{
			idpLoginWorkosUserID: {
				{ID: "om_01IDPLOGIN", UserID: idpLoginWorkosUserID, OrganizationID: idpLoginWorkosOrgID, Organization: "IDP Login Org", RoleSlugs: []string{"member"}},
			},
		},
		orgs: map[string]*workos.Organization{},
	}
}

func TestCompleteIDPLogin_ImportsMissingMembership(t *testing.T) {
	t.Parallel()

	ctx, inst, idpUser, gramUserID := newIDPLoginInstance(t, memberFetcher())

	_, _, ok := inst.identityResolver.HasAccessToOrganization(ctx, idpLoginGramOrgID, gramUserID)
	require.False(t, ok, "no local membership row yet")

	login, err := inst.identityResolver.CompleteIDPLogin(ctx, idpUser, identity.IDPLoginOptions{SkipMembershipSync: false})
	require.NoError(t, err)
	require.Equal(t, gramUserID, login.UserID)
	require.Len(t, login.UserInfo.Organizations, 1)
	require.Equal(t, idpLoginGramOrgID, login.UserInfo.Organizations[0].ID)

	_, _, ok = inst.identityResolver.HasAccessToOrganization(ctx, idpLoginGramOrgID, gramUserID)
	require.True(t, ok)
	member, err := inst.identityResolver.IsOrganizationMember(ctx, idpLoginGramOrgID, gramUserID)
	require.NoError(t, err)
	require.True(t, member)

	rel, err := orgRepo.New(inst.conn).GetOrganizationUserRelationship(ctx, orgRepo.GetOrganizationUserRelationshipParams{
		OrganizationID: idpLoginGramOrgID,
		UserID:         conv.ToPGText(gramUserID),
	})
	require.NoError(t, err)
	require.Equal(t, "om_01IDPLOGIN", rel.WorkosMembershipID.String)
}

func TestCompleteIDPLogin_RefreshesStaleCachedMembership(t *testing.T) {
	t.Parallel()

	ctx, inst, idpUser, gramUserID := newIDPLoginInstance(t, memberFetcher())

	// Warm the cache while the user has no organizations, then add the row
	// behind its back the way the webhook path does.
	cached, _, err := inst.identityResolver.GetUserInfo(ctx, gramUserID)
	require.NoError(t, err)
	require.Empty(t, cached.Organizations)
	_, err = orgRepo.New(inst.conn).UpsertOrganizationUserRelationship(ctx, orgRepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: idpLoginGramOrgID,
		UserID:         conv.ToPGText(gramUserID),
	})
	require.NoError(t, err)

	_, _, ok := inst.identityResolver.HasAccessToOrganization(ctx, idpLoginGramOrgID, gramUserID)
	require.False(t, ok, "cached organization list is stale")

	_, err = inst.identityResolver.CompleteIDPLogin(ctx, idpUser, identity.IDPLoginOptions{SkipMembershipSync: false})
	require.NoError(t, err)

	_, _, ok = inst.identityResolver.HasAccessToOrganization(ctx, idpLoginGramOrgID, gramUserID)
	require.True(t, ok)
}

func TestCompleteIDPLogin_RevokesMembershipWorkOSNoLongerReports(t *testing.T) {
	t.Parallel()

	// A stale local row: WorkOS revoked the member but the webhook has not
	// landed. Login must judge by WorkOS, not by the leftover row.
	fetcher := memberFetcher()
	ctx, inst, idpUser, gramUserID := newIDPLoginInstance(t, fetcher)
	require.NoError(t, inst.identityResolver.SyncMembershipsFromWorkOS(ctx, gramUserID, idpLoginWorkosUserID))
	member, err := inst.identityResolver.IsOrganizationMember(ctx, idpLoginGramOrgID, gramUserID)
	require.NoError(t, err)
	require.True(t, member)

	fetcher.members[idpLoginWorkosUserID] = nil
	login, err := inst.identityResolver.CompleteIDPLogin(ctx, idpUser, identity.IDPLoginOptions{SkipMembershipSync: false})
	require.NoError(t, err)
	require.Empty(t, login.UserInfo.Organizations)

	member, err = inst.identityResolver.IsOrganizationMember(ctx, idpLoginGramOrgID, gramUserID)
	require.NoError(t, err)
	require.False(t, member)
	_, _, ok := inst.identityResolver.HasAccessToOrganization(ctx, idpLoginGramOrgID, gramUserID)
	require.False(t, ok)
}

func TestCompleteIDPLogin_SyncFailureFailsClosed(t *testing.T) {
	t.Parallel()

	fetcher := memberFetcher()
	fetcher.listMembershipsErr = errors.New("workos unavailable")
	ctx, inst, idpUser, gramUserID := newIDPLoginInstance(t, fetcher)

	_, err := inst.identityResolver.CompleteIDPLogin(ctx, idpUser, identity.IDPLoginOptions{SkipMembershipSync: false})
	require.ErrorContains(t, err, "workos unavailable")

	_, _, ok := inst.identityResolver.HasAccessToOrganization(ctx, idpLoginGramOrgID, gramUserID)
	require.False(t, ok)
	_, err = orgRepo.New(inst.conn).GetOrganizationUserRelationship(ctx, orgRepo.GetOrganizationUserRelationshipParams{
		OrganizationID: idpLoginGramOrgID,
		UserID:         conv.ToPGText(gramUserID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestCompleteIDPLogin_SkipMembershipSyncStillRefreshesCache(t *testing.T) {
	t.Parallel()

	fetcher := memberFetcher()
	ctx, inst, idpUser, gramUserID := newIDPLoginInstance(t, fetcher)

	cached, _, err := inst.identityResolver.GetUserInfo(ctx, gramUserID)
	require.NoError(t, err)
	require.Empty(t, cached.Organizations)
	_, err = orgRepo.New(inst.conn).UpsertOrganizationUserRelationship(ctx, orgRepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: idpLoginGramOrgID,
		UserID:         conv.ToPGText(gramUserID),
	})
	require.NoError(t, err)

	login, err := inst.identityResolver.CompleteIDPLogin(ctx, idpUser, identity.IDPLoginOptions{SkipMembershipSync: true})
	require.NoError(t, err)
	require.Zero(t, fetcher.listMembershipsCalls, "support logins never reconcile against WorkOS")
	require.Len(t, login.UserInfo.Organizations, 1)

	rel, err := orgRepo.New(inst.conn).GetOrganizationUserRelationship(ctx, orgRepo.GetOrganizationUserRelationshipParams{
		OrganizationID: idpLoginGramOrgID,
		UserID:         conv.ToPGText(gramUserID),
	})
	require.NoError(t, err)
	require.Equal(t, pgtype.Text{}, rel.WorkosMembershipID, "skipped sync leaves the local row untouched")
}
