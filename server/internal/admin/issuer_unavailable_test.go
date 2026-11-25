package admin

import (
	"context"
	adminserver "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIssuerUnavailableResponse(t *testing.T) {
	t.Parallel()
	encode := adminserver.EncodeCreateGlobalIssuerError(goahttp.ResponseEncoder, nil)
	response := httptest.NewRecorder()
	err := encode(context.Background(), response, oops.C(oops.CodeUnavailable).AsGoa(context.Background()))
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
}
