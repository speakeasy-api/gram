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
const selectPage = `
SELECT id, created_at, organization_id, project_id, risk_policy_id,
       risk_policy_version, chat_message_id, source, rule_id, description,
       match, start_pos, end_pos, confidence, tags, dead_letter_reason,
       excluded_at, excluded_exclusion_id
FROM risk_results
WHERE ($1::text IS NULL OR organization_id = $1)
  AND ($2::uuid IS NULL OR project_id = $2)
  AND ($3::uuid IS NULL OR risk_policy_id = $3)
  AND id > $4
  AND id < $5
ORDER BY id
LIMIT $6
`

// Source reads risk_results pages from Postgres and publishes them to the
// pipeline. It tracks the last processed id so an interrupted run can resume.
type Source struct {
	pool *pgxpool.Pool

	scanned    int64
	lastCursor uuid.UUID
}

// NewSource builds a Postgres source over pool.
func NewSource(pool *pgxpool.Pool) *Source {
	return &Source{
		pool:       pool,
		scanned:    0,
		lastCursor: uuid.Nil,
	}
}

// Scanned returns the number of rows read so far.
func (s *Source) Scanned() int64 { return s.scanned }

// LastCursor returns the id of the last row read, for resuming a later run.
func (s *Source) LastCursor() uuid.UUID { return s.lastCursor }

// Read implements pipeline.Source. It paginates risk_results by keyset over id,
// publishing each row to out, and returns when the window is exhausted.
func (s *Source) Read(ctx context.Context, criteria pipeline.Criteria, out chan<- SourceRow) error {
	org, _ := criteria[CriteriaOrgID].(string)
	batchSize, _ := criteria[CriteriaBatchSize].(int)
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	// Lower keyset bound: an explicit cursor wins, else derive it from -from,
	// else scan from the beginning.
	cursor := uuid.Nil
	if c, ok := criteria[CriteriaCursor].(uuid.UUID); ok {
		cursor = c
	} else if from, ok := criteria[CriteriaFrom].(time.Time); ok {
		cursor = uuidv7.LowerBound(from)
	}
	s.lastCursor = cursor

	// Upper keyset bound: derive from -to, else scan to the maximum id.
	upper := uuid.Max
	if to, ok := criteria[CriteriaTo].(time.Time); ok {
		upper = uuidv7.LowerBound(to)
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

		rows, err := s.pool.Query(ctx, selectPage, orgArg, projectArg, policyArg, cursor, upper, batchSize)
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
		s.lastCursor = cursor
		log.Printf("source: scanned page=%d total=%d cursor=%s", n, s.scanned, cursor)

		// A short page means we reached the end of the window.
		if n < batchSize {
			return nil
		}
	}
}
