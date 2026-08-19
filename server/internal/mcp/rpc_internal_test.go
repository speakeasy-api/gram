package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
)

func TestSpliceResultProtocolFields_ObjectGainsBothFields(t *testing.T) {
	t.Parallel()

	out, err := spliceResultProtocolFields([]byte(`{}`), serverInfoHostedToolset)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"resultType": "complete",
		"_meta": {"io.modelcontextprotocol/serverInfo": {"name": "Gram", "version": "0.0.0"}}
	}`, string(out))
}

func TestSpliceResultProtocolFields_PreservesExistingResultType(t *testing.T) {
	t.Parallel()

	out, err := spliceResultProtocolFields([]byte(`{"resultType":"input_required"}`), serverInfoHostedToolset)
	require.NoError(t, err)

	// The upstream resultType survives while serverInfo is still injected;
	// the two fields are filled independently.
	require.JSONEq(t, `{
		"resultType": "input_required",
		"_meta": {"io.modelcontextprotocol/serverInfo": {"name": "Gram", "version": "0.0.0"}}
	}`, string(out))
}

func TestSpliceResultProtocolFields_NullMetaTreatedAsAbsent(t *testing.T) {
	t.Parallel()

	out, err := spliceResultProtocolFields([]byte(`{"_meta":null}`), serverInfoHostedToolset)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"resultType": "complete",
		"_meta": {"io.modelcontextprotocol/serverInfo": {"name": "Gram", "version": "0.0.0"}}
	}`, string(out))
}

func TestSpliceResultProtocolFields_PreservesUpstreamServerInfo(t *testing.T) {
	t.Parallel()

	in := `{"_meta":{"io.modelcontextprotocol/serverInfo":{"name":"Upstream","version":"1.2.3"}}}`
	out, err := spliceResultProtocolFields([]byte(in), serverInfoHostedToolset)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"resultType": "complete",
		"_meta": {"io.modelcontextprotocol/serverInfo": {"name": "Upstream", "version": "1.2.3"}}
	}`, string(out))
}

func TestSpliceResultProtocolFields_MergesIntoExistingMeta(t *testing.T) {
	t.Parallel()

	in := `{"_meta":{"com.example/key":"kept"},"content":[]}`
	out, err := spliceResultProtocolFields([]byte(in), serverInfoHostedToolset)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"resultType": "complete",
		"content": [],
		"_meta": {
			"com.example/key": "kept",
			"io.modelcontextprotocol/serverInfo": {"name": "Gram", "version": "0.0.0"}
		}
	}`, string(out))
}

func TestSpliceResultProtocolFields_NonObjectMetaLeftAlone(t *testing.T) {
	t.Parallel()

	out, err := spliceResultProtocolFields([]byte(`{"_meta":"not-an-object"}`), serverInfoHostedToolset)
	require.NoError(t, err)
	require.JSONEq(t, `{"resultType":"complete","_meta":"not-an-object"}`, string(out))
}

func TestSpliceResultProtocolFields_NonObjectResultUnchanged(t *testing.T) {
	t.Parallel()

	for _, in := range []string{`"a string"`, `[1,2,3]`, `null`, `42`} {
		out, err := spliceResultProtocolFields([]byte(in), serverInfoHostedToolset)
		require.NoError(t, err)
		require.Equal(t, in, string(out))
	}
}

func TestSpliceResultProtocolFields_PreservesValueContent(t *testing.T) {
	t.Parallel()

	// Values are re-emitted as raw bytes, so content that float64 decoding
	// would corrupt — like big numbers — survives the splice intact.
	in := `{"content":[{"text":"a","n":123456789012345678901234567890}]}`
	out, err := spliceResultProtocolFields([]byte(in), serverInfoHostedToolset)
	require.NoError(t, err)
	require.Contains(t, string(out), `123456789012345678901234567890`)
	require.Contains(t, string(out), `[{"text":"a","n":123456789012345678901234567890}]`)
}

func TestResultMarshal_DefaultsToHostedIdentity(t *testing.T) {
	t.Parallel()

	bs, err := json.Marshal(result[struct{}]{
		ID:     mcpjsonrpc.NumberID(7),
		Result: struct{}{},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
		"jsonrpc": "2.0",
		"id": 7,
		"result": {
			"resultType": "complete",
			"_meta": {"io.modelcontextprotocol/serverInfo": {"name": "Gram", "version": "0.0.0"}}
		}
	}`, string(bs))
}

func TestResultMarshal_PlatformIdentity(t *testing.T) {
	t.Parallel()

	bs, err := json.Marshal(result[struct{}]{
		ID:             mcpjsonrpc.NumberID(7),
		Result:         struct{}{},
		serverIdentity: serverInfoPlatformToolset,
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
		"jsonrpc": "2.0",
		"id": 7,
		"result": {
			"resultType": "complete",
			"_meta": {"io.modelcontextprotocol/serverInfo": {"name": "Gram Platform Toolset", "version": "0.0.0"}}
		}
	}`, string(bs))
}

func TestResultMarshal_RoundTripDropsInjectedFields(t *testing.T) {
	t.Parallel()

	// The envelope's UnmarshalJSON decodes only the typed result, so
	// marshal -> unmarshal -> marshal is stable rather than lossless: the
	// injected fields are dropped on decode and re-injected on encode.
	type payload struct {
		Key string `json:"key"`
	}
	original := result[payload]{
		ID:     mcpjsonrpc.NumberID(1),
		Result: payload{Key: "value"},
	}
	bs, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded result[payload]
	require.NoError(t, json.Unmarshal(bs, &decoded))
	require.Equal(t, original.Result, decoded.Result)

	again, err := json.Marshal(decoded)
	require.NoError(t, err)
	require.JSONEq(t, string(bs), string(again))
}
