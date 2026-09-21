package scanners_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func TestAsyncShadowGateReason_Engine(t *testing.T) {
	t.Parallel()

	require.Equal(t, scanners.AsyncScanEngineReal, scanners.AsyncShadowGateReasonSampledReal.Engine())
	require.Equal(t, scanners.AsyncScanEngineReal, scanners.AsyncShadowGateReasonNotGated.Engine(), "not-gated handlers always run their real engine")
	require.Equal(t, scanners.AsyncScanEngineStub, scanners.AsyncShadowGateReasonFlagOff.Engine())
	require.Equal(t, scanners.AsyncScanEngineStub, scanners.AsyncShadowGateReasonGateError.Engine())
}
