package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type directorySnapshotSeed struct {
	orgID  string
	userID string
}

// seedDirectorySnapshotData creates an organization, a Gram user, a linked
// directory user with custom attributes, a current group membership, and a
// role assignment — the full directory state the telemetry logger hydrates.
func seedDirectorySnapshotData(t *testing.T, ctx context.Context, conn *pgxpool.Pool, suffix string) directorySnapshotSeed {
	t.Helper()

	orgID := "org_hydrate_" + suffix
	userID := "user_hydrate_" + suffix
	email := suffix + "@hydrate.example.com"
	roleSlug := "hydrate-role-" + suffix
	seedTime := time.Now().UTC()

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:       orgID,
		Name:     orgID,
		Slug:     orgID,
		WorkosID: conv.ToPGText("workos_" + orgID),
	})
	require.NoError(t, err)

	_, err = userrepo.New(conn).UpsertUser(ctx, userrepo.UpsertUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: "Hydrate User",
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	directoryUserID, err := workosrepo.New(conn).UpsertDirectoryUser(ctx, workosrepo.UpsertDirectoryUserParams{
		OrganizationID:        orgID,
		UserID:                conv.ToPGText(userID),
		WorkosDirectoryUserID: "directory_user_" + suffix,
		Email:                 conv.ToPGText(email),
		// department_name and job_title are allowlisted predefined
		// attributes; custom_thing and manager_email must be filtered out.
		Attributes:        []byte(`{"department_name":"Engineering","job_title":"Platform Engineer","custom_thing":"not-stamped","manager_email":"boss@example.com"}`),
		WorkosCreatedAt:   conv.ToPGTimestamptz(seedTime),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID: conv.ToPGText("event_directory_user_" + suffix),
	})
	require.NoError(t, err)

	directoryGroupID, err := workosrepo.New(conn).UpsertDirectoryGroup(ctx, workosrepo.UpsertDirectoryGroupParams{
		OrganizationID:         orgID,
		WorkosDirectoryGroupID: "directory_group_" + suffix,
		Name:                   "Developers",
		Attributes:             []byte(`{"object":"directory_group"}`),
		WorkosCreatedAt:        conv.ToPGTimestamptz(seedTime),
		WorkosUpdatedAt:        conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID:      conv.ToPGText("event_directory_group_" + suffix),
	})
	require.NoError(t, err)

	_, err = workosrepo.New(conn).OpenDirectoryUserGroupMembership(ctx, workosrepo.OpenDirectoryUserGroupMembershipParams{
		DirectoryUserID:        directoryUserID,
		DirectoryGroupID:       directoryGroupID,
		WorkosDirectoryUserID:  "directory_user_" + suffix,
		WorkosDirectoryGroupID: "directory_group_" + suffix,
		WorkosCreatedAt:        conv.ToPGTimestamptz(seedTime),
	})
	require.NoError(t, err)

	err = accessrepo.New(conn).UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{
		WorkosSlug:        roleSlug,
		WorkosName:        roleSlug,
		WorkosDescription: conv.ToPGText(roleSlug),
		WorkosCreatedAt:   conv.ToPGTimestamptz(seedTime),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	inserted, err := accessrepo.New(conn).UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID:     orgID,
		WorkosUserID:       "workos_user_" + suffix,
		WorkosRoleSlug:     roleSlug,
		UserID:             conv.ToPGText(userID),
		WorkosMembershipID: conv.ToPGText("membership_" + suffix),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), inserted)

	return directorySnapshotSeed{orgID: orgID, userID: userID}
}

func TestCreateLog_HydratesDirectorySnapshot(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)

	suffix := "log_" + uuid.New().String()[:8]
	seed := seedDirectorySnapshotData(t, ctx, ti.conn, suffix)

	attrs := telemetry.HTTPLogAttributes{}
	attrs.RecordMethod("GET")
	attrs.RecordStatusCode(200)

	toolInfo := newTestToolInfo(seed.orgID)
	timestamp := time.Now().UTC()

	ti.telemLogger.Log(ctx, telemetry.LogParams{
		Timestamp: timestamp,
		ToolInfo:  toolInfo,
		UserInfo: telemetry.UserInfo{
			UserID: seed.userID,
		},
		Attributes: attrs,
	})

	log := waitForLog(t, ctx, ti.chClient, toolInfo.ProjectID, toolInfo.URN, timestamp)

	// user.attributes carries the allowlisted WorkOS predefined attributes.
	require.Contains(t, log.Attributes, "Engineering")
	require.Contains(t, log.Attributes, "Platform Engineer")
	// Non-allowlisted keys (custom attributes, manager PII) are not stamped.
	require.NotContains(t, log.Attributes, "not-stamped")
	require.NotContains(t, log.Attributes, "boss@example.com")
	// user.groups carries current group names only.
	require.Contains(t, log.Attributes, "Developers")
	// user.roles carries the current role slugs from the role tables.
	require.Contains(t, log.Attributes, "hydrate-role-"+suffix)

	// Hydration must not mutate the caller's attribute map: callers may
	// reuse maps across rows, and a stamped snapshot would otherwise pin
	// stale directory state onto every subsequent row.
	require.NotContains(t, attrs, attr.UserAttributesKey)
	require.NotContains(t, attrs, attr.UserGroupsKey)
	require.NotContains(t, attrs, attr.UserRolesKey)
}

