package productmetrics

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Diversity estimates distinct typed values for a retained attribute key.
type Diversity struct {
	// Namespace identifies resource, scope or point attributes.
	Namespace Namespace

	// Key is the producer-owned attribute key.
	Key string

	// Values is an approximate distinct typed-value count.
	Values uint64
}

// Diagnostics reports bounded analytical estimates, never service metric labels.
type Diagnostics struct {
	// ActiveSeries estimates distinct full series over the selected lookback.
	ActiveSeries uint64

	// NewSeries estimates series first seen after RecentSince within this lookback.
	// It is not a lifetime first-seen count and may change with late arrivals.
	NewSeries uint64

	// Contributions is the duplicate-inclusive delivery count in the range.
	Contributions uint64

	// AttributeDiversity estimates typed-value diversity within each namespace/key.
	AttributeDiversity []Diversity
}

// Diagnostics uses the same tenant/descriptor/window bounds as serving queries.
// Filters and grouping are omitted: diagnostics characterize the full descriptor.
func (r *Repository) Diagnostics(ctx context.Context, q Query, recentSince time.Time) (Diagnostics, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryMaxSeconds*time.Second)
	defer cancel()
	var result Diagnostics
	if _, _, err := buildQuery(q, r.now()); err != nil {
		return result, err
	}
	if len(q.Filters) != 0 || len(q.GroupBy) != 0 || recentSince.Before(q.Start) || !recentSince.Before(q.End) || !recentSince.Equal(recentSince.Truncate(time.Minute)) {
		return result, fmt.Errorf("diagnostics require an unfiltered lookback and a whole-minute recent boundary within it")
	}
	table := "product_metric_sums_1m"
	if q.Definition.Instrument == Histogram {
		table = "product_metric_histograms_1m"
	}
	base := squirrel.Select().From(table).Where(squirrel.Eq{"organization_id": q.Tenant.OrganizationID, "project_id": q.Tenant.ProjectID, "metric_name": q.Definition.Name, "scope_name": q.Definition.ScopeName, "scope_version": q.Definition.ScopeVersion, "unit": q.Definition.Unit}).Where("bucket >= ? AND bucket < ?", q.Start.UTC(), q.End.UTC())
	identity := "tuple(number_kind, resource_attributes, scope_attributes, point_attributes)"
	sql, args, err := base.Columns("uniqCombined64("+identity+") AS active_series", "sum(contributions) AS total_contributions").Column("uniqCombined64If("+identity+", bucket < ?) AS older_series", recentSince.UTC()).ToSql()
	if err != nil {
		return result, fmt.Errorf("build series diagnostics: %w", err)
	}
	var older uint64
	if err := r.conn.QueryRow(boundedQueryContext(ctx), sql, args...).Scan(&result.ActiveSeries, &result.Contributions, &older); err != nil {
		return result, fmt.Errorf("read series diagnostics: %w", err)
	}
	if result.ActiveSeries > older {
		result.NewSeries = result.ActiveSeries - older
	}
	for _, namespace := range []Namespace{Resource, Scope, Point} {
		col, err := (Dimension{Namespace: namespace, Key: "diagnostics"}).column()
		if err != nil {
			return result, err
		}
		sql, args, err := base.Columns("a.key", "uniqCombined64(tuple(a.type, a.value)) AS distinct_values").JoinClause("ARRAY JOIN " + col + " AS a").GroupBy("a.key").OrderBy("a.key").ToSql()
		if err != nil {
			return result, fmt.Errorf("build attribute diagnostics: %w", err)
		}
		rows, err := r.conn.Query(boundedQueryContext(ctx), sql, args...)
		if err != nil {
			return result, fmt.Errorf("read attribute diagnostics: %w", err)
		}
		for rows.Next() {
			d := Diversity{Namespace: namespace, Key: "", Values: 0}
			if err := rows.Scan(&d.Key, &d.Values); err != nil {
				o11y.NoLogDefer(rows.Close)
				return result, fmt.Errorf("scan attribute diagnostics: %w", err)
			}
			result.AttributeDiversity = append(result.AttributeDiversity, d)
			if len(result.AttributeDiversity) > QueryMaxRows {
				o11y.NoLogDefer(rows.Close)
				return result, fmt.Errorf("attribute diagnostics exceed result bound")
			}
		}
		err = rows.Err()
		o11y.NoLogDefer(rows.Close)
		if err != nil {
			return result, fmt.Errorf("iterate attribute diagnostics: %w", err)
		}
	}
	return result, nil
}
