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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/pipeline"
	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/riskfindingscols/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
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

	// Invalid nullable values disable the optional filters.
	var fromArg, toArg pgtype.Timestamptz
	if from, ok := criteria[CriteriaFrom].(time.Time); ok {
		fromArg = conv.ToPGTimestamptz(from)
	}
	if to, ok := criteria[CriteriaTo].(time.Time); ok {
		toArg = conv.ToPGTimestamptz(to)
	}
	orgArg := conv.ToPGTextEmpty(org)
	var projectArg uuid.NullUUID
	if projectID, ok := criteria[CriteriaProjectID].(uuid.UUID); ok {
		projectArg = conv.ToNullUUID(projectID)
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("read interrupted at %s: %w", cursor, err)
		}

		rows, err := repo.New(s.pool).ListSourcePage(ctx, repo.ListSourcePageParams{OrganizationID: orgArg, ProjectID: projectArg, FromTime: fromArg, ToTime: toArg, Cursor: cursor, PageSize: conv.SafeInt32(batchSize)})
		if err != nil {
			return fmt.Errorf("query page after %s: %w", cursor, err)
		}

		n := 0
		for _, row := range rows {
			r := SourceRow{ID: row.ID, CreatedAt: row.CreatedAt.Time, MessageCreatedAt: row.MessageCreatedAt.Time, AssistantID: row.AssistantID}
			cursor = r.ID
			n++

			select {
			case out <- r:
			case <-ctx.Done():
				return fmt.Errorf("publish interrupted at %s: %w", cursor, ctx.Err())
			}
		}

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
