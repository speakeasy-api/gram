// Package riskfindingscols implements the Postgres source, transform, and
// ClickHouse sink that backfill the message_created_at and assistant_id
// columns onto EXISTING ClickHouse risk_findings rows. Unlike the sibling
// riskfindings package (which inserts whole rows), this migration issues
// ALTER TABLE ... UPDATE mutations keyed by finding id: re-inserting enriched
// copies would be wrong because the read path dedups duplicate ids by keeping
// the copy that sorts first under message_created_at DESC ... inserted_at
// DESC, and an old copy's DEFAULT message_created_at (= created_at, the scan
// time) is >= the enriched copy's true event time, so the unenriched copy
// would win.
package riskfindingscols

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/pipeline"
)

// DefaultBatchSize is the number of rows fetched per source page and mutated
// per sink batch when the caller does not override it. It is deliberately much
// smaller than the riskfindings insert batch: every row contributes its id
// three times (two transform() maps plus the IN list) to a single ALTER
// statement, so large batches would push the query text past ClickHouse's
// default 256 KiB max_query_size.
const DefaultBatchSize = 500

// Criteria keys understood by the Postgres source. All are optional; an unset
// time window scans the whole table.
const (
	CriteriaOrgID     = "org_id"     // string; filters organization_id
	CriteriaProjectID = "project_id" // uuid.UUID; filters project_id
	CriteriaFrom      = "from"       // time.Time; created_at >= from (applies with or without a cursor)
	CriteriaTo        = "to"         // time.Time; created_at < to
	CriteriaCursor    = "cursor"     // uuid.UUID; resume after this id (exclusive)
	CriteriaBatchSize = "batch_size" // int; rows per page
)

// SourceRow is the per-finding enrichment tuple read from Postgres: the
// finding id (which is also the ClickHouse row id), the finding's created_at
// (used by the sink to bound the mutation for partition pruning), the event
// time of the scanned chat message, and the assistant linked to the finding's
// chat (empty when none).
type SourceRow struct {
	ID        uuid.UUID
	CreatedAt time.Time

	// MessageCreatedAt is chat_messages.created_at for the scanned message.
	// When the finding is not anchored to a chat message (content-part
	// anchored rows), it falls back to the finding's created_at — the same
	// value the ClickHouse column DEFAULT computes, making the update a
	// no-op for those rows rather than a wrong value.
	MessageCreatedAt time.Time

	// AssistantID is assistant_threads.assistant_id for a live (deleted IS
	// FALSE) thread whose chat_id matches the scanned message's chat. Empty
	// when the finding has no message, the chat backs no thread, or the
	// thread is soft-deleted.
	AssistantID string
}

// selectPage walks risk_results in id order (uuidv7). The id is used ONLY as a
// keyset pagination/resume key (id > cursor); it is deliberately NOT used to
// prune the time window (a row's uuidv7 id and its created_at are minted at
// slightly different instants, so the id timestamp is not a sound bound for a
// created_at filter). Time bounds are enforced exactly by the created_at
// predicates ($3/$4).
//
// Optional filters use the "$n IS NULL OR col = $n" idiom so a single prepared
// statement serves every filter combination.
//
// found IS TRUE AND rule_id IS NOT NULL mirrors both the live outbox emission
// and the riskfindings backfill source: only those rows exist in ClickHouse,
// so mutating any other id would be wasted work.
//
// The chat_messages LEFT JOIN misses only for content-part-anchored findings
// (message-anchored rows cannot dangle: the FK cascades the risk_results row
// away with its message); COALESCE then falls back to the finding's own
// created_at, matching the ClickHouse column DEFAULT. The assistant lookup
// mirrors the live GetAssistantThreadAssistantIDByChatID query
// (chat/queries.sql): a live (deleted IS FALSE) assistant_threads row whose
// chat_id matches the scanned message's chat, with ORDER BY id LIMIT 1 making
// the pick deterministic should a chat ever back multiple threads.
const selectPage = `
SELECT r.id,
       r.created_at,
       COALESCE(cm.created_at, r.created_at) AS message_created_at,
       COALESCE(at.assistant_id::text, '') AS assistant_id
FROM risk_results r
LEFT JOIN chat_messages cm
  ON cm.id = r.chat_message_id
LEFT JOIN LATERAL (
    SELECT t.assistant_id
    FROM assistant_threads t
    WHERE t.chat_id = cm.chat_id
      AND t.deleted IS FALSE
    ORDER BY t.id
    LIMIT 1
) at ON TRUE
WHERE ($1::text IS NULL OR r.organization_id = $1)
  AND ($2::uuid IS NULL OR r.project_id = $2)
  AND ($3::timestamptz IS NULL OR r.created_at >= $3)
  AND ($4::timestamptz IS NULL OR r.created_at < $4)
  AND r.id > $5
  AND r.found IS TRUE
  AND r.rule_id IS NOT NULL
ORDER BY r.id
LIMIT $6
`

