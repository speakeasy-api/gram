package hostedmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestServerHosting(t *testing.T) {
	t.Parallel()

	toolsetID := uuid.New()
	issuer := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	// Stale columns that the derivation must ignore.
	toolset := toolsetsrepo.Toolset{ID: toolsetID, McpEnabled: true, McpIsPublic: true, UserSessionIssuerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	canonical := func(vis string) *mcpserversrepo.McpServer {
		return &mcpserversrepo.McpServer{ID: toolsetID, ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, Visibility: vis, UserSessionIssuerID: issuer}
	}

	for _, tt := range []struct {
		name    string
		wrapper *mcpserversrepo.McpServer
		enabled bool
		public  bool
		issuer  uuid.NullUUID
	}{
		{name: "no wrapper is not hosted", wrapper: nil, enabled: false, public: false, issuer: none},
		{name: "public wrapper", wrapper: canonical(visibility.Public), enabled: true, public: true, issuer: issuer},
		{name: "private wrapper", wrapper: canonical(visibility.Private), enabled: true, public: false, issuer: issuer},
		{name: "disabled wrapper ignores the frozen public flag", wrapper: canonical(visibility.Disabled), enabled: false, public: false, issuer: issuer},
		{name: "unknown visibility fails closed", wrapper: canonical("unexpected"), enabled: false, public: false, issuer: issuer},
		{
			name:    "other server for the toolset is ignored",
			wrapper: &mcpserversrepo.McpServer{ID: uuid.New(), ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, Visibility: visibility.Disabled},
			enabled: false, public: false, issuer: none,
		},
		{
			name:    "server that kept the id but left the toolset is ignored",
			wrapper: &mcpserversrepo.McpServer{ID: toolsetID, Visibility: visibility.Disabled},
			enabled: false, public: false, issuer: none,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ServerHosting(toolset, tt.wrapper)
			require.Equal(t, tt.enabled, got.Enabled)
			require.Equal(t, tt.public, got.Public)
			require.Equal(t, tt.issuer, got.UserSessionIssuerID)
		})
	}
}

func TestServerHostingDisabledReadsHowItReEnables(t *testing.T) {
	t.Parallel()

	toolsetID := uuid.New()
	disabled := &mcpserversrepo.McpServer{ID: toolsetID, ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, Visibility: visibility.Disabled}
	private := toolsetsrepo.Toolset{ID: toolsetID}
	require.False(t, ServerHosting(private, disabled).Public, "without external OAuth it re-enables private")

	withOAuth := private
	withOAuth.ExternalOauthServerID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	require.True(t, ServerHosting(withOAuth, disabled).Public, "external OAuth re-enables public")
}
