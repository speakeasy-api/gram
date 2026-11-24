package platformmcp

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

func TestDispositionRulesOnServer(t *testing.T) {
	t.Parallel()

	const (
		role      = "role:organization:operators"
		serverID  = "11111111-1111-1111-1111-111111111111"
		projectID = "22222222-2222-2222-2222-222222222222"
		otherID   = "33333333-3333-3333-3333-333333333333"
	)
	server := authz.MCPServer(authz.MCPServerIDFrom(serverID), authz.ProjectIDFrom(projectID))
	rule := func(scope authz.Scope, resource string, keys map[string]string) authz.Grant {
		selector := authz.NewSelector(scope, resource)
		maps.Copy(selector, keys)
		grant := authz.NewGrantWithSelector(scope, selector)
		grant.PrincipalUrn = role
		return grant
	}

	tests := []struct {
		name        string
		grants      []authz.Grant
		wantAllowed []string
		wantBlocked []string
	}{
		{
			name:        "a rule on this server counts",
			grants:      []authz.Grant{rule(authz.ScopeMCPConnect, serverID, map[string]string{authz.SelectorKeyDisposition: authz.DispositionReadOnly})},
			wantAllowed: []string{authz.DispositionReadOnly},
		},
		{
			name:        "a rule on every server counts",
			grants:      []authz.Grant{rule(authz.ScopeMCPConnect, authz.WildcardResource, map[string]string{authz.SelectorKeyDisposition: authz.DispositionReadOnly})},
			wantAllowed: []string{authz.DispositionReadOnly},
		},
		{
			name:   "a rule limited to another project does not reach this server",
			grants: []authz.Grant{rule(authz.ScopeMCPConnect, authz.WildcardResource, map[string]string{authz.SelectorKeyProjectID: otherID, authz.SelectorKeyDisposition: authz.DispositionReadOnly})},
		},
		{
			name:   "a rule on another server does not count",
			grants: []authz.Grant{rule(authz.ScopeMCPConnect, otherID, map[string]string{authz.SelectorKeyDisposition: authz.DispositionReadOnly})},
		},
		{
			name: "a rule a server block takes away does not count",
			grants: []authz.Grant{
				rule(authz.ScopeMCPConnect, serverID, map[string]string{authz.SelectorKeyDisposition: authz.DispositionReadOnly}),
				rule(authz.ScopeMCPBlockedConnect, serverID, nil),
			},
		},
		{
			name: "a disposition block on this server counts",
			grants: []authz.Grant{
				rule(authz.ScopeMCPConnect, serverID, nil),
				rule(authz.ScopeMCPBlockedConnect, serverID, map[string]string{authz.SelectorKeyDisposition: authz.DispositionDestructive}),
			},
			wantBlocked: []string{authz.DispositionDestructive},
		},
		{
			name:   "a disposition block on another server does not count",
			grants: []authz.Grant{rule(authz.ScopeMCPBlockedConnect, otherID, map[string]string{authz.SelectorKeyDisposition: authz.DispositionDestructive})},
		},
		{
			name:        "a rule naming a tool and a disposition counts",
			grants:      []authz.Grant{rule(authz.ScopeMCPConnect, serverID, map[string]string{authz.SelectorKeyTool: "list_tasks", authz.SelectorKeyDisposition: authz.DispositionReadOnly})},
			wantAllowed: []string{authz.DispositionReadOnly},
		},
		{
			name:   "a rule with no disposition is not a disposition rule",
			grants: []authz.Grant{rule(authz.ScopeMCPConnect, serverID, nil)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			allowed, blocked, err := dispositionRulesOnServer(tt.grants, server)
			require.NoError(t, err)
			require.ElementsMatch(t, tt.wantAllowed, allowed)
			require.ElementsMatch(t, tt.wantBlocked, blocked)
			require.NotNil(t, allowed, "an empty list reads [] rather than null")
			require.NotNil(t, blocked, "an empty list reads [] rather than null")
		})
	}
}
