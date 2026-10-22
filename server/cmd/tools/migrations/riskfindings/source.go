// Package riskfindings implements the Postgres source, transform, and ClickHouse
// sink that back-fill historical risk_results rows into the ClickHouse
// risk_findings event log. It is the first concrete use of the generic
// cmd/tools/migrations/pipeline harness.
package riskfindings

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	CriteriaSources   = "sources"    // []string; filters source
	CriteriaFrom      = "from"       // time.Time; created_at >= from (applies with or without a cursor)
	CriteriaTo        = "to"         // time.Time; created_at < to
	CriteriaCursor    = "cursor"     // uuid.UUID; resume after this id (exclusive)
	CriteriaBatchSize = "batch_size" // int; rows per page

	// CriteriaGapFill (bool) pages each project over (created_at, id).
	CriteriaGapFill = "gap_fill"

	// CriteriaGapFillCursor (GapFillCursor) resumes a gap-fill run.
	CriteriaGapFillCursor = "gap_fill_cursor"
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
// id timestamp is not a sound bound for a created_at filter. Using it could drop
// a row whose created_at is inside the window but whose id falls just outside it.
// Time bounds are therefore enforced exactly by the created_at predicates ($5/$6).
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
const selectPage = selectRiskResultsWithAttribution + `WHERE ($1::text IS NULL OR r.organization_id = $1)
  AND ($2::uuid IS NULL OR r.project_id = $2)
  AND ($3::uuid IS NULL OR r.risk_policy_id = $3)
  AND ($4::text[] IS NULL OR r.source = ANY($4))
  AND ($5::timestamptz IS NULL OR r.created_at >= $5)
  AND ($6::timestamptz IS NULL OR r.created_at < $6)
  AND r.id > $7
  AND r.found IS TRUE
  AND r.rule_id IS NOT NULL
ORDER BY r.id
LIMIT $8
`

// selectProjectPage is the gap-fill page: one project, keyset over
// (created_at, id). The project and created_at bounds are never optional, so
// the (project_id, created_at) indexes drive the scan instead of a full table
// scan; the extra created_at >= cursor bound starts each page's index range at
// the cursor rather than at the window start.
const selectProjectPage = selectRiskResultsWithAttribution + `
WHERE r.project_id = $1
  AND r.organization_id = $2
  AND ($3::uuid IS NULL OR r.risk_policy_id = $3)
  AND ($4::text[] IS NULL OR r.source = ANY($4))
  AND ($5::timestamptz IS NULL OR r.created_at >= $5)
  AND ($6::timestamptz IS NULL OR r.created_at < $6)
  AND r.created_at >= $7
  AND (r.created_at, r.id) > ($7::timestamptz, $8::uuid)
  AND r.found IS TRUE
  AND r.rule_id IS NOT NULL
ORDER BY r.created_at, r.id
LIMIT $9
`

// selectOrgProjects lists every project of an organization, deleted ones
// included, since risk_results rows outlive a project's soft delete.
const selectOrgProjects = `SELECT id FROM projects WHERE organization_id = $1 ORDER BY id`

// selectRiskResultsWithAttribution is the SELECT list and attribution joins
// shared by both page queries.
//
// The LEFT JOINs denormalize attribution the same way the live writer's
// lookups do (risk/queries.sql):
//
// Message-anchored rows mirror GetChatMessageAttribution: chat id plus
// resolved user ids, message-level values winning over chat-level ones, the
// message event time, and the chat's most recent live assistant_threads link,
// everything collapsing to ” (and message_created_at to the finding's own
// created_at, matching the ClickHouse column DEFAULT) when the message or chat
// is gone. The message's chat must sit in the finding's own project, the
// live query's chat/project guard. It is checked against the finding's project
// rather than the message's own denormalized project_id, which historical rows
// may lack, so a message pointing at a foreign-project chat is rejected
// outright rather than attributed.
//
// Content-part-anchored rows mirror GetChatContentPartAttribution: the part
// resolves its chat and, parent-message-first, its user ids. The guards are
// the live ones: the part must be live (deleted IS FALSE), its project must
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
const selectRiskResultsWithAttribution = `
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
  AND EXISTS (
    SELECT 1 FROM chats mc
    WHERE mc.id = cm.chat_id
      AND mc.project_id = r.project_id
  )
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
`

// GapFillCursor is the gap-fill resume position: the last committed finding
// of a project, in (created_at, id) order. Projects are visited in id order.
type GapFillCursor struct {
	// ProjectID is the project the committed finding belongs to.
	ProjectID uuid.UUID

	// CreatedAt is the committed finding's risk_results.created_at.
	CreatedAt time.Time

	// ID is the committed finding's risk_results id.
	ID uuid.UUID
}

// IsZero reports whether no position is set.
func (c GapFillCursor) IsZero() bool { return c.ProjectID == uuid.Nil }

