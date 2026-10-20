package riskfindings

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/pipeline"
)

func readAll(t *testing.T, source *Source, criteria pipeline.Criteria) []SourceRow {
	t.Helper()
	out := make(chan SourceRow, 64)
	require.NoError(t, source.Read(t.Context(), criteria, out))
	close(out)
	var rows []SourceRow
	for row := range out {
		rows = append(rows, row)
	}
	return rows
}

func rowIDs(rows []SourceRow) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

func TestSourceGapFillPagesEachProjectByCreatedAt(t *testing.T) {
	t.Parallel()

	tn := seedTenant(t)
	other := tn.newProject(t, "second")
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-24 * time.Hour)
	chatID := tn.newChat(t, "", "")
	msgID := tn.newMessage(t, chatID, base)

	finding := func(project uuid.UUID, at time.Time) uuid.UUID {
		return tn.newFinding(t, msgID, at, findingSpec{
			source: "presidio", ruleID: "pii.email_address", match: "a@example.com", startPos: 0, endPos: 13, project: project,
		})
	}
	// Ids are minted in insert order, so created_at order differs from id order.
	firstLate := finding(tn.projectID, base.Add(3*time.Hour))
	firstEarly := finding(tn.projectID, base.Add(time.Hour))
	firstMiddle := finding(tn.projectID, base.Add(2*time.Hour))
	otherLate := finding(other, base.Add(2*time.Hour))
	otherEarly := finding(other, base.Add(time.Hour))
	finding(tn.projectID, base.Add(-time.Hour))

	byProject := map[uuid.UUID][]uuid.UUID{
		tn.projectID: {firstEarly, firstMiddle, firstLate},
		other:        {otherEarly, otherLate},
	}
	projects := []uuid.UUID{tn.projectID, other}
	slices.SortFunc(projects, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	want := append(slices.Clone(byProject[projects[0]]), byProject[projects[1]]...)

	criteria := pipeline.Criteria{
		CriteriaGapFill:   true,
		CriteriaOrgID:     tn.orgID,
		CriteriaFrom:      base,
		CriteriaBatchSize: 2,
	}
	all := readAll(t, NewSource(tn.pool), criteria)
	require.Equal(t, want, rowIDs(all))

	resume := maps.Clone(criteria)
	resume[CriteriaGapFillCursor] = GapFillCursor{ProjectID: projects[0], CreatedAt: all[1].CreatedAt, ID: all[1].ID}
	require.Equal(t, want[2:], rowIDs(readAll(t, NewSource(tn.pool), resume)))

	scoped := maps.Clone(criteria)
	scoped[CriteriaProjectID] = other
	require.Equal(t, byProject[other], rowIDs(readAll(t, NewSource(tn.pool), scoped)))
}

type pageCall struct {
	args []any
}

func scriptedSource(results ...func() ([]SourceRow, error)) (*Source, *[]pageCall) {
	calls := &[]pageCall{}
	source := NewSource(nil)
	source.retryBackoff = 0
	source.query = func(_ context.Context, _ string, args ...any) ([]SourceRow, error) {
		*calls = append(*calls, pageCall{args: slices.Clone(args)})
		next := results[min(len(*calls), len(results))-1]
		return next()
	}
	return source, calls
}

func recoveryConflict() ([]SourceRow, error) {
	return nil, &pgconn.PgError{Code: sqlStateRecoveryConflict, Message: "canceling statement due to conflict with recovery"}
}

func TestSourceRetriesRecoveryConflictFromSamePosition(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	from := time.Date(2026, time.July, 21, 0, 0, 0, 0, time.UTC)
	row := SourceRow{ID: uuid.New(), CreatedAt: from.Add(time.Hour), ProjectID: projectID, Source: "presidio"}
	source, calls := scriptedSource(recoveryConflict, func() ([]SourceRow, error) { return []SourceRow{row}, nil })

	rows := readAll(t, source, pipeline.Criteria{
		CriteriaGapFill:   true,
		CriteriaOrgID:     "org_test",
		CriteriaProjectID: projectID,
		CriteriaFrom:      from,
		CriteriaBatchSize: 2,
	})

	require.Equal(t, []uuid.UUID{row.ID}, rowIDs(rows))
	require.Len(t, *calls, 2)
	require.Equal(t, (*calls)[0].args, (*calls)[1].args)
	require.Equal(t, from, (*calls)[1].args[6])
	require.Equal(t, uuid.Nil, (*calls)[1].args[7])
}

func TestSourceGivesUpAfterRepeatedRecoveryConflicts(t *testing.T) {
	t.Parallel()

	source, calls := scriptedSource(recoveryConflict)
	cursor := uuid.New()
	out := make(chan SourceRow, 1)
	err := source.Read(t.Context(), pipeline.Criteria{CriteriaCursor: cursor, CriteriaBatchSize: 2}, out)

	require.ErrorContains(t, err, "read page after "+cursor.String())
	require.Len(t, *calls, pageAttempts)
}

func TestSourceDoesNotRetryOtherErrors(t *testing.T) {
	t.Parallel()

	source, calls := scriptedSource(func() ([]SourceRow, error) { return nil, errors.New("connection reset") })
	out := make(chan SourceRow, 1)
	err := source.Read(t.Context(), pipeline.Criteria{CriteriaBatchSize: 2}, out)

	require.ErrorContains(t, err, "connection reset")
	require.Len(t, *calls, 1)
}

func TestGapFillCursorRoundTrips(t *testing.T) {
	t.Parallel()

	cursor := GapFillCursor{
		ProjectID: uuid.New(),
		CreatedAt: time.Date(2026, time.August, 3, 4, 5, 6, 789000, time.UTC),
		ID:        uuid.New(),
	}
	parsed, err := ParseGapFillCursor(cursor.String())
	require.NoError(t, err)
	require.Equal(t, cursor, parsed)

	_, err = ParseGapFillCursor(cursor.ID.String())
	require.Error(t, err)
}
