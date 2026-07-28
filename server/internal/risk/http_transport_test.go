package risk_test

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	riskclient "github.com/speakeasy-api/gram/server/gen/http/risk/client"
	gen "github.com/speakeasy-api/gram/server/gen/risk"
)

func TestUpdateRiskPolicyHTTPTransport_ExplicitEmptyShadowMCPAllowedURLs(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("PUT", "/rpc/risk.updateRiskPolicy", nil)
	err := riskclient.EncodeUpdateRiskPolicyRequest(goahttp.RequestEncoder)(req, &gen.UpdateRiskPolicyPayload{
		ID:                   "policy-id",
		Name:                 "Shadow MCP",
		ShadowMcpAllowedUrls: []string{},
	})
	require.NoError(t, err)
	encoded, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"id":"policy-id","name":"Shadow MCP","shadow_mcp_allowed_urls":[]}`, string(encoded))
}

func TestUpdateRiskPolicyHTTPTransport_NilShadowMCPAllowedURLs(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("PUT", "/rpc/risk.updateRiskPolicy", nil)
	err := riskclient.EncodeUpdateRiskPolicyRequest(goahttp.RequestEncoder)(req, &gen.UpdateRiskPolicyPayload{
		ID:   "policy-id",
		Name: "Shadow MCP",
	})
	require.NoError(t, err)
	encoded, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"id":"policy-id","name":"Shadow MCP","shadow_mcp_allowed_urls":null}`, string(encoded))
}
