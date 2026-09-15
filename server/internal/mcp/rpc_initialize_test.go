package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	metadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestParseInitializeParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		raw              string
		wantValid        bool
		wantProtocol     string
		wantClientName   string
		wantClientVer    string
		wantCapabilities []string
	}{
		{
			name:             "empty params",
			raw:              "",
			wantValid:        true,
			wantCapabilities: nil,
		},
		{
			name:             "full params with sorted capabilities",
			raw:              `{"protocolVersion":"2025-03-26","clientInfo":{"name":"cursor","version":"1.2.3"},"capabilities":{"tools":{},"roots":{},"sampling":{}}}`,
			wantValid:        true,
			wantProtocol:     "2025-03-26",
			wantClientName:   "cursor",
			wantClientVer:    "1.2.3",
			wantCapabilities: []string{"roots", "sampling", "tools"},
		},
		{
			name:             "no capabilities",
			raw:              `{"protocolVersion":"2025-03-26","clientInfo":{"name":"claude","version":"1.0.0"}}`,
			wantValid:        true,
			wantProtocol:     "2025-03-26",
			wantClientName:   "claude",
			wantClientVer:    "1.0.0",
			wantCapabilities: nil,
		},
		{
			name:      "malformed params (array instead of object)",
			raw:       `["not","an","object"]`,
			wantValid: false,
		},
		{
			name:      "malformed params (invalid json)",
			raw:       `{not json`,
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			params, capabilities, err := parseInitializeParams(json.RawMessage(tt.raw))
			if !tt.wantValid {
				require.Error(t, err)
				require.Empty(t, capabilities)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantProtocol, params.ProtocolVersion)
			require.Equal(t, tt.wantClientName, params.ClientInfo.Name)
			require.Equal(t, tt.wantClientVer, params.ClientInfo.Version)
			require.Equal(t, tt.wantCapabilities, capabilities)
		})
	}
}

// TestParseInitializeParams_CapabilitiesAlwaysSorted guards against map
// iteration order leaking into the recorded capability list.
func TestParseInitializeParams_CapabilitiesAlwaysSorted(t *testing.T) {
	t.Parallel()

	raw := `{"capabilities":{"zeta":{},"alpha":{},"mu":{},"beta":{}}}`
	for range 20 {
		_, capabilities, err := parseInitializeParams(json.RawMessage(raw))
		require.NoError(t, err)
		require.Equal(t, []string{"alpha", "beta", "mu", "zeta"}, capabilities)
	}
}

func TestParseInitializeParams_DeepClientNameNotTruncatedAtParse(t *testing.T) {
	t.Parallel()

	// Truncation happens at capture time via conv.TruncateString; parsing
	// itself preserves the raw client identity.
	long := strings.Repeat("x", 250)
	raw := `{"clientInfo":{"name":"` + long + `","version":"1.0.0"}}`
	params, _, err := parseInitializeParams(json.RawMessage(raw))
	require.NoError(t, err)
	require.Len(t, params.ClientInfo.Name, 250)
}

// failingDBTX satisfies the sqlc DBTX interfaces with a stub that fails every
// operation, for exercising handlers whose database reads are best-effort.
type failingDBTX struct{}

var errNoDatabaseInTest = errors.New("no database in this test")

func (failingDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errNoDatabaseInTest
}

func (failingDBTX) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errNoDatabaseInTest
}

func (failingDBTX) QueryRow(context.Context, string, ...any) pgx.Row {
	return failingRow{}
}

func (failingDBTX) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errNoDatabaseInTest
}

type failingRow struct{}

func (failingRow) Scan(...any) error { return errNoDatabaseInTest }

// TestHandleInitialize_WritesNegotiatedVersionBackIntoPayload pins the hosted
// write-back: after negotiation the payload's resolution must carry the
// negotiated revision rather than the provisional entry-time default, because
// consumers downstream of dispatch read the in-effect revision from there.
// The failing repositories exercise the documented tolerance of the
// instructions lookup, which must not affect the handshake.
func TestHandleInitialize_WritesNegotiatedVersionBackIntoPayload(t *testing.T) {
	t.Parallel()

	store, payload := newClientIdentityFixture(t)
	require.Equal(t, mcpversions.DefaultInEffect, payload.protocolVersion.InEffect)

	rawParams, err := json.Marshal(map[string]any{
		"protocolVersion": mcpversions.Version20251125,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
	})
	require.NoError(t, err)

	req := &rawRequest{
		JSONRPC: "2.0",
		ID:      mcpjsonrpc.NumberID(1),
		Method:  "initialize",
		Params:  rawParams,
	}

	body, err := handleInitialize(t.Context(), testenv.NewLogger(t), nil, req, payload, nil, toolsets_repo.New(failingDBTX{}), metadata_repo.New(failingDBTX{}), store)
	require.NoError(t, err)

	require.Equal(t, mcpversions.Version20251125, payload.protocolVersion.InEffect)

	var response struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &response), "response body: %s", body)
	require.Equal(t, mcpversions.Version20251125, response.Result.ProtocolVersion)
}
