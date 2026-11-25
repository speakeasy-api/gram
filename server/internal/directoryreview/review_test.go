package directoryreview_test

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/directoryreview"
	reviewrepo "github.com/speakeasy-api/gram/server/internal/directoryreview/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

const testOrg = "org_review"

const roleURN = "role:organization:00000000-0000-0000-0000-000000000001"

func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "directoryreview")
	require.NoError(t, err)

	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(t.Context(), organizationsrepo.UpsertOrganizationMetadataParams{
		ID:       testOrg,
		Name:     testOrg,
		Slug:     testOrg,
		WorkosID: conv.ToPGText("workos_" + testOrg),
	})
	require.NoError(t, err)
	return conn
}

func seedGroup(t *testing.T, conn *pgxpool.Pool, externalID string) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	id, err := directoryrepo.New(conn).UpsertDirectoryGroup(t.Context(), directoryrepo.UpsertDirectoryGroupParams{
		OrganizationID:         testOrg,
		WorkosDirectoryGroupID: externalID,
		Name:                   externalID,
		Attributes:             []byte(`{}`),
		WorkosCreatedAt:        conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:        conv.ToPGTimestamptz(now),
		WorkosLastEventID:      conv.ToPGText("event_" + externalID),
	})
	require.NoError(t, err)
	return id
}

func seedUser(t *testing.T, conn *pgxpool.Pool, externalID, attributes string) {
	t.Helper()
	now := time.Now().UTC()
	_, err := directoryrepo.New(conn).UpsertDirectoryUser(t.Context(), directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        testOrg,
		UserID:                conv.ToPGTextEmpty(""),
		WorkosDirectoryUserID: externalID,
		Email:                 conv.ToPGText(externalID + "@example.com"),
		Attributes:            []byte(attributes),
		RestoreDeleted:        true,
		WorkosCreatedAt:       conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(now),
		WorkosLastEventID:     conv.ToPGText("event_" + externalID),
	})
	require.NoError(t, err)
}

func mapGroup(t *testing.T, conn *pgxpool.Pool, groupID uuid.UUID) {
	t.Helper()
	_, err := accessrepo.New(conn).InsertDirectoryGroupRoleMapping(t.Context(), accessrepo.InsertDirectoryGroupRoleMappingParams{
		OrganizationID: testOrg, DirectoryGroupID: uuid.NullUUID{UUID: groupID, Valid: true}, RoleUrn: roleURN,
	})
	require.NoError(t, err)
}

func unmapGroup(t *testing.T, conn *pgxpool.Pool, groupID uuid.UUID) {
	t.Helper()
	require.NoError(t, testrepo.New(conn).SoftDeleteDirectoryGroupRoleMappingsFixture(t.Context(), testrepo.SoftDeleteDirectoryGroupRoleMappingsFixtureParams{
		OrganizationID: testOrg, DirectoryGroupID: uuid.NullUUID{UUID: groupID, Valid: true},
	}))
}

func mapAttribute(t *testing.T, conn *pgxpool.Pool, key, value string) {
	t.Helper()
	_, err := accessrepo.New(conn).InsertDirectoryAttributeRoleMapping(t.Context(), accessrepo.InsertDirectoryAttributeRoleMappingParams{
		OrganizationID: testOrg, AttributeKey: conv.ToPGText(key), AttributeValue: conv.ToPGText(value), RoleUrn: roleURN,
	})
	require.NoError(t, err)
}

func tombstoneGroup(t *testing.T, conn *pgxpool.Pool, groupID uuid.UUID) {
	t.Helper()
	require.NoError(t, testrepo.New(conn).SetDirectoryGroupWorkOSDeletedFixture(t.Context(), testrepo.SetDirectoryGroupWorkOSDeletedFixtureParams{Deleted: true, ID: groupID}))
}

