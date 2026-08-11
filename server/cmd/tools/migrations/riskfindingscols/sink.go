package riskfindingscols

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
)

// createdAtSlack widens the mutation's created_at bounds beyond the batch's
// minimum and maximum finding created_at. The ClickHouse created_at is the
// exact value copied from Postgres, so in principle no slack is needed; an
// hour of headroom guards against any precision or clock quirks while still
// letting ClickHouse prune all but the batch's own daily partitions.
const createdAtSlack = time.Hour

// Sink batches UpdateRow values and applies each batch as ONE ClickHouse
// mutation:
//
//	ALTER TABLE risk_findings UPDATE
//	    message_created_at = transform(toString(id), [ids...], [values...], message_created_at),
//	    assistant_id       = transform(toString(id), [ids...], [values...], assistant_id)
//	WHERE id IN (ids...)
//	  AND created_at >= <min batch created_at - slack>
//	  AND created_at <= <max batch created_at + slack>
//
// Mutations — not inserts — are required here: the read path dedups duplicate
// ids by keeping the copy sorting first under message_created_at DESC ...
// inserted_at DESC, and an old copy's DEFAULT message_created_at (created_at,
// i.e. scan time) is >= the enriched copy's event time, so a re-inserted
// enriched copy would lose to the original. The created_at bound exists purely
// for partition pruning (the table is partitioned daily on created_at).
//
// Mutations are asynchronous server-side: Exec returns once the mutation is
// queued, and ClickHouse rewrites the affected parts in the background. The
// sink fires and continues, logging each batch's id range; completion can be
// observed via system.mutations.
type Sink struct {
	conn      clickhouse.Conn
	in        chan UpdateRow
	batchSize int
	dryRun    bool

	mutated       int64
	batches       int64
	lastCommitted uuid.UUID
}

// NewSink builds a ClickHouse mutation sink. conn may be nil when dryRun is
// true, in which case batches are counted and logged but no mutation is
// issued. bufferSize sizes the input channel; batchSize bounds each mutation.
func NewSink(conn clickhouse.Conn, bufferSize, batchSize int, dryRun bool) *Sink {
	if bufferSize < 0 {
		bufferSize = 0
	}
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &Sink{
		conn:          conn,
		in:            make(chan UpdateRow, bufferSize),
		batchSize:     batchSize,
		dryRun:        dryRun,
		mutated:       0,
		batches:       0,
		lastCommitted: uuid.Nil,
	}
}

// Input implements pipeline.Sink.
func (s *Sink) Input() chan<- UpdateRow { return s.in }

// Mutated returns the number of rows targeted by submitted mutations (or, in
// dry-run, that would be).
func (s *Sink) Mutated() int64 { return s.mutated }

// Batches returns the number of mutations submitted (or, in dry-run, that
// would be).
func (s *Sink) Batches() int64 { return s.batches }

// LastCommitted returns the id of the last row in the last successfully
// submitted mutation. Because records flow through the pipeline in id order,
// everything up to and including this id has been handed to ClickHouse, so it
// is the safe cursor to resume a later run from (unlike the source's read
// position, which runs ahead). "Submitted" — not "applied": mutations complete
// asynchronously, but a queued mutation survives restarts server-side, so
// resuming past it never loses work. It stays uuid.Nil in dry-run, where no
// mutation is submitted and there is therefore no durable checkpoint.
func (s *Sink) LastCommitted() uuid.UUID { return s.lastCommitted }

