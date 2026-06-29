package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// singleUpstreamToken collapses the per-remote-issuer token map for the
// single-Authorization remote-MCP backend. These cover the branches that the
// DB-backed resolver tests cannot reach while the one_per_issuer index still
// caps a user_session_issuer at one remote client.

func TestSingleUpstreamToken_EmptyMapReturnsEmpty(t *testing.T) {
	t.Parallel()

	token, err := singleUpstreamToken(nil)
	require.NoError(t, err)
	require.Empty(t, token)
}

func TestSingleUpstreamToken_SingleEntryReturnsToken(t *testing.T) {
	t.Parallel()

	token, err := singleUpstreamToken(map[uuid.UUID]string{uuid.New(): "upstream-token"})
	require.NoError(t, err)
	require.Equal(t, "upstream-token", token)
}

func TestSingleUpstreamToken_MultipleEntriesFailsClosed(t *testing.T) {
	t.Parallel()

	token, err := singleUpstreamToken(map[uuid.UUID]string{
		uuid.New(): "token-a",
		uuid.New(): "token-b",
	})
	require.Error(t, err)
	require.Empty(t, token)
}

func TestTunnelGatewayURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		addr    string
		want    string
		wantErr bool
	}{
		{
			name: "host port defaults to http",
			addr: "10.0.0.5:8090",
			want: "http://10.0.0.5:8090",
		},
		{
			name: "http url preserved",
			addr: "http://tunnel-gateway:8090",
			want: "http://tunnel-gateway:8090",
		},
		{
			name: "https url preserved",
			addr: "https://tunnel-gateway.internal:8443",
			want: "https://tunnel-gateway.internal:8443",
		},
		{
			name:    "missing host rejected",
			addr:    "https:///missing-host",
			wantErr: true,
		},
		{
			name:    "unsupported scheme rejected",
			addr:    "ftp://tunnel-gateway:8090",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tunnelGatewayURL(tt.addr)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
