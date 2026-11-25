package policylifecycle

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestWithoutServer(t *testing.T) {
	t.Parallel()

	dead := uuid.MustParse("00000000-0000-0000-0000-00000000000d")
	live := "00000000-0000-0000-0000-00000000000a"
	deadEntry := `{"mcp_server_id": "` + dead.String() + `"}`

	tests := []struct {
		name    string
		scope   string
		want    string
		outcome scopeAction
	}{
		{
			name:  "keeps other servers and their tools",
			scope: `{"servers": [` + deadEntry + `, {"mcp_server_id": "` + live + `", "tools": ["search"]}], "tool_annotations": ["readOnlyHint"]}`,
			want:  `{"servers": [{"mcp_server_id": "` + live + `", "tools": ["search"]}], "tool_annotations": ["readOnlyHint"]}`,
		},
		{
			name:  "drops every duplicate entry",
			scope: `{"servers": [` + deadEntry + `, ` + deadEntry + `, {"mcp_server_id": "` + live + `"}]}`,
			want:  `{"servers": [{"mcp_server_id": "` + live + `"}]}`,
		},
		{
			name:    "keeps entries it cannot read, and such a scope already matched nothing",
			scope:   `{"servers": [` + deadEntry + `, "opaque", {"mcp_server_id": 7}]}`,
			want:    `{"servers": ["opaque", {"mcp_server_id": 7}]}`,
			outcome: scopeKeep,
		},
		{
			name:    "a scope left naming nothing would apply everywhere",
			scope:   `{"servers": [` + deadEntry + `, ` + deadEntry + `]}`,
			want:    `{"servers": []}`,
			outcome: scopeDelete,
		},
		{
			name:  "json true all_servers stays scoped",
			scope: `{"all_servers": true, "servers": [` + deadEntry + `]}`,
			want:  `{"all_servers": true, "servers": []}`,
		},
		{
			name:    "null all_servers reads as false and would apply everywhere",
			scope:   `{"all_servers": null, "servers": [` + deadEntry + `]}`,
			want:    `{"all_servers": null, "servers": []}`,
			outcome: scopeDelete,
		},
		{
			name:    "string all_servers matched nothing and stays as it was",
			scope:   `{"all_servers": "true", "servers": [` + deadEntry + `]}`,
			want:    `{"all_servers": "true", "servers": []}`,
			outcome: scopeKeep,
		},
		{
			name:    "removing the entry that failed closed must not switch the policy on",
			scope:   `{"all_servers": true, "servers": [{"mcp_server_id": "` + dead.String() + `", "tools": []}]}`,
			want:    `{"all_servers": true, "servers": []}`,
			outcome: scopeKeep,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := withoutServer([]byte(tt.scope), dead)
			require.NoError(t, err)
			require.JSONEq(t, tt.want, string(got))
			require.Equal(t, tt.outcome, scopeOutcome([]byte(tt.scope), got))
		})
	}
}
