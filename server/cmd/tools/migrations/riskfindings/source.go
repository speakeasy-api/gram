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
	// it (deleted chat/part, missing message, cross-project part).
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

// selectPage walks risk_results in id order (uuidv7). The id is used ONLY as a
// keyset pagination/resume key (id > cursor); it is deliberately NOT used to
// prune the time window. A row's uuidv7 id and its created_at are minted at
// slightly different instants (generate_uuidv7() vs clock_timestamp()), so the
// id timestamp is not a sound bound for a created_at filter — using it could drop
// a row whose created_at is inside the window but whose id falls just outside it.
// Time bounds are therefore enforced exactly by the created_at predicates ($4/$5).
//
// Optional filters use the "$n IS NULL OR col = $n" idiom so a single prepared
// statement serves every filter combination.
//
// Only true-positive findings are migrated: found IS TRUE AND rule_id IS NOT NULL
// mirrors the live outbox emission (findingCreatedPayloads in
// risk_result_writer.go), so the "nothing found" SourceNone sentinels and
// dead-letter rows — which never reach ClickHouse through the live path — are
// excluded here too.
//
// Rows the Presidio false-positive sweep has marked are migrated WITH their
// false_positive_at timestamp rather than dropped: risk_findings mirrors the
// mark in its own Nullable column and every read query filters
// false_positive_at IS NULL, so backfilling them preserves pre-cutover marks
// (the live FP mutation path — PR 3 — is deferred) without inflating counts.
//
// The LEFT JOINs denormalize attribution the same way the live writer's
// lookups do (risk/queries.sql):
//
// Message-anchored rows mirror GetChatMessageAttribution: chat id plus
// resolved user ids, message-level values winning over chat-level ones, the
// message event time, and the chat's most recent live assistant_threads link —
// everything collapsing to ” (and message_created_at to the finding's own
// created_at, matching the ClickHouse column DEFAULT) when the message or chat
// is gone.
//
// Content-part-anchored rows mirror GetChatContentPartAttribution: the part
// resolves its chat and, parent-message-first, its user ids. The guards are
// the live ones — the part must be live (deleted IS FALSE), its project must
// match the finding's (a NULL project, i.e. deleted project, is unverifiable),
// its chat must sit in the part's own project (nothing in the schema ties a
// part's project to its chat's, so a part pointing at a foreign-project chat
// is rejected outright rather than attributed), and the parent message must
// sit in the part's own chat (a stale or forged parent id must not hand
// another chat's user ids to this row). A part failing any guard keeps fully
// empty attribution, exactly like the live writer's project re-check. Beyond
// the live part lookup (which resolves no event time or assistant), the
// backfill also takes the parent message's created_at when present and the
// part chat's assistant link, since it has the joins at hand.
const selectPage = `
SELECT r.id, r.created_at, r.organization_id, r.project_id, r.risk_policy_id,
       r.risk_policy_version, r.chat_message_id, r.chat_content_part_id, r.source, r.found,
       r.rule_id, r.description,
       r.match, r.start_pos, r.end_pos, r.confidence, r.tags, r.spans, r.dead_letter_reason,
       r.excluded_at, r.excluded_exclusion_id, r.false_positive_at,
       COALESCE(cm.chat_id::text, ccp.chat_id::text, '') AS chat_id,
       COALESCE(NULLIF(cm.user_id, ''), NULLIF(pcm.user_id, ''), NULLIF(c.user_id, ''), '')::text AS user_id,
       COALESCE(NULLIF(cm.external_user_id, ''), NULLIF(pcm.external_user_id, ''), NULLIF(c.external_user_id, ''), '')::text AS external_user_id,
       COALESCE(cm.created_at, pcm.created_at, r.created_at) AS message_created_at,
       COALESCE(thread.assistant_id::text, '') AS assistant_id
FROM risk_results r
LEFT JOIN chat_messages cm
  ON cm.id = r.chat_message_id
LEFT JOIN chat_content_parts ccp
  ON ccp.id = r.chat_content_part_id
  AND ccp.deleted IS FALSE
  AND ccp.project_id = r.project_id
  AND EXISTS (
    SELECT 1 FROM chats pc
    WHERE pc.id = ccp.chat_id
      AND pc.project_id = ccp.project_id
  )
LEFT JOIN chat_messages pcm
  ON pcm.id = ccp.parent_chat_message_id
  AND pcm.chat_id = ccp.chat_id
LEFT JOIN chats c
  ON c.id = COALESCE(cm.chat_id, ccp.chat_id)
  AND c.deleted IS FALSE
LEFT JOIN LATERAL (
  SELECT at.assistant_id FROM assistant_threads at
  WHERE at.chat_id = COALESCE(cm.chat_id, ccp.chat_id) AND at.deleted IS FALSE
  ORDER BY at.created_at DESC LIMIT 1
) thread ON TRUE
WHERE ($1::text IS NULL OR r.organization_id = $1)
  AND ($2::uuid IS NULL OR r.project_id = $2)
  AND ($3::uuid IS NULL OR r.risk_policy_id = $3)
  AND ($4::timestamptz IS NULL OR r.created_at >= $4)
  AND ($5::timestamptz IS NULL OR r.created_at < $5)
  AND r.id > $6
  AND r.found IS TRUE
  AND r.rule_id IS NOT NULL
ORDER BY r.id
LIMIT $7
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
	var fromArg, toArg any
	if from, ok := criteria[CriteriaFrom].(time.Time); ok {
		fromArg = from
	}
	if to, ok := criteria[CriteriaTo].(time.Time); ok {
		toArg = to
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

		rows, err := s.pool.Query(ctx, selectPage, orgArg, projectArg, policyArg, fromArg, toArg, cursor, batchSize)
		if err != nil {
			return fmt.Errorf("query page after %s: %w", cursor, err)
		}

		n := 0
		for rows.Next() {
			var r SourceRow
			if err := rows.Scan(
				&r.ID, &r.CreatedAt, &r.OrganizationID, &r.ProjectID, &r.RiskPolicyID,
				&r.RiskPolicyVersion, &r.ChatMessageID, &r.ContentPartID, &r.Source, &r.Found,
				&r.RuleID, &r.Description,
				&r.Match, &r.StartPos, &r.EndPos, &r.Confidence, &r.Tags, &r.Spans, &r.DeadLetterReason,
				&r.ExcludedAt, &r.ExclusionID, &r.FalsePositiveAt,
				&r.ChatID, &r.UserID, &r.ExternalUserID,
				&r.MessageCreatedAt, &r.AssistantID,
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
