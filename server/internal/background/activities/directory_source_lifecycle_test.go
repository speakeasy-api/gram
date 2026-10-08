package activities_test

import (
	"context"
	"encoding/json"

	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	meteringrepo "github.com/speakeasy-api/gram/server/internal/metering/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	spendrepo "github.com/speakeasy-api/gram/server/internal/spendrules/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
)

func sourceLifecycleEvent(t *testing.T, kind, eventID, orgID, directoryID, sourceID, name string, at time.Time) events.Event {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"id": sourceID, "organization_id": orgID, "directory_id": directoryID,
		"name": name, "email": "directory-person@example.com", "state": "active",
		"custom_attributes": map[string]string{"department_name": "Engineering"},
		"created_at":        directorySyncTime(), "updated_at": at,
	})
	require.NoError(t, err)
	return events.Event{ID: eventID, Event: kind, CreatedAt: at, Data: data}
}

func TestDirectorySourceLifecycleDeletionAndRestoration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "directory_source_lifecycle")
	const orgID, workosOrgID = "gram_source_lifecycle", "org_source_lifecycle"
	seedWorkOSOrganization(t, ctx, conn, orgID, workosOrgID)
	seedWorkOSOrganization(t, ctx, conn, "gram_other_tenant", "org_other_tenant")
	seedWorkOSUser(t, ctx, conn, "directory-person", "user_directory_person")
	_, err := orgrepo.New(conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: orgID, UserID: conv.ToPGText("directory-person"),
	})
	require.NoError(t, err)
	role := seedOrganizationRole(t, ctx, conn, orgID, "engineering")
	roleURN := "role:organization:" + role.ID.String()
	queries := directoryrepo.New(conn)
	stub := workos.NewStubClient()
	activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), conn, stub, cache.NoopCache, nil)
	apply := func(es ...events.Event) {
		t.Helper()
		stub.SetEventPages([][]events.Event{es})
		_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
		require.NoError(t, err)
	}
	at := directorySyncTime()
	apply(
		sourceLifecycleEvent(t, "dsync.user.created", "event_001", workosOrgID, "directory_a", "directory_user_a", "", at),
		sourceLifecycleEvent(t, "dsync.group.created", "event_002", workosOrgID, "directory_a", "group_a", "Engineering", at),
	)
	user, err := queries.GetDirectoryUserByWorkOSID(ctx, "directory_user_a")
	require.NoError(t, err)
	require.Equal(t, "directory_a", user.DirectoryID.String)
	group, err := queries.GetDirectoryGroupSyncStateByWorkOSID(ctx, "group_a")
	require.NoError(t, err)
	_, err = queries.OpenDirectoryUserGroupMembership(ctx, directoryrepo.OpenDirectoryUserGroupMembershipParams{
		DirectoryUserID: user.ID, DirectoryGroupID: group.ID,
		WorkosDirectoryUserID: "directory_user_a", WorkosDirectoryGroupID: "group_a", WorkosCreatedAt: conv.ToPGTimestamptz(at),
	})
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertDirectoryGroupRoleMapping(ctx, accessrepo.UpsertDirectoryGroupRoleMappingParams{
		OrganizationID: orgID, DirectoryGroupID: uuid.NullUUID{UUID: group.ID, Valid: true}, RoleUrn: roleURN,
	})
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertDirectoryAttributeRoleMapping(ctx, accessrepo.UpsertDirectoryAttributeRoleMappingParams{
		OrganizationID: orgID, AttributeKey: conv.ToPGText("department_name"), AttributeValue: conv.ToPGText("Engineering"), RoleUrn: roleURN,
	})
	require.NoError(t, err)
	roles, err := accessrepo.New(conn).ListUserRolePrincipals(ctx, accessrepo.ListUserRolePrincipalsParams{OrganizationID: orgID, UserID: "directory-person"})
	require.NoError(t, err)
	require.NotEmpty(t, roles)
	fixtures := testrepo.New(conn)
	projectID, err := fixtures.CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{ID: uuid.New(), OrganizationID: orgID, Name: "Directory test", Slug: "directory-test"})
	require.NoError(t, err)
	chatID, err := fixtures.SeedCapturedAgentChatFixture(ctx, testrepo.SeedCapturedAgentChatFixtureParams{ID: uuid.New(), ProjectID: projectID, OrganizationID: orgID, UserID: conv.ToPGText("directory-person")})
	require.NoError(t, err)
	messageID, err := fixtures.InsertChatMessage(ctx, testrepo.InsertChatMessageParams{ChatID: chatID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Role: "user", Content: "Test"})
	require.NoError(t, err)
	attribution, err := riskrepo.New(conn).GetChatMessageAttribution(ctx, riskrepo.GetChatMessageAttributionParams{Ids: []uuid.UUID{messageID}, ProjectIds: []uuid.UUID{projectID}})
	require.NoError(t, err)
	require.Len(t, attribution, 1)
	require.Equal(t, "Engineering", attribution[0].Team)

	// Rename keeps the approved source identity and its mapping.
	apply(sourceLifecycleEvent(t, "dsync.group.updated", "event_003", workosOrgID, "directory_a", "group_a", "Platform", at.Add(time.Minute)))
	_, name, _, deleted := getDirectoryGroupRow(t, ctx, conn, "group_a")
	require.Equal(t, "Platform", name)
	require.False(t, deleted)
	for _, seed := range []struct{ org, directory, id string }{
		{orgID, "directory_b", "other_directory"},
		{"gram_other_tenant", "directory_a", "other_tenant"},
		{orgID, "", "unattributed"},
	} {
		_, err := queries.UpsertDirectoryGroup(ctx, directoryrepo.UpsertDirectoryGroupParams{
			OrganizationID: seed.org, WorkosDirectoryGroupID: "group_" + seed.id,
			DirectoryID: conv.ToPGTextEmpty(seed.directory), Name: seed.id, Attributes: []byte(`{}`),
			WorkosCreatedAt: conv.ToPGTimestamptz(at), WorkosUpdatedAt: conv.ToPGTimestamptz(at),
		})
		require.NoError(t, err)
		_, err = queries.UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
			OrganizationID: seed.org, WorkosDirectoryUserID: "user_" + seed.id,
			DirectoryID: conv.ToPGTextEmpty(seed.directory), Attributes: []byte(`{}`),
			WorkosCreatedAt: conv.ToPGTimestamptz(at), WorkosUpdatedAt: conv.ToPGTimestamptz(at),
		})
		require.NoError(t, err)
	}
	deletion := sourceLifecycleEvent(t, "dsync.deleted", "event_010", workosOrgID, "", "directory_a", "", at.Add(time.Hour))
	apply(deletion)
	_, _, _, deleted = getDirectoryGroupRow(t, ctx, conn, "group_a")
	require.True(t, deleted)
	roles, err = accessrepo.New(conn).ListUserRolePrincipals(ctx, accessrepo.ListUserRolePrincipalsParams{OrganizationID: orgID, UserID: "directory-person"})
	require.NoError(t, err)
	require.Empty(t, roles, "both group and attribute mappings must stop contributing")
	sources, err := accessrepo.New(conn).ListUserDirectoryRoleMappingSources(ctx, accessrepo.ListUserDirectoryRoleMappingSourcesParams{OrganizationID: orgID, UserID: "directory-person"})
	require.NoError(t, err)
	require.Empty(t, sources)
	counts, err := accessrepo.New(conn).ListDirectoryMappedRoleMemberCounts(ctx, accessrepo.ListDirectoryMappedRoleMemberCountsParams{OrganizationID: orgID, RoleUrns: []string{roleURN}})
	require.NoError(t, err)
	require.Empty(t, counts)
	actors, err := spendrepo.New(conn).ListOrgActors(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, actors, 1, "directory deletion must preserve membership")
	require.JSONEq(t, `{}`, string(actors[0].Attributes))
	require.Empty(t, actors[0].GroupNames)
	billing, err := meteringrepo.New(conn).ResolveBillingUserAttributes(ctx, meteringrepo.ResolveBillingUserAttributesParams{
		OrganizationIds: []string{orgID}, BillingUserIds: []string{"directory-person"},
	})
	require.NoError(t, err)
	require.Len(t, billing, 1)
	require.Empty(t, billing[0].BillingUserDepartmentName)
	require.Empty(t, billing[0].BillingUserGroupNames)
	attribution, err = riskrepo.New(conn).GetChatMessageAttribution(ctx, riskrepo.GetChatMessageAttributionParams{Ids: []uuid.UUID{messageID}, ProjectIds: []uuid.UUID{projectID}})
	require.NoError(t, err)
	require.Len(t, attribution, 1)
	require.Empty(t, attribution[0].Team)
	options, err := platformrepo.New(conn).ListPlatformMCPPluginAssignmentOptions(ctx, platformrepo.ListPlatformMCPPluginAssignmentOptionsParams{OrganizationID: orgID, ResultLimit: 100})
	require.NoError(t, err)
	for _, option := range options {
		require.NotEqual(t, "directory_group:"+group.ID.String(), option.PrincipalUrn)
		require.NotEqual(t, "department_name: Engineering", option.DisplayName)
	}
	for _, id := range []string{"other_directory", "other_tenant", "unattributed"} {
		_, _, _, deleted := getDirectoryGroupRow(t, ctx, conn, "group_"+id)
		require.False(t, deleted)
		_, err := queries.GetDirectoryUserByWorkOSID(ctx, "user_"+id)
		require.NoError(t, err)
	}

	// Replays and list refreshes cannot restore the tombstone.
	apply(sourceLifecycleEvent(t, "dsync.group.updated", "event_004", workosOrgID, "directory_a", "group_a", "Stale", at))
	_, err = queries.UpsertListedDirectoryGroup(ctx, directoryrepo.UpsertListedDirectoryGroupParams{
		OrganizationID: orgID, WorkosDirectoryGroupID: "group_a", DirectoryID: conv.ToPGText("directory_a"),
		Name: "Listed", Attributes: []byte(`{}`), WorkosCreatedAt: conv.ToPGTimestamptz(at), WorkosUpdatedAt: conv.ToPGTimestamptz(at.Add(2 * time.Hour)),
	})
	require.NoError(t, err)
	_, _, _, deleted = getDirectoryGroupRow(t, ctx, conn, "group_a")
	require.True(t, deleted)
	apply(sourceLifecycleEvent(t, "dsync.group.created", "event_011", workosOrgID, "directory_a", "group_recreated", "Platform", at.Add(2*time.Hour)))
	recreated, err := fixtures.GetDirectoryGroupLifecycleFixture(ctx, testrepo.GetDirectoryGroupLifecycleFixtureParams{OrganizationID: orgID, WorkosDirectoryGroupID: "group_recreated"})
	require.NoError(t, err)
	mappings, err := accessrepo.New(conn).ListDirectoryRoleMappings(ctx, orgID)
	require.NoError(t, err)
	for _, mapping := range mappings {
		require.NotEqual(t, recreated.ID, mapping.DirectoryGroupID.UUID, "a same-name source with a new ID does not inherit mappings")
	}

	// New authoritative events restore the original source. Replaying directory
	// deletion afterwards cannot revoke the newer state.
	apply(
		sourceLifecycleEvent(t, "dsync.group.updated", "event_012", workosOrgID, "directory_a", "group_a", "Platform", at.Add(2*time.Hour)),
		sourceLifecycleEvent(t, "dsync.user.updated", "event_013", workosOrgID, "directory_a", "directory_user_a", "", at.Add(2*time.Hour)),
		deletion,
	)
	roles, err = accessrepo.New(conn).ListUserRolePrincipals(ctx, accessrepo.ListUserRolePrincipalsParams{OrganizationID: orgID, UserID: "directory-person"})
	require.NoError(t, err)
	require.NotEmpty(t, roles)
	_, _, _, deleted = getDirectoryGroupRow(t, ctx, conn, "group_a")
	require.False(t, deleted)
	user, err = queries.GetDirectoryUserByWorkOSID(ctx, "directory_user_a")
	require.NoError(t, err)
	require.False(t, user.WorkosDeleted)

	// Old payloads without directory_id do not erase authoritative attribution.
	apply(sourceLifecycleEvent(t, "dsync.group.updated", "event_014", workosOrgID, "", "group_a", "Platform", at.Add(3*time.Hour)))
	current, err := fixtures.GetDirectoryGroupLifecycleFixture(ctx, testrepo.GetDirectoryGroupLifecycleFixtureParams{OrganizationID: orgID, WorkosDirectoryGroupID: "group_a"})
	require.NoError(t, err)
	require.Equal(t, "directory_a", current.DirectoryID.String)
}