// String renders the cursor as the -gap-fill-cursor flag value.
func (c GapFillCursor) String() string {
	return c.ProjectID.String() + "/" + c.CreatedAt.UTC().Format(time.RFC3339Nano) + "/" + c.ID.String()
}

// ParseGapFillCursor parses a value produced by GapFillCursor.String.
func ParseGapFillCursor(value string) (GapFillCursor, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 3 {
		return GapFillCursor{}, errors.New("want <project_id>/<created_at RFC3339>/<risk_results id>")
	}
	projectID, err := uuid.Parse(parts[0])
	if err != nil {
		return GapFillCursor{}, fmt.Errorf("project id: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil {
		return GapFillCursor{}, fmt.Errorf("created_at: %w", err)
	}
	id, err := uuid.Parse(parts[2])
	if err != nil {
		return GapFillCursor{}, fmt.Errorf("risk_results id: %w", err)
	}
	return GapFillCursor{ProjectID: projectID, CreatedAt: createdAt.UTC(), ID: id}, nil
}

const (
	// pageAttempts bounds how often one page is tried after a recovery
	// conflict. A hot standby cancels long reads while replaying WAL; a short
	// page usually succeeds on the next try.
	pageAttempts = 5

	// pageRetryBackoff is the wait before the first retry, growing linearly
	// per attempt so replay has time to move past the conflicting cleanup.
	pageRetryBackoff = 2 * time.Second

	// sqlStateRecoveryConflict is the code a hot standby returns when it
	// cancels a query that conflicts with WAL replay (serialization_failure).
	sqlStateRecoveryConflict = "40001"
)

// Source reads risk_results pages from Postgres and publishes them to the
// pipeline. It tracks the last processed id so an interrupted run can resume.
type Source struct {
	pool *pgxpool.Pool

	scanned  int64
	bySource map[string]int64

	// retryBackoff is the wait before a page retry, scaled by the attempt.
	retryBackoff time.Duration

	// query runs one page query; tests replace it to inject failures.
	query func(ctx context.Context, query string, args ...any) ([]SourceRow, error)
}

// NewSource builds a Postgres source over pool.
func NewSource(pool *pgxpool.Pool) *Source {
	s := &Source{
		pool:         pool,
		scanned:      0,
		bySource:     make(map[string]int64),
		retryBackoff: pageRetryBackoff,
		query:        nil,
	}
	s.query = s.queryPage
	return s
}

// Scanned returns the number of rows read so far.
func (s *Source) Scanned() int64 { return s.scanned }

// ScannedBySource returns Postgres findings read per source.
func (s *Source) ScannedBySource() map[string]int64 {
	out := make(map[string]int64, len(s.bySource))
	maps.Copy(out, s.bySource)
	return out
}

// sourceFilters holds the optional query arguments; a nil value becomes SQL
// NULL and disables its filter.
type sourceFilters struct {
	org     any
	project any
	policy  any
	sources any
	from    any
	to      any
}

// Read implements pipeline.Source. Gap-fill runs page each project over
// (created_at, id); other runs page the whole table by id.
func (s *Source) Read(ctx context.Context, criteria pipeline.Criteria, out chan<- SourceRow) error {
	batchSize, _ := criteria[CriteriaBatchSize].(int)
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	// Exact time bounds via created_at (nil disables the predicate). These are
	// independent of the cursor and always apply when set: the cursor decides
	// where to resume, -from/-to decide window membership. Applying both is safe —
	// rows flow in cursor order, so every in-window pending row sorts after the cursor, and
	// applying created_at >= from cannot drop it while it does exclude an
	// out-of-window row that uuidv7/created_at skew placed past the cursor.
	filters := sourceFilters{org: nil, project: nil, policy: nil, sources: nil, from: nil, to: nil}
	if org, _ := criteria[CriteriaOrgID].(string); org != "" {
		filters.org = org
	}
	if projectID, ok := criteria[CriteriaProjectID].(uuid.UUID); ok {
		filters.project = projectID
	}
	if policyID, ok := criteria[CriteriaPolicyID].(uuid.UUID); ok {
		filters.policy = policyID
	}
	if sources, ok := criteria[CriteriaSources].([]string); ok && len(sources) > 0 {
		filters.sources = sources
	}
	if from, ok := criteria[CriteriaFrom].(time.Time); ok {
		filters.from = from
	}
	if to, ok := criteria[CriteriaTo].(time.Time); ok {
		filters.to = to
	}

	if gapFill, _ := criteria[CriteriaGapFill].(bool); gapFill {
		cursor, _ := criteria[CriteriaGapFillCursor].(GapFillCursor)
		return s.readByProject(ctx, criteria, filters, cursor, batchSize, out)
	}

	// Keyset lower bound / resume point. The cursor only sets the id resume
	// position (id > cursor); it does NOT relax the time window. -from/-to still
	// apply so a resumed scoped run stays inside its window.
	cursor := uuid.Nil
	if c, ok := criteria[CriteriaCursor].(uuid.UUID); ok {
		cursor = c
	}

	for {
		page, err := s.readPage(ctx, selectPage, cursor.String(),
			filters.org, filters.project, filters.policy, filters.sources, filters.from, filters.to, cursor, batchSize)
		if err != nil {
			return err
		}
		if len(page) > 0 {
			cursor = page[len(page)-1].ID
		}
		if err := s.publish(ctx, page, out); err != nil {
			return err
		}

		// This is the read position, not a safe resume point: rows up to here may
		// still be in flight downstream. The resume cursor is the sink's committed
		// id (see Sink.LastCommitted), printed in the final report.
		log.Printf("source: read page=%d total=%d read_through=%s", len(page), s.scanned, cursor)

		// A short page means we reached the end of the window.
		if len(page) < batchSize {
			return nil
		}
	}
}

// readByProject pages each of the organization's projects (or the one
// -project names) over (created_at, id), resuming from cursor.
func (s *Source) readByProject(ctx context.Context, criteria pipeline.Criteria, filters sourceFilters, cursor GapFillCursor, batchSize int, out chan<- SourceRow) error {
	org, _ := criteria[CriteriaOrgID].(string)
	if org == "" {
		return errors.New("gap-fill requires an organization")
	}

	var projects []uuid.UUID
	if projectID, ok := criteria[CriteriaProjectID].(uuid.UUID); ok {
		projects = []uuid.UUID{projectID}
	} else {
		listed, err := s.listProjects(ctx, org)
		if err != nil {
			return err
		}
		projects = listed
	}
	if !cursor.IsZero() {
		i := slices.Index(projects, cursor.ProjectID)
		if i < 0 {
			return fmt.Errorf("gap-fill cursor project %s is not in the organization's project list", cursor.ProjectID)
		}
		projects = projects[i:]
	}

	start := time.Time{}
	if from, ok := filters.from.(time.Time); ok {
		start = from
	}

	for _, projectID := range projects {
		pos := GapFillCursor{ProjectID: projectID, CreatedAt: start, ID: uuid.Nil}
		if !cursor.IsZero() && projectID == cursor.ProjectID {
			pos = cursor
		}

		for {
			page, err := s.readPage(ctx, selectProjectPage, pos.String(),
				projectID, org, filters.policy, filters.sources, filters.from, filters.to, pos.CreatedAt, pos.ID, batchSize)
			if err != nil {
				return err
			}
			if len(page) > 0 {
				last := page[len(page)-1]
				pos = GapFillCursor{ProjectID: projectID, CreatedAt: last.CreatedAt, ID: last.ID}
			}
			if err := s.publish(ctx, page, out); err != nil {
				return err
			}

			// Read position only; the resume point is the sink's committed cursor.
			log.Printf("source: read page=%d total=%d read_through=%s", len(page), s.scanned, pos)

			if len(page) < batchSize {
				break
			}
		}
	}
	return nil
}

func (s *Source) listProjects(ctx context.Context, org string) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, selectOrgProjects, org)
	if err != nil {
		return nil, fmt.Errorf("list organization projects: %w", err)
	}
	projects, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("read organization projects: %w", err)
	}
	return projects, nil
}

