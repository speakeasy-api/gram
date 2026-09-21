package oktaresourceconnections_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/http/okta_resource_connections/server"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestUnavailableHTTPResponse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	endpoint := middleware.MapErrors()(func(context.Context, any) (any, error) {
		return nil, oops.E(oops.CodeUnavailable, errors.New("feature flag backend unavailable"), "okta connections availability could not be determined")
	})
	_, err := endpoint(ctx, nil)
	require.Error(t, err)

	for name, encode := range map[string]func(context.Context, http.ResponseWriter, error) error{
		"confirm": gen.EncodeConfirmError(goahttp.ResponseEncoder, nil),
		"reset":   gen.EncodeResetError(goahttp.ResponseEncoder, nil),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			require.NoError(t, encode(ctx, response, err))
			require.Equal(t, http.StatusServiceUnavailable, response.Code)
			require.Equal(t, "unavailable", response.Header().Get("goa-error"))
			require.Contains(t, response.Header().Get("Content-Type"), "application/json")
		})
	}
}
