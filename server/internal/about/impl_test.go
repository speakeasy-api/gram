package about

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newTestService(t *testing.T, manifestURL string) *Service {
	t.Helper()

	tracerProvider := testenv.NewTracerProvider(t)

	// NewUnsafePolicy: httptest servers listen on 127.0.0.1, which the
	// default guardian policy blocks as a loopback SSRF target. Tests only,
	// never production (see the assets service's setup_test.go for the same
	// pattern).
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	return &Service{
		logger:                 slog.New(slog.NewTextHandler(testWriter{t}, nil)),
		tracer:                 tracerProvider.Tracer("test"),
		guardianPolicy:         guardianPolicy,
		deviceAgentManifestURL: manifestURL,
		deviceAgentReleasesURL: deviceAgentReleasesBaseURL,
	}
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}

func TestHandleInstallDeviceAgentMacOS_RedirectsToVersionedPkg(t *testing.T) {
	t.Parallel()

	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schema_version":1,"latest":{"speakeasyd":{"version":"0.1.18"}}}`))
	}))
	defer manifest.Close()

	svc := newTestService(t, manifest.URL)

	req := httptest.NewRequest(http.MethodGet, installDeviceAgentMacOSPath, nil)
	rec := httptest.NewRecorder()
	// http.Redirect resolves a relative Location against the request URL if
	// it can't parse it as absolute; our client should never receive one, so
	// assert absolute-ness explicitly below rather than relying on that.
	req.URL, _ = url.Parse("https://app.getgram.ai" + installDeviceAgentMacOSPath)

	svc.handleInstallDeviceAgentMacOS(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t,
		fmt.Sprintf("%s/v0.1.18/speakeasy-agent_0.1.18.pkg", deviceAgentReleasesBaseURL),
		rec.Header().Get("Location"),
	)
}

func TestHandleInstallDeviceAgentMacOS_ManifestUnreachable(t *testing.T) {
	t.Parallel()

	// A closed server: connections to it fail immediately.
	manifest := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	manifest.Close()

	svc := newTestService(t, manifest.URL)

	req := httptest.NewRequest(http.MethodGet, installDeviceAgentMacOSPath, nil)
	rec := httptest.NewRecorder()

	svc.handleInstallDeviceAgentMacOS(rec, req)

	require.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestHandleInstallDeviceAgentMacOS_ManifestNon200(t *testing.T) {
	t.Parallel()

	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer manifest.Close()

	svc := newTestService(t, manifest.URL)

	req := httptest.NewRequest(http.MethodGet, installDeviceAgentMacOSPath, nil)
	rec := httptest.NewRecorder()

	svc.handleInstallDeviceAgentMacOS(rec, req)

	require.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestHandleInstallDeviceAgentMacOS_MalformedManifest(t *testing.T) {
	t.Parallel()

	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer manifest.Close()

	svc := newTestService(t, manifest.URL)

	req := httptest.NewRequest(http.MethodGet, installDeviceAgentMacOSPath, nil)
	rec := httptest.NewRecorder()

	svc.handleInstallDeviceAgentMacOS(rec, req)

	require.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestHandleInstallDeviceAgentMacOS_MissingVersion(t *testing.T) {
	t.Parallel()

	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schema_version":1,"latest":{}}`))
	}))
	defer manifest.Close()

	svc := newTestService(t, manifest.URL)

	req := httptest.NewRequest(http.MethodGet, installDeviceAgentMacOSPath, nil)
	rec := httptest.NewRecorder()

	svc.handleInstallDeviceAgentMacOS(rec, req)

	require.Equal(t, http.StatusBadGateway, rec.Code)
}
