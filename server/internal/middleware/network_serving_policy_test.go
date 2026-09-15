package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/networkaccess"
)

func TestNetworkServingPolicyVersion(t *testing.T) {
	t.Parallel()

	handler := NetworkServingPolicyVersion(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, strconv.Itoa(networkaccess.ServingPolicyVersion), rec.Header().Get(networkaccess.ServingPolicyVersionHeader))
}
