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
	for range 3 {
		_, err := queries.InsertAuditLog(ctx, repo.InsertAuditLogParams{
			OrganizationID: orgID, ActorID: "user:test", ActorType: "user",
			Action: "test:create", SubjectID: uuid.NewString(), SubjectType: "project",
		})
		require.NoError(t, err)
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

	// Bounds are taken from what the rows were actually stamped with, rather
	// than from a backdated fixture: created_at defaults to clock_timestamp(),
	// which advances between statements, so the three rows are strictly
	// ordered and the boundary cases land exactly on a stored value.
	all := window(nil, nil)
	require.Len(t, all, 3, "no bound is no filter")
	newest := all[0].CreatedAt.Time
	middle := all[1].CreatedAt.Time
	oldest := all[2].CreatedAt.Time
	require.True(t, oldest.Before(middle) && middle.Before(newest),
		"rows are stamped in insertion order")

	require.Len(t, window(&middle, nil), 2, "from is inclusive")
	require.Len(t, window(nil, &middle), 1, "to is exclusive")
	require.Len(t, window(&oldest, &newest), 2, "the window is half-open")
	require.Empty(t, window(&newest, &newest), "an empty window matches nothing")
}
