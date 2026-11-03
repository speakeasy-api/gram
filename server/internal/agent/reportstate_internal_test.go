package agent

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLimitStateReportBody(t *testing.T) {
	t.Parallel()

	var readErr error
	handler := limitStateReportBody(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))

	small := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/rpc/agent.reportState", strings.NewReader("{}"))
	handler.ServeHTTP(httptest.NewRecorder(), small)
	require.NoError(t, readErr)

	big := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/rpc/agent.reportState", strings.NewReader(strings.Repeat("x", maxStateReportBytes+1)))
	handler.ServeHTTP(httptest.NewRecorder(), big)
	var tooLarge *http.MaxBytesError
	require.ErrorAs(t, readErr, &tooLarge)
}
