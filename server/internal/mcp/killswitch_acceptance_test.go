package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
	"google.golang.org/protobuf/proto"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	gen "github.com/speakeasy-api/gram/server/gen/killswitches"
	usersessionsgen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/killswitchapi"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	killswitchrepo "github.com/speakeasy-api/gram/server/internal/killswitches/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsrepo "github.com/speakeasy-api/gram/server/internal/tools/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

const (
	acceptanceExternalNote = "Tool calls paused exactly. <b>plain text</b>\nPlain text only."
	acceptanceInternalNote = "INTERNAL-ONLY incident context"
)

type killswitchAcceptanceFixture struct {
	ti         *testInstance
	management *killswitchapi.Service
	auth       *contextvalues.AuthContext
}

type killswitchAcceptanceTarget struct {
	name         string
	server       mcpserversrepo.McpServer
	backendID    uuid.UUID
	endpoint     string
	token        string
	toolset      toolsetsrepo.Toolset
	issuerID     uuid.UUID
	userSession  uuid.UUID
	userJTI      string
	upstream     *acceptanceMCPUpstream
	successToken string
}

type acceptanceMCPUpstream struct {
	mode             string
	backendSessionID string
	calls            atomic.Int32
}

type noMatchResponses struct {
	initialize    string
	toolsList     string
	promptsList   string
	resourcesList string
}

