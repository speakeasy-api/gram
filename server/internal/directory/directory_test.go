package directory_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/directory"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func seedOrganization(t *testing.T, conn *pgxpool.Pool, organizationID string) {
	t.Helper()

	_, err := organizationsrepo.New(conn).UpsertOrganizationMetadata(t.Context(), organizationsrepo.UpsertOrganizationMetadataParams{
		ID:       organizationID,
		Name:     organizationID,
		Slug:     organizationID,
		WorkosID: conv.ToPGText("workos_" + organizationID),
	})
	require.NoError(t, err)
}

func seedDirectoryUser(t *testing.T, conn *pgxpool.Pool, organizationID, userID, externalID, email string, attributes []byte, syncedAt time.Time) directoryrepo.DirectoryUser {
	t.Helper()

	_, err := directoryrepo.New(conn).UpsertDirectoryUser(t.Context(), directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        organizationID,
		UserID:                conv.ToPGText(userID),
		WorkosDirectoryUserID: externalID,
		Email:                 conv.ToPGText(email),
		Attributes:            attributes,
		RestoreDeleted:        true,
		WorkosCreatedAt:       conv.ToPGTimestamptz(syncedAt),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(syncedAt),
		WorkosLastEventID:     conv.ToPGText("event_" + externalID),
	})
	require.NoError(t, err)

	row, err := directoryrepo.New(conn).GetDirectoryUserByWorkOSID(t.Context(), externalID)
	require.NoError(t, err)
	return row
}

func seedDirectoryGroup(t *testing.T, conn *pgxpool.Pool, organizationID, externalID, name string, syncedAt time.Time) directoryrepo.GetDirectoryGroupForMembershipByWorkOSIDRow {
	t.Helper()

	_, err := directoryrepo.New(conn).UpsertDirectoryGroup(t.Context(), directoryrepo.UpsertDirectoryGroupParams{
		OrganizationID:         organizationID,
		WorkosDirectoryGroupID: externalID,
		Name:                   name,
		Attributes:             []byte(`{}`),
		WorkosCreatedAt:        conv.ToPGTimestamptz(syncedAt),
		WorkosUpdatedAt:        conv.ToPGTimestamptz(syncedAt),
		WorkosLastEventID:      conv.ToPGText("event_" + externalID),
	})
	require.NoError(t, err)

	row, err := directoryrepo.New(conn).GetDirectoryGroupForMembershipByWorkOSID(t.Context(), externalID)
	require.NoError(t, err)
	return row
}

func addUserToGroup(t *testing.T, conn *pgxpool.Pool, user directoryrepo.DirectoryUser, groupExternalID string, groupID directoryrepo.GetDirectoryGroupForMembershipByWorkOSIDRow, syncedAt time.Time) {
	t.Helper()

	_, err := directoryrepo.New(conn).OpenDirectoryUserGroupMembership(t.Context(), directoryrepo.OpenDirectoryUserGroupMembershipParams{
		DirectoryUserID:        user.ID,
		DirectoryGroupID:       groupID.ID,
		WorkosDirectoryUserID:  user.WorkosDirectoryUserID,
		WorkosDirectoryGroupID: groupExternalID,
		WorkosCreatedAt:        conv.ToPGTimestamptz(syncedAt),
	})
	require.NoError(t, err)
}

func TestServiceGetUserProfileReturnsLatestProfileAsOneSnapshot(t *testing.T) {
	t.Parallel()
	service, conn := newTestService(t)
	ctx := t.Context()

	const organizationID = "org_directory_latest"
	const userID = "user_directory_latest"
	seedOrganization(t, conn, organizationID)

	olderTime := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	newerTime := olderTime.Add(time.Hour)
	older := seedDirectoryUser(t, conn, organizationID, userID, "directory_user_older", "older@example.com", []byte(`{"department":"Older"}`), olderTime)
	newer := seedDirectoryUser(t, conn, organizationID, userID, "directory_user_newer", "newer@example.com", []byte(`{"department":"Engineering","level":7}`), newerTime)

	olderGroup := seedDirectoryGroup(t, conn, organizationID, "directory_group_older", "Older Group", olderTime)
	addUserToGroup(t, conn, older, "directory_group_older", olderGroup, olderTime)

	newerGroup := seedDirectoryGroup(t, conn, organizationID, "directory_group_newer", "Engineering", newerTime)
	addUserToGroup(t, conn, newer, "directory_group_newer", newerGroup, newerTime)

	profile, err := service.GetUserProfile(ctx, organizationID, userID)
	require.NoError(t, err)
	require.Equal(t, newer.ID, profile.ID)
	require.Equal(t, userID, profile.UserID)
	require.Equal(t, "directory_user_newer", profile.ExternalID)
	require.Equal(t, "newer@example.com", profile.Email)
	require.Equal(t, map[string]any{"department": "Engineering", "level": float64(7)}, profile.RawAttributes)
	require.Equal(t, []directory.Group{{ExternalID: "directory_group_newer", Name: "Engineering"}}, profile.Groups)
}

