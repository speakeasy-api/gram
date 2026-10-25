package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp/visibility"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestGatewayClientAdmissionToolSchemaMatchesUnavailable(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "admission-live", Version: "0.0.1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "admission-unavailable", Version: "0.0.1"}, nil))
	registerGatewayClientAdmissionTool(live, nil)
	registerUnavailableGatewayClientAdmissionTool(unavailable)
	require.JSONEq(t, string(live.Descriptors()[0].InputSchema), string(unavailable.Descriptors()[0].InputSchema))
	require.Equal(t, live.Descriptors()[0].Meta, unavailable.Descriptors()[0].Meta)
	require.Equal(t, bothAudiences, live.Descriptors()[0].Meta.Audiences)
	require.Equal(t, ProjectScopeExplicit, live.Descriptors()[0].Meta.ProjectScope)
	require.Equal(t, discoveryProjectRead, live.Descriptors()[0].Meta.DiscoveryScopes, "no log:read: this is configuration, not attempts")
	require.Equal(t, ExternalAuthorizationMember, live.Descriptors()[0].Meta.Authorization)
	require.True(t, live.Descriptors()[0].Annotations.ReadOnlyHint)
}

// Every composed catalog keeps the tool, refusing readably when its reader is
// not wired, rather than dropping it.
func TestGatewayClientAdmissionStaysInEveryCatalog(t *testing.T) {
	t.Parallel()
	_, withoutReader := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	_, withoutDB := newServer(NewPostgresReader(testenv.NewLogger(t), nil), nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "admission-live", Version: "0.0.1"}, nil))
	registerGatewayClientAdmissionTool(live, nil)
	want := descriptorByName(t, live, gatewayClientAdmissionToolName)
	for name, registrar := range map[string]*Registrar{"no reader": withoutReader, "no database": withoutDB} {
		descriptor := descriptorByName(t, registrar, gatewayClientAdmissionToolName)
		require.Equal(t, want.Meta, descriptor.Meta, name)
		require.JSONEq(t, string(want.InputSchema), string(descriptor.InputSchema), name)
		arguments := fmt.Sprintf(`{"project_id":%q,"gateway_id":%q}`, uuid.NewString(), uuid.NewString())
		_, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testPrincipal()), json.RawMessage(arguments))
		// With a real principal the call reaches the reader; what comes back is
		// already safe to show: the stub's refusal or the bare sentinel.
		safe, ok := recognisedToolError(err)
		require.True(t, ok, "%s: %v", name, err)
		if refusal, isRefusal := errors.AsType[*ToolRefusalError](safe); isRefusal {
			require.Equal(t, unavailableCode, refusal.Code, name)
		} else {
			require.Equal(t, ErrUnavailable, safe, name)
		}
	}
}