func TestDirectorySourceLifecycleConflictingTenantDoesNotBlockEvents(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "directory_conflicting_tenant")
	seedWorkOSOrganization(t, ctx, conn, "gram_original_tenant", "org_original_tenant")
	seedWorkOSOrganization(t, ctx, conn, "gram_event_tenant", "org_event_tenant")
	at := directorySyncTime()
	stub := newWorkOSClientWithEvents([][]events.Event{{
		sourceLifecycleEvent(t, "dsync.group.created", "event_001", "org_original_tenant", "directory_original", "shared_group_id", "Original", at),
		sourceLifecycleEvent(t, "dsync.user.created", "event_002", "org_original_tenant", "directory_original", "shared_user_id", "", at),
	}})
	activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), conn, stub, cache.NoopCache, nil)
	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: "org_original_tenant"})
	require.NoError(t, err)
	inactive := sourceLifecycleEvent(t, "dsync.user.updated", "event_015", "org_event_tenant", "directory_conflicting", "shared_user_id", "", at.Add(time.Hour))
	var data map[string]any
	require.NoError(t, json.Unmarshal(inactive.Data, &data))
	data["state"] = "inactive"
	inactive.Data, err = json.Marshal(data)
	require.NoError(t, err)
	stub.SetEventPages([][]events.Event{{
		sourceLifecycleEvent(t, "dsync.group.updated", "event_011", "org_event_tenant", "directory_conflicting", "shared_group_id", "Conflicting", at.Add(time.Hour)),
		sourceLifecycleEvent(t, "dsync.user.updated", "event_012", "org_event_tenant", "directory_conflicting", "shared_user_id", "", at.Add(time.Hour)),
		sourceLifecycleEvent(t, "dsync.group.deleted", "event_013", "org_event_tenant", "directory_conflicting", "shared_group_id", "", at.Add(time.Hour)),
		sourceLifecycleEvent(t, "dsync.user.deleted", "event_014", "org_event_tenant", "directory_conflicting", "shared_user_id", "", at.Add(time.Hour)),
		inactive,
		sourceLifecycleEvent(t, "dsync.group.created", "event_016", "org_event_tenant", "directory_event", "valid_group_id", "Valid", at.Add(time.Hour)),
	}})
	result, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: "org_event_tenant"})
	require.NoError(t, err)
	require.Equal(t, "event_016", result.LastEventID)
	fixtures := testrepo.New(conn)
	group, err := fixtures.GetDirectoryGroupLifecycleFixture(ctx, testrepo.GetDirectoryGroupLifecycleFixtureParams{OrganizationID: "gram_original_tenant", WorkosDirectoryGroupID: "shared_group_id"})
	require.NoError(t, err)
	require.Equal(t, "directory_original", group.DirectoryID.String)
	require.False(t, group.Deleted)
	require.False(t, group.WorkosDeleted)
	require.Equal(t, "event_001", group.WorkosLastEventID.String)
	user, err := fixtures.GetDirectoryUserLifecycleFixture(ctx, testrepo.GetDirectoryUserLifecycleFixtureParams{OrganizationID: "gram_original_tenant", WorkosDirectoryUserID: "shared_user_id"})
	require.NoError(t, err)
	require.Equal(t, "directory_original", user.DirectoryID.String)
	require.False(t, user.Deleted)
	require.False(t, user.WorkosDeleted)
	require.Equal(t, "event_002", user.WorkosLastEventID.String)
	_, _, _, deleted := getDirectoryGroupRow(t, ctx, conn, "valid_group_id")
	require.False(t, deleted)
}

