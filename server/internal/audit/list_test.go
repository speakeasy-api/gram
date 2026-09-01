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
	// Written now and then backdated: created_at defaults to the insert time,
	// and the window is the only thing under test here.
	days := []int{-10, -3, -1}
	for _, offset := range days {
		row, err := queries.InsertAuditLog(ctx, repo.InsertAuditLogParams{
			OrganizationID: orgID, ActorID: "user:test", ActorType: "user",
			Action: "test:create", SubjectID: uuid.NewString(), SubjectType: "project",
		})
		require.NoError(t, err)
		_, err = conn.Exec(ctx,
			"UPDATE audit_logs SET created_at = now() + make_interval(days => $1) WHERE id = $2",
			offset, row.ID)
		require.NoError(t, err)
	}

	now := time.Now().UTC()
	window := func(from, to *time.Time) []repo.ListAuditLogsRow {
		rows, err := queries.ListAuditLogs(ctx, repo.ListAuditLogsParams{
			OrganizationID: orgID,
			CursorSeq:      pgtype.Int8{},
			CreatedFrom:    conv.PtrToPGTimestamptz(from),
			CreatedTo:      conv.PtrToPGTimestamptz(to),
		})
		require.NoError(t, err)
		return rows
	}

	require.Len(t, window(nil, nil), 3, "no bound is no filter")

	fiveDaysAgo := now.AddDate(0, 0, -5)
	require.Len(t, window(&fiveDaysAgo, nil), 2, "from alone drops what precedes it")

	twoDaysAgo := now.AddDate(0, 0, -2)
	require.Len(t, window(nil, &twoDaysAgo), 2, "to alone drops what follows it")
	require.Len(t, window(&fiveDaysAgo, &twoDaysAgo), 1, "both bounds keep the middle")

	// Half-open: a row landing exactly on `from` is in, exactly on `to` is out.
	rows := window(nil, nil)
	require.Len(t, rows, 3)
	newest := rows[0].CreatedAt.Time
	require.Len(t, window(&newest, nil), 1, "from is inclusive")
	require.Empty(t, window(&newest, &newest), "to is exclusive")
}
