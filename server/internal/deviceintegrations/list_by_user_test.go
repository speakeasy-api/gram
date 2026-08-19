package deviceintegrations

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// seedDeviceUser inserts the users row an mdm_devices.user_id reference needs.
func seedDeviceUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, userID, email string) {
	t.Helper()
	require.NoError(t, testrepo.New(conn).InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{
		ID:          userID,
		Email:       email,
		DisplayName: email,
	}))
}

// One person's devices. Both legs of the filter are load-bearing and neither
// subsumes the other: a device only carries a resolved user id when the MDM's
// reported email matched a member, and the MDM's email can be an alias the
// directory does not know.
func TestListManagedDevicesUserFilter(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	configID := created.Config.ID

	subjectUserID := "user_subject"
	otherUserID := "user_other"
	// mdm_devices.user_id is a real foreign key, so the resolved members have
	// to exist before a device can point at them.
	seedDeviceUser(t, ctx, conn, subjectUserID, "subject@example.test")
	seedDeviceUser(t, ctx, conn, otherUserID, "other@example.test")

	// Resolved to the subject by user id.
	seedDevice(t, ctx, conn, configID, orgID, "d-by-id", "subject@example.test", &subjectUserID, false)
	// The subject's alias: the MDM knows an address that resolved to nobody.
	seedDevice(t, ctx, conn, configID, orgID, "d-by-email", "Subject.Alias@Example.test", nil, false)
	// Somebody else's, by each leg.
	seedDevice(t, ctx, conn, configID, orgID, "d-other-id", "other@example.test", &otherUserID, false)
	seedDevice(t, ctx, conn, configID, orgID, "d-other-email", "unrelated@example.test", nil, false)

	cutoff := conv.ToPGTimestamptz(time.Now().UTC().Add(-activeWindow))

	rows, err := store.repo.ListManagedDevices(ctx, repo.ListManagedDevicesParams{
		DeviceLevel:    false,
		ActiveCutoff:   cutoff,
		OrganizationID: orgID,
		Provider:       conv.PtrToPGTextEmpty(nil),
		CursorID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Bucket:         conv.PtrToPGTextEmpty(nil),
		UserIds:        []string{subjectUserID},
		// Deliberately cased differently from the seeded row: the MDM reports
		// whatever casing the directory gave it.
		UserEmails: []string{"subject.alias@example.test"},
		PageLimit:  10,
	})
	require.NoError(t, err)

	externalIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		externalIDs = append(externalIDs, row.ExternalID)
	}
	require.ElementsMatch(t, []string{"d-by-id", "d-by-email"}, externalIDs)
}

// An empty filter must not narrow the listing, so the identity page and the
// fleet page can share one query.
func TestListManagedDevicesUserFilterEmptyIsUnnarrowed(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	configID := created.Config.ID

	seedDevice(t, ctx, conn, configID, orgID, "d-1", "one@example.test", nil, false)
	seedDevice(t, ctx, conn, configID, orgID, "d-2", "two@example.test", nil, false)

	cutoff := conv.ToPGTimestamptz(time.Now().UTC().Add(-activeWindow))

	rows, err := store.repo.ListManagedDevices(ctx, repo.ListManagedDevicesParams{
		DeviceLevel:    false,
		ActiveCutoff:   cutoff,
		OrganizationID: orgID,
		Provider:       conv.PtrToPGTextEmpty(nil),
		CursorID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Bucket:         conv.PtrToPGTextEmpty(nil),
		UserIds:        nil,
		UserEmails:     nil,
		PageLimit:      10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 2)
}

// A filter carrying only blanks — an empty query-string value, say — has to
// read as no filter. Left unfiltered it would otherwise match no device at
// all, which looks like "this person has no devices".
func TestListManagedDevicesUserFilterBlanksAreNoFilter(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	configID := created.Config.ID

	seedDevice(t, ctx, conn, configID, orgID, "d-1", "one@example.test", nil, false)

	require.Empty(t, nonBlank([]string{"", "   "}))
	require.Equal(t, []string{"kept"}, nonBlank([]string{"", " kept ", "  "}))

	cutoff := conv.ToPGTimestamptz(time.Now().UTC().Add(-activeWindow))
	rows, err := store.repo.ListManagedDevices(ctx, repo.ListManagedDevicesParams{
		DeviceLevel:    false,
		ActiveCutoff:   cutoff,
		OrganizationID: orgID,
		Provider:       conv.PtrToPGTextEmpty(nil),
		CursorID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Bucket:         conv.PtrToPGTextEmpty(nil),
		UserIds:        nonBlank([]string{"", "  "}),
		UserEmails:     nonBlank([]string{""}),
		PageLimit:      10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1, "a blank-only filter must not narrow the listing")
}