// readPage runs one page query to completion, so its snapshot is released
// before any row goes downstream. A recovery conflict re-runs the same page.
func (s *Source) readPage(ctx context.Context, query string, position string, args ...any) ([]SourceRow, error) {
	for attempt := 1; ; attempt++ {
		page, err := s.query(ctx, query, args...)
		if err == nil {
			return page, nil
		}

		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != sqlStateRecoveryConflict || attempt >= pageAttempts {
			return nil, fmt.Errorf("read page after %s: %w", position, err)
		}

		log.Printf("source: recovery conflict on page after %s, retry %d/%d", position, attempt, pageAttempts-1)
		timer := time.NewTimer(time.Duration(attempt) * s.retryBackoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("read interrupted at %s: %w", position, ctx.Err())
		case <-timer.C:
		}
	}
}

func (s *Source) queryPage(ctx context.Context, query string, args ...any) ([]SourceRow, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query page: %w", err)
	}
	defer rows.Close()

	var page []SourceRow
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
			return nil, fmt.Errorf("scan row: %w", err)
		}
		page = append(page, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate page: %w", err)
	}
	return page, nil
}

func (s *Source) publish(ctx context.Context, page []SourceRow, out chan<- SourceRow) error {
	for _, r := range page {
		s.scanned++
		s.bySource[r.Source]++
		select {
		case out <- r:
		case <-ctx.Done():
			return fmt.Errorf("publish interrupted at %s: %w", r.ID, ctx.Err())
		}
	}
	return nil
}
