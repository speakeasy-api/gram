package externalmcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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

	challengedHeader := make(http.Header)
	challengedHeader.Set("WWW-Authenticate", "Bearer resource_metadata=\"https://mcp.example.test/.well-known/oauth-protected-resource\"")
	responses := []*http.Response{
		{
			StatusCode: http.StatusUnauthorized,
			Header:     challengedHeader,
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

func TestAuthRoundTripperParameterGatingUsesSDKHeaders(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, method, version, configuredMethod, configuredVersion, want string
	}{
		{"legacy-call-cannot-be-upgraded", "", "2025-11-25", "tools/call", "2026-07-28", ""},
		{"old-protocol-cannot-be-upgraded", "tools/call", "2025-11-25", "tools/call", "2026-07-28", ""},
		{"non-call-cannot-be-reclassified", "notifications/cancelled", "2026-07-28", "tools/call", "2026-07-28", ""},
		{"call-cannot-be-reclassified", "tools/call", "2026-07-28", "tools/list", "2026-07-28", "selected"},
		{"call-cannot-be-downgraded", "tools/call", "2026-07-28", "tools/call", "2025-11-25", "selected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// SDK notifications may inherit the calling context. Eligibility must come
			// from SDK headers, not from either that context or configured overrides.
			headers := make(http.Header)
			headers.Set("Mcp-Param-Account", "selected")
			ctx := context.WithValue(t.Context(), toolCallContextKey{}, headers)
			req := httptest.NewRequest(http.MethodPost, "https://mcp.example.test", nil).WithContext(ctx)
			req.Header.Set(headerMCPMethod, tc.method)
			req.Header.Set("Mcp-Protocol-Version", tc.version)
			rt := &authRoundTripper{
				headers: map[string]string{headerMCPMethod: tc.configuredMethod, "Mcp-Protocol-Version": tc.configuredVersion},
				base: roundTripperFunc(func(out *http.Request) (*http.Response, error) {
					require.Equal(t, tc.want, out.Header.Get("Mcp-Param-Account"))
					// Parameter gating does not change configured protocol-header precedence.
					require.Equal(t, tc.configuredMethod, out.Header.Get(headerMCPMethod))
					require.Equal(t, tc.configuredVersion, out.Header.Get("Mcp-Protocol-Version"))
					return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
				}),
			}
			resp, err := rt.RoundTrip(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Empty(t, req.Header.Get("Mcp-Param-Account"), "the original request must remain unchanged")
		})
	}
}

func TestAuthRoundTripperReplacesSDKParameters(t *testing.T) {
	t.Parallel()
	for _, annotated := range []bool{true, false} {
		t.Run(strconv.FormatBool(annotated), func(t *testing.T) {
			t.Parallel()
			var headers http.Header
			if annotated {
				headers = make(http.Header)
				headers.Set("Mcp-Param-Account", "=?base64??=")
			}
			ctx := context.WithValue(t.Context(), toolCallContextKey{}, headers)
			req := httptest.NewRequest(http.MethodPost, "https://mcp.example.test", nil).WithContext(ctx)
			req.Header.Set(headerMCPMethod, "tools/call")
			req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
			req.Header.Set("Mcp-Param-Account", "")
			req.Header.Set("Mcp-Param-Optional", "incorrect-sdk-value")
			req.Header.Set("Mcp-Param-Dotted", "incorrect-sdk-value")
			rt := &authRoundTripper{base: roundTripperFunc(func(out *http.Request) (*http.Response, error) {
				require.Empty(t, out.Header.Values("Mcp-Param-Optional"))
				require.Empty(t, out.Header.Values("Mcp-Param-Dotted"))
				if annotated {
					require.Equal(t, []string{"=?base64??="}, out.Header.Values("Mcp-Param-Account"))
				} else {
					require.Empty(t, out.Header.Values("Mcp-Param-Account"))
				}
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
			})}
			resp, err := rt.RoundTrip(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, "incorrect-sdk-value", req.Header.Get("Mcp-Param-Optional"))
		})
	}
}
