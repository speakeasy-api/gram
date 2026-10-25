package platformmcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp/visibility"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestGatewayConnectionHealthToolSchemaMatchesUnavailable(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "health-live", Version: "0.0.1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "health-unavailable", Version: "0.0.1"}, nil))
	registerGatewayConnectionHealthTool(live, nil)
	registerUnavailableGatewayConnectionHealthTool(unavailable)
	require.JSONEq(t, string(live.Descriptors()[0].InputSchema), string(unavailable.Descriptors()[0].InputSchema))
	require.Equal(t, live.Descriptors()[0].Meta, unavailable.Descriptors()[0].Meta)
	require.Equal(t, bothAudiences, live.Descriptors()[0].Meta.Audiences)
	require.Equal(t, ProjectScopeExplicit, live.Descriptors()[0].Meta.ProjectScope)
	require.Equal(t, discoveryLogRead, live.Descriptors()[0].Meta.DiscoveryScopes)
}

// Every composed catalog keeps the tool, refusing readably when connection
// health is not wired, rather than dropping it.
func TestGatewayConnectionHealthStaysInEveryCatalog(t *testing.T) {
	t.Parallel()
	_, withoutReader := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	_, withoutHealth := newServer(NewPostgresReader(testenv.NewLogger(t), nil), nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "health-live", Version: "0.0.1"}, nil))
	registerGatewayConnectionHealthTool(live, nil)
	want := descriptorByName(t, live, gatewayConnectionHealthToolName)
	for name, registrar := range map[string]*Registrar{"no reader": withoutReader, "no health reader": withoutHealth} {
		descriptor := descriptorByName(t, registrar, gatewayConnectionHealthToolName)
		require.Equal(t, want.Meta, descriptor.Meta, name)
		require.JSONEq(t, string(want.InputSchema), string(descriptor.InputSchema), name)
		_, err := descriptor.invoke(t.Context(), json.RawMessage(`{"project_id":"p","gateway_id":"g"}`))
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal, name)
		require.Equal(t, unavailableCode, refusal.Code, name)
	}
}

