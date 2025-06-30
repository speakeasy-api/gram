package testenv

import (
	"log/slog"
	"os"
	"testing"

	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

func NewLogger(*testing.T) *slog.Logger {
	if testing.Verbose() {
		return slog.New(o11y.NewLogHandler(os.Getenv("LOG_LEVEL"), true))
	} else {
		return slog.New(slog.DiscardHandler)
	}
}

func NewMetrics(t *testing.T) *o11y.Metrics {
	t.Helper()

	metrics, err := o11y.NewMetrics(noop.NewMeterProvider())
	require.NoError(t, err, "failed to create metrics provider")
	return metrics
}
