package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A member's self-scoped chat:read grant authorizes only the sessions they own.
func TestChatRead_SelfGrantMatchesOwnSessionsOnly(t *testing.T) {
	t.Parallel()

	self := ChatReadSelfGrant("user_self")
	g := []Grant{self}

	own, _, _ := evaluateGrants(g, ChatReadCheck("chat_1", "user_self").expand())
	require.NotNil(t, own, "owner may read their own session")

	other, _, _ := evaluateGrants(g, ChatReadCheck("chat_2", "user_other").expand())
	require.Nil(t, other, "owner may not read another user's session")

	// Sessions with no owner (external / Elements chats) must not leak to a
	// self-scoped grant: the empty user_id check still carries the dimension.
	external, _, _ := evaluateGrants(g, ChatReadCheck("chat_3", "").expand())
	require.Nil(t, external, "self grant must not match an ownerless session")

	// The list "see all" probe (wildcard user_id) is not satisfied by a self grant.
	all, _, _ := evaluateGrants(g, ChatReadAllCheck("proj_1").expand())
	require.Nil(t, all, "self grant does not grant see-all visibility")
}

// An unconstrained chat:read grant (admins) authorizes every owner's sessions.
func TestChatRead_WildcardGrantMatchesAnyOwner(t *testing.T) {
	t.Parallel()

	admin := NewGrant(ScopeChatRead, WildcardResource)
	g := []Grant{admin}

	a, _, _ := evaluateGrants(g, ChatReadCheck("chat_1", "user_a").expand())
	require.NotNil(t, a)

	b, _, _ := evaluateGrants(g, ChatReadCheck("chat_2", "user_b").expand())
	require.NotNil(t, b)

	external, _, _ := evaluateGrants(g, ChatReadCheck("chat_3", "").expand())
	require.NotNil(t, external, "admins may read ownerless sessions")

	all, _, _ := evaluateGrants(g, ChatReadAllCheck("proj_1").expand())
	require.NotNil(t, all, "unconstrained grant satisfies the see-all probe")
}

// chat:read with a self selector validates and round-trips through the DB shape.
func TestChatRead_SelfSelectorValidates(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateSelector(ScopeChatRead, Selector{
		SelectorKeyResourceKind: ResourceKindChat,
		SelectorKeyResourceID:   WildcardResource,
		SelectorKeyUserID:       "user_self",
	}))

	// user_id is the only extra key allowed on the chat family.
	require.Error(t, ValidateSelector(ScopeChatRead, Selector{
		SelectorKeyResourceKind: ResourceKindChat,
		SelectorKeyResourceID:   WildcardResource,
		SelectorKeyTool:         "x",
	}))
}
