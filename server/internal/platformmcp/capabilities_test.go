package platformmcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// TestAdvertisedCapabilitiesWithholdListChanged pins the capabilities a client
// negotiates against, because listChanged is the whole reason a client decides
// to hold a subscriptions/listen stream open. The runtime serves POSTs
// statelessly, so no session outlives a request to receive a list_changed
// notification, and a stream opened on that promise can only idle until the
// proxy times it out.
func TestAdvertisedCapabilitiesWithholdListChanged(t *testing.T) {
	t.Parallel()

	server, _ := newTestServer(t)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "capabilities-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	capabilities := session.InitializeResult().Capabilities
	require.NotNil(t, capabilities.Tools, "tools must stay advertised")
	require.False(t, capabilities.Tools.ListChanged, "tools.listChanged invites a stream this server cannot feed")
	require.NotNil(t, capabilities.Resources, "resources must stay advertised")
	require.False(t, capabilities.Resources.ListChanged, "resources.listChanged invites a stream this server cannot feed")
	require.False(t, capabilities.Resources.Subscribe, "resource subscriptions need a SubscribeHandler this server does not register")

	// Declaring capabilities replaces the SDK's defaults rather than adding to
	// them, so the one default this server relied on has to be asserted.
	require.NotNil(t, capabilities.Logging, "logging was advertised before capabilities were declared explicitly")
}
