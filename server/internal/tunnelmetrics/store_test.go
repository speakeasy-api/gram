package tunnelmetrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	tunnelv1 "github.com/speakeasy-api/gram/infra/gen/gram/tunnel/v1"
	tunnelrepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

func TestDuplicateAndOlderSnapshotsDoNotInflateCounts(t *testing.T) {
	t.Parallel()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	source := uuid.New()
	project := uuid.New()
	store := NewWriter(conn, nil, func(context.Context, []uuid.UUID) ([]tunnelrepo.ListMetricSourceOwnersRow, error) {
		return []tunnelrepo.ListMetricSourceOwnersRow{{ID: source, ProjectID: project}}, nil
	})
	producer := uuid.NewString()
	bucket := time.Now().UTC().Add(-time.Minute).Truncate(time.Minute)
	row := &tunnelv1.MetricsSnapshot{SourceId: source.String(), ProducerId: producer, BucketUnix: bucket.Unix(), Revision: 2, Kind: "requests", Method: "tools/call", ClientFamily: "claude", Attempts: 12, Successes: 11, Errors: 1, LatencyBins: []uint64{0, 0, 5, 7, 0, 0, 0, 0, 0, 0, 0, 0}}
	require.NoError(t, store.HandleBatch(t.Context(), []*tunnelv1.MetricsSnapshot{row, row}, nil))
	row.Revision = 1
	row.Attempts = 5
	row.Successes = 5
	row.Errors = 0
	row.LatencyBins = []uint64{0, 0, 2, 3, 0, 0, 0, 0, 0, 0, 0, 0}
	require.NoError(t, store.HandleBatch(t.Context(), []*tunnelv1.MetricsSnapshot{row}, nil))
	result, err := store.Read(t.Context(), project, source, bucket.Add(-time.Minute), time.Minute)
	require.NoError(t, err)
	rows := result.Requests
	require.Len(t, rows, 1)
	require.EqualValues(t, 12, rows[0].Attempts)
	require.EqualValues(t, 11, rows[0].Successes)
	require.EqualValues(t, 1, rows[0].Errors)
	require.Equal(t, []uint64{0, 0, 5, 7, 0, 0, 0, 0, 0, 0, 0, 0}, rows[0].Bins)
	row.ClientFamily = "PRIVATE_USER_AGENT_SENTINEL"
	require.False(t, validSnapshot(row))
}

func TestIngestUsesAuthoritativeOwnersAndRetriesLookupFailure(t *testing.T) {
	t.Parallel()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	sources := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	projects := []uuid.UUID{uuid.New(), uuid.New()}
	bucket := time.Now().UTC().Add(-time.Minute).Truncate(time.Minute)
	batch := make([]*tunnelv1.MetricsSnapshot, 0, 4)
	for _, source := range sources {
		batch = append(batch, &tunnelv1.MetricsSnapshot{SourceId: source.String(), ProducerId: uuid.NewString(), Kind: "coverage", BucketUnix: bucket.Unix(), Revision: 1, LatencyBins: make([]uint64, 12)})
	}
	owners := func(context.Context, []uuid.UUID) ([]tunnelrepo.ListMetricSourceOwnersRow, error) {
		return []tunnelrepo.ListMetricSourceOwnersRow{{ID: sources[0], ProjectID: projects[0]}, {ID: sources[1], ProjectID: projects[1]}}, nil
	}
	store := NewWriter(conn, nil, owners)
	require.NoError(t, store.HandleBatch(t.Context(), batch, nil))
	for i, source := range sources {
		for j, project := range projects {
			result, err := store.Read(t.Context(), project, source, bucket, time.Minute)
			require.NoError(t, err)
			if i < 2 && i == j {
				require.Len(t, result.Requests, 1)
			} else {
				require.Empty(t, result.Requests)
			}
		}
	}
	// Unknown and deleted sources must be dropped before insertion, including
	// rows that would otherwise receive a zero project ID.
	for _, source := range sources[2:] {
		result, err := store.Read(t.Context(), uuid.Nil, source, bucket, time.Minute)
		require.NoError(t, err)
		require.Empty(t, result.Requests)
	}
	sentinel := errors.New("ownership database unavailable")
	store = NewWriter(conn, nil, func(context.Context, []uuid.UUID) ([]tunnelrepo.ListMetricSourceOwnersRow, error) {
		return nil, sentinel
	})
	require.ErrorIs(t, store.HandleBatch(t.Context(), batch, nil), sentinel, "lookup failure must nack the batch")
}

