package audit_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	testrepo "github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestListAuditLogs_IncludeAssistantEvents(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	orgID := uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Test Organization", Slug: "test-" + orgID[:8],
	})
	require.NoError(t, err)

	queries := repo.New(conn)
	for _, subjectType := range []string{"project", "assistant"} {
		_, err := queries.InsertAuditLog(ctx, repo.InsertAuditLogParams{
			OrganizationID: orgID, ActorID: "user:test", ActorType: "user",
			Action: "test:create", SubjectID: uuid.NewString(), SubjectType: subjectType,
		})
		require.NoError(t, err)
	}

	base := repo.ListAuditLogsParams{OrganizationID: orgID, CursorSeq: pgtype.Int8{}}
	base.IncludeAssistantEvents = false
	rows, err := queries.ListAuditLogs(ctx, base)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "project", rows[0].SubjectType)

	base.IncludeAssistantEvents = true
	rows, err = queries.ListAuditLogs(ctx, base)
	require.NoError(t, err)
	require.Len(t, rows, 2)
}

func TestListAuditLogs_Window(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	orgID := uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Test Organization", Slug: "test-" + orgID[:8],
	})
	require.NoError(t, err)

	queries := repo.New(conn)

	// Written now, then stamped an hour apart with the same fixture the admin
	// activity test uses. created_at defaults to clock_timestamp(), which would
	// leave the three rows microseconds apart and the boundary cases below
	// resting on that spacing; fixed offsets make every assertion here depend
	// only on the window predicate.
	base := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	stamps := make([]time.Time, 0, 3)
	for i := range 3 {
		row, err := queries.InsertAuditLog(ctx, repo.InsertAuditLogParams{
			OrganizationID: orgID, ActorID: "user:test", ActorType: "user",
			Action: "test:create", SubjectID: uuid.NewString(), SubjectType: "project",
		})
		require.NoError(t, err)

		stamp := base.Add(time.Duration(i) * time.Hour)
		require.NoError(t, queries.UpdateAuditLogCreatedAtForTesting(ctx, repo.UpdateAuditLogCreatedAtForTestingParams{
			CreatedAt: conv.ToPGTimestamptz(stamp),
			Ids:       []uuid.UUID{row.ID},
		}))
		stamps = append(stamps, stamp)
	}

	window := func(from, to *time.Time) []repo.ListAuditLogsRow {
		t.Helper()
		rows, err := queries.ListAuditLogs(ctx, repo.ListAuditLogsParams{
			OrganizationID: orgID,
			CursorSeq:      pgtype.Int8{},
			CreatedFrom:    conv.PtrToPGTimestamptz(from),
			CreatedTo:      conv.PtrToPGTimestamptz(to),
		})
		require.NoError(t, err)
		return rows
	}

	oldest, middle, newest := stamps[0], stamps[1], stamps[2]

	require.Len(t, window(nil, nil), 3, "no bound is no filter")
	require.Len(t, window(&middle, nil), 2, "from is inclusive")
	require.Len(t, window(nil, &middle), 1, "to is exclusive")
	require.Len(t, window(&oldest, &newest), 2, "the window is half-open")
	require.Empty(t, window(&newest, &newest), "an empty window matches nothing")
}

// The directory is global while the feed is not: an actor id that never
// belonged to the reading organization must not resolve to that person's name.
func TestListAuditLogs_ActorNameIsTenantScoped(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	orgs := orgrepo.New(conn)
	readingOrg, otherOrg := uuid.NewString(), uuid.NewString()
	for _, id := range []string{readingOrg, otherOrg} {
		_, err = orgs.UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
			ID: id, Name: "Test Organization", Slug: "test-" + id[:8],
		})
		require.NoError(t, err)
	}

	fixtures := testrepo.New(conn)
	actors := []struct {
		id, name string
		org      string
		departed bool
	}{
		{id: "u-member-" + readingOrg[:8], name: "Current Member", org: readingOrg},
		{id: "u-departed-" + readingOrg[:8], name: "Departed Member", org: readingOrg, departed: true},
		{id: "u-stranger-" + otherOrg[:8], name: "Another Tenant", org: otherOrg},
	}
	for _, actor := range actors {
		require.NoError(t, fixtures.InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{
			ID: actor.id, Email: actor.id + "@example.test", DisplayName: actor.name,
		}))
		require.NoError(t, fixtures.CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
			OrganizationID: actor.org, UserID: conv.ToPGText(actor.id),
		}))
		if actor.departed {
			require.NoError(t, fixtures.ForceSoftDeleteOrganizationUserRelationship(ctx, testrepo.ForceSoftDeleteOrganizationUserRelationshipParams{
				OrganizationID: actor.org, UserID: conv.ToPGText(actor.id),
			}))
		}
	}

	// Every row is written into the reading organization, including one whose
	// actor only ever belonged elsewhere.
	queries := repo.New(conn)
	for _, actor := range actors {
		_, err := queries.InsertAuditLog(ctx, repo.InsertAuditLogParams{
			OrganizationID: readingOrg, ActorID: actor.id, ActorType: "user",
			ActorDisplayName: conv.ToPGText(actor.id + "@example.test"),
			Action:           "test:create", SubjectID: uuid.NewString(), SubjectType: "project",
		})
		require.NoError(t, err)
	}

	rows, err := queries.ListAuditLogs(ctx, repo.ListAuditLogsParams{
		OrganizationID: readingOrg, CursorSeq: pgtype.Int8{},
	})
	require.NoError(t, err)
	require.Len(t, rows, len(actors), "the scoping join must not drop or duplicate rows")

	resolved := make(map[string]string, len(rows))
	for _, row := range rows {
		resolved[row.ActorID] = row.ActorUserDisplayName.String
	}
	require.Equal(t, "Current Member", resolved[actors[0].id])
	require.Equal(t, "Departed Member", resolved[actors[1].id],
		"a soft-deleted membership still names the person who acted")
	require.Empty(t, resolved[actors[2].id],
		"an actor from another organization must not be named here")
}