// Run implements pipeline.Sink. It drains the input channel, flushing whenever
// a batch fills, and flushes the final partial batch when the channel closes.
func (s *Sink) Run(ctx context.Context) error {
	buf := make([]UpdateRow, 0, s.batchSize)

	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		if err := s.flush(ctx, buf); err != nil {
			return err
		}
		s.mutated += int64(len(buf))
		s.batches++
		// Only a real submission advances the durable resume cursor. In
		// dry-run the flush is a no-op, so exposing a cursor would let an
		// operator resume the real migration from a checkpoint that was never
		// written.
		if !s.dryRun && s.conn != nil {
			s.lastCommitted = buf[len(buf)-1].ID
		}
		log.Printf("sink: %s batch=%d rows total=%d ids=[%s..%s]",
			mutationVerb(s.dryRun), len(buf), s.mutated, buf[0].ID, buf[len(buf)-1].ID)
		buf = buf[:0]
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("sink interrupted: %w", ctx.Err())
		case row, ok := <-s.in:
			if !ok {
				return flush()
			}
			buf = append(buf, row)
			if len(buf) >= s.batchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
}

func mutationVerb(dryRun bool) string {
	if dryRun {
		return "would mutate"
	}
	return "submitted mutation"
}

// flush submits one batch as a single mutation. Re-running a batch is
// idempotent by construction: the mutation rewrites each targeted row to the
// same values, so overlap after a resume is harmless.
func (s *Sink) flush(ctx context.Context, rows []UpdateRow) error {
	if s.dryRun {
		return nil
	}
	// A nil connection is only legal in dry-run mode; treating it as a no-op
	// here would count and log submitted mutations while writing nothing.
	if s.conn == nil {
		return errors.New("nil clickhouse connection with dry-run disabled")
	}

	stmt := mutationStatement(rows)
	if err := s.conn.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("submit mutation for batch [%s..%s]: %w", rows[0].ID, rows[len(rows)-1].ID, err)
	}
	return nil
}

var chStringEscaper = strings.NewReplacer(`\`, `\\`, `'`, `\'`)

// mutationStatement renders the single ALTER TABLE ... UPDATE covering rows.
// Values are inlined as literals rather than bound: clickhouse-go's positional
// binding formats time.Time at second precision only, which would truncate the
// sub-second event times this migration exists to backfill. Timestamps are
// rendered the same way the driver renders nanosecond-scale values —
// toDateTime64('<unix-nanos>', 9) — which is timezone-independent, and the id
// and assistant values are canonical UUID strings (escaped anyway, out of
// caution).
func mutationStatement(rows []UpdateRow) string {
	ids := make([]string, len(rows))
	messageTimes := make([]string, len(rows))
	assistants := make([]string, len(rows))
	minCreatedAt := rows[0].CreatedAt
	maxCreatedAt := rows[0].CreatedAt
	for i := range rows {
		ids[i] = "'" + chStringEscaper.Replace(rows[i].ID.String()) + "'"
		messageTimes[i] = fmt.Sprintf("toDateTime64('%d', 9)", rows[i].MessageCreatedAt.UnixNano())
		assistants[i] = "'" + chStringEscaper.Replace(rows[i].AssistantID) + "'"
		if rows[i].CreatedAt.Before(minCreatedAt) {
			minCreatedAt = rows[i].CreatedAt
		}
		if rows[i].CreatedAt.After(maxCreatedAt) {
			maxCreatedAt = rows[i].CreatedAt
		}
	}

	// created_at is bounded on both sides of the batch (with slack) so each
	// mutation prunes to the batch's own daily partitions. A lower bound alone
	// would leave every later partition scanned by every batch, queuing
	// repeated full-tail mutations over a long backfill.
	idList := strings.Join(ids, ", ")
	return fmt.Sprintf(
		"ALTER TABLE risk_findings UPDATE"+
			" message_created_at = transform(toString(id), [%s], [%s], message_created_at),"+
			" assistant_id = transform(toString(id), [%s], [%s], assistant_id)"+
			" WHERE id IN (%s) AND created_at >= toDateTime64('%d', 9) AND created_at <= toDateTime64('%d', 9)",
		idList, strings.Join(messageTimes, ", "),
		idList, strings.Join(assistants, ", "),
		idList, minCreatedAt.Add(-createdAtSlack).UnixNano(), maxCreatedAt.Add(createdAtSlack).UnixNano(),
	)
}
