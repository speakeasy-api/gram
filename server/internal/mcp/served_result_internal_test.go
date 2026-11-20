package mcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// The results the gateway and hosted paths serialize for refusals and held calls are not successes.
func TestMCPResultSucceeded(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	id := mcpjsonrpc.NumberID(1)
	held, err := marshalToolErrorResult(t.Context(), logger, id, "Approval is needed.", serverInfoHostedToolset)
	require.NoError(t, err)
	refusal, err := marshalCodeAdmissionRefusal(t.Context(), logger, id, newCodeAdmissionRefusal(codemode.ErrCapacity), newCodeExecutionTiming(time.Now(), nil))
	require.NoError(t, err)
	memberError, err := marshalMetaToolError(t.Context(), logger, id, "server failed")
	require.NoError(t, err)

	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{name: "ping", body: `{"jsonrpc":"2.0","id":1,"result":{}}`, want: true},
		{name: "tools/list", body: `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`, want: true},
		{name: "tool result", body: `{"jsonrpc":"2.0","id":1,"result":{"content":[],"isError":false}}`, want: true},
		{name: "null error", body: `{"jsonrpc":"2.0","id":1,"error":null,"result":{}}`, want: true},
		{name: "json-rpc error", body: `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"nope"}}`, want: false},
		{name: "tool error", body: `{"jsonrpc":"2.0","id":1,"result":{"content":[],"isError":true}}`, want: false},
		{name: "held for approval", body: string(held), want: false},
		{name: "code mode admission refusal", body: string(refusal), want: false},
		{name: "member error", body: string(memberError), want: false},
		{name: "batch", body: `[{"jsonrpc":"2.0","id":1,"result":{}}]`, want: false},
		{name: "no result", body: `{"jsonrpc":"2.0","id":1}`, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, mcpResultSucceeded(json.RawMessage(tc.body)))
		})
	}
}
