package riskfindingscols

import (
	"fmt"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/pipeline"
)

// TestSinkDryRunCountsButExposesNoCursor exercises the sink lifecycle without a
// real ClickHouse connection: dry-run drains and counts every row across
// batches but must NOT expose a commit cursor, since nothing was submitted.
func TestSinkDryRunCountsButExposesNoCursor(t *testing.T) {
	t.Parallel()

	const batchSize = 2
	sink := NewSink(nil, 4, batchSize, true)

	done := make(chan error, 1)
	go func() { done <- sink.Run(t.Context()) }()

	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	in := sink.Input()
	now := time.Now().UTC()
	for _, id := range ids {
		in <- UpdateRow{ID: id, CreatedAt: now, MessageCreatedAt: now, AssistantID: ""}
	}
	close(in)

	require.NoError(t, <-done)
	require.Equal(t, int64(len(ids)), sink.Mutated())
	require.Equal(t, int64(3), sink.Batches(), "5 rows at batch size 2 are 3 mutations")
	require.Equal(t, uuid.Nil, sink.LastCommitted())
}

// TestSinkEmptyCommitsNothing checks that a sink that never receives a row does
// not advance the commit cursor.
func TestSinkEmptyCommitsNothing(t *testing.T) {
	t.Parallel()

	sink := NewSink(nil, 1, 2, true)

	done := make(chan error, 1)
	go func() { done <- sink.Run(t.Context()) }()
	close(sink.Input())

	require.NoError(t, <-done)
	require.Equal(t, int64(0), sink.Mutated())
	require.Equal(t, int64(0), sink.Batches())
	require.Equal(t, uuid.Nil, sink.LastCommitted())
}

// TestMutationStatementRendersAlignedArraysAndBound pins the exact mutation a
// batch produces: both transform() maps and the IN list share the same id
// order, timestamps render at nanosecond scale (positional binding would
// truncate to seconds, which is why the statement is built by hand), and the
// created_at bounds are the batch minimum minus the slack and the batch
// maximum plus the slack.
func TestMutationStatementRendersAlignedArraysAndBound(t *testing.T) {
	t.Parallel()

	id1 := uuid.Must(uuid.NewV7())
	id2 := uuid.Must(uuid.NewV7())
	created1 := time.Date(2026, 7, 20, 10, 0, 0, 123456789, time.UTC)
	created2 := created1.Add(-2 * time.Hour) // the batch minimum, out of order on purpose
	message1 := created1.Add(-30 * time.Minute)
	message2 := created2.Add(-45 * time.Minute)
	assistantID := uuid.NewString()

	got := mutationStatement([]UpdateRow{
		{ID: id1, CreatedAt: created1, MessageCreatedAt: message1, AssistantID: assistantID},
		{ID: id2, CreatedAt: created2, MessageCreatedAt: message2, AssistantID: ""},
	})

	want := fmt.Sprintf(
		"ALTER TABLE risk_findings UPDATE"+
			" message_created_at = transform(toString(id), ['%[1]s', '%[2]s'], [toDateTime64('%[3]d', 9), toDateTime64('%[4]d', 9)], message_created_at),"+
			" assistant_id = transform(toString(id), ['%[1]s', '%[2]s'], ['%[5]s', ''], assistant_id)"+
			" WHERE id IN ('%[1]s', '%[2]s') AND created_at >= toDateTime64('%[6]d', 9) AND created_at <= toDateTime64('%[7]d', 9)",
		id1, id2, message1.UnixNano(), message2.UnixNano(), assistantID,
		created2.Add(-createdAtSlack).UnixNano(),
		created1.Add(createdAtSlack).UnixNano(),
	)
	require.Equal(t, want, got)
}

