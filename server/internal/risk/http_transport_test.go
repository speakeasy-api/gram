package risk_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	riskserver "github.com/speakeasy-api/gram/server/gen/http/risk/server"
)

func TestUpdateRiskPolicyHTTPTransport_ExplicitEmptyShadowMCPAllowedURLs(t *testing.T) {
	t.Parallel()

	body := new(riskserver.UpdateRiskPolicyRequestBody)
	body.ShadowMcpAllowedUrls = []string{}

	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	require.JSONEq(t, `{"shadow_mcp_allowed_urls":[]}`, string(encoded))
}

func TestUpdateRiskPolicyHTTPTransport_NilShadowMCPAllowedURLs(t *testing.T) {
	t.Parallel()

	body := new(riskserver.UpdateRiskPolicyRequestBody)

	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	require.JSONEq(t, `{"shadow_mcp_allowed_urls":null}`, string(encoded))
}
