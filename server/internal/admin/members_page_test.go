package admin

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestOrganizationMembersPageIsBoundedAndTenantScoped(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{SessionID: "staff-session", OIDCSubject: "staff-subject", Email: "staff@example.test"})
	seedOrg(t, ctx, db, orgFixture{id: "org_members_a", name: "Example A", slug: "members-a"})
	seedOrg(t, ctx, db, orgFixture{id: "org_members_b", name: "Example B", slug: "members-b"})
	queries := testrepo.New(db)
	for _, user := range []struct{ id, org string }{
		{"user_member_a", "org_members_a"},
		{"user_member_b", "org_members_a"},
		{"user_member_c", "org_members_b"},
		{"user_member_removed", "org_members_a"},
		{"user_member_deleted", "org_members_a"},
	} {
		require.NoError(t, queries.InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{ID: user.id, Email: user.id + "@example.test", DisplayName: "Example Member"}))
		seedMembership(t, ctx, db, user.org, user.id)
	}
	require.NoError(t, queries.ForceSoftDeleteOrganizationUserRelationship(ctx, testrepo.ForceSoftDeleteOrganizationUserRelationshipParams{OrganizationID: "org_members_a", UserID: conv.ToPGText("user_member_removed")}))
	require.NoError(t, queries.ForceSoftDeleteUser(ctx, "user_member_deleted"))
	first, err := svc.ListOrganizationMembersPage(ctx, "org_members_a", "", 1)
	require.NoError(t, err)
	require.Equal(t, "org_members_a", first.OrganizationID)
	require.Len(t, first.Members, 1)
	require.Equal(t, "user_member_a", first.Members[0].ID)
	require.Equal(t, "user_member_a", *first.NextUserID)
	second, err := svc.ListOrganizationMembersPage(ctx, "org_members_a", *first.NextUserID, 1)
	require.NoError(t, err)
	require.Len(t, second.Members, 1)
	require.Equal(t, "user_member_b", second.Members[0].ID)
	require.Nil(t, second.NextUserID)
	_, err = svc.ListOrganizationMembersPage(ctx, "org_members_a", "user_member_c", 1)
	require.Error(t, err)
	_, err = svc.ListOrganizationMembersPage(ctx, "org_members_b", *first.NextUserID, 1)
	require.Error(t, err)
	_, err = svc.ListOrganizationMembersPage(ctx, "org_members_a", "user_member_removed", 1)
	require.Error(t, err)
	_, err = svc.ListOrganizationMembersPage(ctx, "members-a", "", 1)
	require.Error(t, err)
}

func TestOrganizationMembersPageRequiresStaffAndValidBounds(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	_, err := svc.ListOrganizationMembersPage(ctx, "org-a", "", 1)
	require.Error(t, err)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{SessionID: "staff-session", OIDCSubject: "staff-subject", Email: "staff@example.test"})
	for _, limit := range []int{0, -1, MaxMemberPageSize + 1} {
		_, err := svc.ListOrganizationMembersPage(ctx, "org-a", "", limit)
		require.Error(t, err)
	}
	_, err = svc.ListOrganizationMembersPage(ctx, "", "", 1)
	require.Error(t, err)
}