func TestServiceGetUserProfileReturnsOnlyActiveGroupsInStableOrder(t *testing.T) {
	t.Parallel()
	service, conn := newTestService(t)
	ctx := t.Context()

	const organizationID = "org_directory_groups"
	const userID = "user_directory_groups"
	seedOrganization(t, conn, organizationID)
	syncedAt := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	user := seedDirectoryUser(t, conn, organizationID, userID, "directory_user_groups", "groups@example.com", []byte(`{}`), syncedAt)

	for externalID, name := range map[string]string{
		"directory_group_zulu":    "Zulu",
		"directory_group_alpha":   "Alpha",
		"directory_group_closed":  "Closed",
		"directory_group_deleted": "Deleted",
	} {
		group := seedDirectoryGroup(t, conn, organizationID, externalID, name, syncedAt)
		addUserToGroup(t, conn, user, externalID, group, syncedAt)
	}

	closed, err := directoryrepo.New(conn).GetDirectoryGroupForMembershipByWorkOSID(ctx, "directory_group_closed")
	require.NoError(t, err)
	_, err = directoryrepo.New(conn).CloseDirectoryUserGroupMembership(ctx, directoryrepo.CloseDirectoryUserGroupMembershipParams{
		DirectoryUserID:  user.ID,
		DirectoryGroupID: closed.ID,
	})
	require.NoError(t, err)

	_, err = directoryrepo.New(conn).DeleteDirectoryGroupByWorkOSID(ctx, directoryrepo.DeleteDirectoryGroupByWorkOSIDParams{
		WorkosDeletedAt:        conv.ToPGTimestamptz(syncedAt.Add(time.Hour)),
		WorkosLastEventID:      conv.ToPGText("event_delete_group"),
		WorkosDirectoryGroupID: "directory_group_deleted",
	})
	require.NoError(t, err)

	profile, err := service.GetUserProfile(ctx, organizationID, userID)
	require.NoError(t, err)
	require.Equal(t, []directory.Group{
		{ExternalID: "directory_group_alpha", Name: "Alpha"},
		{ExternalID: "directory_group_zulu", Name: "Zulu"},
	}, profile.Groups)
}

func TestServiceGetUserProfileScopesByOrganization(t *testing.T) {
	t.Parallel()
	service, conn := newTestService(t)
	ctx := t.Context()

	const userID = "user_directory_tenant"
	seedOrganization(t, conn, "org_directory_one")
	seedOrganization(t, conn, "org_directory_two")
	syncedAt := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	seedDirectoryUser(t, conn, "org_directory_one", userID, "directory_user_one", "one@example.com", []byte(`{"tenant":"one"}`), syncedAt)
	seedDirectoryUser(t, conn, "org_directory_two", userID, "directory_user_two", "two@example.com", []byte(`{"tenant":"two"}`), syncedAt)

	profile, err := service.GetUserProfile(ctx, "org_directory_two", userID)
	require.NoError(t, err)
	require.Equal(t, "directory_user_two", profile.ExternalID)
	require.Equal(t, map[string]any{"tenant": "two"}, profile.RawAttributes)

	_, err = service.GetUserProfile(ctx, "org_directory_missing", userID)
	require.ErrorIs(t, err, directory.ErrUserNotFound)
}

func TestServiceGetUserProfileReturnsEmptyGroups(t *testing.T) {
	t.Parallel()
	service, conn := newTestService(t)
	ctx := t.Context()

	const organizationID = "org_directory_no_groups"
	const userID = "user_directory_no_groups"
	seedOrganization(t, conn, organizationID)
	seedDirectoryUser(t, conn, organizationID, userID, "directory_user_no_groups", "no-groups@example.com", []byte(`{}`), time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC))

	profile, err := service.GetUserProfile(ctx, organizationID, userID)
	require.NoError(t, err)
	require.Empty(t, profile.Groups)
	require.NotNil(t, profile.Groups)

	_, err = service.GetUserProfile(ctx, organizationID, "unknown_user")
	require.ErrorIs(t, err, directory.ErrUserNotFound)
}

func TestServiceGetUserProfileExcludesDeletedUser(t *testing.T) {
	t.Parallel()
	service, conn := newTestService(t)
	ctx := t.Context()

	const organizationID = "org_directory_deleted_user"
	const userID = "user_directory_deleted_user"
	const externalID = "directory_user_deleted"
	seedOrganization(t, conn, organizationID)
	syncedAt := time.Date(2026, time.April, 2, 0, 0, 0, 0, time.UTC)
	seedDirectoryUser(t, conn, organizationID, userID, externalID, "deleted@example.invalid", []byte(`{"department":"Engineering"}`), syncedAt)

	_, err := directoryrepo.New(conn).DeleteDirectoryUserByWorkOSID(ctx, directoryrepo.DeleteDirectoryUserByWorkOSIDParams{
		WorkosDeletedAt:       conv.ToPGTimestamptz(syncedAt.Add(time.Hour)),
		WorkosLastEventID:     conv.ToPGText("event_delete_user"),
		WorkosDirectoryUserID: externalID,
	})
	require.NoError(t, err)

	_, err = service.GetUserProfile(ctx, organizationID, userID)
	require.ErrorIs(t, err, directory.ErrUserNotFound)
}

