// Package tunnelmetrics persists and queries payload-free tunnel aggregates.
package tunnelmetrics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	tunnelv1 "github.com/speakeasy-api/gram/infra/gen/gram/tunnel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	tunnelrepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/tunnel/metrics"
)

// ErrRangeTooLarge reports a read whose aggregated result exceeds
// maxAggregateRows. Reads aggregate per output interval in ClickHouse, so this
// is a defensive cap rather than a limit a busy tunnel reaches.
var ErrRangeTooLarge = errors.New("tunnel metrics range exceeds row limit")

// maxAggregateRows bounds one read's request rows: at most 168 weekly intervals
// times the method and client families the collector emits.
const maxAggregateRows = 20000

type Store struct {
	conn   driver.Conn
	logger *slog.Logger
	owners func(context.Context, []uuid.UUID) ([]tunnelrepo.ListMetricSourceOwnersRow, error)
}

func NewStore(conn driver.Conn) *Store { return &Store{conn: conn, logger: nil, owners: nil} }
func NewWriter(conn driver.Conn, logger *slog.Logger, owners func(context.Context, []uuid.UUID) ([]tunnelrepo.ListMetricSourceOwnersRow, error)) *Store {
	return &Store{conn: conn, logger: logger, owners: owners}
}

func validSnapshot(m *tunnelv1.MetricsSnapshot) bool {
	if m == nil || m.Revision == 0 || len(m.LatencyBins) != 12 {
		return false
	}
	if _, err := uuid.Parse(m.SourceId); err != nil {
		return false
	}
	if _, err := uuid.Parse(m.ProducerId); err != nil {
		return false
	}
	if m.ServerId != "" {
		if _, err := uuid.Parse(m.ServerId); err != nil {
			return false
		}
	}
	if m.BucketUnix%15 != 0 || m.BucketUnix < time.Now().Add(-7*24*time.Hour).Unix() || m.BucketUnix > time.Now().Add(time.Minute).Unix() {
		return false
	}
	// Bounds also keep aggregate sums within exact JSON integer range at the read limit.
	for _, n := range append([]uint64{m.Attempts, m.Successes, m.Errors, m.Canceled, m.Incomplete, m.Rejected, m.ConnectionsOpened, uint64(m.Consumers), uint64(m.Substreams)}, m.LatencyBins...) {
		if n > 1_000_000_000 {
			return false
		}
	}
	switch m.Kind {
	case "requests":
		return m.BucketUnix%60 == 0 && metrics.Method(m.Method) == m.Method && slices.Contains([]string{"claude", "codex", "cursor", "chatgpt", "inspector", "unknown", "other"}, m.ClientFamily)
	case "coverage":
		return m.BucketUnix%60 == 0 && m.Method == "" && m.ClientFamily == "" && m.ServerId == ""
	case "connections":
		return m.Method == "" && m.ClientFamily == "" && m.ServerId == "" && m.Connections <= 10000
	default:
		return false
	}
}

