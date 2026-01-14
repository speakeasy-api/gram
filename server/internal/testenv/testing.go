package testenv

import (
	"log/slog"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracernoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

func DefaultSiteURL(t *testing.T) *url.URL {
	t.Helper()
	val := conv.Default(os.Getenv("GRAM_SITE_URL"), "https://localhost:5173")
	parsed, err := url.Parse(val)
	require.NoError(t, err, "expected default site URL to parse")
	return parsed

}

func NewFunctionsTestOrchestrator(t *testing.T) functions.Orchestrator {
	t.Helper()

	codeRoot := t.TempDir()
	root, err := os.OpenRoot(codeRoot)
	require.NoError(t, err, "expected local functions runner code root to open")
	t.Cleanup(func() {
		require.NoError(t, root.Close(), "test functions runner root should close without error")
	})
	return functions.NewLocalRunner(root)
}

func NewEncryptionClient(t *testing.T) *encryption.Client {
	t.Helper()

	enc, err := encryption.New("dGVzdC1rZXktMTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM=")
	require.NoError(t, err)
	return enc
}

func NewLogger(*testing.T) *slog.Logger {
	if testing.Verbose() {
		return slog.New(o11y.NewLogHandler(&o11y.LogHandlerOptions{
			RawLevel:    os.Getenv("LOG_LEVEL"),
			Pretty:      true,
			DataDogAttr: false,
		}))
	} else {
		return slog.New(slog.DiscardHandler)
	}
}

func NewTracerProvider(t *testing.T) trace.TracerProvider {
	t.Helper()

	return tracernoop.NewTracerProvider()
}

func NewMeterProvider(t *testing.T) metric.MeterProvider {
	t.Helper()

	return metricnoop.NewMeterProvider()
}

func NewMCPRegistryClient(t *testing.T, logger *slog.Logger, tracerProvider trace.TracerProvider) *externalmcp.RegistryClient {
	t.Helper()

	pulseURL, err := url.Parse("https://api.pulsemcp.com")
	require.NoError(t, err, "expected pulse URL to parse")

	client := externalmcp.NewRegistryClient(
		NewLogger(t),
		tracerProvider,
		externalmcp.NewPulseBackend(pulseURL, "test-tenant-id", conv.NewSecret([]byte("test-api-key"))),
	)
	require.NoError(t, err, "expected mcp registry client to initialize without error")

	return client
}
