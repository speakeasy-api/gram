package repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
)

const (
	litellmTelemetryURN       = "litellm:otel:traces"
	litellmInstanceIDExpr     = "toString(attributes.gram.litellm.instance_id)"
	litellmModelRequestIDExpr = "multiIf(" +
		"toString(attributes.gram.litellm.call_id) != '', toString(attributes.gram.litellm.call_id), " +
		"toString(attributes.gen_ai.response.id) != '', toString(attributes.gen_ai.response.id), " +
		"toString(id))"
)

type ListLiteLLMTrafficDiagnosticsParams struct {
	ProjectID         string
	InstanceIDs       []string
	ObservedStartNano int64
	ObservedEndNano   int64
}

type LiteLLMTrafficDiagnosticsRow struct {
	InstanceID                  string `ch:"instance_id"`
	TotalRequests               uint64 `ch:"total_requests"`
	RequestsWithVirtualKeyEmail uint64 `ch:"requests_with_virtual_key_email"`
	RequestsWithPlatformUser    uint64 `ch:"requests_with_platform_user"`
}

func (q *Queries) ListLiteLLMTrafficDiagnostics(ctx context.Context, arg ListLiteLLMTrafficDiagnosticsParams) ([]LiteLLMTrafficDiagnosticsRow, error) {
	if len(arg.InstanceIDs) == 0 {
		return []LiteLLMTrafficDiagnosticsRow{}, nil
	}

	sb := sq.Select(
		litellmInstanceIDExpr+" AS instance_id",
		"uniqExact("+litellmModelRequestIDExpr+") AS total_requests",
		"uniqExactIf("+litellmModelRequestIDExpr+", toString(attributes.gram.litellm.user_email) != '') AS requests_with_virtual_key_email",
		"uniqExactIf("+litellmModelRequestIDExpr+", user_id != '') AS requests_with_platform_user",
	).
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": arg.ProjectID}).
		Where(squirrel.Eq{litellmInstanceIDExpr: arg.InstanceIDs}).
		Where(squirrel.GtOrEq{"observed_time_unix_nano": arg.ObservedStartNano}).
		Where(squirrel.Lt{"observed_time_unix_nano": arg.ObservedEndNano}).
		Where(squirrel.GtOrEq{"time_unix_nano": arg.ObservedStartNano}).
		Where(squirrel.Lt{"time_unix_nano": arg.ObservedEndNano}).
		Where(squirrel.Eq{"gram_urn": litellmTelemetryURN}).
		Where(squirrel.Eq{"event_urn": []string{
			"urn:telemetry:provider_otel:span:chat",
			"urn:telemetry:provider_otel:span:embeddings",
			"urn:telemetry:provider_otel:span:text_completion",
		}}).
		GroupBy("instance_id")
	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build LiteLLM traffic diagnostics query: %w", err)
	}
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query LiteLLM traffic diagnostics: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]LiteLLMTrafficDiagnosticsRow, 0)
	for rows.Next() {
		var row LiteLLMTrafficDiagnosticsRow
		if err := rows.Scan(&row.InstanceID, &row.TotalRequests, &row.RequestsWithVirtualKeyEmail, &row.RequestsWithPlatformUser); err != nil {
			return nil, fmt.Errorf("scan LiteLLM traffic diagnostics: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate LiteLLM traffic diagnostics: %w", err)
	}
	return result, nil
}
