package chrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
)

// sq is the squirrel statement builder pre-configured for ClickHouse (uses ?
// placeholders).
var sq = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

// RiskFindingRow is a single row destined for the risk_findings table. The raw
// matched value is never carried here: only its length, a redacted display
// string, and one-way fingerprints. See internal/risk/finding_ch.go for how it
// is populated and internal/risk/fingerprint.go for the fingerprint scheme.
type RiskFindingRow struct {
	ID                uuid.UUID `ch:"id"`
	CreatedAt         time.Time `ch:"created_at"`
	OrganizationID    string    `ch:"organization_id"`
	ProjectID         string    `ch:"project_id"`
	RequestID         string    `ch:"request_id"`
	ChatMessageID     string    `ch:"chat_message_id"`
	ContentPartID     string    `ch:"content_part_id"`
	RiskPolicyID      string    `ch:"risk_policy_id"`
	RiskPolicyVersion int64     `ch:"risk_policy_version"`
	RuleID            string    `ch:"rule_id"`
	Description       string    `ch:"description"`
	Source            string    `ch:"source"`
	Confidence        float64   `ch:"confidence"`
	Tags              []string  `ch:"tags"`
	StartPos          int32     `ch:"start_pos"`
	EndPos            int32     `ch:"end_pos"`
	DeadLetterReason  string    `ch:"dead_letter_reason"`

	// Denormalized attribution, resolved from Postgres at ingest so
	// session-level and per-user rollups never need a cross-store join. All
	// empty when unresolved (missing message, deleted chat, lookup failure).
	ChatID         string `ch:"chat_id"`
	UserID         string `ch:"user_id"`
	ExternalUserID string `ch:"external_user_id"`

	// MessageCreatedAt is the scanned chat message's event time, the sort and
	// cursor key for the Risk Events listing. Falls back to CreatedAt (scan
	// time) when attribution is unresolved, matching the column's DEFAULT for
	// pre-column rows. AssistantID is the chat's live assistant link at ingest,
	// empty when the chat has none.
	MessageCreatedAt time.Time `ch:"message_created_at"`
	AssistantID      string    `ch:"assistant_id"`

	// ChatSource is the canonical product surface (codex, cursor, claude-code,
	// ...) of the scanned message; Team is the resolved user's WorkOS directory
	// department; UserEmail is the resolved internal user's email. All resolved
	// from Postgres at ingest alongside the ids above (so Watchdog reads never
	// need a Postgres lookup) and empty when unresolved.
	ChatSource string `ch:"chat_source"`
	Team       string `ch:"team"`
	UserEmail  string `ch:"user_email"`

	// Category is the canonical risk category for (source, rule_id), computed
	// via internal/risk/categories at ingest. Empty for dead-letter sentinels.
	Category string `ch:"category"`

	MatchLen                 uint32 `ch:"match_len"`
	MatchRedacted            string `ch:"match_redacted"`
	FingerprintPepperVersion string `ch:"fingerprint_pepper_version"`
	FingerprintGlobalHS256   string `ch:"fingerprint_global_hs256"`
	FingerprintTenantHS256   string `ch:"fingerprint_tenant_hs256"`

	// Exclusion annotation: set when a going-forward exclusion suppressed the
	// finding. Both nil when the finding is not excluded (maps to the Nullable
	// ClickHouse columns).
	ExcludedAt  *time.Time `ch:"excluded_at"`
	ExclusionID *uuid.UUID `ch:"exclusion_id"`

	// FalsePositiveAt is set when a reviewer manually dismissed this finding
	// (risk.markResultsFalsePositive), independent of ExcludedAt. Nil for a
	// freshly-scanned finding or after risk.unmarkResultsFalsePositive.
	FalsePositiveAt *time.Time `ch:"false_positive_at"`

	// Reveal metadata: which text StartPos/EndPos index (Surface), the scanner
	// field and gjson path the span matched, and the recorded tool call id
	// anchoring the finding. All empty when unknown; see the risk_findings
	// column comments for the value sets.
	Surface    string `ch:"surface"`
	Field      string `ch:"field"`
	Path       string `ch:"path"`
	ToolCallID string `ch:"tool_call_id"`
}

