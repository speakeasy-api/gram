// Package riskfindings implements the Postgres source, transform, and ClickHouse
// sink that back-fill historical risk_results rows into the ClickHouse
// risk_findings event log. It is the first concrete use of the generic
// cmd/tools/migrations/pipeline harness.
package riskfindings

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/pipeline"
	"github.com/speakeasy-api/gram/server/internal/uuidv7"
)

// DefaultBatchSize is the number of rows fetched per page when the caller does
// not set a "batch_size" criteria.
const DefaultBatchSize = 5000

// Criteria keys understood by the Postgres source. All are optional except that
// an unset time window scans the whole table.
const (
	CriteriaOrgID     = "org_id"     // string; filters organization_id
	CriteriaProjectID = "project_id" // uuid.UUID; filters project_id
	CriteriaPolicyID  = "policy_id"  // uuid.UUID; filters risk_policy_id
	CriteriaFrom      = "from"       // time.Time; lower bound (inclusive) via uuidv7 id
	CriteriaTo        = "to"         // time.Time; upper bound (exclusive) via uuidv7 id
	CriteriaCursor    = "cursor"     // uuid.UUID; resume after this id (exclusive)
	CriteriaBatchSize = "batch_size" // int; rows per page
)

// SourceRow is one risk_results row as read from Postgres. It is the item type
// flowing from the source into the transform stage.
type SourceRow struct {
	ID                uuid.UUID
	CreatedAt         time.Time
	OrganizationID    string
	ProjectID         uuid.UUID
	RiskPolicyID      uuid.UUID
	RiskPolicyVersion int64
	ChatMessageID     uuid.UUID
	Source            string
	RuleID            *string
	Description       *string
	Match             *string
	StartPos          *int32
	EndPos            *int32
	Confidence        *float64
	Tags              []string
	DeadLetterReason  *string
	ExcludedAt        *time.Time
	ExclusionID       *uuid.UUID
}

// selectPage walks risk_results in id order (uuidv7, so id order is time order).
// Optional filters use the "$n IS NULL OR col = $n" idiom so a single prepared
// statement serves every filter combination. The keyset window (id > cursor,
// id < upper) makes the scan resumable and cheap on the primary key.
//
// Only real findings are migrated: found IS TRUE AND rule_id IS NOT NULL mirrors
// the live outbox emission (findingCreatedPayloads in risk_result_writer.go), so
// the "nothing found" SourceNone sentinels and dead-letter rows — which never
// reach ClickHouse through the live path — are excluded here too.
//
// The keyset bounds (id) are only millisecond-precise because they are derived
// from uuidv7.LowerBound, so the exact created_at predicates ($4/$5) enforce
// sub-millisecond -from/-to boundaries the coarse id window cannot.
const selectPage = `
SELECT id, created_at, organization_id, project_id, risk_policy_id,
       risk_policy_version, chat_message_id, source, rule_id, description,
       match, start_pos, end_pos, confidence, tags, dead_letter_reason,
       excluded_at, excluded_exclusion_id
FROM risk_results
WHERE ($1::text IS NULL OR organization_id = $1)
  AND ($2::uuid IS NULL OR project_id = $2)
  AND ($3::uuid IS NULL OR risk_policy_id = $3)
  AND ($4::timestamptz IS NULL OR created_at >= $4)
  AND ($5::timestamptz IS NULL OR created_at < $5)
  AND id > $6
  AND id < $7
  AND found IS TRUE
  AND rule_id IS NOT NULL
ORDER BY id
LIMIT $8
`

// Source reads risk_results pages from Postgres and publishes them to the
// pipeline. It tracks the last processed id so an interrupted run can resume.
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

// Read implements pipeline.Source. It paginates risk_results by keyset over id,
// publishing each row to out, and returns when the window is exhausted.
func (s *Source) Read(ctx context.Context, criteria pipeline.Criteria, out chan<- SourceRow) error {
	org, _ := criteria[CriteriaOrgID].(string)
	batchSize, _ := criteria[CriteriaBatchSize].(int)
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	// Exact time bounds (nil disables the created_at predicate). These enforce
	// the requested precision; the id keyset bounds below are only a coarse,
	// millisecond-granular prune around them.
	var fromArg, toArg any
	from, hasFrom := criteria[CriteriaFrom].(time.Time)
	if hasFrom {
		fromArg = from
	}
	to, hasTo := criteria[CriteriaTo].(time.Time)
	if hasTo {
		toArg = to
	}

	// Lower keyset bound: an explicit cursor wins, else the floor id of -from,
	// else scan from the beginning. Flooring is safe — it can only include a few
	// extra rows in -from's millisecond, which the created_at >= from predicate
	// then trims.
	cursor := uuid.Nil
	if c, ok := criteria[CriteriaCursor].(uuid.UUID); ok {
		cursor = c
	} else if hasFrom {
		cursor = uuidv7.LowerBound(from)
	}

	// Upper keyset bound: the ceiling id of -to, else the maximum id. Rounding
	// -to up to the next millisecond keeps every row in -to's millisecond inside
	// the keyset window so the created_at < to predicate — not the coarse id
	// bound — decides the boundary.
	upper := uuid.Max
	if hasTo {
		ceil := to.Truncate(time.Millisecond)
		if ceil.Before(to) {
			ceil = ceil.Add(time.Millisecond)
		}
		upper = uuidv7.LowerBound(ceil)
	}

	// nil interface values become SQL NULL, disabling the optional filters.
	var orgArg, projectArg, policyArg any
	if org != "" {
		orgArg = org
	}
	if projectID, ok := criteria[CriteriaProjectID].(uuid.UUID); ok {
		projectArg = projectID
	}
	if policyID, ok := criteria[CriteriaPolicyID].(uuid.UUID); ok {
		policyArg = policyID
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("read interrupted at %s: %w", cursor, err)
		}

		rows, err := s.pool.Query(ctx, selectPage, orgArg, projectArg, policyArg, fromArg, toArg, cursor, upper, batchSize)
		if err != nil {
			return fmt.Errorf("query page after %s: %w", cursor, err)
		}

		n := 0
		for rows.Next() {
			var r SourceRow
			if err := rows.Scan(
				&r.ID, &r.CreatedAt, &r.OrganizationID, &r.ProjectID, &r.RiskPolicyID,
				&r.RiskPolicyVersion, &r.ChatMessageID, &r.Source, &r.RuleID, &r.Description,
				&r.Match, &r.StartPos, &r.EndPos, &r.Confidence, &r.Tags, &r.DeadLetterReason,
				&r.ExcludedAt, &r.ExclusionID,
			); err != nil {
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
		// This is the read position, not a safe resume point: rows up to here may
		// still be in flight downstream. The resume cursor is the sink's committed
		// id (see Sink.LastCommitted), printed in the final report.
		log.Printf("source: read page=%d total=%d read_through=%s", n, s.scanned, cursor)

		// A short page means we reached the end of the window.
		if n < batchSize {
			return nil
		}
	}
}
