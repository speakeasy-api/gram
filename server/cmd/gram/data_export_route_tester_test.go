package gram

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Every registered data export source, and every signal it lists, must send a
// test event through the production tester. A new source or signal fails here
// until it has one.
func TestDataExportRouteTesterCoversEverySourceAndSignal(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	// Unsafe so the test can reach an httptest server on loopback.
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	tester, err := newDataExportRouteTester(testenv.NewLogger(t), testenv.NewMeterProvider(t), policy)
	require.NoError(t, err)

	sources := dataexports.Sources()
	require.NotEmpty(t, sources)
	for _, source := range sources {
		require.NotEmpty(t, source.Signals, source.Name)
		mu.Lock()
		paths = nil
		mu.Unlock()

		results, err := tester.SendTest(t.Context(), source.Name, dataexports.TestDestination{
			OrganizationID:       "org-route-tester",
			ProjectID:            uuid.MustParse("01900000-0000-7000-8000-000000000303"),
			EndpointURL:          server.URL,
			Headers:              nil,
			IncludeSensitiveData: false,
		}, uuid.NewString())
		require.NoError(t, err, source.Name)

		want := make([]string, len(source.Signals))
		require.Len(t, results, len(source.Signals), source.Name)
		for i, signal := range source.Signals {
			require.Equal(t, dataexports.SignalTestResult{Signal: signal, StatusCode: http.StatusOK, Unreachable: false}, results[i], source.Name)
			want[i] = "/v1/" + string(signal)
		}
		mu.Lock()
		got := slices.Sorted(slices.Values(paths))
		mu.Unlock()
		require.Equal(t, slices.Sorted(slices.Values(want)), got, source.Name)
	}
}