// HandleBatch writes aggregate revisions. Reads use the latest revision to
// handle Pub/Sub redelivery and snapshots received out of order.
func (s *Store) HandleBatch(ctx context.Context, messages []*tunnelv1.MetricsSnapshot, _ []gcp.MessageMetadata) error {
	valid := make([]*tunnelv1.MetricsSnapshot, 0, len(messages))
	for _, m := range messages {
		if validSnapshot(m) {
			valid = append(valid, m)
		}
	}
	// Dropped snapshots are acknowledged, so a producer bug or version skew
	// would otherwise disappear without a trace.
	if invalid := len(messages) - len(valid); invalid > 0 && s.logger != nil {
		s.logger.WarnContext(ctx, "dropped invalid tunnel metric snapshots", attr.SlogValueInt(invalid))
	}
	if len(valid) == 0 {
		return nil
	}
	if s.owners == nil {
		return fmt.Errorf("tunnel metrics ownership resolver missing")
	}
	ids := make([]uuid.UUID, 0, len(valid))
	seen := map[uuid.UUID]struct{}{}
	for _, m := range valid {
		id := uuid.MustParse(m.SourceId)
		if _, ok := seen[id]; !ok {
			ids = append(ids, id)
			seen[id] = struct{}{}
		}
	}
	owners, err := s.owners(ctx, ids)
	if err != nil {
		return fmt.Errorf("resolve tunnel metric sources: %w", err)
	}
	allowed := make(map[uuid.UUID]uuid.UUID, len(owners))
	for _, owner := range owners {
		allowed[owner.ID] = owner.ProjectID
	}
	owned := len(valid)
	valid = slices.DeleteFunc(valid, func(m *tunnelv1.MetricsSnapshot) bool { _, ok := allowed[uuid.MustParse(m.SourceId)]; return !ok })
	if unowned := owned - len(valid); unowned > 0 && s.logger != nil {
		// Expected briefly after a source is deleted; persistent counts mean a
		// producer reports sources this database does not know.
		s.logger.InfoContext(ctx, "dropped tunnel metric snapshots for unknown sources", attr.SlogValueInt(unowned))
	}
	if len(valid) == 0 {
		return nil
	}
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"async_insert": 1, "wait_for_async_insert": 1}))
	batch, err := s.conn.PrepareBatch(ctx, `INSERT INTO tunnel_metric_snapshots (gram_project_id,source_id,bucket,kind,producer_id,server_id,method,client_family,revision,attempts,successes,errors,canceled,incomplete,rejected,latency_bins,connections,consumers,substreams,connections_opened)`)
	if err != nil {
		return fmt.Errorf("prepare tunnel metrics batch: %w", err)
	}
	defer o11y.NoLogDefer(batch.Close)
	for _, m := range valid {
		server := uuid.Nil
		if m.ServerId != "" {
			server = uuid.MustParse(m.ServerId)
		}
		if err = batch.Append(allowed[uuid.MustParse(m.SourceId)], uuid.MustParse(m.SourceId), time.Unix(m.BucketUnix, 0).UTC(), m.Kind, uuid.MustParse(m.ProducerId), server, m.Method, m.ClientFamily, m.Revision, m.Attempts, m.Successes, m.Errors, m.Canceled, m.Incomplete, m.Rejected, m.LatencyBins, m.Connections, m.Consumers, m.Substreams, m.ConnectionsOpened); err != nil {
			return fmt.Errorf("append tunnel metrics: %w", err)
		}
	}
	if err = batch.Send(); err != nil {
		return fmt.Errorf("send tunnel metrics batch: %w", err)
	}
	return nil
}

// RequestRow is one output interval of request or coverage snapshots for one
// kind, method and client family, summed across producers and servers after
// keeping each snapshot's latest revision.
type RequestRow struct {
	Bucket         time.Time
	Kind           string
	Method, Client string
	// Attempts through Rejected are summed counters. Incomplete is only a
	// request outcome for kind "requests".
	Attempts, Successes, Errors, Canceled, Incomplete, Rejected uint64
	Bins                                                        []uint64
	// Snapshots is the number of deduplicated snapshots in the interval. For
	// coverage it counts producer-minutes that reported the source.
	Snapshots uint64
	// Lost reports that a producer's loss watermark for this source grew in
	// this interval.
	Lost bool
	// Servers lists MCP servers that received at least one request.
	Servers []string
	Latest  time.Time
}

// ConnectionRow is one output interval of connection samples. Each 15-second
// slot sums every producer's last sample, then the interval averages its slots.
type ConnectionRow struct {
	Bucket                             time.Time
	Connections, Consumers, Substreams float64
	Opened                             uint64
	// Slots is the number of 15-second slots with at least one sample.
	Slots  uint64
	Lost   bool
	Latest time.Time
}

// Aggregates is one read's result, already reduced to output intervals.
type Aggregates struct {
	Requests    []RequestRow
	Connections []ConnectionRow
}

// requestQuery keeps each snapshot key's latest revision, marks coverage rows
// whose producer's loss watermark for this source grew since that producer's
// previous row, then sums per output interval. The window starts one interval
// early so the first interval's rows have a predecessor to compare with.
const requestQuery = `WITH deduplicated AS (
  SELECT bucket, kind, method, client_family, producer_id, server_id,
    argMax(attempts, revision) AS n_attempts, argMax(successes, revision) AS n_successes,
    argMax(errors, revision) AS n_errors, argMax(canceled, revision) AS n_canceled,
    argMax(incomplete, revision) AS n_incomplete, argMax(rejected, revision) AS n_rejected,
    argMax(latency_bins, revision) AS n_bins
  FROM tunnel_metric_snapshots
  WHERE gram_project_id = @project AND source_id = @source AND bucket >= @lookback AND bucket <= now()
    AND kind IN ('requests', 'coverage')
  GROUP BY bucket, kind, method, client_family, producer_id, server_id
), marked AS (
  SELECT *, kind = 'coverage' AND n_incomplete > lagInFrame(n_incomplete, 1, n_incomplete)
      OVER (PARTITION BY kind, producer_id ORDER BY bucket ROWS BETWEEN 1 PRECEDING AND CURRENT ROW) AS n_lost
  FROM deduplicated
)
SELECT toStartOfInterval(bucket, toIntervalSecond(@step)) AS interval_start, kind, method, client_family,
  sum(n_attempts), sum(n_successes), sum(n_errors), sum(n_canceled),
  if(kind = 'requests', sum(n_incomplete), 0), sum(n_rejected), sumForEach(n_bins), count(), max(n_lost),
  arrayMap(id -> toString(id), groupUniqArrayIf(server_id, kind = 'requests' AND n_attempts > 0 AND server_id != toUUID('00000000-0000-0000-0000-000000000000'))),
  max(bucket)
FROM marked
WHERE bucket >= @since
GROUP BY interval_start, kind, method, client_family
ORDER BY interval_start
LIMIT @limit`