func (u *acceptanceMCPUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.calls.Add(1)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("read request body: %v", err), http.StatusBadRequest)
		return
	}
	if u.mode == "tunnel" {
		w.Header().Set(wire.HeaderTunnelAgentSession, "acceptance-agent-session")
	}
	w.Header().Set("Content-Type", "application/json")

	switch {
	case bytes.Contains(body, []byte(`"method":"initialize"`)):
		w.Header().Set(proxy.McpSessionIDHeader, u.backendSessionID)
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"%s","version":"1.0.0"}}}`, u.mode)
	case bytes.Contains(body, []byte(`"method":"tools/call"`)):
		if got := r.Header.Get(proxy.McpSessionIDHeader); got != u.backendSessionID {
			http.Error(w, fmt.Sprintf("unexpected backend session id %q", got), http.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"%s-ok"}],"isError":false}}`, u.mode)
	default:
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`))
	}
}

func newKillswitchAcceptanceFixture(t *testing.T) (context.Context, *killswitchAcceptanceFixture) {
	t.Helper()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.Email)

	selectors, err := authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		Scope:          string(authz.ScopeOrgAdmin),
		Selectors:      selectors,
	})
	require.NoError(t, err)

	management, err := killswitchapi.NewService(ti.logger, ti.tracerProvider, ti.conn, ti.sessionManager, ti.authzEngine, ti.audit)
	require.NoError(t, err)
	return ctx, &killswitchAcceptanceFixture{ti: ti, management: management, auth: authCtx}
}

func (f *killswitchAcceptanceFixture) newHostedTarget(t *testing.T, ctx context.Context, name string) killswitchAcceptanceTarget {
	t.Helper()
	return f.newHostedTargetInProject(t, ctx, name, *f.auth.ProjectID)
}

func (f *killswitchAcceptanceFixture) newHostedTargetInProject(t *testing.T, ctx context.Context, name string, projectID uuid.UUID) killswitchAcceptanceTarget {
	t.Helper()

	projectAuth := *f.auth
	projectAuth.ProjectID = &projectID
	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(f.ti.conn), &projectAuth, "acceptance-"+uuid.NewString()[:8])
	endpoint := "acceptance-" + uuid.NewString()
	issuerID := createUserSessionIssuer(t, ctx, f.ti.conn, projectID)
	server := createToolsetMcpEndpoint(t, ctx, f.ti.conn, projectID, toolset.ID, endpoint, mcpservers.VisibilityPublic, uuid.NullUUID{}, issuerID)
	seedUserMCPConnectGrant(t, ctx, f.ti.conn, f.auth.ActiveOrganizationID, f.auth.UserID, server.ID.String())
	token, sessionID, jti := f.mintUserSession(t, endpoint, server, urn.NewUserSubject(f.auth.UserID))
	return killswitchAcceptanceTarget{name: name, server: server, endpoint: endpoint, token: token, toolset: toolset, issuerID: issuerID, userSession: sessionID, userJTI: jti}
}

func (f *killswitchAcceptanceFixture) addHostedSentinelTool(t *testing.T, ctx context.Context, toolset toolsetsrepo.Toolset, toolName, upstreamURL string) {
	t.Helper()
	require.NotEqual(t, uuid.Nil, toolset.ID)

	deploymentID, err := deploymentsrepo.New(f.ti.conn).InsertDeployment(ctx, deploymentsrepo.InsertDeploymentParams{
		ProjectID:      toolset.ProjectID,
		OrganizationID: f.auth.ActiveOrganizationID,
		UserID:         f.auth.UserID,
		IdempotencyKey: uuid.NewString(),
	})
	require.NoError(t, err)
	require.NoError(t, deploymentsrepo.New(f.ti.conn).CreateDeploymentStatus(ctx, deploymentsrepo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID,
		Status:       "completed",
	}))

	toolURN := urn.NewTool(urn.ToolKindHTTP, toolName, uuid.NewString()[:8])
	require.NoError(t, toolsrepo.New(f.ti.conn).CreateHTTPToolDefinition(ctx, toolsrepo.CreateHTTPToolDefinitionParams{
		ProjectID:       toolset.ProjectID,
		DeploymentID:    deploymentID,
		ToolUrn:         toolURN,
		Name:            toolName,
		UntruncatedName: pgtype.Text{},
		Summary:         "Hosted downstream sentinel",
		Description:     "Hosted downstream sentinel",
		Tags:            []string{},
		HttpMethod:      http.MethodGet,
		Path:            "/sentinel",
		SchemaVersion:   "3.0.0",
		Schema:          []byte(`{}`),
		ServerEnvVar:    "SENTINEL_SERVER_URL",
		Security:        []byte(`[]`),
		HeaderSettings:  []byte(`{}`),
		QuerySettings:   []byte(`{}`),
		PathSettings:    []byte(`{}`),
		ReadOnlyHint:    pgtype.Bool{},
		DestructiveHint: pgtype.Bool{},
		IdempotentHint:  pgtype.Bool{},
		OpenWorldHint:   pgtype.Bool{},
	}))
	_, err = toolsetsrepo.New(f.ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
		ToolsetID: toolset.ID, ToolUrns: []urn.Tool{toolURN}, ResourceUrns: []urn.Resource{}, PredecessorID: uuid.NullUUID{}, Version: 1,
	})
	require.NoError(t, err)

	environmentSlug := "sentinel-" + uuid.NewString()[:8]
	environment, err := environmentsrepo.New(f.ti.conn).CreateEnvironment(ctx, environmentsrepo.CreateEnvironmentParams{
		OrganizationID: f.auth.ActiveOrganizationID, ProjectID: toolset.ProjectID, Name: "Hosted sentinel", Slug: environmentSlug, Description: pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = environmentsrepo.New(f.ti.conn).UpsertEnvironmentEntry(ctx, environmentsrepo.UpsertEnvironmentEntryParams{
		Name: "SENTINEL_SERVER_URL", Value: upstreamURL, IsSecret: false, EnvironmentID: environment.ID, ProjectID: toolset.ProjectID,
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(f.ti.conn).UpdateToolset(ctx, toolsetsrepo.UpdateToolsetParams{
		Name: toolset.Name, Description: toolset.Description, DefaultEnvironmentSlug: conv.ToPGText(environmentSlug),
		McpSlug: toolset.McpSlug, McpIsPublic: toolset.McpIsPublic, CustomDomainID: toolset.CustomDomainID,
		McpEnabled: toolset.McpEnabled, ToolSelectionMode: toolset.ToolSelectionMode, Slug: toolset.Slug, ProjectID: toolset.ProjectID,
	})
	require.NoError(t, err)
}

func (f *killswitchAcceptanceFixture) newPrivateTarget(t *testing.T, ctx context.Context, mode string) killswitchAcceptanceTarget {
	t.Helper()

	upstream := &acceptanceMCPUpstream{mode: mode, backendSessionID: mode + "-backend-session"}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	endpoint := "acceptance-" + mode + "-" + uuid.NewString()
	issuerID := createUserSessionIssuer(t, ctx, f.ti.conn, *f.auth.ProjectID)

	var canonical mcpserversrepo.McpServer
	var backendID uuid.UUID
	switch mode {
	case "remote":
		createdCanonical, remote := createRemoteMcpEndpoint(t, ctx, f.ti.conn, *f.auth.ProjectID, server.URL, endpoint, mcpservers.VisibilityPrivate, issuerID)
		canonical = createdCanonical
		backendID = remote.ID
	case "tunnel":
		backendID = uuid.Must(uuid.NewV7())
		tunnel, err := tunneledmcprepo.New(f.ti.conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
			ID: backendID, ProjectID: *f.auth.ProjectID, Name: "acceptance-tunnel-" + uuid.NewString()[:8],
			KeyHash: uuid.NewString(), KeyPrefix: "gram_tunnel_test",
		})
		require.NoError(t, err)
		canonicalID := uuid.Must(uuid.NewV7())
		canonical, err = mcpserversrepo.New(f.ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
			ID: canonicalID, ProjectID: *f.auth.ProjectID, Name: conv.ToPGText("acceptance private tunnel"),
			Slug: conv.ToPGText("acceptance-private-tunnel-" + uuid.NewString()[:8]), EnvironmentID: uuid.NullUUID{},
			UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true}, RemoteMcpServerID: uuid.NullUUID{},
			TunneledMcpServerID: uuid.NullUUID{UUID: tunnel.ID, Valid: true}, ToolsetID: uuid.NullUUID{}, Visibility: mcpservers.VisibilityPrivate,
		})
		require.NoError(t, err)
		_, err = mcpendpointsrepo.New(f.ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
			ProjectID: *f.auth.ProjectID, CustomDomainID: uuid.NullUUID{}, McpServerID: uuid.NullUUID{UUID: canonical.ID, Valid: true}, Slug: endpoint,
		})
		require.NoError(t, err)
		require.NoError(t, f.ti.tunnelRoutes.Publish(ctx, tunnel.ID.String(), server.URL, time.Hour))
	default:
		t.Fatalf("unsupported private target mode %q", mode)
	}

	seedUserMCPConnectGrant(t, ctx, f.ti.conn, f.auth.ActiveOrganizationID, f.auth.UserID, canonical.ID.String())
	token, sessionID, jti := f.mintUserSession(t, endpoint, canonical, urn.NewUserSubject(f.auth.UserID))
	return killswitchAcceptanceTarget{
		name: mode, server: canonical, backendID: backendID, endpoint: endpoint, token: token, issuerID: issuerID,
		userSession: sessionID, userJTI: jti, upstream: upstream, successToken: mode + "-ok",
	}
}

func (f *killswitchAcceptanceFixture) mintUserSession(t *testing.T, endpoint string, server mcpserversrepo.McpServer, subject urn.SessionSubject) (string, uuid.UUID, string) {
	t.Helper()
	require.True(t, server.UserSessionIssuerID.Valid)

	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{
		Subject: subject, Audience: urn.NewUserSessionIssuer(server.UserSessionIssuerID.UUID).String(),
		Issuer: f.ti.serverURL.String() + "/x/mcp/" + endpoint, Lifetime: time.Hour,
	})
	require.NoError(t, err)
	now := time.Now()
	session, err := usersessionsrepo.New(f.ti.conn).CreateUserSession(t.Context(), usersessionsrepo.CreateUserSessionParams{
		UserSessionIssuerID: server.UserSessionIssuerID.UUID, UserSessionClientID: uuid.NullUUID{}, SubjectUrn: subject, Jti: jti,
		RefreshTokenHash: "acceptance-" + uuid.NewString(), RefreshExpiresAt: pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
		ExpiresAt: pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true}, ToolSelection: nil,
	})
	require.NoError(t, err)
	return token, session.ID, jti
}

func (f *killswitchAcceptanceFixture) create(t *testing.T, ctx context.Context, operationID uuid.UUID, scope *gen.KillswitchScope, schedule *gen.KillswitchSchedule, externalNote string) *gen.KillswitchMutationReceipt {
	t.Helper()

	result, err := f.management.Create(ctx, &gen.CreatePayload{
		OperationID: operationID.String(), CapabilityKey: killswitchapi.CapabilityMCPToolCalls, UserID: f.auth.UserID,
		Scope: scope, Schedule: schedule, InternalNote: acceptanceInternalNote, ExternalNote: externalNote,
	})
	require.NoError(t, err)
	return result
}

func (f *killswitchAcceptanceFixture) call(t *testing.T, target killswitchAcceptanceTarget, body []byte, sessionID string) *httptest.ResponseRecorder {
	t.Helper()

	var headers map[string]string
	if sessionID != "" {
		headers = map[string]string{proxy.McpSessionIDHeader: sessionID}
	}
	response, err := servePublicHTTP(t, t.Context(), f.ti, target.endpoint, body, target.token, headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code, "body=%s", response.Body.String())
	return response
}

func (f *killswitchAcceptanceFixture) initialize(t *testing.T, target killswitchAcceptanceTarget) string {
	t.Helper()
	response := f.call(t, target, makeInitializeBody(), "")
	result := decodeMCPResult(t, response.Body.Bytes())
	require.Equal(t, "2025-03-26", result["protocolVersion"])
	require.NotNil(t, result["capabilities"])
	require.NotNil(t, result["serverInfo"])
	sessionID := response.Header().Get(proxy.McpSessionIDHeader)
	require.NotEmpty(t, sessionID)
	return sessionID
}

func (f *killswitchAcceptanceFixture) captureNoMatchResponses(t *testing.T, target killswitchAcceptanceTarget, sessionID string) noMatchResponses {
	t.Helper()

	initialized := f.call(t, target, makeInitializeBody(), "")
	decodeMCPResult(t, initialized.Body.Bytes())
	toolsList := f.call(t, target, makeToolsListBody(), sessionID)
	promptsList := f.call(t, target, []byte(`{"jsonrpc":"2.0","id":4,"method":"prompts/list","params":{}}`), sessionID)
	resourcesList := f.call(t, target, []byte(`{"jsonrpc":"2.0","id":5,"method":"resources/list","params":{}}`), sessionID)
	return noMatchResponses{
		initialize:    initialized.Body.String(),
		toolsList:     toolsList.Body.String(),
		promptsList:   promptsList.Body.String(),
		resourcesList: resourcesList.Body.String(),
	}
}

func selectedServers(ids ...uuid.UUID) *gen.KillswitchScope {
	serverIDs := make([]string, len(ids))
	for i, id := range ids {
		serverIDs[i] = id.String()
	}
	return &gen.KillswitchScope{Type: "selected_servers", ServerIds: serverIDs}
}

func scheduleNow() *gen.KillswitchSchedule {
	return &gen.KillswitchSchedule{Start: "now", End: "until_lifted"}
}

func requireKillswitchDenied(t *testing.T, response *httptest.ResponseRecorder, externalNote string) {
	t.Helper()

	var envelope map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Equal(t, map[string]any{
		"jsonrpc": "2.0", "id": float64(3),
		"error": map[string]any{
			"code": float64(proxy.RejectCodeForbidden), "message": externalNote,
			"data": map[string]any{"code": proxy.KillswitchRejectionCode},
		},
	}, envelope)
	require.Contains(t, response.Header().Get("Content-Type"), "application/json")

	body := response.Body.String()
	for _, internal := range []string{acceptanceInternalNote, "prescription", "definition", "internal_note", "starts_at", "expires_at", "resume"} {
		require.NotContains(t, strings.ToLower(body), strings.ToLower(internal))
	}
}

func requireSuccessfulToolCall(t *testing.T, response *httptest.ResponseRecorder, token string) {
	t.Helper()
	result := decodeMCPResult(t, response.Body.Bytes())
	if isError, ok := result["isError"]; ok {
		require.Equal(t, false, isError)
	}
	require.Contains(t, response.Body.String(), token)
}

func requireGoaError(t *testing.T, err error, name string) {
	t.Helper()
	var named goa.GoaErrorNamer
	require.ErrorAs(t, err, &named)
	require.Equal(t, name, named.GoaErrorName())
}

func databaseNow(t *testing.T, db *pgxpool.Pool) time.Time {
	t.Helper()
	now, err := killswitchrepo.New(db).GetKillswitchDatabaseTime(t.Context())
	require.NoError(t, err)
	require.True(t, now.Valid)
	return now.Time.UTC()
}

func waitForDatabaseTime(t *testing.T, db *pgxpool.Pool, target time.Time) {
	t.Helper()
	queries := killswitchrepo.New(db)
	timeout := 12 * time.Second
	if remaining := target.Sub(databaseNow(t, db)); remaining > 0 {
		timeout += remaining
	}
	require.Eventually(t, func() bool {
		now, err := queries.GetKillswitchDatabaseTime(t.Context())
		return err == nil && now.Valid && !now.Time.Before(target)
	}, timeout, 25*time.Millisecond, "database time did not reach %s", target.Format(time.RFC3339Nano))
}

func requireUserSessionActive(t *testing.T, f *killswitchAcceptanceFixture, target killswitchAcceptanceTarget) {
	t.Helper()
	row, err := usersessionsrepo.New(f.ti.conn).GetUserSessionByID(t.Context(), usersessionsrepo.GetUserSessionByIDParams{
		ID: target.userSession, ProjectID: target.server.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, target.userSession, row.ID)
	require.Equal(t, target.userJTI, row.Jti)
	require.False(t, row.Deleted)
}

func requireUserSessionRevoked(t *testing.T, f *killswitchAcceptanceFixture, target killswitchAcceptanceTarget) {
	t.Helper()
	rows, err := usersessionsrepo.New(f.ti.conn).ListUserSessionsByProjectID(t.Context(), usersessionsrepo.ListUserSessionsByProjectIDParams{
		ProjectID:           target.server.ProjectID,
		Status:              conv.ToPGText("revoked"),
		SubjectUrn:          pgtype.Text{},
		UserSessionIssuerID: uuid.NullUUID{},
		ClientID:            uuid.NullUUID{},
		ID:                  uuid.NullUUID{UUID: target.userSession, Valid: true},
		Cursor:              uuid.NullUUID{},
		LimitValue:          1,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, target.userSession, rows[0].ID)
	require.Equal(t, target.userJTI, rows[0].Jti)
	require.True(t, rows[0].Deleted)
}

func newAcceptanceUserSessionsService(t *testing.T, f *killswitchAcceptanceFixture) *usersessions.Service {
	t.Helper()
	guardianPolicy, err := guardian.NewUnsafePolicy(f.ti.tracerProvider, []string{})
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	return usersessions.NewService(
		f.ti.logger,
		f.ti.tracerProvider,
		testenv.NewMeterProvider(t),
		f.ti.conn,
		f.ti.sessionManager,
		f.ti.chatSessionsManager,
		f.ti.authzEngine,
		f.ti.audit,
		guardianPolicy,
		f.ti.enc,
		usersessions.NewSigner("test-jwt-secret"),
		f.ti.serverURL.String(),
		ratelimit.NewRedisStore(redisClient),
	)
}

func seedAcceptanceMember(t *testing.T, f *killswitchAcceptanceFixture, userID string) {
	t.Helper()
	_, err := f.ti.conn.Exec(t.Context(), `INSERT INTO users (id, email, display_name) VALUES ($1, $1 || '@example.test', 'Unaffected Member')`, userID) //nolint:glint // notestingrawsql: acceptance fixture needs a second active concrete user
	require.NoError(t, err)
	_, err = f.ti.conn.Exec(t.Context(), `INSERT INTO organization_user_relationships (organization_id, user_id) VALUES ($1, $2)`, f.auth.ActiveOrganizationID, userID) //nolint:glint // notestingrawsql: pair the fixture user with the tested organization
	require.NoError(t, err)
}

func requireHistoryEvent(t *testing.T, event *gen.KillswitchHistoryEvent, version int64, action, status, scopeType, note string, serverIDs ...uuid.UUID) {
	t.Helper()
	require.Equal(t, version, event.Version)
	require.Equal(t, action, string(event.Action))
	require.Equal(t, status, string(event.Status))
	require.Equal(t, scopeType, string(event.Scope.Type))
	require.Equal(t, note, event.ExternalNote)
	require.Equal(t, acceptanceInternalNote, event.InternalNote)
	want := make([]string, len(serverIDs))
	for i, id := range serverIDs {
		want[i] = id.String()
	}
	slices.Sort(want)
	got := slices.Clone(event.Scope.ServerIds)
	slices.Sort(got)
	require.Equal(t, want, got)
}

type expectedLifecycleEvent struct {
	action      audit.Action
	version     int64
	state       string
	operation   string
	operationID uuid.UUID
	actorID     string
}

func requireLifecycleEvents(t *testing.T, f *killswitchAcceptanceFixture, prescriptionID string, expected []expectedLifecycleEvent) {
	t.Helper()
	//nolint:glint // notestingrawsql: bounded read-only acceptance assertion
	rows, err := f.ti.conn.Query(t.Context(), `
		SELECT action, actor_id, subject_type, coalesce(after_snapshot, 'null'::jsonb), coalesce(metadata, 'null'::jsonb)
		FROM audit_logs
		WHERE organization_id = $1 AND subject_id = $2
		ORDER BY seq
	`, f.auth.ActiveOrganizationID, prescriptionID)
	require.NoError(t, err)
	defer rows.Close()

	var got int
	for rows.Next() {
		require.Less(t, got, len(expected))
		var action, actorID, subjectType string
		var snapshotJSON, metadataJSON []byte
		require.NoError(t, rows.Scan(&action, &actorID, &subjectType, &snapshotJSON, &metadataJSON))
		want := expected[got]
		require.Equal(t, string(want.action), action)
		wantActorID := want.actorID
		if wantActorID == "" {
			wantActorID = f.auth.UserID
		}
		require.Equal(t, wantActorID, actorID)
		require.Equal(t, "killswitch_prescription", subjectType)
		var snapshot audit.KillswitchVersionSnapshot
		require.NoError(t, json.Unmarshal(snapshotJSON, &snapshot))
		require.Equal(t, audit.KillswitchVersionSnapshot{Version: want.version, State: want.state}, snapshot)
		var metadata struct {
			Operation   string    `json:"operation"`
			OperationID uuid.UUID `json:"operation_id"`
		}
		require.NoError(t, json.Unmarshal(metadataJSON, &metadata))
		require.Equal(t, want.operation, metadata.Operation)
		require.Equal(t, want.operationID, metadata.OperationID)
		got++
	}
	require.NoError(t, rows.Err())
	require.Len(t, expected, got)
	requireOutboxEvents(t, f, prescriptionID, expected)
	requireNoInternalNoteLeak(t, f)
}

func requireOutboxEvents(t *testing.T, f *killswitchAcceptanceFixture, prescriptionID string, expected []expectedLifecycleEvent) {
	t.Helper()
	//nolint:glint // notestingrawsql: bounded read-only acceptance assertion
	rows, err := f.ti.conn.Query(t.Context(), `
		SELECT message, attributes->>'event_type'
		FROM publish_outbox
		WHERE organization_id = $1 AND attributes->>'event_type' = $2
		ORDER BY id
	`, f.auth.ActiveOrganizationID, string(events.KillswitchV1.EventType()))
	require.NoError(t, err)
	defer rows.Close()

	var payloads []events.AuditLogCreatedPayloadV1
	for rows.Next() {
		var message []byte
		var eventType string
		require.NoError(t, rows.Scan(&message, &eventType))
		require.Equal(t, string(events.KillswitchV1.EventType()), eventType)
		var envelope webhooksv1.Event
		require.NoError(t, proto.Unmarshal(message, &envelope))
		require.Equal(t, string(events.KillswitchV1.EventType()), envelope.GetEventType())
		var payload events.AuditLogCreatedPayloadV1
		require.NoError(t, json.Unmarshal(envelope.GetPayload(), &payload))
		if payload.SubjectID == prescriptionID {
			payloads = append(payloads, payload)
		}
	}
	require.NoError(t, rows.Err())
	require.Len(t, payloads, len(expected))
	wants := make(map[string]expectedLifecycleEvent, len(expected))
	for _, event := range expected {
		wants[fmt.Sprintf("%s:%d", event.action, event.version)] = event
	}
	for _, payload := range payloads {
		require.Equal(t, prescriptionID, payload.SubjectID)
		require.Equal(t, "killswitch_prescription", payload.SubjectType)

		var version int64
		if payload.Action == string(audit.ActionKillswitchExpire) {
			var metadata struct {
				Version int64 `json:"version"`
			}
			require.NoError(t, json.Unmarshal(payload.Metadata, &metadata))
			version = metadata.Version
		} else {
			var snapshot audit.KillswitchVersionSnapshot
			require.NoError(t, json.Unmarshal(payload.AfterSnapshot, &snapshot))
			version = snapshot.Version
		}

		key := fmt.Sprintf("%s:%d", payload.Action, version)
		want, ok := wants[key]
		require.True(t, ok, "unexpected outbox payload %s", key)
		delete(wants, key)
		wantActorID := want.actorID
		if wantActorID == "" {
			wantActorID = f.auth.UserID
		}
		require.Equal(t, wantActorID, payload.ActorID)
		if want.state != "" {
			var snapshot audit.KillswitchVersionSnapshot
			require.NoError(t, json.Unmarshal(payload.AfterSnapshot, &snapshot))
			require.Equal(t, audit.KillswitchVersionSnapshot{Version: want.version, State: want.state}, snapshot)
			var metadata struct {
				Operation   string    `json:"operation"`
				OperationID uuid.UUID `json:"operation_id"`
			}
			require.NoError(t, json.Unmarshal(payload.Metadata, &metadata))
			require.Equal(t, want.operation, metadata.Operation)
			require.Equal(t, want.operationID, metadata.OperationID)
		}
	}
	require.Empty(t, wants)
}

func requireNoInternalNoteLeak(t *testing.T, f *killswitchAcceptanceFixture) {
	t.Helper()
	var auditLeaks int64
	//nolint:glint // notestingrawsql: explicit serialized-field leakage assertion
	err := f.ti.conn.QueryRow(t.Context(), `
		SELECT count(*) FROM audit_logs
		WHERE organization_id = $1 AND audit_logs::text LIKE '%' || $2 || '%'
	`, f.auth.ActiveOrganizationID, acceptanceInternalNote).Scan(&auditLeaks)
	require.NoError(t, err)
	require.Zero(t, auditLeaks)

	rows, err := f.ti.conn.Query(t.Context(), `SELECT message FROM publish_outbox WHERE organization_id = $1`, f.auth.ActiveOrganizationID) //nolint:glint // notestingrawsql: explicit serialized-payload leakage assertion
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var message []byte
		require.NoError(t, rows.Scan(&message))
		require.False(t, bytes.Contains(message, []byte(acceptanceInternalNote)))
	}
	require.NoError(t, rows.Err())
}

func TestKillswitchAcceptancePrivateRemoteAndTunnelProductionComposition(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"remote", "tunnel"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			ctx, f := newKillswitchAcceptanceFixture(t)
			target := f.newPrivateTarget(t, ctx, mode)
			require.NotEqual(t, target.backendID, target.server.ID, "the prescription must use the canonical mcp_servers id")

			memberSession := f.initialize(t, target)
			requireSuccessfulToolCall(t, f.call(t, target, makeToolsCallBody("ping"), memberSession), target.successToken)
			noMatchBefore := f.captureNoMatchResponses(t, target, memberSession)

			otherUserID := "user_" + uuid.NewString()
			seedAcceptanceMember(t, f, otherUserID)
			seedUserMCPConnectGrant(t, ctx, f.ti.conn, f.auth.ActiveOrganizationID, otherUserID, target.server.ID.String())
			other := target
			other.token, other.userSession, other.userJTI = f.mintUserSession(t, target.endpoint, target.server, urn.NewUserSubject(otherUserID))
			otherSession := f.initialize(t, other)
			requireSuccessfulToolCall(t, f.call(t, other, makeToolsCallBody("ping"), otherSession), target.successToken)

			created := f.create(t, ctx, uuid.New(), selectedServers(target.server.ID), scheduleNow(), acceptanceExternalNote)

			postActivationSession := f.initialize(t, target)
			noMatchAfter := f.captureNoMatchResponses(t, target, postActivationSession)
			require.Equal(t, noMatchBefore, noMatchAfter, "initialize and list methods outside tools/call must remain byte-for-byte unchanged")

			checkpoint, err := mcptoolexecution.NewCheckpoint(f.ti.conn, mcptoolexecution.DefaultEvaluationTimeout, testenv.NewMeterProvider(t), f.ti.logger)
			require.NoError(t, err)
			unsupportedCtx := mcpidentity.NewValidatorBoundary().StampAPIKey(t.Context())
			disposition, err := checkpoint.Evaluate(unsupportedCtx, f.auth.ActiveOrganizationID, target.server.ID.String())
			require.NoError(t, err)
			require.Equal(t, killswitches.TransportDispositionContinue, disposition.Kind(), "private serving rejects API-key identities before MCP session composition; the shared checkpoint must classify them unsupported")

			baseline := target.upstream.calls.Load()
			denied := f.call(t, target, makeToolsCallBody("ping"), memberSession)
			requireKillswitchDenied(t, denied, acceptanceExternalNote)
			require.Equal(t, baseline, target.upstream.calls.Load(), "denied call must not reach the upstream")

			unaffected := f.call(t, other, makeToolsCallBody("ping"), otherSession)
			requireSuccessfulToolCall(t, unaffected, target.successToken)
			require.NotContains(t, unaffected.Body.String(), acceptanceExternalNote)
			require.NotContains(t, unaffected.Body.String(), acceptanceInternalNote)
			require.Equal(t, baseline+1, target.upstream.calls.Load())

			_, err = f.management.Lift(ctx, &gen.LiftPayload{OperationID: uuid.NewString(), ID: created.ID, ExpectedVersion: created.Version})
			require.NoError(t, err)
			requireSuccessfulToolCall(t, f.call(t, target, makeToolsCallBody("ping"), memberSession), target.successToken)
			require.Equal(t, baseline+2, target.upstream.calls.Load())
			requireUserSessionActive(t, f, target)
			requireUserSessionActive(t, f, other)
		})
	}
}

func TestKillswitchAcceptanceHostedTransportContract(t *testing.T) {
	t.Parallel()

	ctx, f := newKillswitchAcceptanceFixture(t)
	hosted := f.newHostedTarget(t, ctx, "hosted")
	var downstreamCalls atomic.Int32
	sentinel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"sentinel-ok"}`))
	}))
	t.Cleanup(sentinel.Close)
	f.addHostedSentinelTool(t, ctx, hosted.toolset, "sentinel", sentinel.URL)

	memberSession := f.initialize(t, hosted)
	sentinelBefore := f.call(t, hosted, makeToolsCallBody("sentinel"), memberSession)
	decodeMCPResult(t, sentinelBefore.Body.Bytes())
	require.Contains(t, sentinelBefore.Body.String(), "sentinel-ok")
	require.Equal(t, int32(1), downstreamCalls.Load())
	noMatchBefore := f.captureNoMatchResponses(t, hosted, memberSession)

	anonymous := hosted
	anonymous.token, anonymous.userSession, anonymous.userJTI = f.mintUserSession(t, hosted.endpoint, hosted.server, urn.NewAnonymousSubject(uuid.NewString()))
	anonymousSession := f.initialize(t, anonymous)
	unsupportedBefore := f.call(t, anonymous, makeToolsCallBody("missing_tool"), anonymousSession)

	created := f.create(t, ctx, uuid.New(), selectedServers(hosted.server.ID), scheduleNow(), acceptanceExternalNote)
	require.Equal(t, int64(1), created.Version)

	postActivationSession := f.initialize(t, hosted)
	noMatchAfter := f.captureNoMatchResponses(t, hosted, postActivationSession)
	require.Equal(t, noMatchBefore, noMatchAfter, "initialize and list methods outside tools/call must remain byte-for-byte unchanged")
	unsupportedAfter := f.call(t, anonymous, makeToolsCallBody("missing_tool"), anonymousSession)
	require.JSONEq(t, unsupportedBefore.Body.String(), unsupportedAfter.Body.String(), "unsupported identity behavior must be unchanged")
	require.NotContains(t, unsupportedAfter.Body.String(), acceptanceExternalNote)
	require.NotContains(t, unsupportedAfter.Body.String(), acceptanceInternalNote)

	requireKillswitchDenied(t, f.call(t, hosted, makeToolsCallBody("sentinel"), memberSession), acceptanceExternalNote)
	require.Equal(t, int32(1), downstreamCalls.Load(), "denied hosted call must not reach the configured HTTP tool")

	_, err := f.management.Lift(ctx, &gen.LiftPayload{OperationID: uuid.NewString(), ID: created.ID, ExpectedVersion: created.Version})
	require.NoError(t, err)
	sentinelAfter := f.call(t, hosted, makeToolsCallBody("sentinel"), memberSession)
	decodeMCPResult(t, sentinelAfter.Body.Bytes())
	require.Contains(t, sentinelAfter.Body.String(), "sentinel-ok")
	require.Equal(t, int32(2), downstreamCalls.Load())
	requireUserSessionActive(t, f, hosted)
	requireUserSessionActive(t, f, anonymous)
}

