package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/cmd/internal-audit-events-backfill/testrepo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestOptions(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"--before", "bad"}, {"--before", "2026-11-04"}, {"--before", "2026-11-04T00:00:00Z", "--batch-size", "0"}, {"--before", "2026-11-04T00:00:00Z", "--batch-size", "5001"}, {"--before", "2026-11-04T00:00:00Z", "--max-batches", "0"}, {"--before", "2026-11-04T00:00:00Z", "extra"}} {
		_, err := parseOptions(args)
		require.Error(t, err)
	}
	o, err := parseOptions([]string{"--before", "2026-11-04T00:00:00Z"})
	require.NoError(t, err)
	require.False(t, o.apply)
	require.Equal(t, 500, o.batchSize)
	require.Equal(t, 20, o.maxBatches)
	o, err = parseOptions([]string{"--before", "2026-11-04T00:00:00Z", "--apply", "--batch-size", "5000", "--max-batches", "1"})
	require.NoError(t, err)
	require.True(t, o.apply)
}

type failAfterHeader struct{ calls int }

func (w *failAfterHeader) Write(p []byte) (int, error) {
	w.calls++
	if w.calls > 1 {
		return 0, errors.New("broken output")
	}
	return len(p), nil
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("broken output") }

func TestBackfill(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	infra, cleanup, err := testenv.Launch(ctx, testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	pool, err := infra.CloneTestDatabase(t, "backfill")
	require.NoError(t, err)
	cutoff := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	q := testrepo.New(pool)
	older := pgtype.Timestamptz{Time: cutoff.Add(-time.Hour), Valid: true}
	atCutoff := pgtype.Timestamptz{Time: cutoff, Valid: true}
	newer := pgtype.Timestamptz{Time: cutoff.Add(time.Hour), Valid: true}
	require.NoError(t, q.SeedAuditLogs(ctx, testrepo.SeedAuditLogsParams{Older: older, Cutoff: atCutoff, Newer: newer}))
	require.NoError(t, q.SeedSecurityLogs(ctx, testrepo.SeedSecurityLogsParams{Older: older, Cutoff: atCutoff, Newer: newer}))
	snapshot := func() string {
		s, err := q.Snapshot(ctx)
		require.NoError(t, err)
		return s
	}
	data := func() string {
		s, err := q.Data(ctx)
		require.NoError(t, err)
		return s
	}
	before, originalData := snapshot(), data()
	var out bytes.Buffer
	o := options{before: cutoff, batchSize: 2, maxBatches: 1}
	require.NoError(t, backfill(ctx, pool, o, &out))
	require.Equal(t, before, snapshot())
	require.Equal(t, 2, strings.Count(out.String(), "eligible_before=3"))
	o.apply = true
	out.Reset()
	require.NoError(t, backfill(ctx, pool, o, &out))
	require.Equal(t, 2, strings.Count(out.String(), "updated=2 batches=1 remaining=1"))
	require.Contains(t, out.String(), "rerun")
	out.Reset()
	require.NoError(t, backfill(ctx, pool, o, &out))
	require.Equal(t, 2, strings.Count(out.String(), "updated=1 batches=1 remaining=0"))
	after := snapshot()
	out.Reset()
	require.NoError(t, backfill(ctx, pool, o, &out))
	require.Equal(t, after, snapshot())
	require.Equal(t, 2, strings.Count(out.String(), "updated=0 batches=0 remaining=0"))
	require.Equal(t, originalData, data())
	count, err := q.CountUnchangedAuditLogs(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 3, count)
	count, err = q.CountUnchangedSecurityLogs(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 3, count)
	outboxCount, err := q.CountOutbox(ctx)
	require.NoError(t, err)
	require.Zero(t, outboxCount)
	require.Error(t, backfill(ctx, pool, o, brokenWriter{}))
	err = q.ResetAuditLogs(ctx)
	require.NoError(t, err)
	err = backfill(ctx, pool, o, &failAfterHeader{})
	require.ErrorContains(t, err, "previous batches committed rows=2")
	count, err = q.CountUpdatedAuditLogs(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
}
