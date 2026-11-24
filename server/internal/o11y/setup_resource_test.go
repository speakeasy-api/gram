package o11y

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/partition"
)

func TestNewClueConfig_ResourceCarriesPartition(t *testing.T) {
	t.Parallel()

	exporter := tracetest.NewInMemoryExporter()
	cfg, err := newClueConfig(
		t.Context(),
		"gram-test",
		"test",
		"deadbeef",
		partition.EU,
		nil,
		exporter,
		propagation.TraceContext{},
		sdktrace.AlwaysSample(),
		otel.ErrorHandlerFunc(func(err error) { require.NoError(t, err) }),
	)
	require.NoError(t, err)

	provider, ok := cfg.TracerProvider.(*sdktrace.TracerProvider)
	require.True(t, ok)
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	_, span := provider.Tracer("test").Start(t.Context(), "probe")
	span.End()
	require.NoError(t, provider.ForceFlush(t.Context()))

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	value, found := spans[0].Resource.Set().Value(attr.PartitionKey)
	require.True(t, found)
	require.Equal(t, "eu", value.AsString())
}