func TestKillswitchAcceptanceSessionRevocationDoesNotChangeActivePrescription(t *testing.T) {
	t.Parallel()

	ctx, f := newKillswitchAcceptanceFixture(t)
	target := f.newHostedTarget(t, ctx, "revocation-separation")
	sessionID := f.initialize(t, target)
	createOperation := uuid.New()
	created := f.create(t, ctx, createOperation, selectedServers(target.server.ID), scheduleNow(), "active across revocation")
	requireKillswitchDenied(t, f.call(t, target, makeToolsCallBody("missing_tool"), sessionID), "active across revocation")

	detailBefore, err := f.management.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), detailBefore.Version)
	require.Len(t, detailBefore.History, 1)
	expectedEvents := []expectedLifecycleEvent{
		{action: audit.ActionKillswitchActivate, version: 1, state: "active", operation: "activate", operationID: createOperation},
	}
	requireLifecycleEvents(t, f, created.ID, expectedEvents)

	err = newAcceptanceUserSessionsService(t, f).RevokeUserSession(ctx, &usersessionsgen.RevokeUserSessionPayload{
		ID:               target.userSession.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	requireUserSessionRevoked(t, f, target)

	detailAfterRevoke, err := f.management.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, detailBefore, detailAfterRevoke)
	requireLifecycleEvents(t, f, created.ID, expectedEvents)

	fresh := target
	fresh.token, fresh.userSession, fresh.userJTI = f.mintUserSession(t, target.endpoint, target.server, urn.NewUserSubject(f.auth.UserID))
	freshSession := f.initialize(t, fresh)
	requireKillswitchDenied(t, f.call(t, fresh, makeToolsCallBody("missing_tool"), freshSession), "active across revocation")
	requireUserSessionActive(t, f, fresh)

	detailAfterReconnect, err := f.management.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, detailBefore, detailAfterReconnect)
	requireLifecycleEvents(t, f, created.ID, expectedEvents)
}

