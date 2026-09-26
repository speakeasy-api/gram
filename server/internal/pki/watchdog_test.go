package pki_test

import (
	"context"
	"crypto/x509"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/pki"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestWatchDogCadenceAndPartialFailure(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		reader := sdkmetric.NewManualReader()
		provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		defer o11y.NoLogDefer(func() error { return provider.Shutdown(t.Context()) })
		cert := &x509.Certificate{NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
		calls := 0
		watchdog, err := pki.NewWatchDog(testenv.NewLogger(t), provider,
			pki.WithSource("bundle", func(context.Context) ([]*x509.Certificate, error) {
				calls++
				if calls > 1 {
					return []*x509.Certificate{cert}, errors.New("partial bundle")
				}
				return []*x509.Certificate{cert}, nil
			}),
		)
		require.NoError(t, err)
		require.NoError(t, watchdog.Start(t.Context()))
		defer o11y.NoLogDefer(func() error { return watchdog.Shutdown(t.Context()) })
		synctest.Wait()
		collect := func() map[string]float64 {
			var data metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(t.Context(), &data))
			values := make(map[string]float64)
			for _, scope := range data.ScopeMetrics {
				for _, m := range scope.Metrics {
					switch gauge := m.Data.(type) {
					case metricdata.Gauge[float64]:
						for _, point := range gauge.DataPoints {
							values[m.Name] = point.Value
						}
					case metricdata.Gauge[int64]:
						for _, point := range gauge.DataPoints {
							values[m.Name] = float64(point.Value)
						}
					}
				}
			}
			return values
		}
		want := map[string]float64{"pki.certificate.age": 3600, "pki.certificate.remaining_validity": 3600, "pki.source.observation_success": 1}
		require.Equal(t, want, collect())
		// Repeated collections must not call the loader again.
		require.Equal(t, want, collect())
		time.Sleep(time.Minute) //nolint:forbidigo // GG013: advances only the synctest fake clock to the next observation.
		synctest.Wait()
		require.Equal(t, map[string]float64{"pki.source.observation_success": 0}, collect())
		require.NoError(t, watchdog.Shutdown(t.Context()))
		require.Empty(t, collect(), "shutdown must unregister the watchdog without closing the shared provider")
		require.NoError(t, watchdog.Shutdown(t.Context()))
		require.Error(t, watchdog.Start(t.Context()), "a stopped watchdog cannot restart")
	})
}

func TestWatchDogShutdownCancelsLoading(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		loading := make(chan struct{})
		stopped := make(chan struct{})
		watchdog, err := pki.NewWatchDog(testenv.NewLogger(t), testenv.NewMeterProvider(t),
			pki.WithSource("blocking", func(ctx context.Context) ([]*x509.Certificate, error) {
				close(loading)
				<-ctx.Done()
				close(stopped)
				return nil, ctx.Err()
			}),
		)
		require.NoError(t, err)
		require.NoError(t, watchdog.Start(t.Context()))
		<-loading
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		require.NoError(t, watchdog.Shutdown(ctx))
		select {
		case <-stopped:
		default:
			t.Fatal("Shutdown returned before the source stopped")
		}
	})
}
