package platformmcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	networkingressrepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestNetworkIngressToolHasSameContractWhenUnavailable(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "ingress-live", Version: "0.0.1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "ingress-unavailable", Version: "0.0.1"}, nil))
	registerNetworkIngressTool(live, nil)
	registerUnavailableNetworkIngressTool(unavailable)
	require.JSONEq(t, string(live.Descriptors()[0].InputSchema), string(unavailable.Descriptors()[0].InputSchema))
	require.Equal(t, live.Descriptors()[0].Meta, unavailable.Descriptors()[0].Meta)
	require.Equal(t, ExternalAuthorizationOrgAdmin, live.Descriptors()[0].Meta.Authorization)
	require.Equal(t, ProjectScopeNone, live.Descriptors()[0].Meta.ProjectScope)
	require.Equal(t, []Audience{AudienceExternal}, live.Descriptors()[0].Meta.Audiences)

	server := mcp.NewServer(&mcp.Implementation{Name: "ingress-fallback", Version: "0.0.1"}, nil)
	bindExternalTestPrincipal(server)
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerUnavailableNetworkIngressTool(reg)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "ingress-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: getNetworkIngressToolName, Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, `"code":"feature_unavailable"`)
}

func TestNetworkIngressToolHidesUnexpectedErrors(t *testing.T) {
	t.Parallel()
	pool, err := pgxpool.New(t.Context(), "postgres://sentinel-db-user@sentinel-db-host:5432/sentinel_db")
	require.NoError(t, err)
	pool.Close()
	reader := NewPostgresReader(testenv.NewLogger(t), pool).WithNetworkIngressStatus(mustParseURL(t, "https://app.getgram.test"))
	require.NotNil(t, reader.networkIngress)

	server := mcp.NewServer(&mcp.Implementation{Name: "ingress-error", Version: "0.0.1"}, nil)
	bindExternalTestPrincipal(server)
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerNetworkIngressTool(reg, reader.networkIngress)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "ingress-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: getNetworkIngressToolName, Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, `"code":"feature_unavailable"`)
	for _, forbidden := range []string{"sentinel", "pool", "resolve network ingress organization"} {
		require.NotContains(t, text.Text, forbidden)
	}
}

func TestSafeNetworkIngressErrorCodeWithholdsFreeText(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"provider_error", "kubernetes_api", "invalid_credentials", "deletion_pending"} {
		require.Equal(t, code, safeNetworkIngressErrorCode(code))
	}
	for _, value := range []string{
		"", "Provider_Error", "tskey-client-sentinel secret rejected", "error: https://api.tailscale.com/x",
		"provider-error", "_leading", strings.Repeat("a", 65),
	} {
		require.Empty(t, safeNetworkIngressErrorCode(value), value)
	}
}

func TestNetworkIngressNextActionNamesTheFirstBlockingStep(t *testing.T) {
	t.Parallel()
	ready := NetworkIngressSummary{Enabled: true, CredentialsConfigured: true, Status: "online", DNSName: "private-mcp.example.ts.net"}
	with := func(mutate func(*NetworkIngressSummary)) *NetworkIngressSummary {
		value := ready
		mutate(&value)
		return &value
	}
	for _, tc := range []struct {
		name     string
		output   GetNetworkIngressOutput
		expected string
	}{
		{"not entitled wins over a ready ingress", GetNetworkIngressOutput{Entitled: false, Ingress: &ready}, networkIngressNextRequestEntitlement},
		{"entitled without ingress", GetNetworkIngressOutput{Entitled: true}, networkIngressNextConfigure},
		{"missing credentials", GetNetworkIngressOutput{Entitled: true, Ingress: with(func(i *NetworkIngressSummary) { i.CredentialsConfigured = false })}, networkIngressNextAddCredentials},
		{"disabled", GetNetworkIngressOutput{Entitled: true, Ingress: with(func(i *NetworkIngressSummary) { i.Enabled = false; i.Status = "disabled" })}, networkIngressNextEnable},
		{"error", GetNetworkIngressOutput{Entitled: true, Ingress: with(func(i *NetworkIngressSummary) { i.Status = "error" })}, networkIngressNextRepair},
		{"degraded", GetNetworkIngressOutput{Entitled: true, Ingress: with(func(i *NetworkIngressSummary) { i.Status = "degraded" })}, networkIngressNextRepair},
		{"pending", GetNetworkIngressOutput{Entitled: true, Ingress: with(func(i *NetworkIngressSummary) { i.Status = "pending" })}, networkIngressNextWait},
		{"online without DNS", GetNetworkIngressOutput{Entitled: true, Ingress: with(func(i *NetworkIngressSummary) { i.DNSName = "" })}, networkIngressNextWait},
		{"ready", GetNetworkIngressOutput{Entitled: true, Ingress: &ready}, networkIngressNextNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, networkIngressNextAction(tc.output))
		})
	}
}

