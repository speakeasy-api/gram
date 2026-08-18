package testenv

import (
	"context"
	"flag"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracernoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// NewCacheSuffix returns a cache suffix unique to this test invocation.
func NewCacheSuffix(t *testing.T, base cache.Suffix) cache.Suffix {
	t.Helper()

	return cache.Suffix(string(base) + "-" + t.Name() + "-" + uuid.NewString())
}

func DefaultSiteURL(t *testing.T) *url.URL {
	t.Helper()
	val := conv.Default(os.Getenv("GRAM_SITE_URL"), "https://localhost:5173")
	parsed, err := url.Parse(val)
	require.NoError(t, err, "expected default site URL to parse")
	return parsed

}

func NewEncryptionClient(t *testing.T) *encryption.Client {
	t.Helper()

	enc, err := encryption.New("dGVzdC1rZXktMTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM=")
	require.NoError(t, err)
	return enc
}

func NewLogger(*testing.T) *slog.Logger {
	if isTestingVerbose() {
		return slog.New(o11y.NewLogHandler(&o11y.LogHandlerOptions{
			RawLevel:    os.Getenv("LOG_LEVEL"),
			Pretty:      true,
			DataDogAttr: false,
		}))
	}
	return slog.New(slog.DiscardHandler)
}

func isTestingVerbose() bool {
	if flag.CommandLine == nil || !flag.CommandLine.Parsed() {
		return false
	}

	return testing.Verbose()
}

func NewTracerProvider(t *testing.T) trace.TracerProvider {
	t.Helper()

	return tracernoop.NewTracerProvider()
}

func NewMeterProvider(t *testing.T) metric.MeterProvider {
	t.Helper()

	return metricnoop.NewMeterProvider()
}

// WaitForBlockedBackend blocks until some backend in this database is waiting
// on a lock another one holds. A concurrency test needs it to know its second
// connection has really blocked: a sleep long enough to be safe would still let
// a slow start pass the test for the wrong reason.
//
// The query is raw because pg_stat_activity is a system view sqlc's catalog does
// not carry, and it is scoped to the current database because the Postgres
// instance is shared with every other package's cloned test databases. It reads
// pg_stat_activity rather than pg_locks because a backend queued behind a row
// lock waits on a transactionid lock, whose pg_locks row has no database.
func WaitForBlockedBackend(t *testing.T, ctx context.Context, conn *pgxpool.Pool) {
	t.Helper()

	require.Eventually(t, func() bool {
		var blocked int64
		err := conn.QueryRow(ctx, `
			SELECT count(*)
			FROM pg_stat_activity
			WHERE datname = current_database()
			  AND wait_event_type = 'Lock'
		`).Scan(&blocked)

		return err == nil && blocked > 0
	}, 30*time.Second, 10*time.Millisecond, "expected a backend to block on a lock")
}

// RejectWritesTo makes every insert and update on one table fail with a check
// constraint violation, so a caller can prove how its handler reports a database
// error that is not pgx.ErrNoRows. NOT VALID leaves the rows already there
// alone, so fixtures seeded beforehand stay readable.
//
// It alters the schema, so it is only sound in a test that owns its database,
// and it can be called once per table per database.
func RejectWritesTo(t *testing.T, ctx context.Context, conn *pgxpool.Pool, table string) {
	t.Helper()

	name := pgx.Identifier{"testenv_reject_writes_" + table}.Sanitize()
	stmt := "ALTER TABLE " + pgx.Identifier{table}.Sanitize() +
		" ADD CONSTRAINT " + name + " CHECK (false) NOT VALID"

	_, err := conn.Exec(ctx, stmt)
	require.NoError(t, err)
}

// BeginTx starts a transaction that's rolled back on test cleanup, so tests
// that need a real pgx.Tx (e.g. for savepoint-using functions) don't leak
// open transactions. Callers that need a write visible to a later call in
// the same test (simulating separate requests) must Commit explicitly.
func BeginTx(t *testing.T, ctx context.Context, conn *pgxpool.Pool) pgx.Tx {
	t.Helper()

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	return tx
}
