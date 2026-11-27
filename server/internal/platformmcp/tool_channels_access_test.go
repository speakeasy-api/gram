package platformmcp

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	channelsrepo "github.com/speakeasy-api/gram/server/internal/channels/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
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
	db, err := platformMCPInfra.CloneTestDatabase(t, "channels_access")
	require.NoError(t, err)
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: channelsAccessOrganizationID, Name: "Channels access", Slug: "channels-access",
		WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{}, CreationSource: pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = projectsrepo.New(db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name: "Target project", Slug: "target-project", OrganizationID: channelsAccessOrganizationID,
	})
	require.NoError(t, err)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagChannels, channelsAccessOrganizationID, true)
	return NewChannelsAccessService(testenv.NewLogger(t), authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, nil), db, base, flags)
}

// channelsAccessContext is the context PrepareExternalContext hands a tool: an
// authenticated actor with no browser session, the acting surface marked, and
// freshly loaded grants. The surface is what makes RBAC enforce for a
// session-less caller; without it Evaluate reports every check as allowed.
func channelsAccessContext(t *testing.T, service *ChannelsAccessService, grants ...authz.Grant) context.Context {
	t.Helper()
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{
		UserID:               channelsAccessUserID,
		ActiveOrganizationID: channelsAccessOrganizationID,
	}, urn.NewPrincipal(urn.PrincipalTypeUser, channelsAccessUserID))
	ctx = contextvalues.SetActingSurface(ctx, string(SurfacePlatformMCP))
	return authz.GrantsToContext(ctx, append([]authz.Grant{authz.NewGrant(authz.ScopeProjectRead, channelsAccessProject(t, service).ID.String())}, grants...))
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
	grant := func(scope authz.Scope) authz.Grant {
		return authz.NewGrant(scope, channelsAccessProject(t, service).ID.String())
	}

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
			output, err := service.Get(channelsAccessContext(t, service, test.grants...), channelsAccessPrincipal(), GetMyChannelsAccessInput{ProjectSlug: "target-project"})
			require.NoError(t, err)
			require.Equal(t, test.connect, output.CanConnect)
			require.Equal(t, test.admin, output.CanAdminister)
			require.Equal(t, test.readAll, output.CanReadAllConversations)
			require.Equal(t, test.reason, output.UnavailableReason)
			require.Contains(t, output.NextAction, test.contains)
			require.Equal(t, "https://api.example.test/channels/channels-access/target-project/mcp", output.EndpointURL)
		})
	}
}

// An access exception overrides the role grants, exactly as the Channels
// endpoint enforces it: a member whose exception withdraws participation is not
// told they may connect, and one granted read-all by exception is told so.
func TestChannelsAccessAppliesTheMembersAccessException(t *testing.T) {
	t.Parallel()
	service := newChannelsAccessService(t, "https://api.example.test")
	ctx := t.Context()
	_, err := orgrepo.New(service.db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: channelsAccessOrganizationID, Name: "Channels access", Slug: "channels-access",
		WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{}, CreationSource: pgtype.Text{},
	})
	require.NoError(t, err)
	project := channelsAccessProject(t, service)
	_, err = channelsrepo.New(service.db).UpsertChannelsAccessException(ctx, channelsrepo.UpsertChannelsAccessExceptionParams{
		OrganizationID: channelsAccessOrganizationID, ProjectID: project.ID, SubjectID: channelsAccessUserID,
		CanConnect: false, CanReadAll: true,
	})
	require.NoError(t, err)

	output, err := service.Get(channelsAccessContext(t, service,
		authz.NewGrant(authz.ScopeChannelsConnect, channelsAccessProject(t, service).ID.String()),
		authz.NewGrant(authz.ScopeChannelsAdmin, channelsAccessProject(t, service).ID.String()),
	), channelsAccessPrincipal(), GetMyChannelsAccessInput{ProjectSlug: "target-project"})
	require.NoError(t, err)
	require.False(t, output.CanConnect, "the exception withdraws the role's connect grant")
	require.True(t, output.CanReadAllConversations, "the exception grants read-all")
	require.True(t, output.CanAdminister, "no exception changes administration")
}

// A wildcard grant covers the selected project like a grant naming it.
func TestChannelsAccessHonoursWildcardGrants(t *testing.T) {
	t.Parallel()
	service := newChannelsAccessService(t, "https://api.example.test")
	output, err := service.Get(channelsAccessContext(t, service,
		authz.NewGrant(authz.ScopeChannelsConnect, channelsAccessProject(t, service).ID.String()),
		authz.NewGrant(authz.ScopeChannelsReadAll, authz.WildcardResource),
	), channelsAccessPrincipal(), GetMyChannelsAccessInput{ProjectSlug: "target-project"})
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
		channelsAccessContext(t, service, authz.NewGrant(authz.ScopeChannelsConnect, channelsAccessProject(t, service).ID.String())),
		requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: "https://ai.example.test"},
	)
	output, err := service.Get(ctx, channelsAccessPrincipal(), GetMyChannelsAccessInput{ProjectSlug: "target-project"})
	require.NoError(t, err)
	require.Equal(t, "https://ai.example.test/channels/channels-access/target-project/mcp", output.EndpointURL)
}

