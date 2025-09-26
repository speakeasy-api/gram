package testenv

import (
	"log/slog"
	"os"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracernoop "go.opentelemetry.io/otel/trace/noop"
)

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