func TestDirectoryDeletionWaitsForAttributionTransaction(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "directory_attribution_lock")
	const orgID, workosOrgID = "gram_attribution_lock", "org_attribution_lock"
	seedWorkOSOrganization(t, ctx, conn, orgID, workosOrgID)
	at := directorySyncTime()
	stub := newWorkOSClientWithEvents([][]events.Event{{
		sourceLifecycleEvent(t, "dsync.group.created", "event_001", workosOrgID, "", "attribution_group", "Group", at),
		sourceLifecycleEvent(t, "dsync.user.created", "event_002", workosOrgID, "", "attribution_user", "", at),
	}})
	activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), conn, stub, cache.NoopCache, nil)
	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, ctx, conn)
	require.NoError(t, workosrepo.New(tx).LockOrganizationSync(ctx, workosOrgID))
	queries := directoryrepo.New(tx)
	_, err = queries.AttributeDirectoryUsers(ctx, directoryrepo.AttributeDirectoryUsersParams{OrganizationID: orgID, DirectoryID: "directory_1", WorkosDirectoryUserIds: []string{"attribution_user"}})
	require.NoError(t, err)
	_, err = queries.AttributeDirectoryGroups(ctx, directoryrepo.AttributeDirectoryGroupsParams{OrganizationID: orgID, DirectoryID: "directory_1", WorkosDirectoryGroupIds: []string{"attribution_group"}})
	require.NoError(t, err)
	deletion := sourceLifecycleEvent(t, "dsync.deleted", "event_010", workosOrgID, "", "directory_1", "", at.Add(time.Hour))
	stub.SetEventPages([][]events.Event{{deletion}})
	blockedCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	_, err = activity.Do(blockedCtx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.Error(t, err)
	require.ErrorIs(t, blockedCtx.Err(), context.DeadlineExceeded)
	_, _, _, deleted := getDirectoryGroupRow(t, ctx, conn, "attribution_group")
	require.False(t, deleted, "worker must not mutate sources before acquiring the attribution lock")
	require.NoError(t, tx.Commit(ctx))
	stub.SetEventPages([][]events.Event{{deletion}})
	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	_, _, _, deleted = getDirectoryGroupRow(t, ctx, conn, "attribution_group")
	require.True(t, deleted)
	user, err := testrepo.New(conn).GetDirectoryUserLifecycleFixture(ctx, testrepo.GetDirectoryUserLifecycleFixtureParams{OrganizationID: orgID, WorkosDirectoryUserID: "attribution_user"})
	require.NoError(t, err)
	require.True(t, user.WorkosDeleted)
}