func TestWeekReadAggregatesProducersInClickHouse(t *testing.T) {
	t.Parallel()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	source, project := uuid.New(), uuid.New()
	store := NewWriter(conn, nil, func(context.Context, []uuid.UUID) ([]tunnelrepo.ListMetricSourceOwnersRow, error) {
		return []tunnelrepo.ListMetricSourceOwnersRow{{ID: source, ProjectID: project}}, nil
	})
	server := uuid.NewString()
	producers := []string{uuid.NewString(), uuid.NewString()}
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	bins := func(n uint64) []uint64 { return []uint64{0, 0, n, 0, 0, 0, 0, 0, 0, 0, 0, 0} }
	var batch []*tunnelv1.MetricsSnapshot
	// Every minute of two hours, two producers, two methods and two clients:
	// 960 raw request rows that must come back as a handful of hourly rows.
	for minute := range 120 {
		bucket := hour.Add(time.Duration(minute) * time.Minute).Unix()
		for _, producer := range producers {
			for _, method := range []string{"tools/call", "tools/list"} {
				for _, client := range []string{"claude", "cursor"} {
					batch = append(batch, &tunnelv1.MetricsSnapshot{SourceId: source.String(), ProducerId: producer, ServerId: server, BucketUnix: bucket, Revision: 2, Kind: "requests", Method: method, ClientFamily: client, Attempts: 3, Successes: 2, Errors: 1, Rejected: 1, LatencyBins: bins(3)})
				}
			}
			batch = append(batch, &tunnelv1.MetricsSnapshot{SourceId: source.String(), ProducerId: producer, BucketUnix: bucket, Revision: 1, Kind: "coverage", LatencyBins: make([]uint64, 12)})
		}
	}
	// A superseded revision of one key must not be counted.
	batch = append(batch, &tunnelv1.MetricsSnapshot{SourceId: source.String(), ProducerId: producers[0], ServerId: server, BucketUnix: hour.Unix(), Revision: 1, Kind: "requests", Method: "tools/call", ClientFamily: "claude", Attempts: 100, LatencyBins: bins(100)})
	require.NoError(t, store.HandleBatch(t.Context(), batch, nil))

	since := time.Now().UTC().Add(-7 * 24 * time.Hour).Truncate(time.Hour)
	result, err := store.Read(t.Context(), project, source, since, time.Hour)
	require.NoError(t, err)
	var requestRows, coverageRows int
	var attempts, rejected, coverage uint64
	servers := map[string]struct{}{}
	for _, r := range result.Requests {
		switch r.Kind {
		case "requests":
			requestRows++
			attempts += r.Attempts
			rejected += r.Rejected
			require.EqualValues(t, 3*60*2, r.Bins[2], "latency bins sum across producers and minutes")
			for _, s := range r.Servers {
				servers[s] = struct{}{}
			}
		case "coverage":
			coverageRows++
			coverage += r.Snapshots
			require.False(t, r.Lost)
		}
	}
	require.Equal(t, 2*2*2, requestRows, "two hours of two methods and two clients, summed across producers")
	require.Equal(t, 2, coverageRows)
	require.EqualValues(t, 120*2*2*2*3, attempts)
	require.EqualValues(t, 120*2*2*2, rejected)
	require.EqualValues(t, 120*2, coverage, "one coverage snapshot per producer-minute")
	require.Equal(t, map[string]struct{}{server: {}}, servers)
}

func TestLossIsMarkedOnlyWhereTheWatermarkGrew(t *testing.T) {
	t.Parallel()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	source, project := uuid.New(), uuid.New()
	store := NewWriter(conn, nil, func(context.Context, []uuid.UUID) ([]tunnelrepo.ListMetricSourceOwnersRow, error) {
		return []tunnelrepo.ListMetricSourceOwnersRow{{ID: source, ProjectID: project}}, nil
	})
	producer := uuid.NewString()
	start := time.Now().UTC().Truncate(time.Minute).Add(-10 * time.Minute)
	// The watermark is cumulative since producer boot: 2 before the window,
	// 5 from minute 3 on. Only minute 3 lost data.
	var batch []*tunnelv1.MetricsSnapshot
	for minute := -1; minute < 8; minute++ {
		watermark := uint64(2)
		if minute >= 3 {
			watermark = 5
		}
		bucket := start.Add(time.Duration(minute) * time.Minute)
		batch = append(batch,
			&tunnelv1.MetricsSnapshot{SourceId: source.String(), ProducerId: producer, BucketUnix: bucket.Unix(), Revision: 1, Kind: "coverage", Incomplete: watermark, LatencyBins: make([]uint64, 12)},
			&tunnelv1.MetricsSnapshot{SourceId: source.String(), ProducerId: producer, BucketUnix: bucket.Unix(), Revision: 1, Kind: "connections", Connections: 1, Incomplete: watermark, LatencyBins: make([]uint64, 12)},
		)
	}
	require.NoError(t, store.HandleBatch(t.Context(), batch, nil))
	result, err := store.Read(t.Context(), project, source, start, time.Minute)
	require.NoError(t, err)
	lostRequests := map[int64]bool{}
	for _, r := range result.Requests {
		lostRequests[r.Bucket.Unix()] = r.Lost
	}
	lostConnections := map[int64]bool{}
	for _, r := range result.Connections {
		lostConnections[r.Bucket.Unix()] = r.Lost
	}
	for minute := range 8 {
		bucket := start.Add(time.Duration(minute) * time.Minute).Unix()
		require.Equal(t, minute == 3, lostRequests[bucket], "coverage minute %d", minute)
		require.Equal(t, minute == 3, lostConnections[bucket], "connections minute %d", minute)
	}
	require.Len(t, result.Requests, 8, "the lookback row before the window is not returned")
}