func TestKillswitchAcceptanceFutureStartDynamicAllUsesDatabaseTime(t *testing.T) {
	t.Parallel()

	ctx, f := newKillswitchAcceptanceFixture(t)
	existing := f.newHostedTarget(t, ctx, "future-existing")
	existingSession := f.initialize(t, existing)
	existingBefore := f.call(t, existing, makeToolsCallBody("missing_tool"), existingSession)

	starts := databaseNow(t, f.ti.conn).Add(30 * time.Second)
	startsAt := starts.Format(time.RFC3339Nano)
	created := f.create(t, ctx, uuid.New(), &gen.KillswitchScope{Type: "all_servers"}, &gen.KillswitchSchedule{
		Start: "scheduled", StartsAt: &startsAt, End: "until_lifted",
	}, "future all servers")
	detailBefore, err := f.management.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), detailBefore.Version)
	require.Len(t, detailBefore.History, 1)

	later := f.newHostedTarget(t, ctx, "future-created-after-prescription")
	laterSession := f.initialize(t, later)
	laterBefore := f.call(t, later, makeToolsCallBody("missing_tool"), laterSession)
	require.True(t, databaseNow(t, f.ti.conn).Before(starts), "pre-start assertions exceeded the bounded database-time window")
	require.JSONEq(t, existingBefore.Body.String(), f.call(t, existing, makeToolsCallBody("missing_tool"), existingSession).Body.String())
	require.JSONEq(t, laterBefore.Body.String(), f.call(t, later, makeToolsCallBody("missing_tool"), laterSession).Body.String())

	waitForDatabaseTime(t, f.ti.conn, starts)
	requireKillswitchDenied(t, f.call(t, existing, makeToolsCallBody("missing_tool"), existingSession), "future all servers")
	requireKillswitchDenied(t, f.call(t, later, makeToolsCallBody("missing_tool"), laterSession), "future all servers")

	detailAfter, err := f.management.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, detailBefore.Version, detailAfter.Version)
	require.Equal(t, detailBefore.Scope, detailAfter.Scope)
	require.Equal(t, detailBefore.Schedule, detailAfter.Schedule)
	require.Equal(t, detailBefore.History, detailAfter.History)
	requireUserSessionActive(t, f, existing)
	requireUserSessionActive(t, f, later)
}

