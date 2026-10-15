package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestToolProxy_Do_ExternalMCP_AuthRejected(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"initialize", "tools/call"} {
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
			for _, oauth := range []bool{false, true} {
				name := stage + "/" + http.StatusText(status)
				if oauth {
					name += "/oauth"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					backend := newExternalMCPTestServer(t, false)
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, err := io.ReadAll(r.Body)
						if err != nil {
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						r.Body = io.NopCloser(bytes.NewReader(body))
						var request struct {
							Method string `json:"method"`
						}
						_ = json.Unmarshal(body, &request)
						if request.Method == stage {
							w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://upstream.example/private", error_description="secret-placeholder"`)
							http.Error(w, "secret-placeholder", status)
							return
						}
						backend.Config.Handler.ServeHTTP(w, r)
					}))
					t.Cleanup(upstream.Close)
					reader := sdkmetric.NewManualReader()
					proxy := newMetricToolProxy(t, reader)
					plan := externalMCPPlan(upstream.URL)
					plan.RequiresOAuth = oauth
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					response, err := callToolProxy(t, ctx, proxy, NewExternalMCPToolCallPlan(newExternalMCPToolDescriptor(), plan), `{}`)
					require.NoError(t, err)
					require.Equal(t, http.StatusOK, response.Code)
					require.Empty(t, response.Header().Get("WWW-Authenticate"))
					var result struct {
						IsError bool `json:"isError"`
						Content []struct {
							Type string `json:"type"`
							Text string `json:"text"`
						} `json:"content"`
					}
					require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
					require.True(t, result.IsError)
					require.Len(t, result.Content, 1)
					require.Equal(t, "text", result.Content[0].Type)
					require.Contains(t, result.Content[0].Text, "rejected authentication")
					if oauth {
						require.Contains(t, result.Content[0].Text, "Reauthorize this MCP server")
					} else {
						require.Contains(t, result.Content[0].Text, "administrator")
					}
					for _, sensitive := range []string{"secret-placeholder", upstream.URL, "upstream.example"} {
						require.NotContains(t, response.Body.String(), sensitive)
					}
					set := onlyToolCallAttributes(t, reader)
					require.Equal(t, string(toolCallOutcomeToolError), attributeValue(t, set, attr.OutcomeKey))
				})
			}
		}
	}
}

func TestToolProxy_Do_ExternalMCP_NonAuthFailureUnchanged(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "not json")
	}))
	t.Cleanup(upstream.Close)
	reader := sdkmetric.NewManualReader()
	proxy := newMetricToolProxy(t, reader)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	response, err := callToolProxy(t, ctx, proxy, NewExternalMCPToolCallPlan(newExternalMCPToolDescriptor(), externalMCPPlan(upstream.URL)), `{}`)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnexpected, shareable.Code)
	require.Equal(t, "failed to connect to external MCP server", shareable.Error())
	require.Empty(t, response.Body.String())
}
