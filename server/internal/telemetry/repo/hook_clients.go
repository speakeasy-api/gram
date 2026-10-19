package repo

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// HookClientSessionRow is one (organization, day, client) cell of the hook
// client grid. Client is "curl" for the legacy curl hooks, a binary version
// for hooks binary rows that carry one, "binary" for older binary rows, and
// "" for the organization-day total.
type HookClientSessionRow struct {
	OrganizationID string
	Day            string
	Client         string
	// Machines and Sessions count only sessions that started on Day.
	Machines uint64
	Sessions uint64
	// MachinesAll counts every machine that sent a hook on Day, including
	// sessions started earlier.
	MachinesAll uint64
}

// The session grid counts a session on the UTC day of its first hook. The
// lookback is a week longer than the window so a session started before the
// window is not mistaken for one that started on the window's first day.
const hookClientSessionsQuery = `
WITH hooks AS (
  SELECT toString(attributes.gram.org.id) AS org_id,
         toString(attributes.gen_ai.conversation.id) AS sid,
         coalesce(nullIf(toString(attributes.gram.device_id), ''), nullIf(toString(attributes.gram.hook.hostname), ''), user_email) AS machine,
         multiIf(body LIKE 'Tool: %, Hook: %', 'curl',
                 ifNull(toString(attributes.gram.hook.device.binary_version), '') != '', toString(attributes.gram.hook.device.binary_version),
                 'binary') AS client,
         toDate(observed_timestamp) AS day
  FROM telemetry_logs
  WHERE time_unix_nano > toUnixTimestamp64Nano(now64(9) - toIntervalDay(? + 7))
    AND (body LIKE 'Tool: %, Hook: %' OR body LIKE 'Hook: %')
    AND toString(attributes.gen_ai.conversation.id) != ''
    AND toString(attributes.gram.org.id) NOT IN ('', 'org_gram_demo_workspace')
),
sessions AS (SELECT sid, min(day) AS start_day FROM hooks GROUP BY sid)
SELECT h.org_id AS org_id, toString(h.day) AS day, h.client AS client,
       uniqExactIf(h.machine, h.day = s.start_day) AS machines,
       uniqExactIf(h.sid, h.day = s.start_day) AS sessions,
       uniqExact(h.machine) AS machines_all
FROM hooks h INNER JOIN sessions s ON h.sid = s.sid
WHERE h.day > today() - ?
GROUP BY GROUPING SETS ((org_id, day, client), (org_id, day))
ORDER BY org_id, day, client`

// ListHookClientSessions reads the hook client grid across every organization
// for the last windowDays days. Staff-only: it is not scoped to an
// organization and must not be reached from a customer-facing surface.
func (q *Queries) ListHookClientSessions(ctx context.Context, windowDays int) ([]HookClientSessionRow, error) {
	rows, err := q.conn.Query(ctx, hookClientSessionsQuery, windowDays, windowDays)
	if err != nil {
		return nil, fmt.Errorf("querying hook client sessions: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	out := []HookClientSessionRow{}
	for rows.Next() {
		var row HookClientSessionRow
		if err := rows.Scan(&row.OrganizationID, &row.Day, &row.Client, &row.Machines, &row.Sessions, &row.MachinesAll); err != nil {
			return nil, fmt.Errorf("scanning hook client session row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating hook client session rows: %w", err)
	}
	return out, nil
}

// InferenceHookTrafficRow is one (organization, day, surface) cell of the
// inference hook grid. Surface is the Claude app that sent the transcript, or
// "" for the organization-day total.
type InferenceHookTrafficRow struct {
	OrganizationID string
	Day            string
	Surface        string
	Users          uint64
	Events         uint64
	Sessions       uint64
}

// Inference hooks write one session.observed transcript row per stored
// message. Compliance API imports (claude_chat_ session ids) are left out
// because they arrive in bulk, not through the hooks.
const inferenceHookTrafficQuery = `
SELECT organization_id AS org_id,
       toString(toDate(fromUnixTimestamp64Nano(occurred_at_unix_nano))) AS day,
       surface,
       uniqExact(user_email) AS users,
       count() AS events,
       uniqExact(session_id) AS sessions
FROM agent_events
WHERE occurred_at_unix_nano > toUnixTimestamp64Nano(now64(9) - toIntervalDay(?))
  AND raw_event_name = 'session.observed' AND source = 'transcript' AND provider = 'anthropic'
  AND NOT startsWith(session_id, 'claude_chat_')
  AND organization_id NOT IN ('', 'org_gram_demo_workspace')
GROUP BY GROUPING SETS ((org_id, day, surface), (org_id, day))
ORDER BY org_id, day, surface`

// ListInferenceHookTraffic reads the inference hook grid across every
// organization for the last windowDays days. Staff-only, like
// ListHookClientSessions.
func (q *Queries) ListInferenceHookTraffic(ctx context.Context, windowDays int) ([]InferenceHookTrafficRow, error) {
	rows, err := q.conn.Query(ctx, inferenceHookTrafficQuery, windowDays)
	if err != nil {
		return nil, fmt.Errorf("querying inference hook traffic: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	out := []InferenceHookTrafficRow{}
	for rows.Next() {
		var row InferenceHookTrafficRow
		if err := rows.Scan(&row.OrganizationID, &row.Day, &row.Surface, &row.Users, &row.Events, &row.Sessions); err != nil {
			return nil, fmt.Errorf("scanning inference hook traffic row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating inference hook traffic rows: %w", err)
	}
	return out, nil
}
