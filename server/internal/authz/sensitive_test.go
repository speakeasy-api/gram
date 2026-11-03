package authz_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

func TestSensitiveScopesAdded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		before []authz.Scope
		after  []authz.Scope
		want   []string
	}{
		{
			name:   "a new role with a sensitive grant",
			before: nil,
			after:  []authz.Scope{authz.ScopeProjectRead, authz.ScopeLogRead},
			want:   []string{"log:read"},
		},
		{
			name:   "nothing sensitive added",
			before: []authz.Scope{authz.ScopeProjectRead},
			after:  []authz.Scope{authz.ScopeProjectRead, authz.ScopeProjectWrite},
			want:   nil,
		},
		{
			name:   "already held",
			before: []authz.Scope{authz.ScopeLogRead},
			after:  []authz.Scope{authz.ScopeLogRead, authz.ScopeMCPConnect},
			want:   nil,
		},
		{
			name:   "a broader scope counts what it implies once",
			before: []authz.Scope{authz.ScopeChatRead},
			after:  []authz.Scope{authz.ScopeChatWrite},
			want:   []string{"chat:write"},
		},
		{
			name:   "catalogue order, not grant order",
			before: nil,
			after:  []authz.Scope{authz.ScopeLogRead, authz.ScopeChatRead, authz.ScopeOrgAdmin},
			want:   []string{"org:admin", "chat:read", "log:read"},
		},
		{
			name:   "removals report nothing",
			before: []authz.Scope{authz.ScopeOrgAdmin},
			after:  nil,
			want:   nil,
		},
		{
			name:   "blocks are not grants",
			before: nil,
			after:  []authz.Scope{authz.ScopeOrgBlockedAdmin},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, authz.SensitiveScopesAdded(tt.before, tt.after))
		})
	}
}