func TestGatewayConnectionHealthOverCallTool(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_connection_health")
	require.NoError(t, err)
	chConn, err := platformMCPInfra.NewClickhouseClient(t)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	// The caller is a member, so the attempts they made name them.
	_, err = usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{ID: principal.UserID, Email: "health-member@example.invalid", DisplayName: "Health Member", PhotoUrl: pgtype.Text{}, Admin: false})
	require.NoError(t, err)
	_, err = organizationsrepo.New(conn).UpsertOrganizationUserRelationship(ctx, organizationsrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: principal.OrganizationID, UserID: pgtype.Text{String: principal.UserID, Valid: true}})
	require.NoError(t, err)
	sessions := usersessionsrepo.New(conn)
	issuer, err := sessions.CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{ProjectID: project.ID, OrganizationID: pgtype.Text{String: principal.OrganizationID, Valid: true}, Slug: "health-" + uuid.NewString()[:8], AuthnChallengeMode: "interactive", SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true}, AuthorizationServerMode: pgtype.Text{}, UseAuthenticationHost: pgtype.Bool{}})
	require.NoError(t, err)
	client, err := sessions.CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{ClientID: "client-" + uuid.NewString(), ClientSecretHash: pgtype.Text{}, ClientName: "Example Desktop", RedirectUris: []string{"http://127.0.0.1/callback"}, ClientSecretExpiresAt: pgtype.Timestamptz{}, TokenEndpointAuthMethod: "none", ClientJwks: nil, ClientJwksUri: pgtype.Text{}, UserSessionIssuerID: issuer.ID})
	require.NoError(t, err)
	gateway := func(projectID uuid.UUID, issuerID uuid.NullUUID) uuid.UUID {
		row, err := metamcprepo.New(conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{OrganizationID: principal.OrganizationID, ProjectID: projectID, Name: "Gateway", UserSessionIssuerID: issuerID, Visibility: visibility.Private, NetworkAccessMode: pgtype.Text{String: "", Valid: false}})
		require.NoError(t, err)
		return row.ID
	}
	gatewayID := gateway(project.ID, uuid.NullUUID{UUID: issuer.ID, Valid: true})
	other, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Other", Slug: "other-" + uuid.NewString()[:8], OrganizationID: principal.OrganizationID})
	require.NoError(t, err)
	elsewhere := gateway(other.ID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	providerIssuer, err := remotesessionsrepo.New(conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID: conv.ToNullUUID(project.ID), OrganizationID: conv.ToPGText(principal.OrganizationID),
		Slug: "health-provider-" + uuid.NewString()[:8], Issuer: "https://provider.example.test",
		ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{},
		TokenEndpointAuthMethodsSupported: []string{}, CodeChallengeMethodsSupported: []string{},
	})
	require.NoError(t, err)
	provider := providerIssuer.ID.String()

	logger := testenv.NewLogger(t)
	enabled := func(context.Context, string) (bool, error) { return true, nil }
	writer := telemetry.NewLogger(ctx, logger, nil, nil, chConn, enabled, enabled, nil, telemetry.NewNoopLogPublisher(logger))
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	member := urn.NewUserSubject(principal.UserID).String()
	outsider := urn.NewUserSubject("user_" + uuid.NewString()).String()
	agent := urn.NewAgentSubject(uuid.New()).String()
	unknownProvider := uuid.NewString()
	registered := client.ID.String()
	attempt := func(projectID, gatewayID uuid.UUID, stage, outcome, subject, provider, clientKey string) telemetry.LogParams {
		attrs := map[attr.Key]any{
			attr.EventURNKey:                    "urn:telemetry:gram_service:log:mcp_connection_attempt",
			attr.MetaMcpServerIDKey:             gatewayID.String(),
			attr.ConnectionAttemptStageKey:      stage,
			attr.ConnectionAttemptOutcomeKey:    outcome,
			attr.ConnectionAttemptReasonKey:     "",
			attr.ConnectionAttemptSubjectKey:    subject,
			attr.ConnectionAttemptProviderIDKey: provider,
			attr.ConnectionAttemptClientKeyKey:  clientKey,
		}
		if stage == telemetry.ConnectionStageDiscoveryMember {
			attrs[attr.McpServerIDKey] = uuid.NewString()
		}
		timestamp := at
		if stage == telemetry.ConnectionStageInboundToken && outcome == "refreshed" {
			timestamp = at.Add(time.Minute)
		}
		return telemetry.LogParams{Timestamp: timestamp, ToolInfo: telemetry.ToolInfo{ID: "", URN: "", Name: "", ProjectID: projectID.String(), DeploymentID: "", FunctionID: nil, OrganizationID: principal.OrganizationID}, UserInfo: telemetry.UserInfoByID(""), Attributes: attrs}
	}
	member5 := func(outcome string) telemetry.LogParams {
		return attempt(project.ID, gatewayID, telemetry.ConnectionStageDiscoveryMember, outcome, member, "", registered)
	}
	require.NoError(t, writer.LogBulk(ctx, []telemetry.LogParams{
		attempt(project.ID, gatewayID, telemetry.ConnectionStageDiscoveryListing, "complete", member, "", registered),
		member5("listed"), member5("listed"), member5("listed"), member5("failed"), member5("failed"),
		attempt(project.ID, gatewayID, telemetry.ConnectionStageDiscoveryMember, "listed", "", "", "anonymous"),
		attempt(project.ID, gatewayID, telemetry.ConnectionStageDiscoveryMember, "listed", "", "", "anonymous"),
		attempt(project.ID, gatewayID, telemetry.ConnectionStageDiscoveryMember, "failed", outsider, "", "anonymous"),
		attempt(project.ID, gatewayID, telemetry.ConnectionStageDiscoveryMember, "failed", agent, "", "credential:agent"),
		attempt(project.ID, gatewayID, telemetry.ConnectionStageDiscoveryMember, "failed", member, unknownProvider, registered),
		attempt(other.ID, elsewhere, telemetry.ConnectionStageDiscoveryMember, "failed", member, "", "anonymous"),
		attempt(project.ID, gatewayID, telemetry.ConnectionStageInboundToken, "code_issued", member, "", registered),
		attempt(project.ID, gatewayID, telemetry.ConnectionStageInboundToken, "refreshed", member, "", registered),
		attempt(project.ID, gatewayID, telemetry.ConnectionStageInboundToken, "policy_blocked", member, "", registered),
		attempt(project.ID, gatewayID, telemetry.ConnectionStageUpstreamCallback, "stored", member, provider, registered),
		attempt(project.ID, gatewayID, telemetry.ConnectionStageUpstreamCallback, "failed", member, provider, registered),
		attempt(project.ID, gatewayID, telemetry.ConnectionStageUpstreamRefresh, "refreshed", member, provider, registered),
		attempt(project.ID, gatewayID, telemetry.ConnectionStageUpstreamRefresh, "adopted", member, provider, registered),
		attempt(project.ID, gatewayID, telemetry.ConnectionStageUpstreamRefresh, "failed", member, provider, registered),
	}))

	engine := authz.NewEngine(logger, conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	reader := NewPostgresReader(logger, conn).WithAuthorization(engine).WithConnectionHealth(telemetry.NewConnectionHealth(conn, chConn, enabled, engine))
	projectRead := authz.NewGrant(authz.ScopeProjectRead, project.ID.String())
	logRead := authz.NewGrant(authz.ScopeLogRead, project.ID.String())
	gatewayRead := authz.NewGrant(authz.ScopeMCPRead, gatewayID.String())
	grants := []authz.Grant{projectRead, logRead, gatewayRead}
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
	registerGatewayConnectionHealthTool(reg, reader)
	session := connectTestClient(t, server)
	// call sends JSON arguments through the MCP client and decodes the JSON
	// the tool serialized, returning the raw text as well.
	call := func(arguments map[string]any) (*mcp.CallToolResult, GatewayConnectionHealthOutput, string) {
		t.Helper()
		var output GatewayConnectionHealthOutput
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: gatewayConnectionHealthToolName, Arguments: arguments})
		require.NoError(t, err)
		if result.IsError {
			return result, output, ""
		}
		body, err := json.Marshal(result.StructuredContent)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &output))
		return result, output, string(body)
	}
	refusal := func(arguments map[string]any) string {
		t.Helper()
		result, _, _ := call(arguments)
		require.True(t, result.IsError)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		require.NotContains(t, text.Text, unavailableCode, "permissions and missing targets are never reported as unavailable")
		return text.Text
	}
	base := func() map[string]any {
		return map[string]any{"project_id": project.ID.String(), "gateway_id": gatewayID.String(), "from": at.Add(-time.Hour).Format(time.RFC3339), "to": at.Add(time.Hour).Format(time.RFC3339), "hotspot_min_attempts": 2}
	}
	keyed := func(stage, subject, provider, clientKey string) map[string]any {
		arguments := base()
		arguments["key"] = map[string]any{"stage": stage, "subject": subject, "provider_id": provider, "client": clientKey}
		return arguments
	}

	_, output, raw := call(base())
	require.Equal(t, project.ID.String(), output.ProjectID)
	require.Equal(t, at.Add(-time.Hour).Format(time.RFC3339Nano), output.From)
	require.Equal(t, at.Add(time.Hour).Format(time.RFC3339Nano), output.To)
	require.EqualValues(t, 1800, output.IntervalSeconds)
	require.Contains(t, raw, `"interval_seconds":1800`)
	require.Len(t, output.Stages, 6)
	stageNames := make([]string, 0, len(output.Stages))
	for _, stage := range output.Stages {
		stageNames = append(stageNames, stage.Stage)
	}
	require.Equal(t, []string{"inbound_authorize", "inbound_token", "upstream_callback", "upstream_refresh", "discovery_listing", "discovery_member"}, stageNames)
	require.Zero(t, output.Stages[0].Attempts, "a stage without attempts is listed, empty")
	require.EqualValues(t, 3, output.Stages[1].Attempts, "a policy block stays in the token stage's denominator")
	require.EqualValues(t, 2, output.Stages[2].Attempts)
	require.EqualValues(t, 3, output.Stages[3].Attempts)
	require.EqualValues(t, 1, output.Stages[4].Attempts)
	require.EqualValues(t, 10, output.Stages[5].Attempts, "only the named project's attempts count")
	require.NotNil(t, output.Ranking)
	require.Equal(t, []any{telemetry.ConnectionStageDiscoveryMember, "count", 2}, []any{output.Ranking.Stage, output.Ranking.Sort, output.Ranking.MinAttempts})
	require.Len(t, output.Ranking.Hotspots, 2, "single-attempt keys stay under the floor")
	top := output.Ranking.Hotspots[0]
	require.Equal(t, "Health Member", top.Subject.Label)
	require.Equal(t, member, *top.Subject.Key)
	require.Empty(t, *top.Provider.Key)
	require.Equal(t, "Example Desktop", top.Client.Label)
	require.Equal(t, registered, *top.Client.Key)

	// One visible hotspot alone, through the nested key: its own attempts and
	// timeline, and no ranking.
	_, output, raw = call(keyed(telemetry.ConnectionStageDiscoveryMember, member, "", registered))
	require.NotContains(t, raw, `"ranking"`)
	require.Len(t, output.Stages, 1)
	require.EqualValues(t, 5, output.Stages[0].Attempts)
	require.NotEmpty(t, output.Stages[0].Buckets)
	require.EqualValues(t, 1800, output.IntervalSeconds)
	// The token stage of the same person and client, through the nested key:
	// its own outcomes and timeline, and no ranking.
	_, output, raw = call(keyed(telemetry.ConnectionStageInboundToken, member, "", registered))
	require.NotContains(t, raw, `"ranking"`)
	require.Len(t, output.Stages, 1)
	require.Equal(t, telemetry.ConnectionStageInboundToken, output.Stages[0].Stage)
	require.Equal(t, at.Format(time.RFC3339Nano), output.Stages[0].LastCodeIssuedAt)
	require.Equal(t, at.Add(time.Minute).Format(time.RFC3339Nano), output.Stages[0].LastSeenAt, "a later refresh cannot mask the last sign-in code request")
	require.Contains(t, raw, `"last_code_issued_at"`)
	require.EqualValues(t, 3, output.Stages[0].Attempts)
	outcomes := map[string]uint64{}
	for _, outcome := range output.Stages[0].Outcomes {
		outcomes[outcome.Outcome] = outcome.Count
	}
	require.Equal(t, map[string]uint64{"code_issued": 1, "refreshed": 1, "policy_blocked": 1}, outcomes)
	var bucketed uint64
	for _, bucket := range output.Stages[0].Buckets {
		for _, outcome := range bucket.Outcomes {
			bucketed += outcome.Count
		}
	}
	require.EqualValues(t, 3, bucketed, "the timeline sums to the stage's attempts")

	// Each provider stage of the same person, provider, and client, through
	// the nested key: only that stage's own outcomes.
	for stage, want := range map[string]map[string]uint64{
		telemetry.ConnectionStageUpstreamCallback: {"stored": 1, "failed": 1},
		telemetry.ConnectionStageUpstreamRefresh:  {"refreshed": 1, "adopted": 1, "failed": 1},
	} {
		_, output, raw = call(keyed(stage, member, provider, registered))
		require.NotContains(t, raw, `"ranking"`, stage)
		require.Len(t, output.Stages, 1, stage)
		require.Equal(t, stage, output.Stages[0].Stage)
		got := map[string]uint64{}
		for _, outcome := range output.Stages[0].Outcomes {
			got[outcome.Outcome] = outcome.Count
		}
		require.Equal(t, want, got, stage)
	}
	_, output, _ = call(keyed(telemetry.ConnectionStageDiscoveryMember, "", "", "anonymous"))
	require.EqualValues(t, 2, output.Stages[0].Attempts, "empty key values are selectable")

	for name, key := range map[string][3]string{
		"person outside the organization": {outsider, "", "anonymous"},
		"agent the caller may not read":   {agent, "", "credential:agent"},
		"provider the project cannot see": {member, unknownProvider, registered},
	} {
		require.Contains(t, refusal(keyed(telemetry.ConnectionStageDiscoveryMember, key[0], key[1], key[2])), "not_found", name)
	}

	grants = []authz.Grant{logRead, gatewayRead}
	require.Contains(t, refusal(base()), ErrForbidden.Error(), "no project:read")
	grants = []authz.Grant{projectRead, gatewayRead}
	require.Contains(t, refusal(base()), ErrForbidden.Error(), "no log:read")
	grants = []authz.Grant{projectRead, logRead}
	require.Contains(t, refusal(base()), "not_found", "no read on the gateway")
	grants = []authz.Grant{projectRead, logRead, gatewayRead, authz.NewGrant(authz.ScopeMCPRead, elsewhere.String())}
	crossProject := base()
	crossProject["gateway_id"] = elsewhere.String()
	require.Contains(t, refusal(crossProject), "not_found", "another project's gateway")
	missing := base()
	missing["gateway_id"] = uuid.NewString()
	require.Contains(t, refusal(missing), "not_found", "no such gateway")

	grants = []authz.Grant{projectRead, logRead, gatewayRead}
	tooLong := base()
	tooLong["from"], tooLong["to"] = "2026-01-01T00:00:00Z", "2026-01-31T00:00:00.000000001Z"
	require.Contains(t, refusal(tooLong), "give both from and to")

	reader.WithConnectionHealth(telemetry.NewConnectionHealth(conn, chConn, alwaysDisabledFeature, engine))
	require.Contains(t, refusal(base()), "Enable observability for this organization", "logs disabled")
}
