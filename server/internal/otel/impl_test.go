package otel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	otelserver "github.com/speakeasy-api/gram/server/gen/http/otel/server"
	"github.com/stretchr/testify/require"
)

func TestTracesResponseUsesOTLPSuccessStatus(t *testing.T) {
	t.Parallel()

	response := httptest.NewRecorder()
	err := otelserver.EncodeTracesResponse(nil)(t.Context(), response, nil)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code)
}