func TestGatewayClientAdmissionOverCallTool(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_gateway_client_admission")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	sessions := usersessionsrepo.New(conn)
	issuerIn := func(projectID uuid.UUID, mode string) uuid.UUID {
		issuer, err := sessions.CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{ProjectID: projectID, OrganizationID: pgtype.Text{String: principal.OrganizationID, Valid: true}, Slug: "admission-" + uuid.NewString()[:8], AuthnChallengeMode: "interactive", SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true}, AuthorizationServerMode: pgtype.Text{}, UseAuthenticationHost: pgtype.Bool{}})
		require.NoError(t, err)
		if mode != "" {
			_, err = sessions.UpdateUserSessionIssuer(ctx, usersessionsrepo.UpdateUserSessionIssuerParams{Slug: pgtype.Text{}, AuthnChallengeMode: pgtype.Text{}, SessionDuration: pgtype.Interval{}, ClientIDMetadataAdmissionMode: pgtype.Text{String: mode, Valid: true}, ID: issuer.ID, ProjectID: projectID})
			require.NoError(t, err)
		}
		return issuer.ID
	}
	gatewayIn := func(projectID uuid.UUID, issuerID uuid.NullUUID) uuid.UUID {
		row, err := metamcprepo.New(conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{OrganizationID: principal.OrganizationID, ProjectID: projectID, Name: "Gateway", UserSessionIssuerID: issuerID, Visibility: visibility.Private, NetworkAccessMode: pgtype.Text{String: "", Valid: false}})
		require.NoError(t, err)
		return row.ID
	}
	presets := issuerIn(project.ID, "presets")
	urls := map[string]bool{}
	for i := range gatewayClientAdmissionURLLimit + 1 {
		url := fmt.Sprintf("https://client-%d.example.test/metadata.json", i)
		_, err := sessions.CreateUserSessionIssuerCimdClient(ctx, usersessionsrepo.CreateUserSessionIssuerCimdClientParams{ClientIDMetadataUri: url, UserSessionIssuerID: presets, ProjectID: project.ID})
		require.NoError(t, err)
		urls[url] = true
	}
	restricted := gatewayIn(project.ID, uuid.NullUUID{UUID: presets, Valid: true})
	unset := gatewayIn(project.ID, uuid.NullUUID{UUID: issuerIn(project.ID, ""), Valid: true})
	// Stored custom URLs that do not apply in reporting or disabled.
	withStoredURL := func(issuerID uuid.UUID) uuid.UUID {
		_, err := sessions.CreateUserSessionIssuerCimdClient(ctx, usersessionsrepo.CreateUserSessionIssuerCimdClientParams{ClientIDMetadataUri: "https://stored-" + uuid.NewString()[:8] + ".example.test/metadata.json", UserSessionIssuerID: issuerID, ProjectID: project.ID})
		require.NoError(t, err)
		return issuerID
	}
	reporting := gatewayIn(project.ID, uuid.NullUUID{UUID: withStoredURL(issuerIn(project.ID, "reporting")), Valid: true})
	disabled := gatewayIn(project.ID, uuid.NullUUID{UUID: withStoredURL(issuerIn(project.ID, "disabled")), Valid: true})
	unrecognized := gatewayIn(project.ID, uuid.NullUUID{UUID: issuerIn(project.ID, "not-a-mode"), Valid: true})
	anonymous := gatewayIn(project.ID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	other, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Other", Slug: "other-" + uuid.NewString()[:8], OrganizationID: principal.OrganizationID})
	require.NoError(t, err)
	elsewhere := gatewayIn(other.ID, uuid.NullUUID{UUID: issuerIn(other.ID, "disabled"), Valid: true})
	foreignIssuer := gatewayIn(project.ID, uuid.NullUUID{UUID: issuerIn(other.ID, "presets"), Valid: true})

	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	reader := NewPostgresReader(logger, conn).WithAuthorization(engine)
	projectRead := authz.NewGrant(authz.ScopeProjectRead, project.ID.String())
	gatewaysRead := []authz.Grant{projectRead}
	for _, id := range []uuid.UUID{restricted, unset, reporting, disabled, unrecognized, anonymous, foreignIssuer} {
		gatewaysRead = append(gatewaysRead, authz.NewGrant(authz.ScopeMCPRead, id.String()))
	}
	grants := gatewaysRead
	server := newTestMCPServer()
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			// The dashboard context names another project; the tool reads only
			// the project its arguments name.
			ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID, ProjectID: &other.ID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
			return next(authz.GrantsToContext(contextWithPrincipal(ctx, principal), grants), method, req)
		}
	})
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerGatewayClientAdmissionTool(reg, reader)
	session := connectTestClient(t, server)
	call := func(projectID string, gatewayID uuid.UUID) (*mcp.CallToolResult, GatewayClientAdmissionOutput) {
		t.Helper()
		var output GatewayClientAdmissionOutput
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: gatewayClientAdmissionToolName, Arguments: map[string]any{"project_id": projectID, "gateway_id": gatewayID.String()}})
		require.NoError(t, err)
		if result.IsError {
			return result, output
		}
		body, err := json.Marshal(result.StructuredContent)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &output))
		return result, output
	}
	refusal := func(projectID string, gatewayID uuid.UUID) string {
		t.Helper()
		result, _ := call(projectID, gatewayID)
		require.True(t, result.IsError)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		require.NotContains(t, text.Text, unavailableCode, "permissions and missing targets are never reported as unavailable")
		return text.Text
	}
	exact := project.ID.String()

	_, output := call(exact, restricted)
	require.Equal(t, exact, output.ProjectID)
	require.Equal(t, restricted.String(), output.GatewayID)
	require.True(t, output.SignIn)
	require.Equal(t, "presets", output.Mode)
	require.Contains(t, output.Meaning, "Verified clients")
	require.Len(t, output.CustomClientURLs, gatewayClientAdmissionURLLimit, "a bounded first page")
	require.True(t, output.MoreCustomURLs, "one more URL is configured than listed")
	for _, url := range output.CustomClientURLs {
		require.True(t, urls[url], url)
	}

	_, output = call(exact, unset)
	require.Equal(t, []any{true, "open", false}, []any{output.SignIn, output.Mode, output.MoreCustomURLs}, "an unset mode is the effective open")
	require.Empty(t, output.CustomClientURLs)
	_, output = call(exact, reporting)
	require.Equal(t, "reporting", output.Mode)
	require.Equal(t, []any{[]string{}, false}, []any{output.CustomClientURLs, output.MoreCustomURLs}, "stored URLs apply only in presets")
	require.Contains(t, output.Meaning, "Any client (legacy reporting)")
	_, output = call(exact, disabled)
	require.Equal(t, []any{true, "disabled"}, []any{output.SignIn, output.Mode})
	require.Contains(t, output.Meaning, "dynamic client registration")
	require.Equal(t, []any{[]string{}, false}, []any{output.CustomClientURLs, output.MoreCustomURLs}, "stored URLs apply only in presets")
	_, output = call(exact, unrecognized)
	require.Equal(t, "disabled", output.Mode, "an unknown stored value reads as the disabled enforcement fails closed to")
	_, output = call(exact, anonymous)
	require.False(t, output.SignIn, "a gateway without a sign-in server")
	require.Empty(t, output.Mode)
	require.NotNil(t, output.CustomClientURLs)

	require.Contains(t, refusal("", restricted), "project_id is required", "an empty project never falls back to Default")
	require.Contains(t, refusal("  ", restricted), "project_id is required")
	require.Contains(t, refusal(exact, elsewhere), "not_found", "another project's gateway")
	require.Contains(t, refusal(other.ID.String(), elsewhere), ErrForbidden.Error(), "a project without project:read")
	require.Contains(t, refusal(uuid.NewString(), restricted), ErrForbidden.Error(), "a project outside the organization")
	require.Contains(t, refusal(exact, uuid.New()), "not_found", "no such gateway")
	require.Contains(t, refusal(exact, foreignIssuer), "admission setting is unknown", "a sign-in server out of scope is unknown, never none")

	grants = []authz.Grant{projectRead}
	require.Contains(t, refusal(exact, restricted), "not_found", "a gateway the caller may not read is hidden")
	grants = gatewaysRead[1:]
	require.Contains(t, refusal(exact, restricted), ErrForbidden.Error(), "no project:read")
	grants = gatewaysRead
	_, output = call(exact, restricted)
	require.Equal(t, "presets", output.Mode, "log:read is not required")
}
