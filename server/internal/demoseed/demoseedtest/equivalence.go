package demoseedtest

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// ClickHouseDemoBreakdown counts the tenant's rows per kind in the tables the
// dashboards key on: telemetry by gram_urn and by chat, agent events by
// event name, and risk findings by chat and rule. It lets two seed
// implementations be compared beyond raw table counts.
func ClickHouseDemoBreakdown(ctx context.Context, ch driver.Conn, orgID, projectID string) (map[string]uint64, error) {
	type breakdown struct {
		name, query, arg string
	}
	breakdowns := []breakdown{
		{"telemetry_logs", `SELECT gram_urn, count() FROM telemetry_logs WHERE gram_project_id = toUUID(?) GROUP BY gram_urn`, projectID},
		{"telemetry_chats", `SELECT toString(gram_chat_id), count() FROM telemetry_logs WHERE gram_project_id = toUUID(?) GROUP BY gram_chat_id`, projectID},
		{"agent_events", `SELECT raw_event_name, count() FROM agent_events WHERE organization_id = ? GROUP BY raw_event_name`, orgID},
		{"risk_findings", `SELECT concat(chat_id, ' ', rule_id), count() FROM risk_findings WHERE organization_id = ? GROUP BY chat_id, rule_id`, orgID},
	}
	out := map[string]uint64{}
	for _, b := range breakdowns {
		rows, err := ch.Query(ctx, b.query, b.arg)
		if err != nil {
			return nil, fmt.Errorf("breakdown %s: %w", b.name, err)
		}
		for rows.Next() {
			var key string
			var n uint64
			if err := rows.Scan(&key, &n); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan breakdown %s: %w", b.name, err)
			}
			out[b.name+"/"+key] = n
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("close breakdown %s: %w", b.name, err)
		}
	}
	return out, nil
}
