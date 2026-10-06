// Package riskfindings implements the Postgres source, transform, and ClickHouse
// sink that back-fill historical risk_results rows into the ClickHouse
// risk_findings event log. It is the first concrete use of the generic
// cmd/tools/migrations/pipeline harness.
package riskfindings

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/pipeline"
	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/riskfindings/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// DefaultBatchSize is the default sink batch size and the maximum number of
// rows materialized per source page by SQLc.
const DefaultBatchSize = 5000

// Criteria keys understood by the Postgres source. All are optional except that
// an unset time window scans the whole table.
const (
	CriteriaOrgID     = "org_id"     // string; filters organization_id
	CriteriaProjectID = "project_id" // uuid.UUID; filters project_id
	CriteriaPolicyID  = "policy_id"  // uuid.UUID; filters risk_policy_id
	CriteriaFrom      = "from"       // time.Time; created_at >= from (applies with or without a cursor)
	CriteriaTo        = "to"         // time.Time; created_at < to
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
	// Exactly one of ChatMessageID / ContentPartID is set (the table enforces
	// it): findings anchor to a chat message or to a chat content part.
	ChatMessageID    uuid.NullUUID
	ContentPartID    uuid.NullUUID
	Source           string
	Found            bool
	RuleID           *string
	Description      *string
	Match            *string
	StartPos         *int32
	EndPos           *int32
	Confidence       *float64
	Tags             []string
	Spans            []byte // raw risk_results.spans JSONB: array of {match,field,path,start_pos,end_pos}; nil for legacy/empty rows
	DeadLetterReason *string
	ExcludedAt       *time.Time
	ExclusionID      *uuid.UUID
	FalsePositiveAt  *time.Time

	// Denormalized attribution resolved by the source's LEFT JOINs, mirroring
	// the live writer's lookups: GetChatMessageAttribution for message-anchored
	// rows (message-level user ids win over chat-level ones) and
	// GetChatContentPartAttribution for content-part-anchored rows (parent
	// message first, then the part's chat, with the live project and chat-scope
	// guards). All empty when the anchor no longer resolves or a guard rejects
	// it (deleted part, missing message, cross-project part). Soft-deleted chats
	// retain message/part-derived attribution and live assistant links; only
	// the chat-level user fallback is suppressed, matching the live writer.
	ChatID         string
	UserID         string
	ExternalUserID string

	// MessageCreatedAt is the scanned message's event time
	// (chat_messages.created_at — the part's parent message for content-part
	// anchors), falling back to the finding's own created_at when no message
	// resolves — the same value the ClickHouse column DEFAULT computes.
	MessageCreatedAt time.Time
	// AssistantID is the anchor chat's most recent live assistant_threads
	// link, empty when the chat backs no live thread or no chat resolves.
	AssistantID string
}

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
	if batchSize > math.MaxInt32 {
		return fmt.Errorf("source batch size exceeds SQLc limit %d", math.MaxInt32)
	}
	// SQLc materializes each page, so keep reads bounded independently of the
	// requested sink batch size. Smaller requested pages remain supported.
	pageSize := min(batchSize, DefaultBatchSize)

	// Keyset lower bound / resume point. The cursor only sets the id resume
	// position (id > cursor); it does NOT relax the time window. -from/-to still
	// apply so a resumed scoped run stays inside its window.
	cursor := uuid.Nil
	if c, ok := criteria[CriteriaCursor].(uuid.UUID); ok {
		cursor = c
	}

	// Exact time bounds via created_at (nil disables the predicate). These are
	// independent of the cursor and always apply when set: the cursor decides
	// where to resume, -from/-to decide window membership. Applying both is safe —
	// rows flow in id order, so every in-window pending row has id > cursor, and
	// applying created_at >= from cannot drop it while it does exclude an
	// out-of-window row that uuidv7/created_at skew placed past the cursor.
	var fromArg, toArg pgtype.Timestamptz
	if from, ok := criteria[CriteriaFrom].(time.Time); ok {
		fromArg = conv.ToPGTimestamptz(from)
	}
	if to, ok := criteria[CriteriaTo].(time.Time); ok {
		toArg = conv.ToPGTimestamptz(to)
	}

	// Invalid nullable values disable the optional filters.
	orgArg := conv.ToPGTextEmpty(org)
	var projectArg, policyArg uuid.NullUUID
	if projectID, ok := criteria[CriteriaProjectID].(uuid.UUID); ok {
		projectArg = conv.ToNullUUID(projectID)
	}
	if policyID, ok := criteria[CriteriaPolicyID].(uuid.UUID); ok {
		policyArg = conv.ToNullUUID(policyID)
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("read interrupted at %s: %w", cursor, err)
		}

		rows, err := repo.New(s.pool).ListSourcePage(ctx, repo.ListSourcePageParams{OrganizationID: orgArg, ProjectID: projectArg, PolicyID: policyArg, FromTime: fromArg, ToTime: toArg, Cursor: cursor, PageSize: conv.SafeInt32(pageSize)})
		if err != nil {
			return fmt.Errorf("query page after %s: %w", cursor, err)
		}

		n := 0
		for _, row := range rows {
			r := SourceRow{
				ID: row.ID, CreatedAt: row.CreatedAt.Time, OrganizationID: row.OrganizationID,
				ProjectID: row.ProjectID, RiskPolicyID: row.RiskPolicyID, RiskPolicyVersion: row.RiskPolicyVersion,
				ChatMessageID: row.ChatMessageID, ContentPartID: row.ChatContentPartID, Source: row.Source, Found: row.Found,
				RuleID: conv.FromPGText[string](row.RuleID), Description: conv.FromPGText[string](row.Description),
				Match: conv.FromPGText[string](row.Match), StartPos: conv.FromPGInt4(row.StartPos), EndPos: conv.FromPGInt4(row.EndPos),
				Confidence: conv.FromPGFloat8(row.Confidence), Tags: row.Tags, Spans: row.Spans,
				DeadLetterReason: conv.FromPGText[string](row.DeadLetterReason), ExcludedAt: nil, ExclusionID: nil, FalsePositiveAt: nil,
				ChatID: row.ChatID, UserID: row.UserID, ExternalUserID: row.ExternalUserID,
				MessageCreatedAt: row.MessageCreatedAt.Time, AssistantID: row.AssistantID,
			}
			if row.ExcludedAt.Valid {
				r.ExcludedAt = &row.ExcludedAt.Time
			}
			if row.ExcludedExclusionID.Valid {
				r.ExclusionID = &row.ExcludedExclusionID.UUID
			}
			if row.FalsePositiveAt.Valid {
				r.FalsePositiveAt = &row.FalsePositiveAt.Time
			}
			cursor = r.ID
			n++

			select {
			case out <- r:
			case <-ctx.Done():
				return fmt.Errorf("publish interrupted at %s: %w", cursor, ctx.Err())
			}
		}

		s.scanned += int64(n)
		// This is the read position, not a safe resume point: rows up to here may
		// still be in flight downstream. The resume cursor is the sink's committed
		// id (see Sink.LastCommitted), printed in the final report.
		log.Printf("source: read page=%d total=%d read_through=%s", n, s.scanned, cursor)

		// A short page means we reached the end of the window.
		if n < pageSize {
			return nil
		}
	}
}
