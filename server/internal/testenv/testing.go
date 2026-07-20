package testenv

import (
	"context"
	"flag"
	"log/slog"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracernoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

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
	} else {
		return slog.New(slog.DiscardHandler)
	}
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
