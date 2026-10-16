package platformmcp

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const channelsAccessOrganizationID = "org_example"
const channelsAccessUserID = "user_example"

func newChannelsAccessService(t *testing.T, serverURL string) *ChannelsAccessService {
	t.Helper()
	base, err := url.Parse(serverURL)
	require.NoError(t, err)
	return NewChannelsAccessService(authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, nil), base)
}

// channelsAccessContext is the context PrepareExternalContext hands a tool: an
// authenticated actor with no browser session, the acting surface marked, and
// freshly loaded grants. The surface is what makes RBAC enforce for a
// session-less caller; without it Evaluate reports every check as allowed.
func channelsAccessContext(t *testing.T, grants ...authz.Grant) context.Context {
	t.Helper()
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{
		UserID:               channelsAccessUserID,
		ActiveOrganizationID: channelsAccessOrganizationID,
	}, urn.NewPrincipal(urn.PrincipalTypeUser, channelsAccessUserID))
	ctx = contextvalues.SetActingSurface(ctx, string(SurfacePlatformMCP))
	return authz.GrantsToContext(ctx, grants)
}

func channelsAccessPrincipal() Principal {
	return Principal{UserID: channelsAccessUserID, OrganizationID: channelsAccessOrganizationID, ConnectionID: "connection_example", Generation: "generation_example", ClientID: "client_example", Surface: SurfacePlatformMCP}
}

// The three Channels permissions are reported independently. None of them is
// implied by another, and none is implied by MCP access or by organization
// administration, so a caller cannot read one permission's answer and conclude
// anything about the others.
func TestChannelsAccessReportsEachPermissionIndependently(t *testing.T) {
	t.Parallel()
	service := newChannelsAccessService(t, "https://api.example.test")
	grant := func(scope authz.Scope) authz.Grant { return authz.NewGrant(scope, channelsAccessOrganizationID) }

	for _, test := range []struct {
		name     string
		grants   []authz.Grant
		connect  bool
		admin    bool
		readAll  bool
		reason   string
		contains string
	}{
		{
			name: "no grants at all", reason: "no_channels_permission",
			contains: "Ask an organization administrator to grant you channels:connect",
		},
		{
			name:     "mcp and organization access grant no channels permission",
			grants:   []authz.Grant{grant(authz.ScopeMCPConnect), grant(authz.ScopeMCPRead), grant(authz.ScopeMCPWrite), grant(authz.ScopeOrgAdmin), grant(authz.ScopeOrgRead)},
			reason:   "no_channels_permission",
			contains: "separate from MCP access",
		},
		{
			name: "connect only", grants: []authz.Grant{grant(authz.ScopeChannelsConnect)},
			connect: true, contains: "channels_connect",
		},
		{
			name: "administer only", grants: []authz.Grant{grant(authz.ScopeChannelsAdmin)},
			admin: true, contains: "may administer Channels but not take part in it",
		},
		{
			name: "read all only", grants: []authz.Grant{grant(authz.ScopeChannelsReadAll)},
			readAll: true, contains: "may observe conversations but not take part in them",
		},
		{
			name:    "all three",
			grants:  []authz.Grant{grant(authz.ScopeChannelsConnect), grant(authz.ScopeChannelsAdmin), grant(authz.ScopeChannelsReadAll)},
			connect: true, admin: true, readAll: true, contains: "channels_connect",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			output, err := service.Get(channelsAccessContext(t, test.grants...), channelsAccessPrincipal())
			require.NoError(t, err)
			require.Equal(t, test.connect, output.CanConnect)
			require.Equal(t, test.admin, output.CanAdminister)
			require.Equal(t, test.readAll, output.CanReadAllConversations)
			require.Equal(t, test.reason, output.UnavailableReason)
			require.Contains(t, output.NextAction, test.contains)
			require.Equal(t, "https://api.example.test/channels/mcp", output.EndpointURL)
		})
	}
}

// A wildcard grant covers the caller's organization like a grant naming it.
func TestChannelsAccessHonoursWildcardGrants(t *testing.T) {
	t.Parallel()
	service := newChannelsAccessService(t, "https://api.example.test")
	output, err := service.Get(channelsAccessContext(t,
		authz.NewGrant(authz.ScopeChannelsConnect, channelsAccessOrganizationID),
		authz.NewGrant(authz.ScopeChannelsReadAll, authz.WildcardResource),
	), channelsAccessPrincipal())
	require.NoError(t, err)
	require.True(t, output.CanConnect)
	require.True(t, output.CanReadAllConversations)
	require.False(t, output.CanAdminister)
}

// The endpoint named is the one on the platform host the request arrived on.
// Naming the canonical host to a caller connected to an extra platform host
// would send them to an origin their session does not cover.
func TestChannelsAccessNamesTheRequestsPlatformHost(t *testing.T) {
	t.Parallel()
	service := newChannelsAccessService(t, "https://api.example.test")
	ctx := requestorigin.WithContext(
		channelsAccessContext(t, authz.NewGrant(authz.ScopeChannelsConnect, channelsAccessOrganizationID)),
		requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: "https://ai.example.test"},
	)
	output, err := service.Get(ctx, channelsAccessPrincipal())
	require.NoError(t, err)
	require.Equal(t, "https://ai.example.test/channels/mcp", output.EndpointURL)
}

// The answer is about the caller's own access and names nothing inside
// Channels: no conversation, no message, no presence, no credential.
func TestChannelsAccessReturnsNoChannelContents(t *testing.T) {
	t.Parallel()
	service := newChannelsAccessService(t, "https://api.example.test")
	output, err := service.Get(channelsAccessContext(t, authz.NewGrant(authz.ScopeChannelsAdmin, channelsAccessOrganizationID)), channelsAccessPrincipal())
	require.NoError(t, err)
	require.Equal(t, GetMyChannelsAccessOutput{
		EndpointURL:             "https://api.example.test/channels/mcp",
		CanConnect:              false,
		CanAdminister:           true,
		CanReadAllConversations: false,
		UnavailableReason:       "",
		NextAction:              output.NextAction,
	}, output)
}

// A service composed without an engine or a server URL registers the tool as a
// readable refusal rather than dropping it from the catalogue.
func TestChannelsAccessIncompleteServiceIsNotValid(t *testing.T) {
	t.Parallel()
	base, err := url.Parse("https://api.example.test")
	require.NoError(t, err)
	var nilService *ChannelsAccessService
	require.False(t, nilService.valid())
	require.False(t, NewChannelsAccessService(nil, base).valid())
	require.False(t, NewChannelsAccessService(authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, nil), nil).valid())
	require.True(t, newChannelsAccessService(t, "https://api.example.test").valid())
}