// connectionQuery keeps each producer's latest sample per 15-second slot, sums
// producers per slot, then averages slots per output interval.
const connectionQuery = `WITH deduplicated AS (
  SELECT bucket, producer_id,
    argMax(connections, revision) AS n_connections, argMax(consumers, revision) AS n_consumers,
    argMax(substreams, revision) AS n_substreams, argMax(connections_opened, revision) AS n_opened,
    argMax(incomplete, revision) AS n_incomplete
  FROM tunnel_metric_snapshots
  WHERE gram_project_id = @project AND source_id = @source AND bucket >= @lookback AND bucket <= now()
    AND kind = 'connections'
  GROUP BY bucket, producer_id
), marked AS (
  SELECT *, n_incomplete > lagInFrame(n_incomplete, 1, n_incomplete)
      OVER (PARTITION BY producer_id ORDER BY bucket ROWS BETWEEN 1 PRECEDING AND CURRENT ROW) AS n_lost
  FROM deduplicated
), slots AS (
  SELECT bucket, sum(n_connections) AS s_connections, sum(n_consumers) AS s_consumers,
    sum(n_substreams) AS s_substreams, sum(n_opened) AS s_opened, max(n_lost) AS s_lost
  FROM marked
  WHERE bucket >= @since
  GROUP BY bucket
)
SELECT toStartOfInterval(bucket, toIntervalSecond(@step)) AS interval_start,
  avg(s_connections), avg(s_consumers), avg(s_substreams), sum(s_opened), count(), max(s_lost), max(bucket)
FROM slots
GROUP BY interval_start
ORDER BY interval_start`

// Read requires prior project/source authorization by the management handler.
// All dimensions and time bounds are parameterized.
func (s *Store) Read(ctx context.Context, project, source uuid.UUID, since time.Time, step time.Duration) (Aggregates, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	args := []any{
		clickhouse.Named("project", project.String()),
		clickhouse.Named("source", source.String()),
		clickhouse.Named("since", since.UTC()),
		clickhouse.Named("lookback", since.Add(-step).UTC()),
		clickhouse.Named("step", int64(step.Seconds())),
		clickhouse.Named("limit", maxAggregateRows+1),
	}
	result := Aggregates{Requests: make([]RequestRow, 0), Connections: make([]ConnectionRow, 0)}

	rows, err := s.conn.Query(ctx, requestQuery, args...)
	if err != nil {
		return Aggregates{}, fmt.Errorf("query tunnel request metrics: %w", err)
	}
	defer o11y.NoLogDefer(rows.Close)
	for rows.Next() {
		var r RequestRow
		var lost uint8
		if err = rows.Scan(&r.Bucket, &r.Kind, &r.Method, &r.Client, &r.Attempts, &r.Successes, &r.Errors, &r.Canceled, &r.Incomplete, &r.Rejected, &r.Bins, &r.Snapshots, &lost, &r.Servers, &r.Latest); err != nil {
			return Aggregates{}, fmt.Errorf("scan tunnel request metrics: %w", err)
		}
		r.Lost = lost != 0
		result.Requests = append(result.Requests, r)
	}
	if err = rows.Err(); err != nil {
		return Aggregates{}, fmt.Errorf("read tunnel request metric rows: %w", err)
	}
	if len(result.Requests) > maxAggregateRows {
		return Aggregates{}, ErrRangeTooLarge
	}

	connections, err := s.conn.Query(ctx, connectionQuery, args...)
	if err != nil {
		return Aggregates{}, fmt.Errorf("query tunnel connection metrics: %w", err)
	}
	defer o11y.NoLogDefer(connections.Close)
	for connections.Next() {
		var r ConnectionRow
		var lost uint8
		if err = connections.Scan(&r.Bucket, &r.Connections, &r.Consumers, &r.Substreams, &r.Opened, &r.Slots, &lost, &r.Latest); err != nil {
			return Aggregates{}, fmt.Errorf("scan tunnel connection metrics: %w", err)
		}
		r.Lost = lost != 0
		result.Connections = append(result.Connections, r)
	}
	if err = connections.Err(); err != nil {
		return Aggregates{}, fmt.Errorf("read tunnel connection metric rows: %w", err)
	}
	return result, nil
}
