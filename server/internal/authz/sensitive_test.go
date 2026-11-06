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

func TestSensitiveScopesWidened(t *testing.T) {
	t.Parallel()

	// A scope held on the named resources alone, keyed as ScopeCoverageTx
	// keys the grants it reads.
	restricted := func(scope authz.Scope, resources ...string) authz.ScopeCoverage {
		keys := make([]string, 0, len(resources))
		for _, resource := range resources {
			keys = append(keys, authz.NewSelector(scope, resource).Key())
		}
		return authz.ScopeCoverage{Scope: scope, Unrestricted: false, Resources: keys}
	}
	everywhere := func(scope authz.Scope) authz.ScopeCoverage {
		return authz.ScopeCoverage{Scope: scope, Unrestricted: true, Resources: nil}
	}

	tests := []struct {
		name   string
		before []authz.ScopeCoverage
		after  []authz.ScopeCoverage
		want   []string
	}{
		{
			name:   "held on one project, now everywhere",
			before: []authz.ScopeCoverage{restricted(authz.ScopeLogRead, "project-1")},
			after:  []authz.ScopeCoverage{restricted(authz.ScopeLogRead, "project-1"), everywhere(authz.ScopeLogRead)},
			want:   []string{"log:read"},
		},
		{
			name:   "held everywhere, another restricted grant adds nothing",
			before: []authz.ScopeCoverage{everywhere(authz.ScopeLogRead)},
			after:  []authz.ScopeCoverage{everywhere(authz.ScopeLogRead), restricted(authz.ScopeLogRead, "project-1")},
			want:   nil,
		},
		{
			name:   "held on one project, now on another too",
			before: []authz.ScopeCoverage{restricted(authz.ScopeLogRead, "project-1")},
			after:  []authz.ScopeCoverage{restricted(authz.ScopeLogRead, "project-1"), restricted(authz.ScopeLogRead, "project-2")},
			want:   []string{"log:read"},
		},
		{
			name:   "remaining restricted coverage does not widen",
			before: []authz.ScopeCoverage{restricted(authz.ScopeLogRead, "project-1")},
			after:  []authz.ScopeCoverage{restricted(authz.ScopeLogRead, "project-1")},
			want:   nil,
		},
		{
			name:   "held on two projects, now on one of them",
			before: []authz.ScopeCoverage{restricted(authz.ScopeLogRead, "project-1", "project-2")},
			after:  []authz.ScopeCoverage{restricted(authz.ScopeLogRead, "project-2")},
			want:   nil,
		},
		{
			name:   "not held before counts whatever its breadth",
			before: nil,
			after:  []authz.ScopeCoverage{restricted(authz.ScopeChatRead, "chat-1")},
			want:   []string{"chat:read"},
		},
		{
			name:   "an unrestricted broader scope widens what it implies",
			before: []authz.ScopeCoverage{restricted(authz.ScopeChatRead, "chat-1")},
			after:  []authz.ScopeCoverage{restricted(authz.ScopeChatRead, "chat-1"), everywhere(authz.ScopeChatWrite)},
			want:   []string{"chat:read", "chat:write"},
		},
		{
			name:   "a broader scope on another resource widens what it implies",
			before: []authz.ScopeCoverage{restricted(authz.ScopeChatRead, "chat-1")},
			after:  []authz.ScopeCoverage{restricted(authz.ScopeChatRead, "chat-1"), restricted(authz.ScopeChatWrite, "chat-2")},
			want:   []string{"chat:read", "chat:write"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, authz.SensitiveScopesWidened(tt.before, tt.after))
		})
	}
}
