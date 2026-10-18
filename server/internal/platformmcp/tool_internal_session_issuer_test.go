package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestInternalSessionIssuerToolContract(t *testing.T) {
	t.Parallel()
	reg := newRegistrar(newTestMCPServer())
	registerInternalSessionIssuerTool(reg, nil)
	d := reg.Descriptors()[0]
	require.Equal(t, "get_internal_session_issuer", d.Name)
	require.Equal(t, ExternalAuthorizationOrgAdmin, d.Meta.Authorization)
	require.Equal(t, ProjectScopeNone, d.Meta.ProjectScope)
	require.Equal(t, externalOnly, d.Meta.Audiences)
	require.True(t, d.Annotations.ReadOnlyHint)
	require.Empty(t, reg.For(AudienceAssistant))
	require.Contains(t, internalSessionIssuerNextSteps[internalsessionissuers.StateEnabled], "company-managed Google or Microsoft account, magic link, or password")
	require.Contains(t, internalSessionIssuerNextSteps[internalsessionissuers.StateEnabled], "registering alone changes nothing")
	require.Contains(t, internalSessionIssuerNextSteps[internalsessionissuers.StateEnabled], "chooses which identity providers a service accepts on IDP and SSO > Identity providers > Services")
}

func TestInternalSessionIssuerToolStates(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool, err := platformMCPInfra.CloneTestDatabase(t, "internal_session_issuer_tool")
	require.NoError(t, err)
	principal, _ := seedRegistrationLifecycle(t, ctx, pool)
	setDomains := func(domains ...string) {
		t.Helper()
		require.NoError(t, testrepo.New(pool).SetOrganizationVerifiedDomainsFixture(ctx, testrepo.SetOrganizationVerifiedDomainsFixtureParams{ID: principal.OrganizationID, VerifiedDomains: append([]string{}, domains...)}))
	}
	setDomains()
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagInternalSessionIssuerSigningPaused, principal.OrganizationID, false)
	gate := func(provisioning bool) *internalsessionissuers.Gate {
		return internalsessionissuers.NewGate(pool, flags, internalsessionissuers.GateConfig{Provisioning: provisioning, SigningPaused: false})
	}
	reader := NewPostgresReader(testenv.NewLogger(t), pool).WithInternalSessionIssuer(gate(true))
	server := newTestMCPServer()
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			return next(contextWithPrincipal(ctx, principal), method, req)
		}
	})
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerInternalSessionIssuerTool(reg, reader)
	session := connectTestClient(t, server)
	read := func() InternalSessionIssuerStatus {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_internal_session_issuer", Arguments: map[string]any{}})
		require.NoError(t, err)
		require.False(t, result.IsError)
		body, err := json.Marshal(result.StructuredContent)
		require.NoError(t, err)
		var status InternalSessionIssuerStatus
		require.NoError(t, json.Unmarshal(body, &status))
		return status
	}

	require.Equal(t, internalsessionissuers.StateUnavailable, read().State, "rollout flag off")
	flags.SetFlag(feature.FlagInternalSessionIssuer, principal.OrganizationID, true)
	require.Equal(t, internalsessionissuers.StateVerifiedDomainRequired, read().State)
	setDomains("example.com")
	reader.WithInternalSessionIssuer(gate(false))
	require.Equal(t, internalsessionissuers.StateUnavailable, read().State, "deployment can't provision")
	reader.WithInternalSessionIssuer(gate(true))
	statusBeforeSetup := read()
	require.Equal(t, internalsessionissuers.StateSetupRequired, statusBeforeSetup.State)
	require.True(t, statusBeforeSetup.CanEnable)

	credential := provisiontest.CreatePlatformSigningCredential(t, ctx, pool)
	provisioner := provisiontest.NewProvisioner(t, pool, provisiontest.NewKMSClients(t).Factory, "https://issuer.example.com", credential, "")
	_, err = provisioner.EnsureEMAIssuer(ctx, principal.OrganizationID)
	require.NoError(t, err)
	issuer := "https://issuer.example.com/oauth/idp/org/" + principal.OrganizationID
	status := read()
	require.Equal(t, internalsessionissuers.StateEnabled, status.State)
	require.Equal(t, issuer, status.Issuer)

	flags.SetFlag(feature.FlagInternalSessionIssuerSigningPaused, principal.OrganizationID, true)
	require.Equal(t, internalsessionissuers.StateSigningPaused, read().State)
	flags.SetFlag(feature.FlagInternalSessionIssuerSigningPaused, principal.OrganizationID, false)

	require.NoError(t, internalsessionissuers.NewLifecycle(pool, audit.NewLogger()).SetDisabled(ctx, principal.OrganizationID, true))
	status = read()
	require.Equal(t, internalsessionissuers.StateDisabled, status.State)
	require.Equal(t, issuer, status.Issuer)
	require.True(t, status.CanEnable)
	setDomains()
	status = read()
	require.Equal(t, internalsessionissuers.StateDisabled, status.State)
	require.False(t, status.CanEnable)
	require.NotContains(t, status.NextStep, "An organization administrator can")
	setDomains("example.com")

	_, err = provisioner.EnsureEMAIssuer(ctx, principal.OrganizationID)
	require.NoError(t, err)
	flags.SetFlag(feature.FlagInternalSessionIssuer, principal.OrganizationID, false)
	status = read()
	require.Equal(t, internalsessionissuers.StateUnavailable, status.State)
	require.Empty(t, status.Issuer, "a rolled-back organization sees no issuer")
	require.NoError(t, internalsessionissuers.NewLifecycle(pool, audit.NewLogger()).SetDisabled(ctx, principal.OrganizationID, true))
	status = read()
	require.Equal(t, internalsessionissuers.StateDisabled, status.State)
	require.Empty(t, status.Issuer, "rollback hides a disabled issuer too")

	// Missing deployment configuration must not leak a stored issuer.
	reader.WithInternalSessionIssuer(nil)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_internal_session_issuer", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	body, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(body), issuer)
}
