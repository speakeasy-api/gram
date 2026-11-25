package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
)

type SkillSessionVersion struct {
	ID              uuid.UUID
	CreatedAt       time.Time
	SeenAt          time.Time
	OrganizationID  string
	ProjectID       uuid.UUID
	SessionID       string
	SkillID         uuid.UUID
	SkillVersionID  uuid.UUID
	CanonicalSHA256 string
	Surface         string
}

// InsertSkillSessionVersions writes rows using a server-side async insert
// (async_insert=1). The connection sets wait_for_async_insert: fire-and-forget
// on the server connection, acknowledged on the test connection (see
// InsertTelemetryLogs).
func (q *Queries) InsertSkillSessionVersions(ctx context.Context, rows []SkillSessionVersion) error {
	if len(rows) == 0 {
		return nil
	}

	builder := sq.Insert("skill_session_versions").Columns(
		"id",
		"created_at",
		"seen_at",
		"organization_id",
		"project_id",
		"session_id",
		"skill_id",
		"skill_version_id",
		"canonical_sha256",
		"surface",
	)
	for _, row := range rows {
		builder = builder.Values(
			row.ID,
			row.CreatedAt,
			row.SeenAt,
			row.OrganizationID,
			row.ProjectID,
			row.SessionID,
			row.SkillID,
			row.SkillVersionID,
			row.CanonicalSHA256,
			row.Surface,
		)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("building skill session version insert: %w", err)
	}
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"async_insert": 1,
	}))
	if err := q.conn.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("inserting skill session versions: %w", err)
	}
	return nil
}
