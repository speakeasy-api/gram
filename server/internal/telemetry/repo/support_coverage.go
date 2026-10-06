package repo

import (
	"context"
	"fmt"
	"time"
)

// SurfaceEvidenceRow is one hook_source's aggregate evidence in the window.
// hook_source is raw so the caller can report unmapped sources; folding is
// internal/agentsurface's job.
type SurfaceEvidenceRow struct {
	HookSource         string
	Sessions           uint64
	Tokens             int64
	AttributedSessions uint64
	DeviceOnlySessions uint64
	LastSeen           time.Time
}

// SurfaceEvidenceParams scopes an evidence read to one project and window.
type SurfaceEvidenceParams struct {
	GramProjectIDs []string
	From           time.Time
	To             time.Time
}

// ListSurfaceEvidence returns per-hook_source session, token and identity
// evidence from chat_session_summaries.
//
// Sessions bound to a user and sessions bound only to a device hostname are
// counted separately: company-credential sessions emit no user identity, so
// merging them would let a hostname read as identity coverage.
func (q *Queries) ListSurfaceEvidence(ctx context.Context, arg SurfaceEvidenceParams) ([]SurfaceEvidenceRow, error) {
	if len(arg.GramProjectIDs) == 0 {
		return []SurfaceEvidenceRow{}, nil
	}

	// The per-chat merge must precede the per-surface rollup, or a chat
	// spanning two hourly buckets counts twice. Aggregate aliases are prefixed
	// so none shadows the column it derives from — a collision lets ClickHouse
	// fold the subquery into the outer aggregate.
	const query = `
		SELECT
			s_hook_source AS hook_source,
			count() AS sessions,
			sum(s_total_tokens) AS tokens,
			countIf(s_user_email != '') AS attributed_sessions,
			countIf(s_user_email = '' AND s_has_hostname) AS device_only_sessions,
			max(s_end_time) AS last_seen_unix_nano
		FROM (
			SELECT
				chat_id,
				max(session_hook_source) AS s_hook_source,
				max(session_user_email) AS s_user_email,
				arrayExists(x -> x != '', groupUniqArrayArray(hostnames)) AS s_has_hostname,
				sum(total_tokens) AS s_total_tokens,
				max(end_time_unix_nano) AS s_end_time
			FROM chat_session_summaries
			WHERE gram_project_id IN (?)
			  AND time_bucket >= ?
			  AND time_bucket <= ?
			GROUP BY chat_id
		)
		WHERE hook_source != ''
		GROUP BY hook_source`

	rows, err := q.conn.Query(ctx, query, arg.GramProjectIDs, arg.From.UTC(), arg.To.UTC())
	if err != nil {
		return nil, fmt.Errorf("querying surface evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results := make([]SurfaceEvidenceRow, 0)
	for rows.Next() {
		var row SurfaceEvidenceRow
		var lastSeenUnixNano int64
		if err := rows.Scan(&row.HookSource, &row.Sessions, &row.Tokens, &row.AttributedSessions, &row.DeviceOnlySessions, &lastSeenUnixNano); err != nil {
			return nil, fmt.Errorf("scanning surface evidence: %w", err)
		}
		if lastSeenUnixNano > 0 {
			row.LastSeen = time.Unix(0, lastSeenUnixNano).UTC()
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating surface evidence: %w", err)
	}
	return results, nil
}

// SurfaceShadowRow is one (hook_source, shadow server) pair observed in the
// window.
type SurfaceShadowRow struct {
	HookSource         string
	CanonicalServerURL string
	LastSeen           time.Time
}

// ListSurfaceShadowExposure returns which surfaces reached which shadow MCP
// servers in the window, derived from trace_summaries, which already carries
// both the server URL and the hook_source per trace.
//
// "Shadow" means a URL shadow_mcp_inventory_urls knows about, which keeps the
// Gram-hosted exclusion in one place.
//
// Servers are not counted here: two aliases of one surface produce two rows
// for the same server, and only the caller owns the fold that collapses them.
func (q *Queries) ListSurfaceShadowExposure(ctx context.Context, arg SurfaceEvidenceParams) ([]SurfaceShadowRow, error) {
	if len(arg.GramProjectIDs) == 0 {
		return []SurfaceShadowRow{}, nil
	}

	// Mirrors unproxiedMcpServerUsagePerTrace: server URL and hook_source sit
	// on different rows of a trace and must be merged before pairing.
	const query = `
		SELECT
			t_hook_source AS hook_source,
			t_server_url AS server_url,
			max(t_start_time) AS last_seen_unix_nano
		FROM (
			SELECT
				trace_id,
				any(hook_source) AS t_hook_source,
				max(mcp_server_url) AS t_server_url,
				min(start_time_unix_nano) AS t_start_time
			FROM trace_summaries
			WHERE gram_project_id IN (?)
			  AND start_time_unix_nano >= ?
			  AND start_time_unix_nano <= ?
			GROUP BY trace_id
			HAVING t_server_url != '' AND t_hook_source != ''
		)
		WHERE server_url IN (
			SELECT canonical_server_url
			FROM shadow_mcp_inventory_urls
			WHERE gram_project_id IN (?)
		)
		GROUP BY hook_source, server_url`

	rows, err := q.conn.Query(ctx, query,
		arg.GramProjectIDs, arg.From.UTC().UnixNano(), arg.To.UTC().UnixNano(), arg.GramProjectIDs)
	if err != nil {
		return nil, fmt.Errorf("querying surface shadow exposure: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results := make([]SurfaceShadowRow, 0)
	for rows.Next() {
		var hookSource string
		var serverURL string
		var lastSeenUnixNano int64
		if err := rows.Scan(&hookSource, &serverURL, &lastSeenUnixNano); err != nil {
			return nil, fmt.Errorf("scanning surface shadow exposure: %w", err)
		}
		row := SurfaceShadowRow{HookSource: hookSource, CanonicalServerURL: serverURL, LastSeen: time.Time{}}
		if lastSeenUnixNano > 0 {
			row.LastSeen = time.Unix(0, lastSeenUnixNano).UTC()
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating surface shadow exposure: %w", err)
	}
	return results, nil
}

// MapChatHookSources resolves chat ids to their session's hook_source. Blocks
// carry a chat id but no surface, so this is what attributes a policy decision
// to a surface. Chats absent from the result are unattributed.
func (q *Queries) MapChatHookSources(ctx context.Context, projectIDs []string, chatIDs []string) (map[string]string, error) {
	if len(projectIDs) == 0 || len(chatIDs) == 0 {
		return map[string]string{}, nil
	}

	const query = `
		SELECT chat_id, max(session_hook_source) AS hook_source
		FROM chat_session_summaries
		WHERE gram_project_id IN (?) AND chat_id IN (?)
		GROUP BY chat_id
		HAVING hook_source != ''`

	rows, err := q.conn.Query(ctx, query, projectIDs, chatIDs)
	if err != nil {
		return nil, fmt.Errorf("querying chat hook sources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	sources := make(map[string]string, len(chatIDs))
	for rows.Next() {
		var chatID, hookSource string
		if err := rows.Scan(&chatID, &hookSource); err != nil {
			return nil, fmt.Errorf("scanning chat hook sources: %w", err)
		}
		sources[chatID] = hookSource
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating chat hook sources: %w", err)
	}
	return sources, nil
}
