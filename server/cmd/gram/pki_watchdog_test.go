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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/o11y"
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
	watchdog := &pkiWatchdog{mu: sync.RWMutex{}, sources: sources, observations: nil}
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer o11y.NoLogDefer(func() error { return provider.Shutdown(t.Context()) })
	registration, err := watchdog.register(provider)
	require.NoError(t, err)
	defer o11y.NoLogDefer(registration.Unregister)
	collect := func() map[string]map[string]float64 {
		watchdog.observe(t.Context(), testenv.NewLogger(t))
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
	values := collect()
	require.Negative(t, values["pki.certificate.remaining_validity"]["bundle/0"])
	require.Negative(t, values["pki.certificate.age"]["bundle/1"])
	require.Equal(t, map[string]float64{"bundle": 1, "missing": 0, "env": 1}, values["pki.source.observation_success"])
	require.NoError(t, os.WriteFile(path, append(future, []byte("malformed")...), 0600))
	values = collect()
	require.Equal(t, map[string]float64{"bundle": 0, "missing": 0, "env": 1}, values["pki.source.observation_success"])
	require.NotContains(t, values["pki.certificate.age"], "bundle/0")
	require.Positive(t, values["pki.certificate.remaining_validity"]["env/0"])
	replacement := path + ".new"
	require.NoError(t, os.WriteFile(replacement, future, 0600))
	require.NoError(t, os.Rename(replacement, path))
	values = collect()
	require.Equal(t, map[string]float64{"bundle": 1, "missing": 0, "env": 1}, values["pki.source.observation_success"])
	require.Positive(t, values["pki.certificate.remaining_validity"]["bundle/0"])
	require.NotContains(t, values["pki.certificate.age"], "bundle/1")
}

func TestPKIBundleRejectsSkippedMalformedBlockAndOverflow(t *testing.T) {
	now := time.Now()
	valid := string(pkiTestPEM(t, now, now.Add(time.Hour)))
	source := pkiSource{name: "bundle", location: "PKI_TEST_BUNDLE", environment: true}
	t.Setenv(source.location, "-----BEGIN CERTIFICATE-----\ninvalid\n"+valid)
	certs, err := readPKICertificates(source)
	require.Error(t, err)
	require.Nil(t, certs)
	t.Setenv(source.location, strings.Repeat(valid, pkiMaxCertificates+1))
	certs, err = readPKICertificates(source)
	require.Error(t, err)
	require.Nil(t, certs)
}
