package platformmcp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

type scopeManagementStub struct {
	state            *gen.RemoteMcpServerScopes
	writes           int
	expected         []string
	version          string
	expectedVersion  string
	readError        error
	writeError       error
	failVerification bool
	projectID        uuid.UUID
}

func (s *scopeManagementStub) GetServerScopesForConfirmation(ctx context.Context, _ *gen.GetServerScopesPayload) (*gen.RemoteMcpServerScopes, string, error) {
	auth, _ := contextvalues.GetAuthContext(ctx)
	if auth != nil && auth.ProjectID != nil {
		s.projectID = *auth.ProjectID
	}
	if s.readError != nil {
		return nil, "", s.readError
	}
	return s.state, s.version, nil
}

func (s *scopeManagementStub) SetServerScopePinWithExpectedScopes(_ context.Context, input *gen.SetServerScopePinPayload, expected []string, expectedVersion string) (*gen.RemoteMcpServerScopes, error) {
	s.writes++
	s.expected = expected
	s.expectedVersion = expectedVersion
	if s.writeError != nil {
		return nil, s.writeError
	}
	s.state.PinnedScopes = input.Scopes
	if s.failVerification {
		s.readError = errors.New("read unavailable")
	}
	return s.state, nil
}

func scopeToolFixture(t *testing.T) (context.Context, Principal, *MCPScopesService, *scopeManagementStub, SetMCPScopePinInput) {
	t.Helper()
	principal := testPrincipal()
	projectID := uuid.New()
	auth := new(contextvalues.AuthContext)
	auth.ActiveOrganizationID, auth.UserID = principal.OrganizationID, principal.UserID
	ctx := contextvalues.SetAuthContext(t.Context(), auth)
	management := &scopeManagementStub{
		state:  &gen.RemoteMcpServerScopes{ResourceURL: "https://example.com/mcp", PinnedScopes: []string{"read"}, AdvertisedScopesKnown: false, AdvertisedScopes: nil, ChallengeScopes: nil, CanPin: true, DiscoveryEnabled: true, Clients: nil, SharedServerCount: 2},
		writes: 0, expected: nil, version: "version-1", expectedVersion: "", readError: nil, writeError: nil, failVerification: false, projectID: uuid.Nil,
	}
	service := &MCPScopesService{management: management, projects: &stubAnalyticsProjects{project: ResolvedProject{ID: projectID, Slug: "example", Name: "Example"}, err: nil, inputs: nil}}
	input := SetMCPScopePinInput{ProjectID: projectID.String(), MCPID: uuid.NewString(), Scopes: []string{"write"}, ExpectedScopes: []string{"read"}, ExpectedVersion: "version-1", Confirmed: false}
	return ctx, principal, service, management, input
}

func TestMCPScopesPreviewAndCompareAndSet(t *testing.T) {
	t.Parallel()
	ctx, principal, service, management, input := scopeToolFixture(t)
	preview, err := service.set(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, preview.ConfirmationRequired)
	require.Equal(t, 2, preview.SharedServerCount)
	require.Equal(t, []string{"write"}, preview.ProposedScopes)
	require.Equal(t, "version-1", preview.ConfirmationVersion)
	require.Zero(t, management.writes)
	require.Equal(t, input.ProjectID, management.projectID.String())
	input.Confirmed = true
	result, err := service.set(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, result.Verified)
	require.Equal(t, []string{"write"}, result.PinnedScopes)
	require.Equal(t, []string{"read"}, management.expected)
	require.Equal(t, "version-1", management.expectedVersion)
	require.Equal(t, 1, management.writes)
	_, err = service.set(ctx, principal, input)
	require.NoError(t, err)
	require.Equal(t, 1, management.writes, "retrying the desired state is a no-op")
}

func TestMCPScopesRefusesStaleStateAndPropagatesAtomicConflict(t *testing.T) {
	t.Parallel()
	ctx, principal, service, management, input := scopeToolFixture(t)
	input.Confirmed = true
	input.ExpectedScopes = []string{"old"}
	_, err := service.set(ctx, principal, input)
	require.ErrorContains(t, err, "pinned scopes changed")
	require.Zero(t, management.writes)
	input.ExpectedScopes = []string{"read"}
	management.writeError = errors.New("atomic scope conflict")
	_, err = service.set(ctx, principal, input)
	require.ErrorContains(t, err, "atomic scope conflict")
}

