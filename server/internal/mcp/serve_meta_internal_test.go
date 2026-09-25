package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestMetaServerDiscover_InstructionsAndCacheScope(t *testing.T) {
	t.Parallel()

	for _, gated := range []bool{false, true} {
		for _, instructions := range []pgtype.Text{{}, {Valid: true}, {String: " \t\n", Valid: true}, {String: "Operator instructions", Valid: true}} {
			meta := &metamcprepo.MetaMcpServer{
				Instructions:        instructions,
				UserSessionIssuerID: uuid.NullUUID{UUID: uuid.New(), Valid: gated},
			}
			bs, err := (&Service{}).handleMetaServerDiscover(t.Context(), testenv.NewLogger(t), meta, &metaGateContext{}, &rawRequest{ID: mcpjsonrpc.NumberID(7)})
			require.NoError(t, err)
			var response struct {
				Result struct {
					Instructions string `json:"instructions"`
					CacheScope   string `json:"cacheScope"`
					TTLMs        *int   `json:"ttlMs"`
				} `json:"result"`
			}
			require.NoError(t, json.Unmarshal(bs, &response))
			want := metamcp.Instructions
			if strings.TrimSpace(instructions.String) != "" {
				want = instructions.String
			}
			require.Equal(t, want, response.Result.Instructions)
			scope := "public"
			if gated {
				scope = "private"
			}
			require.Equal(t, scope, response.Result.CacheScope)
			require.NotNil(t, response.Result.TTLMs)
			require.Zero(t, *response.Result.TTLMs)
		}
	}
}
