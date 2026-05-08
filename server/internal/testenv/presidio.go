package testenv

import (
	"testing"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis/presidiotest"
)

type PresidioClientFunc func(t *testing.T) *risk_analysis.PresidioClient

// NewTestPresidio returns a Presidio client factory backed by an in-process
// mock server. The mock implements deterministic regex-based detection over
// the same HTTP API the real Presidio Analyzer exposes, so tests skip the
// 60s+ container boot and avoid CI flakes from the ML image.
func NewTestPresidio() (*presidiotest.MockServer, PresidioClientFunc) {
	server := presidiotest.NewMockServer(nil)
	return server, newPresidioClientFunc(server)
}

func newPresidioClientFunc(server *presidiotest.MockServer) PresidioClientFunc {
	return func(t *testing.T) *risk_analysis.PresidioClient {
		t.Helper()

		return risk_analysis.NewPresidioClient(
			server.URL(),
			NewTracerProvider(t),
			NewMeterProvider(t),
			NewLogger(t),
		)
	}
}
