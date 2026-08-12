package repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
)

// ProjectTokenBucket is one project's tokens-under-management total over a
// window. Used by the internal pricing tracker to roll observed volume up to
// the owning organization before quoting a PAYG rate.
type ProjectTokenBucket struct {
	ProjectID string `ch:"project_id"`
	Tokens    int64  `ch:"tokens"`
}

// GetTumTokensByProject sums the tokens-under-management measure per project
// over the window, scoped identically to the billed totals (measure derived
// from billing.TumComponents via tumMeasureExpr; population scoped by
// ExcludedHookSources). Unlike GetTumWindowTotal it groups by project so a
// caller aggregating several projects into one organization needs only a
// single ClickHouse round trip. Projects with no usage are omitted.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetTumTokensByProject(ctx context.Context, arg GetTokensUnderManagementParams) ([]ProjectTokenBucket, error) {
	if len(arg.ProjectIDs) == 0 {
		return nil, nil
	}

	sb := tumObservedBase(sq.Select(
		"toString(gram_project_id) AS project_id",
		tumMeasureExpr+" AS tokens",
	), arg).
		GroupBy("project_id")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tum tokens by project query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []ProjectTokenBucket
	for rows.Next() {
		var bucket ProjectTokenBucket
		if err := rows.ScanStruct(&bucket); err != nil {
			return nil, fmt.Errorf("scanning tum tokens by project row: %w", err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return buckets, nil
}

// InferenceSpendByProjectParams scopes an inference-spend rollup to a set of
// projects, a time window, and the Gram-hosted completion surfaces.
type InferenceSpendByProjectParams struct {
	ProjectIDs    []string
	StartUnixNano int64
	EndUnixNano   int64
	// IncludedHookSources restricts the sum to Gram-server-run completion
	// surfaces (billing.GramHostedInferenceSources) — playground, elements,
	// risk-analysis, assistants, and so on. Inference spend is the dollars Gram
	// itself pays upstream serving or reacting to a customer, the mirror image
	// of the tokens-under-management exclusion list. Empty means no source
	// filter (all rows counted), which callers should avoid for this measure.
	IncludedHookSources []string
}

// ProjectCostBucket is one project's Gram-hosted inference spend, in US
// dollars, over a window.
type ProjectCostBucket struct {
	ProjectID string  `ch:"project_id"`
	Cost      float64 `ch:"total_cost"`
}

// GetInferenceSpendByProject sums the per-completion cost
// (gen_ai.usage.cost) that Gram-server-run completions recorded, per project,
// over the window. It reads raw telemetry_logs rather than
// attribute_metrics_summaries because that aggregate admits only observed
// agent traffic by construction — the Gram-hosted completion surfaces this
// measure sums never reach it. The read is scoped by gram_project_id (the
// table's primary sort key) and the time partition, so it prunes efficiently.
// Projects with no spend are omitted.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetInferenceSpendByProject(ctx context.Context, arg InferenceSpendByProjectParams) ([]ProjectCostBucket, error) {
	if len(arg.ProjectIDs) == 0 {
		return nil, nil
	}

	sb := sq.Select(
		"toString(gram_project_id) AS project_id",
		"sumIf(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != '') AS total_cost",
	).
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": arg.ProjectIDs}).
		Where("time_unix_nano >= ?", arg.StartUnixNano).
		Where("time_unix_nano < ?", arg.EndUnixNano)

	if len(arg.IncludedHookSources) > 0 {
		sb = sb.Where(squirrel.Eq{"hook_source": arg.IncludedHookSources})
	}

	sb = sb.GroupBy("project_id")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building inference spend by project query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []ProjectCostBucket
	for rows.Next() {
		var bucket ProjectCostBucket
		if err := rows.ScanStruct(&bucket); err != nil {
			return nil, fmt.Errorf("scanning inference spend by project row: %w", err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return buckets, nil
}
