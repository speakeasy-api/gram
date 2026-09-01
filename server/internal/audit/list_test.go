package audit_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	audittestrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
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
	fixtures := audittestrepo.New(conn)

	// Written now, then stamped at hours apart. created_at defaults to
	// clock_timestamp(), which would leave the three rows microseconds apart
	// and the boundary cases below resting on that spacing; fixed offsets make
	// every assertion here depend only on the window predicate.
	base := time.Now().UTC().Truncate(time.Second)
	stamps := []time.Time{
		base.Add(-3 * time.Hour),
		base.Add(-2 * time.Hour),
		base.Add(-1 * time.Hour),
	}
	for _, stamp := range stamps {
		row, err := queries.InsertAuditLog(ctx, repo.InsertAuditLogParams{
			OrganizationID: orgID, ActorID: "user:test", ActorType: "user",
			Action: "test:create", SubjectID: uuid.NewString(), SubjectType: "project",
		})
		require.NoError(t, err)
		require.NoError(t, fixtures.BackdateAuditLog(ctx, audittestrepo.BackdateAuditLogParams{
			CreatedAt: conv.ToPGTimestamptz(stamp),
			ID:        row.ID,
		}))
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
