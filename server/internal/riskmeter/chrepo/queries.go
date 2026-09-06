// Package chrepo persists raw risk evaluation measurements to ClickHouse.
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

// EvaluationRow is one validated risk_evaluations row.
type EvaluationRow struct {
	// ID identifies this physical attempt and is unchanged on redelivery.
	ID uuid.UUID
	// EvaluationID identifies the logical evaluation across retries.
	EvaluationID uuid.UUID
	// OrganizationID identifies the tenant that requested the evaluation.
	OrganizationID string
	// ProjectID identifies the evaluated project's tenant boundary.
	ProjectID uuid.UUID
	// OperationID anchors the evaluation independently of batch grouping.
	OperationID string
	// Detector identifies the independently measured scanner class.
	Detector string
	// ExecutionMode distinguishes realtime, batch, and shadow execution.
	ExecutionMode string
	// Outcome records whether the attempt completed, failed, cancelled, or skipped.
	Outcome string
	// RecordKind separates scan volume from physical inference accounting.
	RecordKind string
	// OccurredAt is the attempt's start time.
	OccurredAt time.Time
	// ProducedAt is when the terminal measurement was published.
	ProducedAt time.Time
	// InsertedAt is when this delivery reached the consumer.
	InsertedAt time.Time
	// STokens is canonical scan volume; nil means unknown rather than zero.
	STokens *int64
	// MeasurementMethod versions the canonical tokenizer contract.
	MeasurementMethod string
	// Model identifies the provider model used by an inference attempt.
	Model string
	// ProviderRequestID identifies the physical request when supplied by the provider.
	ProviderRequestID string
	// PromptTokens retains provider-reported input usage; nil means unavailable.
	PromptTokens *int64
	// CompletionTokens retains provider-reported output usage; nil means unavailable.
	CompletionTokens *int64
	// CostUSD retains provider-reported cost; nil must not become a known zero.
	CostUSD *float64
	// MeasurementError distinguishes failed tokenization from unmeasured work.
	MeasurementError bool
	// PolicyID identifies the policy whose evaluation caused this work.
	PolicyID string
	// PolicyVersion distinguishes changes to the evaluated policy.
	PolicyVersion int64
}

// Queries writes risk evaluation measurements.
type Queries struct {
	conn clickhouse.Conn
}

// New creates a risk evaluation repository backed by conn.
func New(conn clickhouse.Conn) *Queries {
	return &Queries{conn: conn}
}

// InsertRiskEvaluations synchronously inserts a nonempty batch.
func (q *Queries) InsertRiskEvaluations(ctx context.Context, rows []EvaluationRow) error {
	if len(rows) == 0 {
		return nil
	}
	builder := sq.Insert("risk_evaluations").Columns(
		"id", "evaluation_id", "organization_id", "project_id", "operation_id",
		"detector", "execution_mode", "outcome", "record_kind", "occurred_at",
		"produced_at", "inserted_at", "stokens", "measurement_method", "model",
		"provider_request_id", "prompt_tokens", "completion_tokens", "cost_usd",
		"measurement_error", "policy_id", "policy_version",
	)
	for _, row := range rows {
		var stokens, promptTokens, completionTokens, costUSD any
		if row.STokens != nil {
			stokens = *row.STokens
		}
		if row.PromptTokens != nil {
			promptTokens = *row.PromptTokens
		}
		if row.CompletionTokens != nil {
			completionTokens = *row.CompletionTokens
		}
		if row.CostUSD != nil {
			costUSD = *row.CostUSD
		}
		builder = builder.Values(
			row.ID, row.EvaluationID, row.OrganizationID, row.ProjectID, row.OperationID,
			row.Detector, row.ExecutionMode, row.Outcome, row.RecordKind,
			row.OccurredAt.Format("2006-01-02 15:04:05.999999999"),
			row.ProducedAt.Format("2006-01-02 15:04:05.999999999"),
			row.InsertedAt.Format("2006-01-02 15:04:05.999999999"),
			stokens, row.MeasurementMethod, row.Model, row.ProviderRequestID,
			promptTokens, completionTokens, costUSD, row.MeasurementError,
			row.PolicyID, row.PolicyVersion,
		)
	}
	query, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("build risk_evaluations insert query: %w", err)
	}
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"async_insert": 0}))
	if err := q.conn.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert risk_evaluations: %w", err)
	}
	return nil
}