func TestOlderListedGroupFillsAttributionWithoutReplacingEventState(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "directory_listing_attribution")
	const orgID, workosOrgID = "gram_listing_attribution", "org_listing_attribution"
	seedWorkOSOrganization(t, ctx, conn, orgID, workosOrgID)
	at := directorySyncTime()
	stub := newWorkOSClientWithEvents([][]events.Event{{
		sourceLifecycleEvent(t, "dsync.group.created", "event_001", workosOrgID, "", "listed_group", "Current", at.Add(time.Hour)),
	}})
	activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), conn, stub, cache.NoopCache, nil)
	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	queries := directoryrepo.New(conn)
	listed := directoryrepo.UpsertListedDirectoryGroupParams{
		OrganizationID: orgID, WorkosDirectoryGroupID: "listed_group", DirectoryID: conv.ToPGText("directory_1"),
		Name: "Old", Attributes: []byte(`{"old":true}`), WorkosCreatedAt: conv.ToPGTimestamptz(at), WorkosUpdatedAt: conv.ToPGTimestamptz(at),
	}
	count, err := queries.UpsertListedDirectoryGroup(ctx, listed)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	fixtures := testrepo.New(conn)
	group, err := fixtures.GetDirectoryGroupLifecycleFixture(ctx, testrepo.GetDirectoryGroupLifecycleFixtureParams{OrganizationID: orgID, WorkosDirectoryGroupID: "listed_group"})
	require.NoError(t, err)
	require.Equal(t, "directory_1", group.DirectoryID.String)
	_, name, attributes, deleted := getDirectoryGroupRow(t, ctx, conn, "listed_group")
	require.Equal(t, "Current", name)
	require.JSONEq(t, `{}`, string(attributes))
	require.Equal(t, "event_001", group.WorkosLastEventID.String)
	require.False(t, deleted)
	listed.DirectoryID = conv.ToPGText("stale_directory")
	count, err = queries.UpsertListedDirectoryGroup(ctx, listed)
	require.NoError(t, err)
	require.Zero(t, count, "older snapshots cannot replace known attribution")
	stub.SetEventPages([][]events.Event{{sourceLifecycleEvent(t, "dsync.deleted", "event_010", workosOrgID, "", "directory_1", "", at.Add(2*time.Hour))}})
	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	_, _, _, deleted = getDirectoryGroupRow(t, ctx, conn, "listed_group")
	require.True(t, deleted, "the filled attribution must participate in directory deletion")
}

