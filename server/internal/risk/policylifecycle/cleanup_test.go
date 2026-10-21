package policylifecycle

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
)

func TestWithoutServer(t *testing.T) {
	t.Parallel()

	dead := uuid.MustParse("00000000-0000-0000-0000-00000000000d")
	live := "00000000-0000-0000-0000-00000000000a"
	deadEntry := `{"mcp_server_id": "` + dead.String() + `"}`

	tests := []struct {
		name     string
		scope    string
		want     string
		unscoped bool
	}{
		{
			name:  "keeps other servers and their tools",
			scope: `{"servers": [` + deadEntry + `, {"mcp_server_id": "` + live + `", "tools": ["search"]}], "tool_annotations": ["read_only"]}`,
			want:  `{"servers": [{"mcp_server_id": "` + live + `", "tools": ["search"]}], "tool_annotations": ["read_only"]}`,
		},
		{
			name:  "drops every duplicate entry",
			scope: `{"servers": [` + deadEntry + `, ` + deadEntry + `, {"mcp_server_id": "` + live + `"}]}`,
			want:  `{"servers": [{"mcp_server_id": "` + live + `"}]}`,
		},
		{
			name:  "keeps entries it cannot read",
			scope: `{"servers": [` + deadEntry + `, "opaque", {"mcp_server_id": 7}]}`,
			want:  `{"servers": ["opaque", {"mcp_server_id": 7}]}`,
		},
		{
			name:     "a scope left naming nothing would apply everywhere",
			scope:    `{"servers": [` + deadEntry + `, ` + deadEntry + `]}`,
			want:     `{"servers": []}`,
			unscoped: true,
		},
		{
			name:  "json true all_servers stays scoped",
			scope: `{"all_servers": true, "servers": [` + deadEntry + `]}`,
			want:  `{"all_servers": true, "servers": []}`,
		},
		{
			name:     "null all_servers reads as false and would apply everywhere",
			scope:    `{"all_servers": null, "servers": [` + deadEntry + `]}`,
			want:     `{"all_servers": null, "servers": []}`,
			unscoped: true,
		},
		{
			name:  "string all_servers still fails closed",
			scope: `{"all_servers": "true", "servers": [` + deadEntry + `]}`,
			want:  `{"all_servers": "true", "servers": []}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := withoutServer([]byte(tt.scope), dead)
			require.NoError(t, err)
			require.JSONEq(t, tt.want, string(got))
			require.Equal(t, tt.unscoped, policycore.DecodeMCPScope(got) == nil)
		})
	}
}
