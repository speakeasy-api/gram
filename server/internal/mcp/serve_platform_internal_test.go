package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// runPlatformInitialize runs handlePlatformInitialize for a handshake requesting
// the given version (empty omits the field) and returns the answered
// protocolVersion plus the resolution after the handler's write-back.
func runPlatformInitialize(t *testing.T, requested string) (string, mcpversions.Resolution) {
	t.Helper()

	params := map[string]any{
		"capabilities": map[string]any{},
		"clientInfo":   map[string]any{"name": "test-client", "version": "1.0.0"},
	}
	if requested != "" {
		params["protocolVersion"] = requested
	}
	rawParams, err := json.Marshal(params)
	require.NoError(t, err)

	req := &rawRequest{
		JSONRPC: "2.0",
		ID:      mcpjsonrpc.NumberID(1),
		Method:  "initialize",
		Params:  rawParams,
	}

	// A conforming handshake request declares no version, so entry-time
	// resolution hands the handler the default.
	resolution := mcpversions.Resolve("", mcpversions.SupportedPlatformToolset())

	body, err := handlePlatformInitialize(t.Context(), testenv.NewLogger(t), nil, req, &resolution)
	require.NoError(t, err)

	var response struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &response), "response body: %s", body)

	return response.Result.ProtocolVersion, resolution
}

func TestHandlePlatformInitialize_EchoesEverySupportedVersion(t *testing.T) {
	t.Parallel()

	for _, v := range mcpversions.SupportedPlatformToolset() {
		answered, resolution := runPlatformInitialize(t, v)
		require.Equal(t, v, answered, "a supported requested version must be echoed")
		require.Equal(t, v, resolution.InEffect, "the negotiated answer must be written back into the resolution")
	}
}

func TestHandlePlatformInitialize_AnswersUnsupportedVersionWithNewestSupported(t *testing.T) {
	t.Parallel()

	supported := mcpversions.SupportedPlatformToolset()
	newest := supported[len(supported)-1]

	answered, resolution := runPlatformInitialize(t, mcpversions.Version20260728)
	require.Equal(t, newest, answered)
	require.Equal(t, newest, resolution.InEffect)
}

// TestHandlePlatformInitialize_AnswersAbsentVersionWithDefault pins that the
// no-version cohort is not handed the ceiling: a handshake naming no revision
// is answered the spec's unversioned default.
func TestHandlePlatformInitialize_AnswersAbsentVersionWithDefault(t *testing.T) {
	t.Parallel()

	answered, resolution := runPlatformInitialize(t, "")
	require.Equal(t, mcpversions.DefaultInEffect, answered)
	require.Equal(t, mcpversions.DefaultInEffect, resolution.InEffect)
}

// TestHandlePlatformInitialize_MalformedParamsNegotiateTheDefault mirrors the
// malformed-params guarantee on the hosted surface: a params shape that fails
// to decode must not fail the handshake, and negotiation proceeds as if the
// client requested nothing.
func TestHandlePlatformInitialize_MalformedParamsNegotiateTheDefault(t *testing.T) {
	t.Parallel()

	req := &rawRequest{
		JSONRPC: "2.0",
		ID:      mcpjsonrpc.NumberID(1),
		Method:  "initialize",
		Params:  json.RawMessage(`["unexpected"]`),
	}
	resolution := mcpversions.Resolve("", mcpversions.SupportedPlatformToolset())

	body, err := handlePlatformInitialize(t.Context(), testenv.NewLogger(t), nil, req, &resolution)
	require.NoError(t, err)

	var response struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &response), "response body: %s", body)
	require.Equal(t, mcpversions.DefaultInEffect, response.Result.ProtocolVersion)
	require.Equal(t, mcpversions.DefaultInEffect, resolution.InEffect)
}
