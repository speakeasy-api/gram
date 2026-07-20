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
	ID                       uuid.UUID `ch:"id"`
	CreatedAt                time.Time `ch:"created_at"`
	OrganizationID           string    `ch:"organization_id"`
	ProjectID                string    `ch:"project_id"`
	RequestID                string    `ch:"request_id"`
	ChatMessageID            string    `ch:"chat_message_id"`
	RiskPolicyID             string    `ch:"risk_policy_id"`
	RiskPolicyVersion        int64     `ch:"risk_policy_version"`
	RuleID                   string    `ch:"rule_id"`
	Description              string    `ch:"description"`
	Source                   string    `ch:"source"`
	Confidence               float64   `ch:"confidence"`
	Tags                     []string  `ch:"tags"`
	StartPos                 int32     `ch:"start_pos"`
	EndPos                   int32     `ch:"end_pos"`
	DeadLetterReason         string    `ch:"dead_letter_reason"`
	MatchLen                 uint32    `ch:"match_len"`
	MatchRedacted            string    `ch:"match_redacted"`
	FingerprintPepperVersion string    `ch:"fingerprint_pepper_version"`
	FingerprintGlobalHS256   string    `ch:"fingerprint_global_hs256"`
	FingerprintTenantHS256   string    `ch:"fingerprint_tenant_hs256"`

	// Exclusion annotation: set when a going-forward exclusion suppressed the
	// finding. Both nil when the finding is not excluded (maps to the Nullable
	// ClickHouse columns).
	ExcludedAt  *time.Time `ch:"excluded_at"`
	ExclusionID *uuid.UUID `ch:"exclusion_id"`
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

	ctx = clickhouse.Context(ctx,
		clickhouse.WithAsync(false),
		clickhouse.WithSettings(clickhouse.Settings{
			"async_insert":          1,
			"wait_for_async_insert": 0,
		}),
	)

	builder := sq.Insert("risk_findings").
		Columns(
			"id",
			"created_at",
			"organization_id",
			"project_id",
			"request_id",
			"chat_message_id",
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
			"match_len",
			"match_redacted",
			"fingerprint_pepper_version",
			"fingerprint_global_hs256",
			"fingerprint_tenant_hs256",
			"excluded_at",
			"exclusion_id",
		)

	for _, row := range rows {
		builder = builder.Values(
			row.ID,
			row.CreatedAt,
			row.OrganizationID,
			row.ProjectID,
			row.RequestID,
			row.ChatMessageID,
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
