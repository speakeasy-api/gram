package mcpconnectionattempts

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// attemptColumns reduces an attempt's rows to one in ClickHouse, as Summarize does in
// Go, with the same tie-breaks on equal times.
const attemptColumns = `
	organization_id,
	attempt_id,
	min(attempt_started_at) AS started_at,
	toBool(countIf(is_final) > 0) AS has_final,
	toString(argMinIf((step, outcome, reason), (event_time, step, outcome, reason), is_final).1) AS final_step,
	toString(argMinIf((step, outcome, reason), (event_time, step, outcome, reason), is_final).2) AS final_outcome,
	toString(argMinIf((step, outcome, reason), (event_time, step, outcome, reason), is_final).3) AS final_reason,
	minIf(event_time, is_final) AS final_at,
	toString(argMax((step, reason), (event_time, step, reason)).1) AS last_step,
	toString(argMax((step, reason), (event_time, step, reason)).2) AS last_reason,
	max(event_time) AS last_at,
	argMinIf(subject_urn, (event_time, subject_urn), subject_urn != '') AS subject,
	any(app_key) AS app,
	any(server_id) AS server,
	argMinIf(provider_host, (event_time, provider_host), provider_host != '') AS provider,
	toString(if(countIf(client_family NOT IN ('', 'browser')) > 0,
		argMinIf(client_family, (event_time, client_family), client_family NOT IN ('', 'browser')),
		if(countIf(client_family = 'browser') > 0, 'browser', ''))) AS client,
	toUInt64(max(repeats)) + 1 AS weight,
	toBool(countIf(step_result = 'signed_in') > 0) AS signed_in,
	arraySort(arrayFilter(x -> x != '', arrayDistinct(arrayConcat(
		groupUniqArray(server_id), groupUniqArray(member_id), groupUniqArray(auth_client_id),
		groupUniqArray(remote_session_issuer_id), groupUniqArray(user_session_issuer_id),
		groupUniqArray(remote_session_client_id))))) AS touched`

// ReadAttempts summarizes every attempt with a raw row matching where, calling each
// once per attempt. where is a SQL condition on mcp_connection_events and args bind it.
// Callers filter on organization_id or a time range so the read stays bounded.
func ReadAttempts(ctx context.Context, ch driver.Conn, where string, each func(AttemptSummary), args ...any) error {
	rows, err := ch.Query(ctx, `SELECT`+attemptColumns+` FROM `+eventsTable+` WHERE `+where+` GROUP BY organization_id, attempt_id`, args...)
	if err != nil {
		return fmt.Errorf("query attempts: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	for rows.Next() {
		var a AttemptSummary
		if err := rows.ScanStruct(&a); err != nil {
			return fmt.Errorf("scan attempt: %w", err)
		}
		each(a)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read attempt rows: %w", err)
	}
	return nil
}