// Source reads enrichment tuples from Postgres page by page and publishes them
// to the pipeline. It tracks the last processed id so an interrupted run can
// resume.
type Source struct {
	pool *pgxpool.Pool

	scanned int64
}

// NewSource builds a Postgres source over pool.
func NewSource(pool *pgxpool.Pool) *Source {
	return &Source{
		pool:    pool,
		scanned: 0,
	}
}

// Scanned returns the number of rows read so far.
func (s *Source) Scanned() int64 { return s.scanned }

// Read implements pipeline.Source. It paginates risk_results by keyset over
// id, publishing each enrichment tuple to out, and returns when the window is
// exhausted.
func (s *Source) Read(ctx context.Context, criteria pipeline.Criteria, out chan<- SourceRow) error {
	org, _ := criteria[CriteriaOrgID].(string)
	batchSize, _ := criteria[CriteriaBatchSize].(int)
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	// Keyset lower bound / resume point. The cursor only sets the id resume
	// position (id > cursor); it does NOT relax the time window. -from/-to
	// still apply so a resumed scoped run stays inside its window.
	cursor := uuid.Nil
	if c, ok := criteria[CriteriaCursor].(uuid.UUID); ok {
		cursor = c
	}

	// nil interface values become SQL NULL, disabling the optional filters.
	var fromArg, toArg any
	if from, ok := criteria[CriteriaFrom].(time.Time); ok {
		fromArg = from
	}
	if to, ok := criteria[CriteriaTo].(time.Time); ok {
		toArg = to
	}
	var orgArg, projectArg any
	if org != "" {
		orgArg = org
	}
	if projectID, ok := criteria[CriteriaProjectID].(uuid.UUID); ok {
		projectArg = projectID
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("read interrupted at %s: %w", cursor, err)
		}

		rows, err := s.pool.Query(ctx, selectPage, orgArg, projectArg, fromArg, toArg, cursor, batchSize)
		if err != nil {
			return fmt.Errorf("query page after %s: %w", cursor, err)
		}

		n := 0
		for rows.Next() {
			var r SourceRow
			if err := rows.Scan(&r.ID, &r.CreatedAt, &r.MessageCreatedAt, &r.AssistantID); err != nil {
				rows.Close()
				return fmt.Errorf("scan row after %s: %w", cursor, err)
			}
			cursor = r.ID
			n++

			select {
			case out <- r:
			case <-ctx.Done():
				rows.Close()
				return fmt.Errorf("publish interrupted at %s: %w", cursor, ctx.Err())
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("iterate page after %s: %w", cursor, err)
		}
		rows.Close()

		s.scanned += int64(n)
		// This is the read position, not a safe resume point: rows up to here
		// may still be in flight downstream. The resume cursor is the sink's
		// committed id (see Sink.LastCommitted), printed in the final report.
		log.Printf("source: read page=%d total=%d read_through=%s", n, s.scanned, cursor)

		// A short page means we reached the end of the window.
		if n < batchSize {
			return nil
		}
	}
}
