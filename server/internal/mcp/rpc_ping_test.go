package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestHandlePing_IncludesEmptyResultObject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := testenv.NewLogger(t)

	bs, err := handlePing(ctx, logger, msgID{format: 1, Number: 42})
	require.NoError(t, err)

	// MCP/JSON-RPC require the result field be present even when empty.
	// Cursor's zod schema rejects responses missing result/error/method.
	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(bs, &decoded))
	require.Contains(t, decoded, "result")
	require.JSONEq(t, `{}`, string(decoded["result"]))
	require.JSONEq(t, `42`, string(decoded["id"]))
	require.JSONEq(t, `"2.0"`, string(decoded["jsonrpc"]))
}
