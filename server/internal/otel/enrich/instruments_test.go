package enrich

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/stretchr/testify/require"
)

func TestRecordEnricherDurationIgnoresUnavailableInstrument(t *testing.T) {
	t.Parallel()

	m := &Instruments{
		logEnricherDuration:    nil,
		metricEnricherDuration: nil,
		spanEnricherDuration:   nil,
	}
	require.NotPanics(t, func() {
		m.recordSpanEnricherDuration(t.Context(), "test-enricher", 0.25, o11y.OutcomeSuccess)
	})
}

func TestRecordLogEnricherDurationIgnoresUnavailableInstrument(t *testing.T) {
	t.Parallel()

	m := &Instruments{
		logEnricherDuration:    nil,
		metricEnricherDuration: nil,
		spanEnricherDuration:   nil,
	}
	require.NotPanics(t, func() {
		m.recordLogEnricherDuration(t.Context(), "test-enricher", 0.25, o11y.OutcomeSuccess)
	})
}

func TestRecordColumnValueMissingIgnoresUnavailableInstrument(t *testing.T) {
	t.Parallel()

	m := &Instruments{
		logEnricherDuration:    nil,
		metricEnricherDuration: nil,
		spanEnricherDuration:   nil,
		columnValueMissing:     nil,
	}
	require.NotPanics(t, func() {
		m.recordColumnValueMissing(t.Context(), "claude-code", "api_request", "turn_id")
	})
}

func TestRecordMetricEnricherDurationIgnoresUnavailableInstrument(t *testing.T) {
	t.Parallel()

	m := &Instruments{
		logEnricherDuration:    nil,
		metricEnricherDuration: nil,
		spanEnricherDuration:   nil,
	}
	require.NotPanics(t, func() {
		m.recordMetricEnricherDuration(t.Context(), "test-enricher", 0.25, o11y.OutcomeSuccess)
	})
}

func TestRecordReservedAttributesDroppedIgnoresUnavailableInstrument(t *testing.T) {
	t.Parallel()

	m := &Instruments{
		logEnricherDuration:       nil,
		metricEnricherDuration:    nil,
		spanEnricherDuration:      nil,
		columnValueMissing:        nil,
		reservedAttributesDropped: nil,
	}
	require.NotPanics(t, func() {
		m.RecordReservedAttributesDropped(t.Context(), SignalLog, 2)
	})
}
