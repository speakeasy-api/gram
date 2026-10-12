package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestEnterpriseIssuerToolContract(t *testing.T) {
	t.Parallel()
	reg := newRegistrar(newTestMCPServer())
	registerEnterpriseIssuerTool(reg, nil)
	d := reg.Descriptors()[0]
	require.Equal(t, "get_enterprise_issuer", d.Name)
	require.Equal(t, ExternalAuthorizationOrgAdmin, d.Meta.Authorization)
	require.Equal(t, ProjectScopeNone, d.Meta.ProjectScope)
	require.Equal(t, externalOnly, d.Meta.Audiences)
	require.True(t, d.Annotations.ReadOnlyHint)
	require.Empty(t, reg.For(AudienceAssistant))
}

func TestEnterpriseIssuerToolSetupStates(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool, err := platformMCPInfra.CloneTestDatabase(t, "enterprise_issuer_tool")
	require.NoError(t, err)
	principal, _ := seedRegistrationLifecycle(t, ctx, pool)
	redis, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	features := productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), pool, redis)
	reader := NewPostgresReader(testenv.NewLogger(t), pool).WithEnterpriseIssuer(features, true)
	server := newTestMCPServer()
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			return next(contextWithPrincipal(ctx, principal), method, req)
		}
	})
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerEnterpriseIssuerTool(reg, reader)
	session := connectTestClient(t, server)
	read := func() EnterpriseIssuerStatus {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_enterprise_issuer", Arguments: map[string]any{}})
		require.NoError(t, err)
		require.False(t, result.IsError)
		body, err := json.Marshal(result.StructuredContent)
		require.NoError(t, err)
		var status EnterpriseIssuerStatus
		require.NoError(t, json.Unmarshal(body, &status))
		return status
	}

	status := read()
	require.Equal(t, EnterpriseIssuerEntitlementRequired, status.State)
	require.Empty(t, status.Issuer)
	require.NoError(t, features.SetFeatureEnabled(ctx, principal.OrganizationID, productfeatures.FeatureSSO, true))
	reader.WithEnterpriseIssuer(features, false)
	require.Equal(t, EnterpriseIssuerProvisioningUnavailable, read().State)
	reader.WithEnterpriseIssuer(features, true)
	require.Equal(t, EnterpriseIssuerSetupRequired, read().State)

	credential := provisiontest.CreatePlatformSigningCredential(t, ctx, pool)
	provisioner := provisiontest.NewProvisioner(t, pool, provisiontest.NewKMSClients(t).Factory, "https://issuer.example.com", credential, "")
	_, err = provisioner.EnsureEMAIssuer(ctx, principal.OrganizationID)
	require.NoError(t, err)
	status = read()
	require.Equal(t, EnterpriseIssuerEnabled, status.State)
	require.Equal(t, "https://issuer.example.com/oauth/idp/org/"+principal.OrganizationID, status.Issuer)

	require.NoError(t, provisioner.DisableEMAIssuer(ctx, principal.OrganizationID))
	status = read()
	require.Equal(t, EnterpriseIssuerDisabled, status.State)
	require.Equal(t, "https://issuer.example.com/oauth/idp/org/"+principal.OrganizationID, status.Issuer)

	// A stored issuer must not bypass missing deployment configuration.
	reader.WithEnterpriseIssuer(nil, true)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_enterprise_issuer", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	body, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(body), status.Issuer)
}
