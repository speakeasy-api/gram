package gram

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/pki"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func pkiTestPEM(t *testing.T, before, after time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: before, NotAfter: after}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Headers: nil, Bytes: der})
}

func TestPKIObservationFailureAndRotation(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	expired := pkiTestPEM(t, now.Add(-2*time.Hour), now.Add(-time.Hour))
	future := pkiTestPEM(t, now.Add(time.Hour), now.Add(2*time.Hour))
	path := filepath.Join(t.TempDir(), "bundle.pem")
	require.NoError(t, os.WriteFile(path, append(expired, future...), 0600))
	t.Setenv("PKI_TEST_CERT", string(future))
	sources, err := pkiSources([]string{"bundle=" + path, "missing=" + path + ".missing"}, []string{"env=PKI_TEST_CERT"})
	require.NoError(t, err)
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer o11y.NoLogDefer(func() error { return provider.Shutdown(t.Context()) })
	watchdog, err := pki.NewWatchDog(testenv.NewLogger(t), provider, append(sources, pki.WithObservationInterval(10*time.Millisecond))...)
	require.NoError(t, err)
	require.NoError(t, watchdog.Start(t.Context()))
	defer o11y.NoLogDefer(func() error { return watchdog.Shutdown(t.Context()) })
	collect := func() map[string]map[string]float64 {
		var data metricdata.ResourceMetrics
		require.NoError(t, reader.Collect(t.Context(), &data))
		values := make(map[string]map[string]float64)
		for _, scope := range data.ScopeMetrics {
			for _, m := range scope.Metrics {
				values[m.Name] = make(map[string]float64)
				switch gauge := m.Data.(type) {
				case metricdata.Gauge[float64]:
					for _, point := range gauge.DataPoints {
						name, _ := point.Attributes.Value(attribute.Key("pki.source.name"))
						index, _ := point.Attributes.Value(attribute.Key("pki.certificate.index"))
						values[m.Name][name.AsString()+"/"+index.Emit()] = point.Value
					}
				case metricdata.Gauge[int64]:
					for _, point := range gauge.DataPoints {
						name, _ := point.Attributes.Value(attribute.Key("pki.source.name"))
						values[m.Name][name.AsString()] = float64(point.Value)
					}
				}
			}
		}
		return values
	}
	var values map[string]map[string]float64
	awaitSuccess := func(expected map[string]float64) {
		t.Helper()
		require.Eventually(t, func() bool {
			values = collect()
			return reflect.DeepEqual(expected, values["pki.source.observation_success"])
		}, time.Second, time.Millisecond)
	}
	awaitSuccess(map[string]float64{"bundle": 1, "missing": 0, "env": 1})
	require.Negative(t, values["pki.certificate.remaining_validity"]["bundle/0"])
	require.Negative(t, values["pki.certificate.age"]["bundle/1"])
	require.NoError(t, os.WriteFile(path, append(future, []byte("malformed")...), 0600))
	awaitSuccess(map[string]float64{"bundle": 0, "missing": 0, "env": 1})
	require.NotContains(t, values["pki.certificate.age"], "bundle/0")
	require.Positive(t, values["pki.certificate.remaining_validity"]["env/0"])
	replacement := path + ".new"
	require.NoError(t, os.WriteFile(replacement, future, 0600))
	require.NoError(t, os.Rename(replacement, path))
	awaitSuccess(map[string]float64{"bundle": 1, "missing": 0, "env": 1})
	require.Positive(t, values["pki.certificate.remaining_validity"]["bundle/0"])
	require.NotContains(t, values["pki.certificate.age"], "bundle/1")
}

func TestPKIBundleRejectsSkippedMalformedBlockAndOverflow(t *testing.T) {
	now := time.Now()
	valid := string(pkiTestPEM(t, now, now.Add(time.Hour)))
	source := pkiSource{location: "PKI_TEST_BUNDLE", environment: true}
	t.Setenv(source.location, "-----BEGIN CERTIFICATE-----\ninvalid\n"+valid)
	certs, err := readPKICertificates(t.Context(), source)
	require.Error(t, err)
	require.Nil(t, certs)
	t.Setenv(source.location, strings.Repeat(valid, pki.MaxCertificates+1))
	certs, err = readPKICertificates(t.Context(), source)
	require.Error(t, err)
	require.Nil(t, certs)
}
