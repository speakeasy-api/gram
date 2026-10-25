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
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp/visibility"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
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

func TestGatewayConnectionHealthReadsTheNamedProjectWithItsOwnGrants(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_connection_health")
	require.NoError(t, err)
	chConn, err := platformMCPInfra.NewClickhouseClient(t)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	gateway := func(projectID uuid.UUID) uuid.UUID {
		row, err := metamcprepo.New(conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{OrganizationID: principal.OrganizationID, ProjectID: projectID, Name: "Gateway", UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Visibility: visibility.Private, NetworkAccessMode: pgtype.Text{String: "", Valid: false}})
		require.NoError(t, err)
		return row.ID
	}
	gatewayID := gateway(project.ID)
	other, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Other", Slug: "other-" + uuid.NewString()[:8], OrganizationID: principal.OrganizationID})
	require.NoError(t, err)
	elsewhere := gateway(other.ID)

	logger := testenv.NewLogger(t)
	enabled := func(context.Context, string) (bool, error) { return true, nil }
	writer := telemetry.NewLogger(ctx, logger, nil, nil, chConn, enabled, enabled, nil, telemetry.NewNoopLogPublisher(logger))
	at := time.Now().UTC().Add(-time.Hour)
	attempt := func(projectID, gatewayID uuid.UUID, stage, outcome, subject string) telemetry.LogParams {
		attrs := map[attr.Key]any{
			attr.EventURNKey:                   "urn:telemetry:gram_service:log:mcp_connection_attempt",
			attr.MetaMcpServerIDKey:            gatewayID.String(),
			attr.ConnectionAttemptStageKey:     stage,
			attr.ConnectionAttemptOutcomeKey:   outcome,
			attr.ConnectionAttemptReasonKey:    "",
			attr.ConnectionAttemptSubjectKey:   subject,
			attr.ConnectionAttemptClientKeyKey: "anonymous",
		}
		if stage == telemetry.ConnectionStageDiscoveryMember {
			attrs[attr.McpServerIDKey] = uuid.NewString()
		}
		return telemetry.LogParams{Timestamp: at, ToolInfo: telemetry.ToolInfo{ID: "", URN: "", Name: "", ProjectID: projectID.String(), DeploymentID: "", FunctionID: nil, OrganizationID: principal.OrganizationID}, UserInfo: telemetry.UserInfoByID(""), Attributes: attrs}
	}
	member := urn.NewUserSubject(principal.UserID).String()
	require.NoError(t, writer.LogBulk(ctx, []telemetry.LogParams{
		attempt(project.ID, gatewayID, telemetry.ConnectionStageDiscoveryListing, "partial", member),
		attempt(project.ID, gatewayID, telemetry.ConnectionStageDiscoveryMember, "listed", member),
		attempt(project.ID, gatewayID, telemetry.ConnectionStageDiscoveryMember, "failed", member),
		attempt(project.ID, gatewayID, telemetry.ConnectionStageDiscoveryMember, "listed", ""),
		attempt(other.ID, elsewhere, telemetry.ConnectionStageDiscoveryMember, "failed", member),
	}))

	engine := authz.NewEngine(logger, conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	reader := NewPostgresReader(logger, conn).WithAuthorization(engine).WithConnectionHealth(telemetry.NewConnectionHealth(conn, chConn, enabled, engine))
	// The caller's dashboard context names another project; the tool reads
	// only the project its input names.
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID, ProjectID: &other.ID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	projectRead := authz.NewGrant(authz.ScopeProjectRead, project.ID.String())
	logRead := authz.NewGrant(authz.ScopeLogRead, project.ID.String())
	gatewayRead := authz.NewGrant(authz.ScopeMCPRead, gatewayID.String())
	call := func(grants []authz.Grant, input GatewayConnectionHealthInput) (GatewayConnectionHealthOutput, error) {
		return reader.GetGatewayConnectionHealth(authz.GrantsToContext(ctx, grants), principal, input)
	}
	input := GatewayConnectionHealthInput{ProjectID: project.ID.String(), GatewayID: gatewayID.String(), HotspotMinAttempts: 1}

	output, err := call([]authz.Grant{projectRead, logRead, gatewayRead}, input)
	require.NoError(t, err)
	require.Equal(t, project.ID.String(), output.ProjectID)
	require.Len(t, output.Stages, 2)
	require.EqualValues(t, 1, output.Stages[0].Attempts)
	require.EqualValues(t, 3, output.Stages[1].Attempts, "only the named project's attempts count")
	require.Empty(t, output.Stages[1].Buckets, "the summary carries no timeline")
	require.NotEmpty(t, output.Hotspots)
	require.NotEmpty(t, output.NotRecorded)
	require.Equal(t, 7*24*time.Hour, mustParseTime(t, output.To).Sub(mustParseTime(t, output.From)))

	// One hotspot alone: its own attempts and timeline, no ranking.
	keyed := input
	keyed.Key = &GatewayConnectionKeyInput{Stage: telemetry.ConnectionStageDiscoveryMember, Subject: member, ProviderID: "", Client: "anonymous"}
	output, err = call([]authz.Grant{projectRead, logRead, gatewayRead}, keyed)
	require.NoError(t, err)
	require.Len(t, output.Stages, 1)
	require.EqualValues(t, 2, output.Stages[0].Attempts)
	require.NotEmpty(t, output.Stages[0].Buckets)
	require.Empty(t, output.Hotspots)

	_, err = call([]authz.Grant{logRead, gatewayRead}, input)
	require.Error(t, err, "log:read without project:read reads nothing")
	require.True(t, isAuthorizationDenied(err), "%v", err)
	_, err = call([]authz.Grant{projectRead, gatewayRead}, input)
	require.Error(t, err, "project:read without log:read reads nothing")
	_, err = call([]authz.Grant{projectRead, logRead}, input)
	require.ErrorIs(t, err, ErrMCPConnectionSettingsNotFound, "a gateway the caller may not read is not found")
	elsewhereInput := input
	elsewhereInput.GatewayID = elsewhere.String()
	_, err = call([]authz.Grant{projectRead, logRead, gatewayRead, authz.NewGrant(authz.ScopeMCPRead, elsewhere.String())}, elsewhereInput)
	require.ErrorIs(t, err, ErrMCPConnectionSettingsNotFound, "another project's gateway is not found")

	far := input
	far.From, far.To = "9999-12-24T00:00:00Z", "9999-12-31T00:00:00Z"
	_, err = call([]authz.Grant{projectRead, logRead, gatewayRead}, far)
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "invalid_request", refusal.Code)

	reader.WithConnectionHealth(telemetry.NewConnectionHealth(conn, chConn, func(context.Context, string) (bool, error) { return false, nil }, engine))
	_, err = call([]authz.Grant{projectRead, logRead, gatewayRead}, input)
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "observability_disabled", refusal.Code)
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	require.NoError(t, err)
	return parsed
}
