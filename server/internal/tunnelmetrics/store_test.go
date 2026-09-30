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
	store := NewWriter(conn, func(context.Context, []uuid.UUID) ([]tunnelrepo.ListMetricSourceOwnersRow, error) {
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
	rows, err := store.Read(t.Context(), project, source, bucket.Add(-time.Minute))
	require.NoError(t, err)
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
	store := NewWriter(conn, owners)
	require.NoError(t, store.HandleBatch(t.Context(), batch, nil))
	for i, source := range sources {
		for j, project := range projects {
			rows, err := store.Read(t.Context(), project, source, bucket)
			require.NoError(t, err)
			if i < 2 && i == j {
				require.Len(t, rows, 1)
			} else {
				require.Empty(t, rows)
			}
		}
	}
	// Unknown and deleted sources must be dropped before insertion, including
	// rows that would otherwise receive a zero project ID.
	for _, source := range sources[2:] {
		rows, err := store.Read(t.Context(), uuid.Nil, source, bucket)
		require.NoError(t, err)
		require.Empty(t, rows)
	}
	sentinel := errors.New("ownership database unavailable")
	store = NewWriter(conn, func(context.Context, []uuid.UUID) ([]tunnelrepo.ListMetricSourceOwnersRow, error) {
		return nil, sentinel
	})
	require.ErrorIs(t, store.HandleBatch(t.Context(), batch, nil), sentinel, "lookup failure must nack the batch")
}
