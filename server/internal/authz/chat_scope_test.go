package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// An unrestricted chat:read grant (held by admins) satisfies the chat read
// check; a principal with no chat:read grant does not. Members never hold a
// chat:read grant — their access to sessions they own is granted by
// owner-matching in the chat handlers, not by this scope.
func TestChatRead_RequiresUnrestrictedGrant(t *testing.T) {
	t.Parallel()

	admin := []Grant{NewGrant(ScopeChatRead, WildcardResource)}
	got, _, _ := evaluateGrants(admin, ChatReadCheck("chat_1").expand())
	require.NotNil(t, got, "admin chat:read satisfies the read check")

	none, _, _ := evaluateGrants(nil, ChatReadCheck("chat_1").expand())
	require.Nil(t, none, "no chat:read grant means no chat read access")
}

// chat:read carries no extra selector dimensions: only resource_kind and
// resource_id are valid on the chat family.
func TestChatRead_SelectorRejectsExtraKeys(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateSelector(ScopeChatRead, Selector{
		SelectorKeyResourceKind: ResourceKindChat,
		SelectorKeyResourceID:   WildcardResource,
	}))

	require.Error(t, ValidateSelector(ScopeChatRead, Selector{
		SelectorKeyResourceKind: ResourceKindChat,
		SelectorKeyResourceID:   WildcardResource,
		"user_id":               "user_self",
	}), "chat scopes allow no extra selector keys")
}
