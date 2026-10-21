package platformmcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/googlechatconnections"
	"github.com/stretchr/testify/require"
)

func TestGoogleChatToolsKeepAdminOnlyContractWhenUnavailable(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "live", Version: "1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "unavailable", Version: "1"}, nil))
	registerGoogleChatTools(live, &googlechatconnections.Service{})
	registerGoogleChatTools(unavailable, nil)
	require.Empty(t, live.For(AudienceAssistant))
	for _, name := range []string{"list_google_chat_connections", "configure_google_chat_space", "manage_google_chat_space", "map_google_chat_identity"} {
		a := descriptorByName(t, live, name)
		b := descriptorByName(t, unavailable, name)
		require.Equal(t, ExternalAuthorizationOrgAdmin, a.Meta.Authorization)
		require.Equal(t, externalOnly, a.Meta.Audiences)
		require.Equal(t, a.Meta, b.Meta)
		require.JSONEq(t, string(a.InputSchema), string(b.InputSchema))
		require.NotContains(t, string(a.InputSchema), "client_secret")
		require.NotContains(t, string(a.InputSchema), "service_account_json")
	}
}