func TestMCPScopesRefusesUnavailableOrUnauthorizedPin(t *testing.T) {
	t.Parallel()
	for _, available := range []bool{false, true} {
		ctx, principal, service, management, input := scopeToolFixture(t)
		management.state.CanPin = available
		management.state.DiscoveryEnabled = !available
		input.Confirmed = true
		_, err := service.set(ctx, principal, input)
		require.Error(t, err)
		require.Zero(t, management.writes)
	}
}

func TestMCPScopesKeepsCommittedStateWhenVerificationFails(t *testing.T) {
	t.Parallel()
	ctx, principal, service, management, input := scopeToolFixture(t)
	management.failVerification = true
	input.Confirmed = true
	result, err := service.set(ctx, principal, input)
	require.NoError(t, err)
	require.False(t, result.Verified)
	require.Equal(t, []string{"write"}, result.PinnedScopes)
	require.Contains(t, result.NextStep, "verification read failed")
}

func TestMCPScopesChecksIdentityBeforeReading(t *testing.T) {
	t.Parallel()
	ctx, principal, service, management, input := scopeToolFixture(t)
	principal.OrganizationID = "another-organization"
	_, err := service.set(ctx, principal, input)
	require.ErrorIs(t, err, ErrForbidden)
	require.Zero(t, management.writes)
	require.Equal(t, uuid.Nil, management.projectID)
}

func TestMCPScopesToolsRemainListedWithoutServices(t *testing.T) {
	t.Parallel()
	reg := newRegistrar(newTestMCPServer())
	registerMCPScopesTools(reg, nil)
	require.Len(t, reg.Descriptors(), 2)
	require.Empty(t, reg.For(AudienceAssistant))
	for _, d := range reg.Descriptors() {
		require.Equal(t, ExternalAuthorizationOrgAdmin, d.Meta.Authorization)
		require.Equal(t, ProjectScopeExplicit, d.Meta.ProjectScope)
		require.Equal(t, externalOnly, d.Meta.Audiences)
		if d.Name == "set_mcp_scope_pin" {
			require.Equal(t, discoveryMCPWrite, d.Meta.DiscoveryScopes)
		} else {
			require.Equal(t, discoveryMCPRead, d.Meta.DiscoveryScopes)
		}
	}
}

func TestMCPScopesToolsServeStructuredPreview(t *testing.T) {
	t.Parallel()
	ctx, principal, service, management, input := scopeToolFixture(t)
	server := newTestMCPServer()
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(callCtx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			auth, _ := contextvalues.GetAuthContext(ctx)
			callCtx = contextvalues.SetAuthContext(callCtx, auth)
			return next(contextWithPrincipal(callCtx, principal), method, req)
		}
	})
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerMCPScopesTools(reg, service)
	session := connectTestClient(t, server)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "set_mcp_scope_pin", Arguments: input})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.NotNil(t, result.StructuredContent)
	require.Zero(t, management.writes)
}

func TestMCPScopesRequiresMatchingResourceVersionEvenForNoop(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"", "stale-version"} {
		ctx, principal, service, management, input := scopeToolFixture(t)
		input.Confirmed = true
		input.Scopes = []string{"read"}
		input.ExpectedVersion = version
		_, err := service.set(ctx, principal, input)
		require.ErrorContains(t, err, "affected MCP servers or resource changed")
		require.Zero(t, management.writes)
	}
}

func TestMCPScopesClearsWithoutDiscoveryThroughOwningAuthorization(t *testing.T) {
	t.Parallel()
	for _, alreadyEmpty := range []bool{false, true} {
		ctx, principal, service, management, input := scopeToolFixture(t)
		management.state.CanPin = false
		management.state.DiscoveryEnabled = false
		input.Scopes = []string{}
		if alreadyEmpty {
			management.state.PinnedScopes = []string{}
			input.ExpectedScopes = []string{}
		}
		preview, err := service.set(ctx, principal, input)
		require.NoError(t, err)
		require.True(t, preview.ConfirmationRequired)
		require.Zero(t, management.writes)
		input.Confirmed = true
		management.writeError = ErrForbidden
		_, err = service.set(ctx, principal, input)
		require.ErrorIs(t, err, ErrForbidden, "even an empty clear requires owning mcp:write authorization")
		require.Equal(t, 1, management.writes)
		management.writeError = nil
		result, err := service.set(ctx, principal, input)
		require.NoError(t, err)
		require.Empty(t, result.PinnedScopes)
		require.True(t, result.Verified)
		require.Equal(t, 2, management.writes)
	}
}
