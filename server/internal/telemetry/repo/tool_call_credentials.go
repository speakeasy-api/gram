package repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
)

// ListToolCallCredentialsParams selects the agent tool calls to read the
// upstream credential of.
type ListToolCallCredentialsParams struct {
	// GramProjectID scopes the read to one project.
	GramProjectID string

	// TraceIDs are the traces to read, usually one page of listed tool calls.
	TraceIDs []string

	// TimeStart and TimeEnd bound the scan in Unix nanoseconds, so ClickHouse
	// prunes partitions and the trace_id bloom filter narrows the rest.
	TimeStart int64
	TimeEnd   int64
}

// ToolCallCredential is the auth client an agent's gateway tool call was
// signed in upstream with.
type ToolCallCredential struct {
	TraceID               string `ch:"row_trace_id"`
	AuthClientType        string `ch:"auth_client_type"`
	AuthClientID          string `ch:"auth_client_id"`
	ConnectionOwnerUserID string `ch:"connection_owner_user_id"`
}

// agentAuthClientRowFilter is a tool-call row the gateway wrote for an agent.
// Hook rows are excluded: the hooks service does not strip client-supplied
// auth client attributes, so on a hook row they would be self-reported.
const agentAuthClientRowFilter = "event_source = 'tool_call' AND toString(attributes.gram.authorization.actor.type) = 'agent' AND toString(attributes.gram.auth_client.type) != ''"

// ListToolCallCredentials reads, for each listed trace, the auth client its
// agent tool call was signed in upstream with. Traces recorded before the
// gateway named it, and calls by anyone but an agent, are absent.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListToolCallCredentials(ctx context.Context, arg ListToolCallCredentialsParams) (map[string]ToolCallCredential, error) {
	credentials := make(map[string]ToolCallCredential, len(arg.TraceIDs))
	if len(arg.TraceIDs) == 0 {
		return credentials, nil
	}

	query, args, err := sq.Select(
		"toString(assumeNotNull(trace_id)) AS row_trace_id",
		"argMax(toString(attributes.gram.auth_client.type), time_unix_nano) AS auth_client_type",
		"argMax(toString(attributes.gram.auth_client.id), time_unix_nano) AS auth_client_id",
		"argMax(toString(attributes.gram.auth_client.connection_owner_user_id), time_unix_nano) AS connection_owner_user_id",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where(squirrel.Eq{"trace_id": arg.TraceIDs}).
		Where(agentAuthClientRowFilter).
		GroupBy("row_trace_id").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tool call credentials query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var row ToolCallCredential
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan tool call credential: %w", err)
		}
		credentials[row.TraceID] = row
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return credentials, nil
}