// The answer is about the caller's own access and names nothing inside
// Channels: no conversation, no message, no presence, no credential.
func TestChannelsAccessReturnsNoChannelContents(t *testing.T) {
	t.Parallel()
	service := newChannelsAccessService(t, "https://api.example.test")
	output, err := service.Get(channelsAccessContext(t, service, authz.NewGrant(authz.ScopeChannelsAdmin, channelsAccessProject(t, service).ID.String())), channelsAccessPrincipal(), GetMyChannelsAccessInput{ProjectSlug: "target-project"})
	require.NoError(t, err)
	require.Equal(t, GetMyChannelsAccessOutput{
		EndpointURL:             "https://api.example.test/channels/channels-access/target-project/mcp",
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
	service := newChannelsAccessService(t, "https://api.example.test")
	require.False(t, NewChannelsAccessService(testenv.NewLogger(t), nil, service.db, base, service.flags).valid())
	require.False(t, NewChannelsAccessService(testenv.NewLogger(t), service.engine, nil, base, service.flags).valid())
	require.False(t, NewChannelsAccessService(testenv.NewLogger(t), service.engine, service.db, nil, service.flags).valid())
	require.True(t, service.valid())
}

// Disabled discovery never reaches RBAC and leaks no endpoint
// or permission claims. The flag is evaluated for the authenticated org, not
// the user or a deployment-wide identity.
func TestChannelsAccessOrganizationFlag(t *testing.T) {
	t.Parallel()
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagChannels, "another_organization", true)
	for _, test := range []struct {
		name  string
		flags feature.Provider
	}{
		{name: "missing provider", flags: nil},
		{name: "other organization enabled", flags: flags},
		{name: "disabled", flags: &feature.InMemory{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := newChannelsAccessService(t, "https://api.example.test")
			service.flags = test.flags
			output, err := service.Get(channelsAccessContext(t, service), channelsAccessPrincipal(), GetMyChannelsAccessInput{ProjectSlug: "target-project"})
			require.NoError(t, err)
			require.Equal(t, channelsAccessNotEnabled(), output)
		})
	}
}

type channelsAccessFailingFlags struct{ *feature.InMemory }

func (channelsAccessFailingFlags) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, errors.New("flag evaluation failed")
}

func TestChannelsAccessFlagErrorFailsClosed(t *testing.T) {
	t.Parallel()
	service := newChannelsAccessService(t, "https://api.example.test")
	service.flags = channelsAccessFailingFlags{InMemory: &feature.InMemory{}}
	output, err := service.Get(channelsAccessContext(t, service), channelsAccessPrincipal(), GetMyChannelsAccessInput{ProjectSlug: "target-project"})
	require.ErrorIs(t, err, ErrUnavailable)
	require.Empty(t, output.EndpointURL)
	require.False(t, output.CanConnect)
}

func channelsAccessProject(t *testing.T, service *ChannelsAccessService) ResolvedProject {
	t.Helper()
	project, err := service.resolveProject(t.Context(), channelsAccessOrganizationID, FindMCPInput{ProjectSlug: "target-project"})
	require.NoError(t, err)
	return project
}

func TestChannelsAccessRequiresExactlyOneProject(t *testing.T) {
	t.Parallel()
	service := newChannelsAccessService(t, "https://api.example.test")
	for _, input := range []GetMyChannelsAccessInput{{}, {ProjectID: channelsAccessProject(t, service).ID.String(), ProjectSlug: "target-project"}} {
		output, err := service.Get(channelsAccessContext(t, service), channelsAccessPrincipal(), input)
		require.ErrorContains(t, err, "exactly one of project_id or project_slug is required")
		require.Empty(t, output.EndpointURL)
	}
}

