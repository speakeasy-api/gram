package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestHandleResourcesTemplatesList_ReturnsEmptyList(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)

	bs, err := handleResourcesTemplatesList(ctx, logger, &rawRequest{
		JSONRPC: "2.0",
		ID:      mcpjsonrpc.NumberID(7),
		Method:  "resources/templates/list",
		Params:  nil,
	})
	require.NoError(t, err)

	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(bs, &decoded))
	require.JSONEq(t, `7`, string(decoded["id"]))
	require.JSONEq(t, `"2.0"`, string(decoded["jsonrpc"]))
	require.JSONEq(t, `{
		"resourceTemplates": [],
		"resultType": "complete",
		"_meta": {
			"io.modelcontextprotocol/serverInfo": {
				"name": "Gram",
				"version": "0.0.0"
			}
		}
	}`, string(decoded["result"]))
}
