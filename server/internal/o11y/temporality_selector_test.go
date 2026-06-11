package o11y

import (
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestInstrumentKindTemporalitySelector(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		kind sdkmetric.InstrumentKind
		want metricdata.Temporality
	}{
		{"counter", sdkmetric.InstrumentKindCounter, metricdata.DeltaTemporality},
		{"histogram", sdkmetric.InstrumentKindHistogram, metricdata.DeltaTemporality},
		{"gauge", sdkmetric.InstrumentKindGauge, metricdata.DeltaTemporality},
		{"observable_counter", sdkmetric.InstrumentKindObservableCounter, metricdata.DeltaTemporality},
		{"observable_gauge", sdkmetric.InstrumentKindObservableGauge, metricdata.DeltaTemporality},
		{"up_down_counter", sdkmetric.InstrumentKindUpDownCounter, metricdata.CumulativeTemporality},
		{"observable_up_down_counter", sdkmetric.InstrumentKindObservableUpDownCounter, metricdata.CumulativeTemporality},
	}

	for _, tc := range cases {
		require.Equal(t, tc.want, instrumentKindTemporalitySelector(tc.kind), "kind %s", tc.name)
	}
}