// chNullable maps a nil pointer to an untyped nil interface so a Nullable
// ClickHouse column binds as NULL. Passing a typed-nil pointer straight through
// would reach the driver as a non-nil interface, and for types implementing
// driver.Valuer (e.g. uuid.UUID) the driver would call Value() on the nil
// pointer and panic. A non-nil pointer binds as its dereferenced value.
func chNullable[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// InsertRiskFindings writes findings using a server-side async insert
// (async_insert=1, wait_for_async_insert=0). The call is fire-and-forget from
// CH's perspective: it acks once the rows are queued in CH's async insert
// buffer, not once they are committed to disk.
func (q *Queries) InsertRiskFindings(ctx context.Context, rows []RiskFindingRow) error {
	if len(rows) == 0 {
		return nil
	}

	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"async_insert":          1,
		"wait_for_async_insert": 0,
	}))

	builder := sq.Insert("risk_findings").
		Columns(
			"id",
			"created_at",
			"inserted_at",
			"organization_id",
			"project_id",
			"request_id",
			"chat_message_id",
			"content_part_id",
			"risk_policy_id",
			"risk_policy_version",
			"rule_id",
			"description",
			"source",
			"confidence",
			"tags",
			"start_pos",
			"end_pos",
			"dead_letter_reason",
			"chat_id",
			"user_id",
			"external_user_id",
			"category",
			"match_len",
			"match_redacted",
			"fingerprint_pepper_version",
			"fingerprint_global_hs256",
			"fingerprint_tenant_hs256",
			"excluded_at",
			"exclusion_id",
			"false_positive_at",
			"message_created_at",
			"assistant_id",
			"chat_source",
			"team",
			"user_email",
			"surface",
			"field",
			"path",
			"tool_call_id",
		)

	// inserted_at must be strictly increasing within this batch, not just
	// "now" — the read-side dedup (ROW_NUMBER() OVER (PARTITION BY id ORDER BY
	// inserted_at DESC), see overview.go) relies on it to pick the latest
	// state for an id. Two things independently break that if inserted_at is
	// left off this column list and allowed to fall back to the table's
	// DEFAULT now64(9): (1) ClickHouse evaluates a DEFAULT once per INSERT
	// statement, so every row in one multi-row batch gets the identical
	// timestamp, and (2) under async_insert, the server can coalesce several
	// distinct client Exec calls that arrive close together into one physical
	// flush, evaluating DEFAULT once for the whole coalesced write — so even
	// splitting a batch into separate statements doesn't guarantee distinct
	// timestamps. A finding can appear twice within that flush window (a
	// rapid mark/unmark, or a redelivered Pub/Sub backlog), so an identical
	// inserted_at would make the dedup's choice between them
	// non-deterministic. The fix is to bind an explicit, strictly-increasing
	// value per row — but bind it as a formatted string, not a native
	// time.Time: the clickhouse-go driver truncates a time.Time bound through
	// this Exec(ctx, query, args...) path to whole-second precision (verified
	// empirically), silently discarding the very sub-second ordering this
	// exists to provide.
	insertedAt := time.Now().UTC()
	for i, row := range rows {
		builder = builder.Values(
			row.ID,
			row.CreatedAt,
			insertedAt.Add(time.Duration(i)*time.Nanosecond).Format("2006-01-02 15:04:05.999999999"),
			row.OrganizationID,
			row.ProjectID,
			row.RequestID,
			row.ChatMessageID,
			row.ContentPartID,
			row.RiskPolicyID,
			row.RiskPolicyVersion,
			row.RuleID,
			row.Description,
			row.Source,
			row.Confidence,
			row.Tags,
			row.StartPos,
			row.EndPos,
			row.DeadLetterReason,
			row.ChatID,
			row.UserID,
			row.ExternalUserID,
			row.Category,
			row.MatchLen,
			row.MatchRedacted,
			row.FingerprintPepperVersion,
			row.FingerprintGlobalHS256,
			row.FingerprintTenantHS256,
			// Bind the Nullable columns as untyped nil when absent. A typed-nil
			// pointer (e.g. (*uuid.UUID)(nil)) reaches the driver as a non-nil
			// interface, and the driver calls Value() on it — which panics for
			// uuid.UUID. chNullable collapses a nil pointer to a nil interface so
			// the column binds as NULL.
			chNullable(row.ExcludedAt),
			chNullable(row.ExclusionID),
			chNullable(row.FalsePositiveAt),
			row.MessageCreatedAt,
			row.AssistantID,
			row.ChatSource,
			row.Team,
			row.UserEmail,
			row.Surface,
			row.Field,
			row.Path,
			row.ToolCallID,
		)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("build risk_findings insert query: %w", err)
	}

	if err := q.conn.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert risk_findings: %w", err)
	}

	return nil
}
