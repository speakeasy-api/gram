package mcpservers

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

func TestResolveAuthorizationMode(t *testing.T) {
	t.Parallel()

	set := func() uuid.NullUUID { return uuid.NullUUID{UUID: uuid.New(), Valid: true} }
	unset := uuid.NullUUID{UUID: uuid.Nil, Valid: false}

	tests := []struct {
		name       string
		server     *repo.McpServer
		want       AuthorizationMode
		wantReason string
	}{
		{
			name:       "no row",
			server:     nil,
			want:       AuthorizationModeInvalid,
			wantReason: "no mcp server row",
		},
		{
			name:       "disabled serves nothing",
			server:     &repo.McpServer{Visibility: VisibilityDisabled, ToolsetID: set()},
			want:       AuthorizationModeDisabled,
			wantReason: "",
		},
		{
			name:       "private without an issuer authenticates against Gram",
			server:     &repo.McpServer{Visibility: VisibilityPrivate, ToolsetID: set()},
			want:       AuthorizationModeGram,
			wantReason: "",
		},
		{
			name:       "public without an issuer authenticates against Gram",
			server:     &repo.McpServer{Visibility: VisibilityPublic, ToolsetID: set()},
			want:       AuthorizationModeGram,
			wantReason: "",
		},
		{
			// Orthogonal to visibility: an issuer gates a server of either
			// visibility, which is why the mode is not a restatement of the
			// visibility column.
			name:       "private with an issuer is issuer-gated",
			server:     &repo.McpServer{Visibility: VisibilityPrivate, ToolsetID: set(), UserSessionIssuerID: set()},
			want:       AuthorizationModeIssuerGated,
			wantReason: "",
		},
		{
			name:       "public with an issuer is issuer-gated",
			server:     &repo.McpServer{Visibility: VisibilityPublic, ToolsetID: set(), UserSessionIssuerID: set()},
			want:       AuthorizationModeIssuerGated,
			wantReason: "",
		},
		{
			// The schema forces an issuer on every tunneled backend, but a
			// public tunnel serves anonymously, so the gate must not run.
			name:       "public tunneled serves anonymously despite its forced issuer",
			server:     &repo.McpServer{Visibility: VisibilityPublic, TunneledMcpServerID: set(), UserSessionIssuerID: set()},
			want:       AuthorizationModeGram,
			wantReason: "",
		},
		{
			name:       "private tunneled is still gated",
			server:     &repo.McpServer{Visibility: VisibilityPrivate, TunneledMcpServerID: set(), UserSessionIssuerID: set()},
			want:       AuthorizationModeIssuerGated,
			wantReason: "",
		},
		{
			name:       "upstream on a hosted backend naming an issuer",
			server:     &repo.McpServer{Visibility: VisibilityUpstream, ToolsetID: set(), RemoteSessionIssuerID: set()},
			want:       AuthorizationModeUpstream,
			wantReason: "",
		},
		{
			name:       "upstream naming no issuer has no metadata to advertise",
			server:     &repo.McpServer{Visibility: VisibilityUpstream, ToolsetID: set(), RemoteSessionIssuerID: unset},
			want:       AuthorizationModeInvalid,
			wantReason: "remote_session_issuer_id",
		},
		{
			name:       "upstream that is also issuer-gated would have its issuer resynced away",
			server:     &repo.McpServer{Visibility: VisibilityUpstream, ToolsetID: set(), RemoteSessionIssuerID: set(), UserSessionIssuerID: set()},
			want:       AuthorizationModeInvalid,
			wantReason: "user_session_issuer_id to be NULL",
		},
		{
			name:       "upstream on a remote backend is not served",
			server:     &repo.McpServer{Visibility: VisibilityUpstream, RemoteMcpServerID: set(), RemoteSessionIssuerID: set()},
			want:       AuthorizationModeInvalid,
			wantReason: "hosted (toolset) backends only",
		},
		{
			name:       "upstream on a tunneled backend is not served",
			server:     &repo.McpServer{Visibility: VisibilityUpstream, TunneledMcpServerID: set(), RemoteSessionIssuerID: set()},
			want:       AuthorizationModeInvalid,
			wantReason: "hosted (toolset) backends only",
		},
		{
			name:       "upstream on an unproxied backend is not served",
			server:     &repo.McpServer{Visibility: VisibilityUpstream, UnproxiedMcpServerID: set(), RemoteSessionIssuerID: set()},
			want:       AuthorizationModeInvalid,
			wantReason: "hosted (toolset) backends only",
		},
		{
			// The reason a fifth value cannot quietly inherit public's
			// treatment: it lands here rather than in any serving arm.
			name:       "an unrecognized visibility fails closed",
			server:     &repo.McpServer{Visibility: "something-new", ToolsetID: set()},
			want:       AuthorizationModeInvalid,
			wantReason: "unrecognized mcp server visibility",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, reason := ResolveAuthorizationMode(tt.server)
			require.Equal(t, tt.want, got)
			if tt.wantReason == "" {
				require.Empty(t, reason)
				return
			}
			require.Contains(t, reason, tt.wantReason)
		})
	}
}
