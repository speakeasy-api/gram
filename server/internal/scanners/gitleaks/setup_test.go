package gitleaks_test

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/enforcereply"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newReplyWriter(t *testing.T) (*miniredis.Miniredis, *redis.Client, *enforcereply.Writer) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), Protocol: 2})
	t.Cleanup(func() { _ = client.Close() })
	return mr, client, enforcereply.NewWriter(client)
}

func newTestFingerprinter(t *testing.T) risk.Fingerprinter {
	t.Helper()
	key := base64.StdEncoding.EncodeToString([]byte("synthetic-fingerprint-key-material"))
	fingerprinter, err := risk.ParsePepperKeyRing(fmt.Appendf(nil, `{"current":"v1","keys":{"v1":%q}}`, key))
	require.NoError(t, err)
	return fingerprinter
}

func newTestEnforceHandler(t *testing.T, meterProvider metric.MeterProvider, writer *enforcereply.Writer, maxRequestAge time.Duration) (*gitleaks.EnforceHandler, risk.Fingerprinter) {
	t.Helper()
	fingerprinter := newTestFingerprinter(t)
	handler, err := gitleaks.NewEnforceHandler(
		testenv.NewLogger(t),
		meterProvider,
		writer,
		func(tenantID string, message []byte) (string, error) {
			sum, _, fingerprintErr := fingerprinter.TenantedHS256(tenantID, message)
			return risk.EncodeFingerprint(sum), fingerprintErr
		},
		gitleaks.EnforceHandlerConfig{MaxRequestAge: maxRequestAge},
	)
	require.NoError(t, err)
	return handler, fingerprinter
}

func newTestMeterProvider(t *testing.T) (*sdkmetric.MeterProvider, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	return provider, reader
}
