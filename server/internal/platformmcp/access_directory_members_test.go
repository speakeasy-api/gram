package platformmcp

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestAccessReadServiceFindsDirectoryRoleMembers(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_directory_members")
	require.NoError(t, err)
	principal, _ := seedRegistrationLifecycle(t, ctx, conn)
	service := NewAccessReadService(testenv.NewLogger(t), conn, allowBudget(), "access-read-key")
	role := seedAccessRole(t, ctx, conn, principal.OrganizationID, "operators", "Operators")
	groupID := seedAccessDirectoryGroup(t, ctx, conn, principal.OrganizationID)
	_, err = accessrepo.New(conn).InsertDirectoryGroupRoleMapping(ctx, accessrepo.InsertDirectoryGroupRoleMappingParams{
		OrganizationID: principal.OrganizationID, DirectoryGroupID: uuid.NullUUID{UUID: groupID, Valid: true}, RoleUrn: role.String(),
	})
	require.NoError(t, err)
	_, err = accessrepo.New(conn).InsertDirectoryAttributeRoleMapping(ctx, accessrepo.InsertDirectoryAttributeRoleMappingParams{
		OrganizationID: principal.OrganizationID, AttributeKey: conv.ToPGText("department"), AttributeValue: conv.ToPGText("Operations"), RoleUrn: role.String(),
	})
	require.NoError(t, err)

	// Five group members and five attribute matches overlap on two members.
	for i := range 8 {
		userID := fmt.Sprintf("directory-member-%d", i)
		email := fmt.Sprintf("person%d@example.test", i)
		seedAccessMember(t, ctx, conn, principal.OrganizationID, userID, email)
		attributes := `{}`
		if i >= 3 {
			attributes = `{"department":"Operations"}`
		}
		profile := seedAccessDirectoryProfile(t, ctx, conn, principal.OrganizationID, userID, email, attributes)
		if i < 5 {
			_, err = directoryrepo.New(conn).OpenDirectoryUserGroupMembership(ctx, directoryrepo.OpenDirectoryUserGroupMembershipParams{
				DirectoryUserID: profile, DirectoryGroupID: groupID, WorkosDirectoryUserID: "directory-" + userID,
				WorkosDirectoryGroupID: "group-operators", WorkosCreatedAt: conv.ToPGTimestamptz(time.Now().UTC()),
			})
			require.NoError(t, err)
		}
		if i == 0 {
			// A direct assignment of the same role must not duplicate a member.
			_, err = accessrepo.New(conn).UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{
				OrganizationID: principal.OrganizationID, WorkosUserID: "workos-" + userID, UserID: conv.ToPGText(userID),
				WorkosMembershipID: conv.ToPGText("membership-" + userID), WorkosUpdatedAt: conv.ToPGTimestamptz(time.Now().UTC()), WorkosRoleSlug: "operators",
			})
			require.NoError(t, err)
		}
	}
	// Provisioned directory users who have not joined the organization are not members.
	seedAccessDirectoryProfile(t, ctx, conn, principal.OrganizationID, "", "unjoined@example.test", `{"department":"Operations"}`)

	roles, err := service.ListRoles(ctx, principal)
	require.NoError(t, err)
	var reference string
	for _, candidate := range roles.Roles {
		if candidate.Name == "Operators" {
			reference = candidate.Reference
			require.Equal(t, NewSubjectCount(8), candidate.MemberCount)
		}
	}
	require.NotEmpty(t, reference)
	members, err := service.ListMembers(ctx, principal, ListAccessMembersInput{RoleReference: reference})
	require.NoError(t, err)
	require.Equal(t, NewSubjectCount(8), members.TotalMatches)
	require.Len(t, members.Members, 8)
	require.False(t, members.Suppressed)
	require.False(t, members.Truncated)
	for _, member := range members.Members {
		require.Equal(t, []string{"Operators"}, member.Roles)
		require.NotContains(t, member.MaskedIdentity, "example.test")
		userID, err := service.references.Decode(member.Reference, principal, subjectKindAccessMember, service.now())
		require.NoError(t, err)
		// Assignment concurrency versions continue to cover direct roles only.
		direct, err := accessrepo.New(conn).ListActiveRoleIDsByWorkosUser(ctx, accessrepo.ListActiveRoleIDsByWorkosUserParams{
			OrganizationID: principal.OrganizationID, WorkosUserID: "workos-" + userID,
		})
		require.NoError(t, err)
		expected, err := accessMemberRoleVersion(service.versionKey, userID, direct)
		require.NoError(t, err)
		require.Equal(t, expected, member.Version)
	}
	byName, err := service.ListMembers(ctx, principal, ListAccessMembersInput{Query: "OPERATORS", Limit: 2})
	require.NoError(t, err)
	require.Equal(t, NewSubjectCount(8), byName.TotalMatches)
	require.Len(t, byName.Members, 2)
	require.True(t, byName.Truncated)
	small, err := service.ListMembers(ctx, principal, ListAccessMembersInput{RoleReference: reference, Query: "person1"})
	require.NoError(t, err)
	require.True(t, small.Suppressed)
	require.Empty(t, small.Members)
	exact, err := service.ListMembers(ctx, principal, ListAccessMembersInput{RoleReference: reference, Query: "person1@example.test"})
	require.NoError(t, err)
	require.True(t, exact.ExactEmailMatch)
	require.Equal(t, []string{"Operators"}, exact.Members[0].Roles)
}