// TestMutationStatementEscapesStrings guards the literal escaping: a value
// containing quotes or backslashes must not break out of its string literal.
func TestMutationStatementEscapesStrings(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	got := mutationStatement([]UpdateRow{
		{ID: uuid.Must(uuid.NewV7()), CreatedAt: now, MessageCreatedAt: now, AssistantID: `o'brien\`},
	})
	require.Contains(t, got, `'o\'brien\\'`)
}

// TestPipelineMutatesClickHouseRows drives the full source -> transform ->
// sink pipeline against real Postgres and ClickHouse: pre-inserted
// risk_findings rows carrying the column DEFAULTs are enriched in place by the
// mutation, while a row outside the -to window stays untouched.
func TestPipelineMutatesClickHouseRows(t *testing.T) {
	t.Parallel()

	tn := seedTenant(t)
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	ensureBackfillColumns(t, conn)

	// Recent relative dates: risk_findings has a 90-day created_at TTL, so
	// hardcoded old dates would expire at insert.
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-48 * time.Hour)
	scanAt := base.Add(2 * time.Hour)

	// A: live assistant thread, message an event time behind the scan time.
	chatA := tn.newChat(t)
	assistantID, _ := tn.linkAssistant(t, chatA)
	messageTimeA := base
	findingA := tn.newFinding(t, tn.newMessage(t, chatA, messageTimeA), scanAt)

	// B: no assistant.
	chatB := tn.newChat(t)
	messageTimeB := base.Add(time.Minute)
	findingB := tn.newFinding(t, tn.newMessage(t, chatB, messageTimeB), scanAt)

	// C: enrichable in Postgres (live thread via chat A), but outside the -to
	// window — its ClickHouse row must keep its DEFAULT values, proving the
	// bound holds end to end.
	outsideAt := scanAt.Add(time.Hour)
	findingC := tn.newFinding(t, tn.newMessage(t, chatA, base.Add(2*time.Minute)), outsideAt)

	insertFindings(t, conn, tn, []chFinding{
		{id: findingA, createdAt: scanAt},
		{id: findingB, createdAt: scanAt},
		{id: findingC, createdAt: outsideAt},
	})

	source := NewSource(tn.pool)
	sink := NewSink(conn, 16, 2, false)
	require.NoError(t, pipeline.Run[SourceRow, UpdateRow](t.Context(), source, NewTransformer(), sink, pipeline.Criteria{
		CriteriaOrgID: tn.orgID,
		CriteriaTo:    outsideAt, // exclusive: keeps C out
	}, 16))

	require.EqualValues(t, 2, source.Scanned())
	require.EqualValues(t, 2, sink.Mutated())
	require.EqualValues(t, 1, sink.Batches())
	require.NotEqual(t, uuid.Nil, sink.LastCommitted())

	// The mutation is asynchronous server-side: poll until the enriched values
	// are visible.
	type enriched struct {
		messageCreatedAt time.Time
		assistantID      string
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		rows, err := conn.Query(t.Context(), `
			SELECT id, message_created_at, assistant_id
			FROM risk_findings
			WHERE organization_id = ?
		`, tn.orgID)
		if !assert.NoError(c, err) {
			return
		}
		defer func() { _ = rows.Close() }()

		got := map[uuid.UUID]enriched{}
		for rows.Next() {
			var (
				id  uuid.UUID
				row enriched
			)
			if !assert.NoError(c, rows.Scan(&id, &row.messageCreatedAt, &row.assistantID)) {
				return
			}
			got[id] = row
		}
		if !assert.Len(c, got, 3) {
			return
		}

		a := got[findingA]
		assert.True(c, messageTimeA.Equal(a.messageCreatedAt), "A backfills the message event time, got %s", a.messageCreatedAt)
		assert.Equal(c, assistantID.String(), a.assistantID)

		b := got[findingB]
		assert.True(c, messageTimeB.Equal(b.messageCreatedAt), "B backfills the message event time, got %s", b.messageCreatedAt)
		assert.Empty(c, b.assistantID)

		cRow := got[findingC]
		assert.True(c, outsideAt.Equal(cRow.messageCreatedAt), "C stays at its DEFAULT (created_at), got %s", cRow.messageCreatedAt)
		assert.Empty(c, cRow.assistantID)
	}, 10*time.Second, 100*time.Millisecond)
}

// ensureBackfillColumns adds the two backfill target columns to the test
// container's risk_findings table. They land in server/clickhouse/schema.sql
// through a parallel workstream; ADD COLUMN IF NOT EXISTS keeps this a no-op
// once that ships.
func ensureBackfillColumns(t *testing.T, conn clickhouse.Conn) {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, conn.Exec(ctx, "ALTER TABLE risk_findings ADD COLUMN IF NOT EXISTS message_created_at DateTime64(9) DEFAULT created_at"))
	require.NoError(t, conn.Exec(ctx, "ALTER TABLE risk_findings ADD COLUMN IF NOT EXISTS assistant_id String DEFAULT ''"))
}

type chFinding struct {
	id        uuid.UUID
	createdAt time.Time
}

// insertFindings seeds pre-migration risk_findings rows: only identity and
// required columns are set, so message_created_at and assistant_id carry their
// column DEFAULTs exactly like rows inserted before the columns existed.
func insertFindings(t *testing.T, conn clickhouse.Conn, tn *tenant, rows []chFinding) {
	t.Helper()
	ctx := t.Context()

	batch, err := conn.PrepareBatch(ctx, "INSERT INTO risk_findings (id, created_at, organization_id, project_id, rule_id, source, tags)")
	require.NoError(t, err)
	for _, row := range rows {
		require.NoError(t, batch.Append(row.id, row.createdAt, tn.orgID, tn.projectID.String(), "pii.email_address", "presidio", []string{"pii"}))
	}
	require.NoError(t, batch.Send())
}
