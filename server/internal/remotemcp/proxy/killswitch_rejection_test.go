package proxy

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"
)

func TestKillswitchMatchRejectionEnvelope(t *testing.T) {
	t.Parallel()
	id, err := jsonrpc.MakeID(float64(42))
	require.NoError(t, err)
	payload, err := marshalErrorResponse(id, NewKillswitchMatchRejection("  Exact external note.  "))
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(payload, &got))
	require.Equal(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(42),
		"error": map[string]any{
			"code":    float64(RejectCodeForbidden),
			"message": "  Exact external note.  ",
			"data": map[string]any{
				"code": KillswitchRejectionCode,
			},
		},
	}, got)
}

func TestKillswitchInfrastructureRejectionHasNoMatchLanguage(t *testing.T) {
	t.Parallel()
	rejection := NewKillswitchInfrastructureRejection()
	require.Equal(t, RejectCodeInternalError, rejection.Code)
	require.Equal(t, "service temporarily unavailable", rejection.Message)
	require.Nil(t, rejection.Data)
	require.NotContains(t, rejection.Message, KillswitchRejectionCode)
}
