// Package chrepo writes immutable workload readings to ClickHouse.
package chrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
)

var sq = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

// ReadingRow is one row destined for billing_meter_readings.
type ReadingRow struct {
	// ID is the deterministic reading UUID.
	ID uuid.UUID `ch:"id"`

	// OrganizationID identifies the owning organization.
	OrganizationID string `ch:"organization_id"`

	// ProjectID identifies the owning project.
	ProjectID uuid.UUID `ch:"project_id"`

	// MeterID identifies the workload meter.
	MeterID string `ch:"meter_id"`

	// OperationID identifies the domain operation within the meter.
	OperationID string `ch:"operation_id"`

	// Unit is the workload measurement unit.
	Unit string `ch:"unit"`

	// Value is the signed workload value.
	Value int64 `ch:"value"`

	// OccurredAt is the UTC work-execution time.
	OccurredAt time.Time `ch:"occurred_at"`

	// ProducedAt is the UTC source-production time used as the replacement version.
	ProducedAt time.Time `ch:"produced_at"`

	// InsertedAt is the UTC ClickHouse ingestion time used for delivery diagnostics.
	InsertedAt time.Time `ch:"inserted_at"`

	// CorrectsReadingID identifies the original reading for a compensation.
	CorrectsReadingID *uuid.UUID `ch:"corrects_reading_id"`

	// Attributes contains reading dimensions such as the tokenizer codec.
	Attributes map[string]string `ch:"attributes"`
}

// Queries writes billing meter readings to ClickHouse.
type Queries struct {
	conn clickhouse.Conn
}

// New creates a meter reading repository backed by conn.
func New(conn clickhouse.Conn) *Queries {
	return &Queries{conn: conn}
}

// InsertReadings synchronously inserts a nonempty batch in one statement.
func (q *Queries) InsertReadings(ctx context.Context, rows []ReadingRow) error {
	if len(rows) == 0 {
		return nil
	}

	builder := sq.Insert("billing_meter_readings").Columns(
		"id",
		"organization_id",
		"project_id",
		"meter_id",
		"operation_id",
		"unit",
		"value",
		"occurred_at",
		"produced_at",
		"inserted_at",
		"corrects_reading_id",
		"attributes",
	)
	for _, row := range rows {
		var correctsReadingID any
		if row.CorrectsReadingID != nil {
			correctsReadingID = *row.CorrectsReadingID
		}
		builder = builder.Values(
			row.ID,
			row.OrganizationID,
			row.ProjectID,
			row.MeterID,
			row.OperationID,
			row.Unit,
			row.Value,
			row.OccurredAt.Format("2006-01-02 15:04:05.999999999"),
			row.ProducedAt.Format("2006-01-02 15:04:05.999999999"),
			row.InsertedAt.Format("2006-01-02 15:04:05.999999999"),
			correctsReadingID,
			row.Attributes,
		)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("build billing_meter_readings insert query: %w", err)
	}

	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"async_insert": 0}))
	if err := q.conn.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert billing_meter_readings: %w", err)
	}
	return nil
}