func TestServiceGetUserProfileAcceptsNullAttributeValues(t *testing.T) {
	t.Parallel()
	service, conn := newTestService(t)
	ctx := t.Context()

	const organizationID = "org_directory_null_attribute"
	const userID = "user_directory_null_attribute"
	seedOrganization(t, conn, organizationID)
	seedDirectoryUser(
		t,
		conn,
		organizationID,
		userID,
		"directory_user_null_attribute",
		"null-attribute@example.com",
		[]byte(`{"department_name":null,"job_title":"Platform Engineer"}`),
		time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC),
	)

	profile, err := service.GetUserProfile(ctx, organizationID, userID)
	require.NoError(t, err)
	require.Contains(t, profile.RawAttributes, "department_name")
	require.Nil(t, profile.RawAttributes["department_name"])
	require.Equal(t, "Platform Engineer", profile.RawAttributes["job_title"])
	require.Equal(t, directory.UserAttributes{JobTitle: "Platform Engineer"}, profile.Attributes())
	require.NotNil(t, profile.Groups)
	require.Empty(t, profile.Groups)
}

func TestServiceGetUserProfileTreatsNonObjectAttributesAsEmpty(t *testing.T) {
	t.Parallel()
	service, conn := newTestService(t)
	ctx := t.Context()

	const organizationID = "org_directory_non_object_attributes"
	const userID = "user_directory_non_object_attributes"
	seedOrganization(t, conn, organizationID)
	seedDirectoryUser(
		t,
		conn,
		organizationID,
		userID,
		"directory_user_non_object_attributes",
		"non-object-attributes@example.com",
		[]byte(`["department_name","job_title"]`),
		time.Date(2026, time.May, 2, 0, 0, 0, 0, time.UTC),
	)

	profile, err := service.GetUserProfile(ctx, organizationID, userID)
	require.NoError(t, err)
	require.NotNil(t, profile.RawAttributes)
	require.Empty(t, profile.RawAttributes)
	require.True(t, profile.Attributes().IsZero())
}

func TestServiceDirectoryAssociations(t *testing.T) {
	t.Parallel()

	service, conn := newTestService(t)
	ctx := t.Context()

	const organizationID = "org_directory_associations"
	const email = "member@example.com"
	seedOrganization(t, conn, organizationID)
	syncedAt := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	user := seedDirectoryUser(
		t,
		conn,
		organizationID,
		"user_directory_associations",
		"directory_user_associations",
		email,
		[]byte(`{"department":"engineering","unused":null}`),
		syncedAt,
	)
	seedDirectoryUser(
		t,
		conn,
		organizationID,
		"user_directory_non_object_attributes",
		"directory_user_non_object_attributes",
		email,
		[]byte(`[]`),
		syncedAt.Add(time.Minute),
	)
	group := seedDirectoryGroup(t, conn, organizationID, "directory_group_associations", "Engineering", syncedAt)
	addUserToGroup(t, conn, user, "directory_group_associations", group, syncedAt)

	associations, err := service.ResolveUserAssociationsByEmails(ctx, organizationID, []string{
		" MEMBER@example.com ",
		email,
		"",
	})
	require.NoError(t, err)
	require.Equal(t, map[string]directory.UserAssociations{
		email: {
			GroupIDs: []uuid.UUID{group.ID},
			Attributes: []directory.AttributeValue{
				{Key: "department", Value: "engineering"},
			},
		},
	}, associations)

	groups, err := service.ListActiveGroups(ctx, organizationID)
	require.NoError(t, err)
	require.Equal(t, []directory.GroupSummary{
		{ID: group.ID, Name: "Engineering", MemberCount: 1},
	}, groups)

	attributes, err := service.ListActiveAttributeValues(ctx, organizationID)
	require.NoError(t, err)
	require.Equal(t, []directory.AttributeValueSummary{
		{AttributeValue: directory.AttributeValue{Key: "department", Value: "engineering"}, MemberCount: 1},
	}, attributes)

	exists, err := service.GroupExists(ctx, organizationID, group.ID)
	require.NoError(t, err)
	require.True(t, exists)

	exists, err = service.AttributeValueExists(ctx, organizationID, directory.AttributeValue{Key: "department", Value: "engineering"})
	require.NoError(t, err)
	require.True(t, exists)

	exists, err = service.AttributeValueExists(ctx, organizationID, directory.AttributeValue{Key: "unused", Value: "null"})
	require.NoError(t, err)
	require.False(t, exists)
}

func TestServiceResolveUserAssociationsByEmailsWithNoEmails(t *testing.T) {
	t.Parallel()

	service, _ := newTestService(t)
	associations, err := service.ResolveUserAssociationsByEmails(t.Context(), "org_directory_empty_emails", []string{"", "  "})
	require.NoError(t, err)
	require.Empty(t, associations)
	require.NotNil(t, associations)
}
