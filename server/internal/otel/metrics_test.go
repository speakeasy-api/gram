package otel

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/stretchr/testify/require"
)

func TestRecordEnricherDurationIgnoresUnavailableInstrument(t *testing.T) {
	t.Parallel()

	m := &metrics{
		logEnricherDuration:  nil,
		spanEnricherDuration: nil,
	}
	require.NotPanics(t, func() {
		m.recordEnricherDuration(t.Context(), "test-enricher", 0.25, o11y.OutcomeSuccess)
	})
}
