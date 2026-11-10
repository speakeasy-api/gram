package riskfindings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"maps"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
)

// DefaultSinkBatchSize is the number of rows accumulated before a flush when the
// caller does not override it.
const DefaultSinkBatchSize = 5000

// insertStatement lists the columns explicitly (omitting inserted_at so its
// DEFAULT now64(9) applies). AppendStruct binds FindingRow fields to these
// columns by their ch tags; every listed column must have a tagged field.
const insertStatement = `INSERT INTO risk_findings (
	id, created_at, organization_id, project_id, request_id, chat_message_id,
	content_part_id, chat_id, user_id, external_user_id,
	risk_policy_id, risk_policy_version, rule_id, description, source, confidence,
	category, tags, start_pos, end_pos, dead_letter_reason, match_len, match_redacted,
	fingerprint_pepper_version, fingerprint_global_hs256, fingerprint_tenant_hs256,
	excluded_at, exclusion_id, false_positive_at,
	message_created_at, assistant_id, surface, field, path, tool_call_id
)`

// SinkOptions configures batching, writes, and gap detection.
type SinkOptions struct {
	// BufferSize sizes the sink input channel.
	BufferSize int

	// BatchSize is the target rows per ClickHouse insert and lookup. A flush
	// can exceed it to keep all spans of one finding together.
	BatchSize int

	// DryRun disables writes while retaining lookups and counts.
	DryRun bool

	// LiftPartitionGuard disables the per-insert partition limit.
	LiftPartitionGuard bool

	// GapFill enables destination existence checks.
	GapFill bool
}

// SourceStats reports gap-fill outcomes for one finding source.
type SourceStats struct {
	// PresentByID counts rows found under the migration id.
	PresentByID int64

	// MatchedByKey counts rows found under a live writer id.
	MatchedByKey int64

	// StateCopies counts suppression copies appended or planned.
	StateCopies int64

	// SkippedCHNewer counts missing Postgres marks ignored because ClickHouse
	// has an explicit reversal or a newer state event.
	SkippedCHNewer int64

	// Inserted counts missing rows inserted or planned.
	Inserted int64
}

// Sink batches FindingRow values and inserts them into ClickHouse.
type Sink struct {
	conn               clickhouse.Conn
	in                 chan FindingRow
	batchSize          int
	dryRun             bool
	liftPartitionGuard bool
	gapFill            bool
	now                func() time.Time

	inserted             int64
	lastCommitted        uuid.UUID
	lastCommittedGapFill GapFillCursor
	bySource             map[string]SourceStats
}

// NewSink builds a ClickHouse sink.
func NewSink(conn clickhouse.Conn, options SinkOptions) *Sink {
	if options.BufferSize < 0 {
		options.BufferSize = 0
	}
	if options.BatchSize <= 0 {
		options.BatchSize = DefaultSinkBatchSize
	}
	return &Sink{
		conn:                 conn,
		in:                   make(chan FindingRow, options.BufferSize),
		batchSize:            options.BatchSize,
		dryRun:               options.DryRun,
		liftPartitionGuard:   options.LiftPartitionGuard,
		gapFill:              options.GapFill,
		now:                  time.Now,
		inserted:             0,
		lastCommitted:        uuid.Nil,
		lastCommittedGapFill: GapFillCursor{ProjectID: uuid.Nil, CreatedAt: time.Time{}, ID: uuid.Nil},
		bySource:             make(map[string]SourceStats),
	}
}

// Input implements pipeline.Sink.
func (s *Sink) Input() chan<- FindingRow { return s.in }

// Inserted returns the number of rows written (or, in dry-run, that would be).
func (s *Sink) Inserted() int64 { return s.inserted }

// StatsBySource returns gap-fill outcomes per source.
func (s *Sink) StatsBySource() map[string]SourceStats {
	out := make(map[string]SourceStats, len(s.bySource))
	maps.Copy(out, s.bySource)
	return out
}

// LastCommitted returns the Postgres id of the last finding in the last
// successfully processed batch. It stays uuid.Nil in dry-run mode because no
// durable checkpoint exists.
func (s *Sink) LastCommitted() uuid.UUID { return s.lastCommitted }

// LastCommittedGapFill returns the gap-fill resume position of the last
// committed batch; zero in dry-run mode and outside gap-fill.
func (s *Sink) LastCommittedGapFill() GapFillCursor { return s.lastCommittedGapFill }

// Run implements pipeline.Sink. It keeps every transformed row from one
// Postgres finding in the same flush so a committed cursor cannot split spans.
func (s *Sink) Run(ctx context.Context) error {
	buf := make([]FindingRow, 0, s.batchSize)

	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		if err := s.flush(ctx, buf); err != nil {
			return err
		}
		if !s.dryRun && s.conn != nil {
			last := buf[len(buf)-1]
			s.lastCommitted = last.sourceID
			if s.gapFill {
				projectID, err := uuid.Parse(last.ProjectID)
				if err != nil {
					return fmt.Errorf("parse committed project id: %w", err)
				}
				s.lastCommittedGapFill = GapFillCursor{ProjectID: projectID, CreatedAt: last.CreatedAt, ID: last.sourceID}
			}
		}
		committed := s.lastCommitted.String()
		if s.gapFill {
			committed = s.lastCommittedGapFill.String()
		}
		log.Printf("sink: flushed batch=%d inserted=%d committed=%s", len(buf), s.inserted, committed)
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
			if row.sourceID == uuid.Nil {
				row.sourceID = row.ID
			}
			if len(buf) >= s.batchSize && buf[len(buf)-1].sourceID != row.sourceID {
				if err := flush(); err != nil {
					return err
				}
			}
			buf = append(buf, row)
		}
	}
}

func (s *Sink) flush(ctx context.Context, rows []FindingRow) error {
	if s.gapFill {
		if s.conn == nil {
			return errors.New("gap-fill requires a ClickHouse connection")
		}
		return s.flushGapFill(ctx, rows)
	}

	if !s.dryRun {
		if err := s.insertRows(ctx, rows); err != nil {
			return err
		}
	}
	s.inserted += int64(len(rows))
	for _, row := range rows {
		stats := s.bySource[row.Source]
		stats.Inserted++
		s.bySource[row.Source] = stats
	}
	return nil
}

// insertRows writes one native batch.
func (s *Sink) insertRows(ctx context.Context, rows []FindingRow) error {
	if len(rows) == 0 {
		return nil
	}

	token := deduplicationToken(rows)
	settings := clickhouse.Settings{"insert_deduplication_token": token}
	if s.liftPartitionGuard {
		settings["max_partitions_per_insert_block"] = 0
	}
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(settings))

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