func restoreGroup(t *testing.T, conn *pgxpool.Pool, groupID uuid.UUID) {
	t.Helper()
	require.NoError(t, testrepo.New(conn).SetDirectoryGroupWorkOSDeletedFixture(t.Context(), testrepo.SetDirectoryGroupWorkOSDeletedFixtureParams{Deleted: false, ID: groupID}))
}

// setUserAttributes stands in for a directory user event: it reads the user's
// attributes before and after the write and reconciles the change, as the
// event handler does.
func setUserAttributes(t *testing.T, conn *pgxpool.Pool, externalID, attributes string) directoryreview.Changes {
	t.Helper()
	before := userSnapshot(t, conn, externalID)
	require.NoError(t, testrepo.New(conn).SetDirectoryUserAttributesFixture(t.Context(), testrepo.SetDirectoryUserAttributesFixtureParams{
		Attributes: []byte(attributes), WorkosDirectoryUserID: externalID,
	}))
	after := userSnapshot(t, conn, externalID)
	changes, err := directoryreview.ReconcileUserChange(t.Context(), conn, testOrg, externalID, before, after)
	require.NoError(t, err)
	return changes
}

func deactivateUser(t *testing.T, conn *pgxpool.Pool, externalID string) directoryreview.Changes {
	t.Helper()
	before := userSnapshot(t, conn, externalID)
	require.NoError(t, testrepo.New(conn).SetDirectoryUserWorkOSDeletedFixture(t.Context(), externalID))
	changes, err := directoryreview.ReconcileUserChange(t.Context(), conn, testOrg, externalID, before, nil)
	require.NoError(t, err)
	return changes
}

func userSnapshot(t *testing.T, conn *pgxpool.Pool, externalID string) directoryreview.UserAttributes {
	t.Helper()
	attributes, err := directoryreview.LoadUserAttributes(t.Context(), conn, testOrg, externalID)
	require.NoError(t, err)
	return attributes
}

func sweep(t *testing.T, conn *pgxpool.Pool) directoryreview.Changes {
	t.Helper()
	changes, err := directoryreview.ReconcileOrganization(t.Context(), conn, testOrg)
	require.NoError(t, err)
	return changes
}

type itemRow = testrepo.ListDirectoryReviewItemsFixtureRow

// listItems returns every item, oldest first, with its source as a group's
// external ID or key=value.
func listItems(t *testing.T, conn *pgxpool.Pool) []itemRow {
	t.Helper()
	rows, err := testrepo.New(conn).ListDirectoryReviewItemsFixture(t.Context(), testOrg)
	require.NoError(t, err)
	return rows
}

func resolve(t *testing.T, conn *pgxpool.Pool, kind, state string) {
	t.Helper()
	require.NoError(t, testrepo.New(conn).SetPendingDirectoryReviewItemsStateFixture(t.Context(), testrepo.SetPendingDirectoryReviewItemsStateFixtureParams{
		State: state, OrganizationID: testOrg, Kind: kind,
	}))
}

func TestEventsBeforeFirstSweepRaiseNothing(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)

	group := seedGroup(t, conn, "group_new")
	changes, err := directoryreview.ReconcileGroup(t.Context(), conn, testOrg, group)
	require.NoError(t, err)
	require.True(t, changes.Empty())
	require.Empty(t, listItems(t, conn))
}

func TestGroupsBeforeBaselineAreNotNew(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)

	seedGroup(t, conn, "group_existing")
	sweep(t, conn)
	require.Empty(t, listItems(t, conn), "groups that exist at the baseline belong to bulk setup")

	group := seedGroup(t, conn, "group_new")
	changes, err := directoryreview.ReconcileGroup(t.Context(), conn, testOrg, group)
	require.NoError(t, err)
	require.Len(t, changes.Opened, 1)
	require.Equal(t, []itemRow{{Kind: directoryreview.KindNewGroup, Source: "group_new", Occurrence: 1, State: directoryreview.StatePending}}, listItems(t, conn))

	// Replaying the event or sweeping again opens no second item.
	_, err = directoryreview.ReconcileGroup(t.Context(), conn, testOrg, group)
	require.NoError(t, err)
	sweep(t, conn)
	require.Len(t, listItems(t, conn), 1)
}