func TestCreateLog_NoDirectorySnapshotWhenNoDirectoryData(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)

	attrs := telemetry.HTTPLogAttributes{}
	attrs.RecordMethod("GET")
	attrs.RecordStatusCode(200)

	toolInfo := newTestToolInfo(ti.orgID)
	timestamp := time.Now().UTC()

	ti.telemLogger.Log(ctx, telemetry.LogParams{
		Timestamp: timestamp,
		ToolInfo:  toolInfo,
		UserInfo: telemetry.UserInfo{
			UserID: "user_without_directory_data",
		},
		Attributes: attrs,
	})

	log := waitForLog(t, ctx, ti.chClient, toolInfo.ProjectID, toolInfo.URN, timestamp)

	// Empty snapshot parts are omitted rather than stamped as empty payloads.
	require.NotContains(t, log.Attributes, "groups")
	require.NotContains(t, log.Attributes, "roles")
	require.Contains(t, log.Attributes, "user_without_directory_data")
}

func TestUserInfoResolver_HydratesAndCachesUntilExpiry(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	logger := testenv.NewLogger(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	suffix := "cache_" + uuid.New().String()[:8]
	seed := seedDirectorySnapshotData(t, ctx, conn, suffix)

	resolver := telemetry.NewUserInfoResolver(logger, conn, cache.NewRedisCacheAdapter(redisClient))

	info := resolver.Hydrate(ctx, seed.orgID, telemetry.UserInfo{UserID: seed.userID})
	require.Equal(t, telemetry.UserAttributes{
		DepartmentName: "Engineering",
		JobTitle:       "Platform Engineer",
	}, info.Attributes)
	require.Equal(t, []string{"Developers"}, info.Groups)
	require.Equal(t, []string{"hydrate-role-" + suffix}, info.Roles)

	// Change the persisted attributes; within the TTL the cached snapshot
	// is still served.
	_, err = workosrepo.New(conn).UpsertDirectoryUser(ctx, workosrepo.UpsertDirectoryUserParams{
		OrganizationID:        seed.orgID,
		UserID:                conv.ToPGText(seed.userID),
		WorkosDirectoryUserID: "directory_user_" + suffix,
		Email:                 conv.ToPGText(suffix + "@hydrate.example.com"),
		Attributes:            []byte(`{"department_name":"Sales"}`),
		WorkosCreatedAt:       conv.ToPGTimestamptz(time.Now().UTC()),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(time.Now().UTC()),
		WorkosLastEventID:     conv.ToPGText("event_directory_user_update_" + suffix),
	})
	require.NoError(t, err)

	cached := resolver.Hydrate(ctx, seed.orgID, telemetry.UserInfo{UserID: seed.userID})
	require.Equal(t, "Engineering", cached.Attributes.DepartmentName)

	// Once the cache entry expires (simulated by dropping it) the snapshot
	// is reloaded from Postgres.
	require.NoError(t, resolver.InvalidateForTest(ctx, seed.orgID, seed.userID))
	refreshed := resolver.Hydrate(ctx, seed.orgID, telemetry.UserInfo{UserID: seed.userID})
	require.Equal(t, "Sales", refreshed.Attributes.DepartmentName)
}

func TestUserInfoResolver_EmptySnapshotForUnknownUser(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	logger := testenv.NewLogger(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	resolver := telemetry.NewUserInfoResolver(logger, conn, cache.NewRedisCacheAdapter(redisClient))

	info := resolver.Hydrate(ctx, "org_unknown", telemetry.UserInfo{UserID: "user_unknown"})
	require.True(t, info.Attributes.IsZero())
	require.Empty(t, info.Groups)
	require.Empty(t, info.Roles)
}