func TestChannelsAccessProjectIsolation(t *testing.T) {
	t.Parallel()
	service := newChannelsAccessService(t, "https://api.example.test")
	other, err := projectsrepo.New(service.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{Name: "Other project", Slug: "other-project", OrganizationID: channelsAccessOrganizationID})
	require.NoError(t, err)
	_, err = channelsrepo.New(service.db).UpsertChannelsAccessException(t.Context(), channelsrepo.UpsertChannelsAccessExceptionParams{
		OrganizationID: channelsAccessOrganizationID, ProjectID: other.ID, SubjectID: channelsAccessUserID, CanConnect: true, CanReadAll: true,
	})
	require.NoError(t, err)
	ctx := channelsAccessContext(t, service,
		authz.NewGrant(authz.ScopeChannelsConnect, other.ID.String()),
		authz.NewGrant(authz.ScopeChannelsAdmin, other.ID.String()),
		authz.NewGrant(authz.ScopeChannelsReadAll, other.ID.String()),
		authz.NewGrant(authz.ScopeChannelsConnect, channelsAccessOrganizationID),
		authz.NewGrant(authz.ScopeChannelsAdmin, channelsAccessOrganizationID),
		authz.NewGrant(authz.ScopeChannelsReadAll, channelsAccessOrganizationID),
	)
	output, err := service.Get(ctx, channelsAccessPrincipal(), GetMyChannelsAccessInput{ProjectSlug: "target-project"})
	require.NoError(t, err)
	require.False(t, output.CanConnect)
	require.False(t, output.CanAdminister)
	require.False(t, output.CanReadAllConversations)
	require.Equal(t, "no_channels_permission", output.UnavailableReason)
}

func TestChannelsAccessResolvesExactProjectID(t *testing.T) {
	t.Parallel()
	service := newChannelsAccessService(t, "https://api.example.test")
	project := channelsAccessProject(t, service)
	output, err := service.Get(channelsAccessContext(t, service, authz.NewGrant(authz.ScopeChannelsConnect, project.ID.String())), channelsAccessPrincipal(), GetMyChannelsAccessInput{ProjectID: project.ID.String()})
	require.NoError(t, err)
	require.True(t, output.CanConnect)
	require.Equal(t, "https://api.example.test/channels/channels-access/target-project/mcp", output.EndpointURL)
}

func TestChannelsAccessToolContract(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "live", Version: "1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "unavailable", Version: "1"}, nil))
	registerChannelsAccessTool(live, &ChannelsAccessService{})
	registerChannelsAccessTool(unavailable, nil)
	a := descriptorByName(t, live, getMyChannelsAccessName)
	b := descriptorByName(t, unavailable, getMyChannelsAccessName)
	require.JSONEq(t, string(a.InputSchema), string(b.InputSchema))
	require.Equal(t, a.output, b.output)
	require.Equal(t, a.Meta, b.Meta)
	require.Equal(t, ExternalAuthorizationMember, a.Meta.Authorization)
	require.Equal(t, ProjectScopeExplicit, a.Meta.ProjectScope)
	require.Equal(t, externalOnly, a.Meta.Audiences)
	require.Empty(t, live.For(AudienceAssistant))
	require.Contains(t, string(a.InputSchema), "project_slug")
	require.NotContains(t, string(a.InputSchema), "project_id")
	require.Contains(t, a.Description, "no default project fallback")
}

func TestChannelsAccessRejectsMissingAndForeignProjects(t *testing.T) {
	t.Parallel()
	service := newChannelsAccessService(t, "https://api.example.test")
	_, err := orgrepo.New(service.db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: "other_org", Name: "Other organization", Slug: "other-org",
		WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{}, CreationSource: pgtype.Text{},
	})
	require.NoError(t, err)
	foreign, err := projectsrepo.New(service.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{Name: "Foreign project", Slug: "foreign-project", OrganizationID: "other_org"})
	require.NoError(t, err)
	for _, input := range []GetMyChannelsAccessInput{{ProjectID: foreign.ID.String()}, {ProjectSlug: "missing-project"}} {
		output, err := service.Get(channelsAccessContext(t, service), channelsAccessPrincipal(), input)
		require.ErrorIs(t, err, ErrForbidden)
		require.Empty(t, output.EndpointURL)
	}
}

func TestChannelsAccessRequiresLiveProjectRead(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		orgRead bool
		blocked bool
	}{
		{name: "no project read"},
		{name: "organization read is not project read", orgRead: true},
		{name: "blocked project read", blocked: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := newChannelsAccessService(t, "https://api.example.test")
			project := channelsAccessProject(t, service)
			grants := []authz.Grant{authz.NewGrant(authz.ScopeChannelsConnect, project.ID.String())}
			if test.orgRead {
				grants = append(grants, authz.NewGrant(authz.ScopeOrgRead, channelsAccessOrganizationID))
			}
			if test.blocked {
				grants = append(grants, authz.NewGrant(authz.ScopeProjectRead, project.ID.String()), authz.NewGrant(authz.ScopeProjectBlockedRead, project.ID.String()))
			}
			ctx := authz.GrantsToContext(channelsAccessContext(t, service), grants)
			output, err := service.Get(ctx, channelsAccessPrincipal(), GetMyChannelsAccessInput{ProjectID: project.ID.String()})
			require.ErrorIs(t, err, ErrForbidden)
			require.Empty(t, output.EndpointURL)
			require.False(t, output.CanConnect)
		})
	}
}