func TestDirectoryDeletionLargeFixture(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "directory_large_deletion")
	const orgID, workosOrgID = "gram_large_directory", "org_large_directory"
	seedWorkOSOrganization(t, ctx, conn, orgID, workosOrgID)
	fixtures := testrepo.New(conn)
	_, err := fixtures.SeedDirectoryUsersFixture(ctx, testrepo.SeedDirectoryUsersFixtureParams{OrganizationID: orgID, IDPrefix: "large_user_", DirectoryID: conv.ToPGText("large_directory"), WorkosCreatedAt: conv.ToPGTimestamptz(directorySyncTime()), RowCount: 10000})
	require.NoError(t, err)
	_, err = fixtures.SeedDirectoryGroupsFixture(ctx, testrepo.SeedDirectoryGroupsFixtureParams{OrganizationID: orgID, IDPrefix: "large_group_", DirectoryID: conv.ToPGText("large_directory"), WorkosCreatedAt: conv.ToPGTimestamptz(directorySyncTime()), RowCount: 2000})
	require.NoError(t, err)
	stub := newWorkOSClientWithEvents([][]events.Event{{sourceLifecycleEvent(t, "dsync.deleted", "event_020", workosOrgID, "", "large_directory", "", directorySyncTime().Add(time.Hour))}})
	activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), conn, stub, cache.NoopCache, nil)
	start := time.Now()
	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	t.Logf("directory deletion transaction for 10,000 users and 2,000 groups: %s", time.Since(start))
	counts, err := fixtures.CountDeletedDirectorySourcesFixture(ctx, testrepo.CountDeletedDirectorySourcesFixtureParams{OrganizationID: orgID, DirectoryID: "large_directory"})
	require.NoError(t, err)
	require.Equal(t, int64(10000), counts.Users)
	require.Equal(t, int64(2000), counts.Groups)
}