func TestNewGroupIsRaisedOnceEver(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	sweep(t, conn)

	group := seedGroup(t, conn, "group_new")
	_, err := directoryreview.ReconcileGroup(t.Context(), conn, testOrg, group)
	require.NoError(t, err)

	// Mapping the group directly settles the item.
	mapGroup(t, conn, group)
	changes, err := directoryreview.ReconcileGroup(t.Context(), conn, testOrg, group)
	require.NoError(t, err)
	require.Len(t, changes.Superseded, 1)

	// Unmapping it later does not make it new again.
	unmapGroup(t, conn, group)
	_, err = directoryreview.ReconcileGroup(t.Context(), conn, testOrg, group)
	require.NoError(t, err)
	sweep(t, conn)
	require.Equal(t, []itemRow{{Kind: directoryreview.KindNewGroup, Source: "group_new", Occurrence: 1, State: directoryreview.StateSuperseded}}, listItems(t, conn))
}

func TestNewGroupDeletedBeforeDecisionIsSuperseded(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	sweep(t, conn)

	group := seedGroup(t, conn, "group_new")
	sweep(t, conn)
	tombstoneGroup(t, conn, group)
	sweep(t, conn)

	require.Equal(t, []itemRow{{Kind: directoryreview.KindNewGroup, Source: "group_new", Occurrence: 1, State: directoryreview.StateSuperseded}}, listItems(t, conn),
		"an unmapped group that goes is not a missing source")
}

func TestMissingGroupOccurrences(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)

	group := seedGroup(t, conn, "group_mapped")
	mapGroup(t, conn, group)
	tombstoneGroup(t, conn, group)

	// A source already missing at the baseline is still raised.
	sweep(t, conn)
	sweep(t, conn)
	require.Equal(t, []itemRow{{Kind: directoryreview.KindSourceMissing, Source: "group_mapped", Occurrence: 1, State: directoryreview.StatePending}}, listItems(t, conn))

	// The group comes back: the item is settled.
	restoreGroup(t, conn, group)
	changes, err := directoryreview.ReconcileGroup(t.Context(), conn, testOrg, group)
	require.NoError(t, err)
	require.Len(t, changes.Superseded, 1)

	// It goes again: a second occurrence.
	tombstoneGroup(t, conn, group)
	changes, err = directoryreview.ReconcileGroup(t.Context(), conn, testOrg, group)
	require.NoError(t, err)
	require.Len(t, changes.Opened, 1)
	require.Equal(t, int32(2), changes.Opened[0].Occurrence)

	// Keeping the mapping settles this occurrence for good: no reopen.
	resolve(t, conn, directoryreview.KindSourceMissing, directoryreview.StateDismissed)
	sweep(t, conn)
	_, err = directoryreview.ReconcileGroup(t.Context(), conn, testOrg, group)
	require.NoError(t, err)
	require.Equal(t, []itemRow{
		{Kind: directoryreview.KindSourceMissing, Source: "group_mapped", Occurrence: 1, State: directoryreview.StateSuperseded},
		{Kind: directoryreview.KindSourceMissing, Source: "group_mapped", Occurrence: 2, State: directoryreview.StateDismissed},
	}, listItems(t, conn))
}

func TestMissingGroupSettledWhenUnmapped(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)

	group := seedGroup(t, conn, "group_mapped")
	mapGroup(t, conn, group)
	tombstoneGroup(t, conn, group)
	sweep(t, conn)

	unmapGroup(t, conn, group)
	changes, err := directoryreview.ReconcileGroup(t.Context(), conn, testOrg, group)
	require.NoError(t, err)
	require.Len(t, changes.Superseded, 1)
	require.Empty(t, changes.Opened)
}

