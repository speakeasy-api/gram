package mcpregistry

import (
	"context"
	"encoding/json"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/registry_discovery"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
	goa "goa.design/goa/v3/pkg"
)

func TestDiscoveryErrorFormatterPreservesSharedErrors(t *testing.T) {
	t.Parallel()
	for _, code := range []oops.Code{
		oops.CodeUnauthorized, oops.CodeForbidden, oops.CodeBadRequest,
		oops.CodeNotFound, oops.CodeConflict, oops.CodeUnsupportedMedia,
		oops.CodeInvalid, oops.CodeInvariantViolation, oops.CodeUnexpected,
		oops.CodeGatewayError,
	} {
		t.Run(string(code), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			err := oops.C(code).AsGoa(ctx)
			require.Equal(t, goahttp.NewErrorResponse(ctx, err), discoveryErrorFormatter(ctx, err))
		})
	}
}

func TestDiscoveryErrorFormatterSanitizesDecoderErrors(t *testing.T) {
	t.Parallel()
	response := discoveryErrorFormatter(context.Background(), goa.InvalidFieldTypeError("limit", "REJECTED_SECRET", "integer"))
	require.Equal(t, 400, response.StatusCode())
	body, err := json.Marshal(response)
	require.NoError(t, err)
	require.JSONEq(t, `{"error":"invalid discovery request"}`, string(body))
	for _, tc := range []struct {
		name   string
		status int
	}{
		{"discovery_bad_request", 400}, {"discovery_not_found", 404},
	} {
		response := discoveryErrorFormatter(context.Background(), &gen.RegistryDiscoveryError{Name: tc.name, Message: "safe discovery message"})
		require.Equal(t, tc.status, response.StatusCode())
		body, err := json.Marshal(response)
		require.NoError(t, err)
		require.JSONEq(t, `{"error":"safe discovery message"}`, string(body))
	}
}
