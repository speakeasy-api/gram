package platformmcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

func authClientCheck(clientID, serverID string) *accessgen.AuthzChallenge {
	return &accessgen.AuthzChallenge{
		Scope:        string(authz.ScopeMCPAuth),
		ResourceKind: new(authz.ResourceKindMCPAuthClient),
		ResourceID:   new(clientID),
		Selector:     map[string]string{authz.ChallengeKeyMCPServerID: serverID},
		Reason:       "connection_owner_mismatch",
		Timestamp:    "2026-11-19T10:00:00Z",
	}
}

func TestGroupAgentChecksNamesTheServerAnAuthClientWasUsedFor(t *testing.T) {
	t.Parallel()

	const linear, github = "server-linear", "server-github"
	result := &accessgen.ListChallengesResult{
		Challenges: []*accessgen.AuthzChallenge{
			authClientCheck("client-a", linear),
			authClientCheck("client-a", linear),
			authClientCheck("client-a", github),
		},
		Total: 3,
	}

	out := groupAgentChecks(result, map[string]string{linear: "Linear", github: "GitHub"})

	require.Equal(t, []AgentCheckGroup{
		{Scope: string(authz.ScopeMCPAuth), ResourceKind: authz.ResourceKindMCPAuthClient, ResourceID: "client-a", ResourceName: "Linear", ServerID: linear, Count: 2, LastAt: "2026-11-19T10:00:00Z", Reason: "connection_owner_mismatch"},
		{Scope: string(authz.ScopeMCPAuth), ResourceKind: authz.ResourceKindMCPAuthClient, ResourceID: "client-a", ResourceName: "GitHub", ServerID: github, Count: 1, LastAt: "2026-11-19T10:00:00Z", Reason: "connection_owner_mismatch"},
	}, out.Groups, "one client used on two servers is two groups, each named for its server")
}

func TestCheckServerID(t *testing.T) {
	t.Parallel()

	server := &accessgen.AuthzChallenge{Scope: string(authz.ScopeMCPConnect), ResourceKind: new(authz.ResourceKindMCP), ResourceID: new("server-linear")}
	project := &accessgen.AuthzChallenge{Scope: string(authz.ScopeProjectRead), ResourceKind: new("project"), ResourceID: new("project-1")}

	require.Equal(t, "server-linear", checkServerID(server))
	require.Equal(t, "server-github", checkServerID(authClientCheck("client-a", "server-github")))
	require.Empty(t, checkServerID(project))
}
