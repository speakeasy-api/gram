package analytics

import (
	"context"
	"fmt"
	"time"

	signalquery "github.com/speakeasy-api/gram/server/internal/telemetry/analytics/signals"
)

func (e *Engine) signalValues(ctx context.Context, tenant Tenant, request ValuesRequest) ([]DimensionValue, error) {
	if request.SignalScope == nil {
		return nil, newError(ErrUnsatisfiable, signalquery.Dataset, "signal_scope", "", "name a sensor and evaluation scope")
	}
	limit := request.Limit
	if limit == 0 {
		limit = DefaultValuesLimit
	}
	scopeContext, scope, err := signalquery.NormalizeScope(signalquery.Context{OrganizationID: tenant.OrganizationID, ProjectID: tenant.ProjectID, From: time.Unix(0, request.FromUnixNano), To: time.Unix(0, request.ToUnixNano), Now: time.Now().UTC(), Step: "auto", RequestedAt: nil}, *request.SignalScope)
	if err != nil {
		return nil, signalFailure(err)
	}
	if scope.Trigger == "manual" {
		scopeContext, err = e.signalManualContext(ctx, scopeContext, scope.RunID)
		if err != nil {
			return nil, err
		}
	}
	query, err := signalquery.CompileValues(scopeContext, scope, request.Dimension, request.Attribute, limit)
	if err != nil {
		return nil, signalFailure(err)
	}
	rows, err := runSignalPlan(ctx, e.ch, query)
	if err != nil {
		return nil, err
	}
	out := make([]DimensionValue, 0, len(rows))
	for _, row := range rows {
		value, valueOK := row["value"].(string)
		count, countOK := row["n"].(int64)
		if !valueOK || !countOK {
			return nil, fmt.Errorf("signal dimension value has invalid shape")
		}
		present := row["present"] == int64(1) || row["present"] == true
		out = append(out, DimensionValue{Value: value, Count: count, Present: new(present)})
	}
	return out, nil
}
