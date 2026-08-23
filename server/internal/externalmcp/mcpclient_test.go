package externalmcp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestAuthRoundTripperRetainsPriorOAuthChallenge(t *testing.T) {
	t.Parallel()

	responses := []*http.Response{
		{
			StatusCode: http.StatusUnauthorized,
			Header:     http.Header{"WWW-Authenticate": []string{"Bearer resource_metadata=\"https://mcp.example.test/.well-known/oauth-protected-resource\""}},
			Body:       io.NopCloser(strings.NewReader("")),
		},
		{
			StatusCode: http.StatusForbidden,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
		},
	}
	rt := &authRoundTripper{base: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		response := responses[0]
		responses = responses[1:]
		return response, nil
	})}

	first := httptest.NewRequest(http.MethodGet, "https://mcp.example.test", nil)
	response, err := rt.RoundTrip(first)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	second := httptest.NewRequest(http.MethodGet, "https://mcp.example.test", nil)
	response, err = rt.RoundTrip(second)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	require.True(t, rt.authRejected)
	require.Equal(t, http.StatusForbidden, rt.statusCode)
	require.Equal(t, "Bearer resource_metadata=\"https://mcp.example.test/.well-known/oauth-protected-resource\"", rt.wwwAuthenticate)
}
