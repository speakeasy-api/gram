// Package tunnelmetrics persists and queries payload-free tunnel aggregates.
package tunnelmetrics

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	tunnelv1 "github.com/speakeasy-api/gram/infra/gen/gram/tunnel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	tunnelrepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/tunnel/metrics"
)

var ErrRangeTooLarge = errors.New("tunnel metrics range exceeds row limit")

type Store struct {
	conn   driver.Conn
	owners func(context.Context, []uuid.UUID) ([]tunnelrepo.ListMetricSourceOwnersRow, error)
}

func NewStore(conn driver.Conn) *Store { return &Store{conn: conn, owners: nil} }
func NewWriter(conn driver.Conn, owners func(context.Context, []uuid.UUID) ([]tunnelrepo.ListMetricSourceOwnersRow, error)) *Store {
	return &Store{conn: conn, owners: owners}
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
	for _, n := range append([]uint64{m.Attempts, m.Successes, m.Errors, m.Canceled, m.Incomplete, m.ConnectionsOpened, uint64(m.Consumers), uint64(m.Substreams)}, m.LatencyBins...) {
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
	valid = slices.DeleteFunc(valid, func(m *tunnelv1.MetricsSnapshot) bool { _, ok := allowed[uuid.MustParse(m.SourceId)]; return !ok })
	if len(valid) == 0 {
		return nil
	}
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"async_insert": 1, "wait_for_async_insert": 1}))
	batch, err := s.conn.PrepareBatch(ctx, `INSERT INTO tunnel_metric_snapshots (gram_project_id,source_id,bucket,kind,producer_id,server_id,method,client_family,revision,attempts,successes,errors,canceled,incomplete,latency_bins,connections,consumers,substreams,connections_opened)`)
	if err != nil {
		return fmt.Errorf("prepare tunnel metrics batch: %w", err)
	}
	defer o11y.NoLogDefer(batch.Close)
	for _, m := range valid {
		server := uuid.Nil
		if m.ServerId != "" {
			server = uuid.MustParse(m.ServerId)
		}
		if err = batch.Append(allowed[uuid.MustParse(m.SourceId)], uuid.MustParse(m.SourceId), time.Unix(m.BucketUnix, 0).UTC(), m.Kind, uuid.MustParse(m.ProducerId), server, m.Method, m.ClientFamily, m.Revision, m.Attempts, m.Successes, m.Errors, m.Canceled, m.Incomplete, m.LatencyBins, m.Connections, m.Consumers, m.Substreams, m.ConnectionsOpened); err != nil {
			return fmt.Errorf("append tunnel metrics: %w", err)
		}
	}
	if err = batch.Send(); err != nil {
		return fmt.Errorf("send tunnel metrics batch: %w", err)
	}
	return nil
}

type Row struct {
	Bucket                                                    time.Time
	Kind, Method, Client, Server                              string
	Attempts, Successes, Errors, Canceled, Incomplete, Opened uint64
	Bins                                                      []uint64
	Connections, Consumers, Substreams                        uint32
}

// Read requires prior project/source authorization by the management handler.
// All dimensions and time bounds are parameterized. Deduplicate before summing.
func (s *Store) Read(ctx context.Context, project, source uuid.UUID, since time.Time) ([]Row, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.conn.Query(ctx, `SELECT bucket,kind,method,client_family,toString(server_id),
 argMax(attempts,revision) AS n_attempts,argMax(successes,revision) AS n_successes,argMax(errors,revision) AS n_errors,
 argMax(canceled,revision) AS n_canceled,argMax(incomplete,revision) AS n_incomplete,argMax(connections_opened,revision) AS n_opened,
 argMax(latency_bins,revision) AS n_bins,argMax(connections,revision) AS n_connections,argMax(consumers,revision) AS n_consumers,
 argMax(substreams,revision) AS n_substreams
 FROM tunnel_metric_snapshots WHERE gram_project_id=? AND source_id=? AND bucket>=? AND bucket<=now()
 GROUP BY bucket,kind,method,client_family,producer_id,server_id
 ORDER BY bucket LIMIT 200001`, project, source, since)
	if err != nil {
		return nil, fmt.Errorf("query tunnel metrics: %w", err)
	}
	defer o11y.NoLogDefer(rows.Close)
	result := make([]Row, 0)
	for rows.Next() {
		var r Row
		if err = rows.Scan(&r.Bucket, &r.Kind, &r.Method, &r.Client, &r.Server, &r.Attempts, &r.Successes, &r.Errors, &r.Canceled, &r.Incomplete, &r.Opened, &r.Bins, &r.Connections, &r.Consumers, &r.Substreams); err != nil {
			return nil, fmt.Errorf("scan tunnel metrics: %w", err)
		}
		result = append(result, r)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read tunnel metric rows: %w", err)
	}
	if len(result) > 200000 {
		return nil, ErrRangeTooLarge
	}
	return result, nil
}