func TestAccessReadServiceDirectoryProfileSelectionAndRevocation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                        string
		linkedUser                  string
		differentOrganization       bool
		preferredNonMatchingProfile bool
		revoke                      string
		wantRole                    bool
	}{
		{name: "linked", linkedUser: "member", wantRole: true},
		{name: "unlinked email fallback", wantRole: true},
		{name: "another users profile", linkedUser: "other-member"},
		{name: "another organization", differentOrganization: true},
		{name: "linked profile wins", preferredNonMatchingProfile: true},
		{name: "deleted profile", linkedUser: "member", revoke: "profile"},
		{name: "deleted mapping", linkedUser: "member", revoke: "mapping"},
		{name: "deleted role", linkedUser: "member", revoke: "role"},
		{name: "closed group membership", linkedUser: "member", revoke: "membership"},
		{name: "deleted group", linkedUser: "member", revoke: "group"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_directory_profile")
			require.NoError(t, err)
			principal, _ := seedRegistrationLifecycle(t, ctx, conn)
			service := NewAccessReadService(testenv.NewLogger(t), conn, allowBudget(), "access-read-key")
			seedAccessMember(t, ctx, conn, principal.OrganizationID, "member", "person@example.test")
			seedAccessMember(t, ctx, conn, principal.OrganizationID, "other-member", "other@example.test")
			role := seedAccessRole(t, ctx, conn, principal.OrganizationID, "operators", "Operators")
			org := principal.OrganizationID
			if tc.differentOrganization {
				other, _ := seedRegistrationLifecycle(t, ctx, conn)
				org = other.OrganizationID
			}
			groupID := seedAccessDirectoryGroup(t, ctx, conn, org)
			mapping, err := accessrepo.New(conn).InsertDirectoryGroupRoleMapping(ctx, accessrepo.InsertDirectoryGroupRoleMappingParams{
				OrganizationID: org, DirectoryGroupID: uuid.NullUUID{UUID: groupID, Valid: true}, RoleUrn: role.String(),
			})
			require.NoError(t, err)
			profile := seedAccessDirectoryProfile(t, ctx, conn, org, tc.linkedUser, "PERSON@EXAMPLE.TEST", `{}`)
			if tc.preferredNonMatchingProfile {
				seedAccessDirectoryProfile(t, ctx, conn, org, "member", "updated@example.test", `{}`)
			}
			now := conv.ToPGTimestamptz(time.Now().UTC())
			_, err = directoryrepo.New(conn).OpenDirectoryUserGroupMembership(ctx, directoryrepo.OpenDirectoryUserGroupMembershipParams{
				DirectoryUserID: profile, DirectoryGroupID: groupID, WorkosDirectoryUserID: "profile", WorkosDirectoryGroupID: "group-operators", WorkosCreatedAt: now,
			})
			require.NoError(t, err)
			switch tc.revoke {
			case "profile":
				err = directoryrepo.New(conn).SetOrganizationDirectoryUserDeletionFixture(ctx, directoryrepo.SetOrganizationDirectoryUserDeletionFixtureParams{OrganizationID: org, LocalDeleted: true, WorkosDeleted: true})
			case "mapping":
				_, err = accessrepo.New(conn).DeleteDirectoryRoleMapping(ctx, accessrepo.DeleteDirectoryRoleMappingParams{OrganizationID: org, ID: mapping.ID})
			case "role":
				_, err = accessrepo.New(conn).MarkOrganizationRoleDeletedLocally(ctx, accessrepo.MarkOrganizationRoleDeletedLocallyParams{OrganizationID: org, WorkosSlug: "operators"})
			case "membership":
				_, err = directoryrepo.New(conn).CloseDirectoryUserGroupMembership(ctx, directoryrepo.CloseDirectoryUserGroupMembershipParams{DirectoryUserID: profile, DirectoryGroupID: groupID})
			case "group":
				_, err = directoryrepo.New(conn).DeleteDirectoryGroupByWorkOSID(ctx, directoryrepo.DeleteDirectoryGroupByWorkOSIDParams{OrganizationID: org, WorkosDirectoryGroupID: "group-operators", WorkosDeletedAt: now})
			}
			require.NoError(t, err)
			output, err := service.ListMembers(ctx, principal, ListAccessMembersInput{Query: "person@example.test"})
			require.NoError(t, err)
			require.True(t, output.ExactEmailMatch)
			require.Len(t, output.Members, 1)
			require.Equal(t, tc.wantRole, len(output.Members[0].Roles) == 1)
			// Keep the search's role projection aligned with actual authorization.
			principals, err := authz.ResolveUserPrincipals(ctx, conn, principal.OrganizationID, "member")
			require.NoError(t, err)
			require.Equal(t, tc.wantRole, slices.Contains(principals, role))
		})
	}
}

func seedAccessDirectoryGroup(t *testing.T, ctx context.Context, conn *pgxpool.Pool, org string) uuid.UUID {
	t.Helper()
	now := conv.ToPGTimestamptz(time.Now().UTC())
	group, err := directoryrepo.New(conn).UpsertDirectoryGroup(ctx, directoryrepo.UpsertDirectoryGroupParams{
		OrganizationID: org, WorkosDirectoryGroupID: "group-operators", Name: "Operations", Attributes: []byte(`{}`), WorkosCreatedAt: now, WorkosUpdatedAt: now,
	})
	require.NoError(t, err)
	return group
}

func seedAccessDirectoryProfile(t *testing.T, ctx context.Context, conn *pgxpool.Pool, org, userID, email, attributes string) uuid.UUID {
	t.Helper()
	now := conv.ToPGTimestamptz(time.Now().UTC())
	profile, err := directoryrepo.New(conn).UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
		OrganizationID: org, UserID: conv.ToPGTextEmpty(userID), WorkosDirectoryUserID: "directory-" + email,
		Email: conv.ToPGText(email), Attributes: []byte(attributes), WorkosCreatedAt: now, WorkosUpdatedAt: now,
	})
	require.NoError(t, err)
	return profile
}
