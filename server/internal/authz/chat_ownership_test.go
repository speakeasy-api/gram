package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	s "github.com/speakeasy-api/gram/server/internal/authz/schema"
)

// TestChatOwnership runs the declared chat ownership rule through every
// migration mode: the old engine path, where attribute branches are answered
// before the engine sees the grant half, must reach the same decision as the
// registry evaluator.
func TestChatOwnership(t *testing.T) {
	t.Parallel()

	self := fixtureUserID
	other := "user_other"
	empty := ""
	external := "external_1"
	role := "role:organization:role_1"
	liveAgent := func(owner string) *AgentOwner {
		return &AgentOwner{ID: "agent_1", OwnerUserID: owner, Deleted: false, OwnerReassignmentRequired: false}
	}
	agentOnly := func(agent *AgentOwner) ChatOwner {
		return ChatOwner{UserID: nil, ExternalUserID: nil, Agent: agent}
	}
	chat := Chat("chat_1")

	cases := []struct {
		name   string
		owner  ChatOwner
		grants []Grant
		read   bool
		write  bool
	}{
		{name: "author reads and writes", owner: ChatOwner{UserID: &self, ExternalUserID: nil, Agent: nil}, read: true, write: true},
		{name: "someone else's session needs a grant", owner: ChatOwner{UserID: &other, ExternalUserID: nil, Agent: nil}, read: false, write: false},
		{name: "chat:read reaches someone else's session for reading only", owner: ChatOwner{UserID: &other, ExternalUserID: nil, Agent: nil},
			grants: []Grant{grantOf(role, string(ScopeChatRead), ResourceKindChat, "chat_1")}, read: true, write: false},
		{name: "chat:write reaches someone else's session", owner: ChatOwner{UserID: &other, ExternalUserID: nil, Agent: nil},
			grants: []Grant{grantOf(role, string(ScopeChatWrite), ResourceKindChat, "*")}, read: true, write: true},
		{name: "an empty author names nobody", owner: ChatOwner{UserID: &empty, ExternalUserID: nil, Agent: liveAgent(self)}, read: false, write: false},
		{name: "anonymous session", owner: ChatOwner{UserID: nil, ExternalUserID: nil, Agent: nil}, read: false, write: false},
		{name: "owner of a live agent reads its session", owner: agentOnly(liveAgent(self)), read: true, write: false},
		{name: "another user's agent", owner: agentOnly(liveAgent(other)), read: false, write: false},
		{name: "deleted agent", owner: agentOnly(&AgentOwner{ID: "agent_1", OwnerUserID: self, Deleted: true, OwnerReassignmentRequired: false}), read: false, write: false},
		{name: "agent latched for reassignment", owner: agentOnly(&AgentOwner{ID: "agent_1", OwnerUserID: self, Deleted: false, OwnerReassignmentRequired: true}), read: false, write: false},
		{name: "a session with a human stays the human's", owner: ChatOwner{UserID: &other, ExternalUserID: nil, Agent: liveAgent(self)}, read: false, write: false},
		{name: "a session with an external user stays theirs", owner: ChatOwner{UserID: nil, ExternalUserID: &external, Agent: liveAgent(self)}, read: false, write: false},
		{name: "an empty external user does not", owner: ChatOwner{UserID: nil, ExternalUserID: &empty, Agent: liveAgent(self)}, read: true, write: false},
	}

	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeShadow, MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			recorder := &capturedComparisons{}
			client, _ := newRoutedTestClient(t, nil, fixedModes(mode), recorder)
			for _, tc := range cases {
				ctx := humanClientContext(t, tc.grants...)
				read := chat.WithOwner(tc.owner).Read()
				write := chat.WithOwner(tc.owner).Write()

				require.Equal(t, tc.read, client.Require(ctx, read) == nil, "%s: require read", tc.name)
				require.Equal(t, tc.write, client.Require(ctx, write) == nil, "%s: require write", tc.name)
				allowed, err := client.Evaluate(ctx, read)
				require.NoError(t, err, tc.name)
				require.Equal(t, tc.read, allowed, "%s: evaluate read", tc.name)
				matched, err := client.MatchMany(ctx, []Operation{read, write})
				require.NoError(t, err, tc.name)
				require.Equal(t, []bool{tc.read, tc.write}, matched, "%s: match many", tc.name)
			}
			for _, outcome := range recorder.outcomes() {
				require.Equal(t, ShadowMatch, outcome)
			}
		})
	}
}

// TestChatGrantPartOfOwnedPermissions checks the engine is asked only for the
// grant half of the chat permissions, under the scopes it knows.
func TestChatGrantPartOfOwnedPermissions(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t)
	for _, tc := range []struct {
		perm  string
		scope Scope
	}{{perm: "read", scope: ScopeChatRead}, {perm: "write", scope: ScopeChatWrite}} {
		def, ok := client.grantPart(s.PermissionRef{Type: "chat", Permission: tc.perm})
		require.True(t, ok, tc.perm)
		require.Equal(t, string(tc.scope), def.AnswersScope, tc.perm)
	}
}