func TestAttributeValueChanges(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)

	mapAttribute(t, conn, "department", "Engineering")
	seedUser(t, conn, "user_a", `{"department":"Engineering"}`)
	sweep(t, conn)
	require.Empty(t, listItems(t, conn))

	// The only holder moves to a new value: the new value is raised and the
	// mapped value goes missing.
	changes := setUserAttributes(t, conn, "user_a", `{"department":"Sales"}`)
	require.Len(t, changes.Opened, 2)
	require.ElementsMatch(t, []itemRow{
		{Kind: directoryreview.KindNewAttributeValue, Source: "department=Sales", Occurrence: 1, State: directoryreview.StatePending},
		{Kind: directoryreview.KindSourceMissing, Source: "department=Engineering", Occurrence: 1, State: directoryreview.StatePending},
	}, listItems(t, conn))

	// A second holder of an existing value raises nothing new.
	seedUser(t, conn, "user_b", `{}`)
	changes = setUserAttributes(t, conn, "user_b", `{"department":"Sales"}`)
	require.Empty(t, changes.Opened)

	// Someone carries the mapped value again: its missing item is settled.
	changes = setUserAttributes(t, conn, "user_b", `{"department":"Engineering"}`)
	require.Len(t, changes.Superseded, 1)
	require.Equal(t, directoryreview.KindSourceMissing, changes.Superseded[0].Kind)

	// The holder leaves the directory: the value goes missing a second time.
	changes = deactivateUser(t, conn, "user_b")
	require.Len(t, changes.Opened, 1)
	require.Equal(t, int32(2), changes.Opened[0].Occurrence)

	// The sweep does not reopen an occurrence it cannot date.
	resolve(t, conn, directoryreview.KindSourceMissing, directoryreview.StateDismissed)
	changes = sweep(t, conn)
	require.Empty(t, changes.Opened)
}

// A whole directory's deletion tombstones its users without a per-user event,
// so only the sweep sees the mapped value go again.
func TestMissingAttributeRecursAfterHolderDeletedWithoutEvent(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)

	mapAttribute(t, conn, "department", "Engineering")
	seedUser(t, conn, "user_a", `{"department":"Engineering"}`)
	sweep(t, conn)

	// The value goes, and an admin keeps the mapping.
	changes := setUserAttributes(t, conn, "user_a", `{}`)
	require.Len(t, changes.Opened, 1)
	resolve(t, conn, directoryreview.KindSourceMissing, directoryreview.StateDismissed)

	// It comes back, then its only holder is deleted with no event.
	seedUser(t, conn, "user_b", `{"department":"Engineering"}`)
	require.NoError(t, testrepo.New(conn).SetDirectoryUserWorkOSDeletedFixture(t.Context(), "user_b"))

	changes = sweep(t, conn)
	require.Len(t, changes.Opened, 1)
	require.Equal(t, int32(2), changes.Opened[0].Occurrence)

	// Keeping the mapping again settles it: the same deletion reopens nothing.
	resolve(t, conn, directoryreview.KindSourceMissing, directoryreview.StateDismissed)
	changes = sweep(t, conn)
	require.Empty(t, changes.Opened)
}

func TestNewAttributeValueNeedsMappedKey(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	sweep(t, conn)

	seedUser(t, conn, "user_a", `{}`)
	changes := setUserAttributes(t, conn, "user_a", `{"department":"Sales"}`)
	require.True(t, changes.Empty(), "values of a key with no mappings are not decisions")
}

func TestNewAttributeValueSettledWhenLastHolderLeaves(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)

	mapAttribute(t, conn, "department", "Engineering")
	seedUser(t, conn, "user_eng", `{"department":"Engineering"}`)
	seedUser(t, conn, "user_a", `{}`)
	sweep(t, conn)

	setUserAttributes(t, conn, "user_a", `{"department":"Sales"}`)
	changes := deactivateUser(t, conn, "user_a")
	require.Len(t, changes.Superseded, 1)
	require.Equal(t, directoryreview.KindNewAttributeValue, changes.Superseded[0].Kind)

	// Raised once ever: the value coming back is not new again.
	seedUser(t, conn, "user_c", `{}`)
	changes = setUserAttributes(t, conn, "user_c", `{"department":"Sales"}`)
	require.Empty(t, changes.Opened)
}

