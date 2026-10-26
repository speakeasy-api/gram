package demoseed

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
)

const (
	// tunnelRequestMinutes is the per-minute request history: just under a
	// day, ending tunnelHistoryGapMinutes ago so the latest intervals stay gaps.
	tunnelRequestMinutes = 1420

	// tunnelConnectionSamples is the 15-second connection history covering
	// the same window.
	tunnelConnectionSamples = 5680

	// tunnelHistoryGapMinutes keeps the newest history 20 minutes old: the
	// seeded tunnel has no connected agent, so recent activity would be false.
	tunnelHistoryGapMinutes = 20
)

// chTunnelMetrics writes payload-free metric snapshots for the Private
// inventory tunnel seeded in seed_04_servers.go. Request rows split between
// tool calls and listings and between two client families. Connection rows
// vary agents, client sessions and in-flight requests, with one reconnect.
// The project-scoped purge in chPurge removes the previous generation.
func (s *seeder) chTunnelMetrics(ctx context.Context, ch driver.Conn) error {
	args := []any{
		clickhouse.Named("proj", s.projectID().String()),
		clickhouse.Named("source", s.tunnelSourceID().String()),
		clickhouse.Named("server", s.tunnelServerID().String()),
		clickhouse.Named("request_producer", s.det("tunnel-producer-requests").String()),
		clickhouse.Named("connection_producer", s.det("tunnel-producer-connections").String()),
		clickhouse.Named("no_server", uuid.Nil.String()),
	}
	if err := ch.Exec(ctx, fmt.Sprintf(`INSERT INTO tunnel_metric_snapshots
  (gram_project_id, source_id, bucket, kind, producer_id, server_id, method,
   client_family, revision, attempts, successes, errors, canceled, incomplete,
   latency_bins, connections, consumers, substreams, connections_opened)
SELECT toUUID(@proj), toUUID(@source),
       toStartOfMinute(now()) - toIntervalMinute(number + %d),
       'requests', toUUID(@request_producer), toUUID(@server),
       if(number %% 5 = 0, 'tools/list', 'tools/call'),
       if(number %% 3 = 0, 'claude', 'cursor'), 1,
       12 + number %% 21, 11 + number %% 21, 1, 0, 0,
       [0, 0, 4, 7 + number %% 21, 1, 0, 0, 0, 0, 0, 0, 0], 0, 0, 0, 0
FROM numbers(%d)`, tunnelHistoryGapMinutes, tunnelRequestMinutes), args...); err != nil {
		return fmt.Errorf("insert demo tunnel request metrics: %w", err)
	}
	if err := ch.Exec(ctx, fmt.Sprintf(`INSERT INTO tunnel_metric_snapshots
  (gram_project_id, source_id, bucket, kind, producer_id, server_id, method,
   client_family, revision, attempts, successes, errors, canceled, incomplete,
   latency_bins, connections, consumers, substreams, connections_opened)
SELECT toUUID(@proj), toUUID(@source),
       toStartOfInterval(now(), INTERVAL 15 SECOND) - toIntervalSecond(15 * number + %d),
       'connections', toUUID(@connection_producer), toUUID(@no_server), '', '', 1,
       0, 0, 0, 0, 0, [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
       if(number %% 240 < 12, 1, 2), 4 + number %% 9, number %% 4,
       if(number = %d, 2, 0)
FROM numbers(%d)`, tunnelHistoryGapMinutes*60, tunnelConnectionSamples-1, tunnelConnectionSamples), args...); err != nil {
		return fmt.Errorf("insert demo tunnel connection metrics: %w", err)
	}
	return s.chRunChecks(ctx, ch, []chCheck{{
		cond: fmt.Sprintf(`(SELECT count() FROM tunnel_metric_snapshots FINAL WHERE gram_project_id = toUUID(@proj) AND source_id = toUUID(@tunnel_source)) != %d`,
			tunnelRequestMinutes+tunnelConnectionSamples),
		settings: chFinalSettings,
		msg:      "demo seed postflight: tunnel metric rows missing",
	}})
}
