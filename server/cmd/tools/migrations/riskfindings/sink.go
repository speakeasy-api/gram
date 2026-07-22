package riskfindings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
)

// DefaultSinkBatchSize is the number of rows accumulated before a flush when the
// caller does not override it.
const DefaultSinkBatchSize = 5000

// insertStatement lists the columns explicitly (omitting inserted_at so its
// DEFAULT now64(9) applies). AppendStruct binds FindingRow fields to these
// columns by their ch tags.
const insertStatement = `INSERT INTO risk_findings (
	id, created_at, organization_id, project_id, request_id, chat_message_id,
	risk_policy_id, risk_policy_version, rule_id, description, source, confidence,
	tags, start_pos, end_pos, dead_letter_reason, match_len, match_redacted,
	fingerprint_pepper_version, fingerprint_global_hs256, fingerprint_tenant_hs256,
	excluded_at, exclusion_id
)`

// Sink batches FindingRow values and inserts them into the ClickHouse
// risk_findings table via native prepared batches. It owns the buffered input
// channel exposed by Input; the pipeline's transform stage is the sole producer
// and closes the channel when the source is exhausted.
type Sink struct {
	conn      clickhouse.Conn
	in        chan FindingRow
	batchSize int
	dryRun    bool

	inserted      int64
	lastCommitted uuid.UUID
}

// NewSink builds a ClickHouse sink. conn may be nil when dryRun is true, in which
// case rows are counted but never written. bufferSize sizes the input channel;
// batchSize bounds each insert.
func NewSink(conn clickhouse.Conn, bufferSize, batchSize int, dryRun bool) *Sink {
	if bufferSize < 0 {
		bufferSize = 0
	}
	if batchSize <= 0 {
		batchSize = DefaultSinkBatchSize
	}
	return &Sink{
		conn:          conn,
		in:            make(chan FindingRow, bufferSize),
		batchSize:     batchSize,
		dryRun:        dryRun,
		inserted:      0,
		lastCommitted: uuid.Nil,
	}
}

// Input implements pipeline.Sink.
func (s *Sink) Input() chan<- FindingRow { return s.in }

// Inserted returns the number of rows written (or, in dry-run, that would be).
func (s *Sink) Inserted() int64 { return s.inserted }

// LastCommitted returns the id of the last row in the last successfully flushed
// batch. Because records flow through the pipeline in id order, everything up to
// and including this id is durably written, so it is the safe cursor to resume a
// later run from (unlike the source's read position, which runs ahead). It stays
// uuid.Nil in dry-run, where no batch is actually written and there is therefore
// no durable checkpoint.
func (s *Sink) LastCommitted() uuid.UUID { return s.lastCommitted }

// Run implements pipeline.Sink. It drains the input channel, flushing whenever a
// batch fills, and flushes the final partial batch when the channel closes.
func (s *Sink) Run(ctx context.Context) error {
	buf := make([]FindingRow, 0, s.batchSize)

	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		if err := s.flush(ctx, buf); err != nil {
			return err
		}
		s.inserted += int64(len(buf))
		// Only a real write advances the durable resume cursor. In dry-run the
		// flush is a no-op, so exposing a cursor would let an operator resume the
		// real migration from a checkpoint that was never written.
		if !s.dryRun && s.conn != nil {
			s.lastCommitted = buf[len(buf)-1].ID
		}
		log.Printf("sink: flushed batch=%d total=%d committed=%s", len(buf), s.inserted, s.lastCommitted)
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

// flush writes one batch. A deterministic per-batch deduplication token makes a
// re-run of the same batch idempotent on Replicated engines (ignored on plain
// MergeTree, where resuming from the checkpoint bounds any duplication to the
// single in-flight batch).
func (s *Sink) flush(ctx context.Context, rows []FindingRow) error {
	if s.dryRun || s.conn == nil {
		return nil
	}

	token := deduplicationToken(rows)
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"insert_deduplication_token": token,
		// risk_findings is PARTITION BY toYYYYMMDD(created_at). A single backfill
		// batch is time-ordered but can still span more than the default 100
		// partitions when historical data is sparse, so lift the guard (0 = no
		// limit) for this one-shot operator insert.
		"max_partitions_per_insert_block": 0,
	}))

	batch, err := s.conn.PrepareBatch(ctx, insertStatement)
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}

	if err := appendRows(batch, rows); err != nil {
		_ = batch.Abort()
		return err
	}

	if err := batch.Send(); err != nil {
		_ = batch.Abort()
		return fmt.Errorf("send batch: %w", err)
	}
	return nil
}

// deduplicationToken identifies a batch by the full ordered set of its row ids,
// not just its endpoints: two batches can share the same first and last id but
// differ in their interior (e.g. reruns under a different -batch-size or filter),
// and an endpoints-only token would make a Replicated engine wrongly drop the
// second as a duplicate. Hashing every id makes the token collide only for a
// genuinely identical batch.
func deduplicationToken(rows []FindingRow) string {
	h := sha256.New()
	for i := range rows {
		id := rows[i].ID
		_, _ = h.Write(id[:])
	}
	return "riskfindings-backfill:" + hex.EncodeToString(h.Sum(nil))
}

func appendRows(batch driver.Batch, rows []FindingRow) error {
	for i := range rows {
		if err := batch.AppendStruct(&rows[i]); err != nil {
			return fmt.Errorf("append row %s: %w", rows[i].ID, err)
		}
	}
	return nil
}
