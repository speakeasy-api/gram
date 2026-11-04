package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestOptions(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	infra, cleanup, err := testenv.Launch(ctx, testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	pool, err := infra.CloneTestDatabase(t, "backfill")
	require.NoError(t, err)
	cutoff := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	for _, table := range []string{"audit_logs", "security_audit_logs"} {
		// Identifiers are fixed test constants, never operator input.
		_, err = pool.Exec(ctx, `INSERT INTO `+table+` (organization_id, actor_id, actor_type, subject_id, subject_type, action, created_at, is_internal, metadata) VALUES
 ('test-org','actor','user','one','catalog_promotion','other',$1,false,'{"keep":true}'),
 ('test-org','actor','user','two','mcp_server','mcp-server:promote-catalogue',$1,false,NULL),
 ('test-org','actor','user','three','catalog_promotion','other',$1,false,NULL),
 ('test-org','actor','user','unrelated','mcp_server','mcp-server:update',$1,false,NULL),
 ('test-org','actor','user','exact-cutoff','catalog_promotion','other',$2,false,NULL),
 ('test-org','actor','user','new','catalog_promotion','other',$3,false,NULL),
 ('test-org','actor','user','already','catalog_promotion','other',$1,true,NULL)`, cutoff.Add(-time.Hour), cutoff, cutoff.Add(time.Hour))
		require.NoError(t, err)
	}
	snapshot := func() string {
		var s string
		err := pool.QueryRow(ctx, `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM audit_logs a),(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM security_audit_logs a),(SELECT count(*) FROM outbox),(SELECT count(*) FROM publish_outbox))::text`).Scan(&s)
		require.NoError(t, err)
		return s
	}
	data := func() string {
		var s string
		err := pool.QueryRow(ctx, `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(a)-'is_internal' ORDER BY id) FROM audit_logs a),(SELECT jsonb_agg(to_jsonb(a)-'is_internal' ORDER BY id) FROM security_audit_logs a))::text`).Scan(&s)
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
	for _, table := range []string{"audit_logs", "security_audit_logs"} {
		var count int
		err = pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE NOT is_internal AND subject_id IN ('unrelated','exact-cutoff','new')`).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 3, count)
	}
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM outbox)+(SELECT count(*) FROM publish_outbox)`).Scan(&count))
	require.Zero(t, count)
	require.Error(t, backfill(ctx, pool, o, brokenWriter{}))
	_, err = pool.Exec(ctx, "UPDATE audit_logs SET is_internal=false WHERE subject_id IN ('one','two','three')")
	require.NoError(t, err)
	err = backfill(ctx, pool, o, &failAfterHeader{})
	require.ErrorContains(t, err, "previous batches committed rows=2")
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM audit_logs WHERE is_internal AND subject_id IN ('one','two','three')").Scan(&count))
	require.Equal(t, 2, count)
}
