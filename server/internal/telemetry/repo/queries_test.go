package repo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveAttributeColumn_AtPrefixUserAttribute(t *testing.T) {
	t.Parallel()
	got := resolveAttributeColumn("@user.region")
	require.Equal(t, "toString(attributes.app.user.region)", got)
}

func TestResolveAttributeColumn_FallbackJSONAccessor(t *testing.T) {
	t.Parallel()
	got := resolveAttributeColumn("http.route")
	require.Equal(t, "toString(attributes.http.route)", got)
}

func TestResolveAttributeColumn_MaterializedConversationID(t *testing.T) {
	t.Parallel()
	got := resolveAttributeColumn("gen_ai.conversation.id")
	require.Equal(t, "chat_id", got)
}

func TestResolveAttributeColumn_MaterializedToolURN(t *testing.T) {
	t.Parallel()
	got := resolveAttributeColumn("gram.tool.urn")
	require.Equal(t, "urn", got)
}