func TestOptedOutKeyRaisesNoNewValues(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)

	mapAttribute(t, conn, "department", "Engineering")
	seedUser(t, conn, "user_eng", `{"department":"Engineering"}`)
	seedUser(t, conn, "user_a", `{}`)
	seedUser(t, conn, "user_b", `{}`)
	sweep(t, conn)

	setUserAttributes(t, conn, "user_a", `{"department":"Sales"}`)
	require.NoError(t, reviewrepo.New(conn).InsertDirectoryReviewAttributeOptOut(t.Context(), reviewrepo.InsertDirectoryReviewAttributeOptOutParams{
		OrganizationID: testOrg, AttributeKey: "department",
	}))

	changes, err := directoryreview.SupersedeAttributeKey(t.Context(), conn, testOrg, "department")
	require.NoError(t, err)
	require.Len(t, changes.Superseded, 1, "opting out settles the key's pending new values")

	changes = setUserAttributes(t, conn, "user_b", `{"department":"Marketing"}`)
	require.Empty(t, changes.Opened)
}

func TestSweepCoversOrganizationsWithDirectoryData(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)

	// testOrg has directory mappings left but SCIM off; a second org has SCIM
	// on and nothing else; a third has neither.
	group := seedGroup(t, conn, "group_mapped")
	mapGroup(t, conn, group)
	for _, id := range []string{"org_scim", "org_none"} {
		_, err := organizationsrepo.New(conn).UpsertOrganizationMetadata(t.Context(), organizationsrepo.UpsertOrganizationMetadataParams{
			ID: id, Name: id, Slug: id, WorkosID: conv.ToPGText("workos_" + id),
		})
		require.NoError(t, err)
	}
	require.NoError(t, organizationsrepo.New(conn).SetSCIMEnabled(t.Context(), organizationsrepo.SetSCIMEnabledParams{
		Enabled: pgtype.Bool{Bool: true, Valid: true}, WorkosLastEventID: conv.ToPGText("event_scim"), WorkosID: conv.ToPGText("workos_org_scim"),
	}))

	require.NoError(t, directoryreview.Sweep(t.Context(), conn))

	baselined, err := testrepo.New(conn).ListDirectoryReviewOrgStateIDsFixture(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{testOrg, "org_scim"}, baselined)
}

func TestLoadUserAttributesMatchesMappingText(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)

	seedUser(t, conn, "user_types", `{"s":"x","n":12,"b":true,"z":null,"o":{"a":1},"l":["x"]}`)
	got, err := directoryreview.LoadUserAttributes(t.Context(), conn, testOrg, "user_types")
	require.NoError(t, err)
	require.Equal(t, directoryreview.UserAttributes{
		"s": "x", "n": "12", "b": "true", "o": `{"a": 1}`, "l": `["x"]`,
	}, got, "every value is the text ->> returns, nested values included, and nulls are left out")

	seedUser(t, conn, "user_empty", `{}`)
	empty, err := directoryreview.LoadUserAttributes(t.Context(), conn, testOrg, "user_empty")
	require.NoError(t, err)
	require.NotNil(t, empty, "a live user with no attributes is not the same as a missing one")
	require.Empty(t, empty)

	missing, err := directoryreview.LoadUserAttributes(t.Context(), conn, "org_other", "user_types")
	require.NoError(t, err)
	require.Nil(t, missing, "another organization's user is not found")
}

func TestNestedAttributeValueGoingMissingIsRaised(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)

	seedUser(t, conn, "user_nested", `{"team":{"name":"core"}}`)
	mapAttribute(t, conn, "team", `{"name": "core"}`)
	sweep(t, conn)

	changes := setUserAttributes(t, conn, "user_nested", `{}`)
	require.Len(t, changes.Opened, 1, "the mapped nested value lost its last holder")
}