func TestKillswitchAcceptanceScopesLifecycleAndReceipts(t *testing.T) {
	t.Parallel()

	ctx, f := newKillswitchAcceptanceFixture(t)
	a := f.newHostedTarget(t, ctx, "A")
	b := f.newHostedTarget(t, ctx, "B")
	projectSlug := "acceptance-cross-project-" + uuid.NewString()[:8]
	secondProject, err := projectsrepo.New(f.ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: projectSlug, Slug: projectSlug, OrganizationID: f.auth.ActiveOrganizationID,
	})
	require.NoError(t, err)
	c := f.newHostedTargetInProject(t, ctx, "C", secondProject.ID)
	require.Equal(t, f.auth.ActiveOrganizationID, secondProject.OrganizationID)
	require.NotEqual(t, a.server.ProjectID, c.server.ProjectID)

	sentinel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"lifecycle-sentinel-ok"}`))
	}))
	t.Cleanup(sentinel.Close)
	f.addHostedSentinelTool(t, ctx, b.toolset, "sentinel-b", sentinel.URL)
	f.addHostedSentinelTool(t, ctx, c.toolset, "sentinel-c", sentinel.URL)
	aSession := f.initialize(t, a)
	bSession := f.initialize(t, b)
	cSession := f.initialize(t, c)
	sessions := []struct {
		target    killswitchAcceptanceTarget
		sessionID string
		toolName  string
	}{{a, aSession, "missing_tool"}, {b, bSession, "sentinel-b"}, {c, cSession, "sentinel-c"}}
	for _, member := range sessions[1:] {
		requireSuccessfulToolCall(t, f.call(t, member.target, makeToolsCallBody(member.toolName), member.sessionID), "lifecycle-sentinel-ok")
	}

	createOperation := uuid.New()
	created := f.create(t, ctx, createOperation, selectedServers(c.server.ID, a.server.ID, b.server.ID), scheduleNow(), "three servers")
	require.Equal(t, int64(1), created.Version)
	require.False(t, created.Replayed)
	detail, err := f.management.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), detail.Version)
	require.Equal(t, "selected_servers", string(detail.Scope.Type))
	require.ElementsMatch(t, []string{a.server.ID.String(), b.server.ID.String(), c.server.ID.String()}, detail.Scope.ServerIds)
	require.Len(t, detail.History, 1)
	requireHistoryEvent(t, detail.History[0], 1, "created", "active", "selected_servers", "three servers", a.server.ID, b.server.ID, c.server.ID)
	for _, member := range sessions {
		requireKillswitchDenied(t, f.call(t, member.target, makeToolsCallBody(member.toolName), member.sessionID), "three servers")
	}

	replayed := f.create(t, ctx, createOperation, selectedServers(a.server.ID, b.server.ID, c.server.ID), scheduleNow(), "three servers")
	require.True(t, replayed.Replayed)
	require.Equal(t, created.ID, replayed.ID)
	_, err = f.management.Create(ctx, &gen.CreatePayload{
		OperationID: createOperation.String(), CapabilityKey: killswitchapi.CapabilityMCPToolCalls, UserID: f.auth.UserID,
		Scope: selectedServers(a.server.ID, b.server.ID, c.server.ID), Schedule: scheduleNow(),
		InternalNote: acceptanceInternalNote, ExternalNote: "conflicting replay",
	})
	requireGoaError(t, err, "operation_conflict")

	editOperation := uuid.New()
	edited, err := f.management.Edit(ctx, &gen.EditPayload{
		OperationID: editOperation.String(), ID: created.ID, ExpectedVersion: created.Version,
		Scope: selectedServers(a.server.ID), Schedule: scheduleNow(), InternalNote: acceptanceInternalNote, ExternalNote: "one server",
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), edited.Version)
	requireKillswitchDenied(t, f.call(t, a, makeToolsCallBody("missing_tool"), aSession), "one server")
	requireSuccessfulToolCall(t, f.call(t, b, makeToolsCallBody("sentinel-b"), bSession), "lifecycle-sentinel-ok")
	requireSuccessfulToolCall(t, f.call(t, c, makeToolsCallBody("sentinel-c"), cSession), "lifecycle-sentinel-ok")

	_, err = f.management.Edit(ctx, &gen.EditPayload{
		OperationID: uuid.NewString(), ID: created.ID, ExpectedVersion: created.Version,
		Scope: selectedServers(b.server.ID), Schedule: scheduleNow(), InternalNote: acceptanceInternalNote, ExternalNote: "stale edit",
	})
	requireGoaError(t, err, "version_conflict")
	detail, err = f.management.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, int64(2), detail.Version)
	require.Len(t, detail.History, 2)
	requireHistoryEvent(t, detail.History[0], 2, "edited", "active", "selected_servers", "one server", a.server.ID)
	requireHistoryEvent(t, detail.History[1], 1, "created", "active", "selected_servers", "three servers", a.server.ID, b.server.ID, c.server.ID)

	allOperation := uuid.New()
	all, err := f.management.Edit(ctx, &gen.EditPayload{
		OperationID: allOperation.String(), ID: created.ID, ExpectedVersion: edited.Version,
		Scope: &gen.KillswitchScope{Type: "all_servers"}, Schedule: scheduleNow(),
		InternalNote: acceptanceInternalNote, ExternalNote: "all current and future servers",
	})
	require.NoError(t, err)
	d := f.newHostedTarget(t, ctx, "D-created-after-all")
	requireKillswitchDenied(t, f.call(t, d, makeToolsCallBody("missing_tool"), ""), "all current and future servers")

	liftOperation := uuid.New()
	lifted, err := f.management.Lift(ctx, &gen.LiftPayload{OperationID: liftOperation.String(), ID: created.ID, ExpectedVersion: all.Version})
	require.NoError(t, err)
	require.Equal(t, int64(4), lifted.Result.Version)
	require.Empty(t, lifted.RemainingOverlaps)
	liftReplay, err := f.management.Lift(ctx, &gen.LiftPayload{OperationID: liftOperation.String(), ID: created.ID, ExpectedVersion: all.Version})
	require.NoError(t, err)
	require.True(t, liftReplay.Result.Replayed)

	detail, err = f.management.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, int64(4), detail.Version)
	require.Len(t, detail.History, 4)
	requireHistoryEvent(t, detail.History[0], 4, "lifted", "lifted", "all_servers", "all current and future servers")
	requireHistoryEvent(t, detail.History[1], 3, "edited", "active", "all_servers", "all current and future servers")
	requireHistoryEvent(t, detail.History[2], 2, "edited", "active", "selected_servers", "one server", a.server.ID)
	requireHistoryEvent(t, detail.History[3], 1, "created", "active", "selected_servers", "three servers", a.server.ID, b.server.ID, c.server.ID)

	requireLifecycleEvents(t, f, created.ID, []expectedLifecycleEvent{
		{action: audit.ActionKillswitchActivate, version: 1, state: "active", operation: "activate", operationID: createOperation},
		{action: audit.ActionKillswitchChange, version: 2, state: "active", operation: "change", operationID: editOperation},
		{action: audit.ActionKillswitchChange, version: 3, state: "active", operation: "change", operationID: allOperation},
		{action: audit.ActionKillswitchDeactivate, version: 4, state: "inactive", operation: "deactivate", operationID: liftOperation},
	})
	for _, target := range []killswitchAcceptanceTarget{a, b, c, d} {
		requireUserSessionActive(t, f, target)
	}
}

func TestKillswitchAcceptanceLiftSelectedWhileAllRemains(t *testing.T) {
	t.Parallel()

	ctx, f := newKillswitchAcceptanceFixture(t)
	target := f.newHostedTarget(t, ctx, "overlap")
	sessionID := f.initialize(t, target)
	baseline := f.call(t, target, makeToolsCallBody("missing_tool"), sessionID).Body.String()
	all := f.create(t, ctx, uuid.New(), &gen.KillswitchScope{Type: "all_servers"}, scheduleNow(), "all overlap")
	selected := f.create(t, ctx, uuid.New(), selectedServers(target.server.ID), scheduleNow(), "selected overlap")
	requireKillswitchDenied(t, f.call(t, target, makeToolsCallBody("missing_tool"), sessionID), "selected overlap")

	liftedSelected, err := f.management.Lift(ctx, &gen.LiftPayload{OperationID: uuid.NewString(), ID: selected.ID, ExpectedVersion: selected.Version})
	require.NoError(t, err)
	require.Len(t, liftedSelected.RemainingOverlaps, 1)
	require.Equal(t, all.ID, liftedSelected.RemainingOverlaps[0].ID)
	requireKillswitchDenied(t, f.call(t, target, makeToolsCallBody("missing_tool"), sessionID), "all overlap")

	liftedAll, err := f.management.Lift(ctx, &gen.LiftPayload{OperationID: uuid.NewString(), ID: all.ID, ExpectedVersion: all.Version})
	require.NoError(t, err)
	require.Empty(t, liftedAll.RemainingOverlaps)
	require.JSONEq(t, baseline, f.call(t, target, makeToolsCallBody("missing_tool"), sessionID).Body.String())
	requireUserSessionActive(t, f, target)
}

func TestKillswitchAcceptanceExpiryOverlapUsesDatabaseTimeAndRecordsOnce(t *testing.T) {
	t.Parallel()

	ctx, f := newKillswitchAcceptanceFixture(t)
	target := f.newHostedTarget(t, ctx, "expiry-overlap")
	sessionID := f.initialize(t, target)

	all := f.create(t, ctx, uuid.New(), &gen.KillswitchScope{Type: "all_servers"}, scheduleNow(), "all remains active")
	expires := databaseNow(t, f.ti.conn).Add(5 * time.Second)
	expiresAt := expires.Format(time.RFC3339Nano)
	createOperation := uuid.New()
	created := f.create(t, ctx, createOperation, selectedServers(target.server.ID), &gen.KillswitchSchedule{
		Start: "now", End: "bounded", EndsAt: &expiresAt,
	}, "selected until expiry")
	requireKillswitchDenied(t, f.call(t, target, makeToolsCallBody("missing_tool"), sessionID), "selected until expiry")
	waitForDatabaseTime(t, f.ti.conn, expires)
	requireKillswitchDenied(t, f.call(t, target, makeToolsCallBody("missing_tool"), sessionID), "all remains active")

	const sweeps = 4
	results := make([]killswitches.ExpiryBatchResult, sweeps)
	errs := make([]error, sweeps)
	var wg sync.WaitGroup
	wg.Add(sweeps)
	for i := range sweeps {
		go func() {
			defer wg.Done()
			results[i], errs[i] = killswitches.NewMaintenanceService(f.ti.conn, audit.NewLogger()).RecordDueExpiries(t.Context(), 100)
		}()
	}
	wg.Wait()
	var recorded int64
	for i := range sweeps {
		require.NoError(t, errs[i])
		recorded += results[i].Recorded
	}
	require.Equal(t, int64(1), recorded)
	retry, err := killswitches.NewMaintenanceService(f.ti.conn, audit.NewLogger()).RecordDueExpiries(t.Context(), 100)
	require.NoError(t, err)
	require.Zero(t, retry.Recorded)

	var markers int64
	err = f.ti.conn.QueryRow(t.Context(), `SELECT count(*) FROM killswitch_expiry_events WHERE prescription_id = $1`, created.ID).Scan(&markers) //nolint:glint // notestingrawsql: exact marker assertion for one prescription
	require.NoError(t, err)
	require.Equal(t, int64(1), markers)
	requireExpiryEvents(t, f, created.ID, createOperation, expires)
	requireKillswitchDenied(t, f.call(t, target, makeToolsCallBody("missing_tool"), sessionID), "all remains active")
	allDetail, err := f.management.Get(ctx, &gen.GetPayload{ID: all.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), allDetail.Version)
	require.Len(t, allDetail.History, 1)
	requireUserSessionActive(t, f, target)
}

func requireExpiryEvents(t *testing.T, f *killswitchAcceptanceFixture, prescriptionID string, createOperation uuid.UUID, expires time.Time) {
	t.Helper()
	var action, actorID, actorDisplayName string
	var metadataJSON []byte
	var auditCount int64
	//nolint:glint // notestingrawsql: bounded read-only expiry assertion
	err := f.ti.conn.QueryRow(t.Context(), `
		SELECT action, actor_id, coalesce(actor_display_name, ''), coalesce(metadata, 'null'::jsonb), count(*) OVER ()
		FROM audit_logs
		WHERE organization_id = $1 AND subject_id = $2 AND action = $3
	`, f.auth.ActiveOrganizationID, prescriptionID, string(audit.ActionKillswitchExpire)).Scan(&action, &actorID, &actorDisplayName, &metadataJSON, &auditCount)
	require.NoError(t, err)
	require.Equal(t, int64(1), auditCount)
	require.Equal(t, string(audit.ActionKillswitchExpire), action)
	require.Equal(t, "system", actorID)
	require.Equal(t, "System", actorDisplayName)
	var metadata struct {
		Version   int64     `json:"version"`
		ExpiredAt time.Time `json:"expired_at"`
	}
	require.NoError(t, json.Unmarshal(metadataJSON, &metadata))
	require.Equal(t, int64(1), metadata.Version)
	require.Equal(t, expires, metadata.ExpiredAt)

	requireOutboxEvents(t, f, prescriptionID, []expectedLifecycleEvent{
		{action: audit.ActionKillswitchActivate, version: 1, state: "active", operation: "activate", operationID: createOperation},
		{action: audit.ActionKillswitchExpire, version: 1, actorID: "system"},
	})
	requireNoInternalNoteLeak(t, f)
}