func TestGetNetworkIngressReportsLiveStateWithoutCredentials(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_network_ingress")
	require.NoError(t, err)
	principal, _ := seedRegistrationLifecycle(t, ctx, conn)
	organization, err := organizationsrepo.New(conn).GetOrganizationMetadata(ctx, principal.OrganizationID)
	require.NoError(t, err)
	service := NewPostgresReader(testenv.NewLogger(t), conn).
		WithNetworkIngressStatus(mustParseURL(t, "https://app.getgram.test")).networkIngress
	require.NotNil(t, service)

	got, err := service.Get(ctx, principal)
	require.NoError(t, err)
	require.False(t, got.Entitled)
	require.False(t, got.Configured)
	require.False(t, got.ReadyForPrivateAccess)
	require.Nil(t, got.Ingress)
	require.Equal(t, networkIngressNextRequestEntitlement, got.NextAction)
	require.Equal(t, "https://app.getgram.test/"+organization.Slug+"/domains", got.SetupURL)

	_, err = featurerepo.New(conn).EnableFeature(ctx, featurerepo.EnableFeatureParams{
		OrganizationID: principal.OrganizationID,
		FeatureName:    string(productfeatures.FeatureNetworkIngress),
	})
	require.NoError(t, err)
	got, err = service.Get(ctx, principal)
	require.NoError(t, err)
	require.True(t, got.Entitled)
	require.Equal(t, networkIngressNextConfigure, got.NextAction)

	const credentialSentinel = "sentinel-encrypted-tailscale-credentials"
	const resourceSentinel = "sentinel-provider-resource"
	attestor := "attestor-" + uuid.NewString()[:8]
	created, err := networkingressrepo.New(conn).CreateNetworkIngress(ctx, networkingressrepo.CreateNetworkIngressParams{
		ID:                     uuid.New(),
		OrganizationID:         principal.OrganizationID,
		Provider:               "tailscale",
		Hostname:               "private-mcp",
		EndpointNamespaceKind:  "platform",
		CustomDomainID:         uuid.NullUUID{},
		Enabled:                true,
		IdentityRequired:       false,
		CredentialsEncrypted:   pgtype.Text{String: credentialSentinel, Valid: true},
		AttestorNamespace:      attestor,
		AttestorServiceAccount: attestor,
		ProviderResources:      []byte(`{"tailnet_key":"` + resourceSentinel + `"}`),
	})
	require.NoError(t, err)
	got, err = service.Get(ctx, principal)
	require.NoError(t, err)
	require.True(t, got.Configured)
	require.NotNil(t, got.Ingress)
	require.True(t, got.Ingress.CredentialsConfigured)
	require.Equal(t, "pending", got.Ingress.Status)
	require.Equal(t, networkIngressNextWait, got.NextAction)
	require.False(t, got.ReadyForPrivateAccess)

	updated, err := networkingressrepo.New(conn).RecordNetworkIngressObservation(ctx, networkingressrepo.RecordNetworkIngressObservationParams{
		Status: "online", DnsName: pgtype.Text{String: "private-mcp.example.ts.net", Valid: true}, LastError: pgtype.Text{},
		ID: created.ID, OrganizationID: principal.OrganizationID, ExpectedUpdatedAt: created.UpdatedAt,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, updated)
	got, err = service.Get(ctx, principal)
	require.NoError(t, err)
	require.True(t, got.ReadyForPrivateAccess)
	require.Equal(t, networkIngressNextNone, got.NextAction)
	require.Equal(t, "private-mcp.example.ts.net", got.Ingress.DNSName)
	require.NotEmpty(t, got.Ingress.ConnectedSince)
	require.NotEmpty(t, got.Ingress.HealthCheckedAt)

	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	for _, forbidden := range []string{credentialSentinel, resourceSentinel, attestor, principal.OrganizationID} {
		require.NotContains(t, string(encoded), forbidden)
	}

	foreign := principal
	foreign.OrganizationID = "other-org-" + uuid.NewString()
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID: foreign.OrganizationID, Name: "Other organization", Slug: "other-" + uuid.NewString()[:8],
		WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	other, err := service.Get(ctx, foreign)
	require.NoError(t, err)
	require.False(t, other.Entitled)
	require.False(t, other.Configured)
	require.Nil(t, other.Ingress)
}
