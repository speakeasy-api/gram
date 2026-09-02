package assistants

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsRepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcpRepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	remotesessionsRepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersessionsRepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// stubWorkflowSignaler satisfies WorkflowSignaler for handler tests that
// don't run Temporal. It records signalled threads so creation paths can
// assert the eager runtime boot was kicked off.
type stubWorkflowSignaler struct {
	signalledThreads []uuid.UUID
}

func (s *stubWorkflowSignaler) SignalCoordinator(context.Context, uuid.UUID) error {
	return nil
}

func (s *stubWorkflowSignaler) SignalThread(_ context.Context, threadID, _ uuid.UUID) error {
	s.signalledThreads = append(s.signalledThreads, threadID)
	return nil
}

func TestServiceRequiresProjectGrants(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID := newRBACService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	assistantID := uuid.NewString()
	for name, call := range map[string]func(context.Context) error{
		"list": func(ctx context.Context) error {
			_, err := svc.ListAssistants(ctx, &gen.ListAssistantsPayload{
				SessionToken:     nil,
				ProjectSlugInput: nil,
			})
			return err
		},
		"get": func(ctx context.Context) error {
			_, err := svc.GetAssistant(ctx, &gen.GetAssistantPayload{
				ID:               assistantID,
				SessionToken:     nil,
				ProjectSlugInput: nil,
			})
			return err
		},
		"create": func(ctx context.Context) error {
			_, err := svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{
				SessionToken:     nil,
				ProjectSlugInput: nil,
				Name:             "Assistant",
				Model:            "openai/gpt-4o-mini",
				Instructions:     "",
				Toolsets:         nil,
				WarmTTLSeconds:   nil,
				MaxConcurrency:   nil,
				Status:           nil,
			})
			return err
		},
		"update": func(ctx context.Context) error {
			_, err := svc.UpdateAssistant(ctx, &gen.UpdateAssistantPayload{
				SessionToken:     nil,
				ProjectSlugInput: nil,
				ID:               assistantID,
				Name:             nil,
				Model:            nil,
				Instructions:     nil,
				Toolsets:         nil,
				WarmTTLSeconds:   nil,
				MaxConcurrency:   nil,
				Status:           nil,
			})
			return err
		},
		"delete": func(ctx context.Context) error {
			return svc.DeleteAssistant(ctx, &gen.DeleteAssistantPayload{
				ID:               assistantID,
				SessionToken:     nil,
				ProjectSlugInput: nil,
			})
		},
		"getManaged": func(ctx context.Context) error {
			_, err := svc.GetManagedAssistant(ctx, &gen.GetManagedAssistantPayload{
				SessionToken:     nil,
				ProjectSlugInput: nil,
			})
			return err
		},
		"ensureManaged": func(ctx context.Context) error {
			_, err := svc.EnsureManagedAssistant(ctx, &gen.EnsureManagedAssistantPayload{
				SessionToken:     nil,
				ProjectSlugInput: nil,
			})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			requireOopsCode(t, call(ctx), oops.CodeForbidden)
		})
	}

	readCtx := authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, projectID.String()),
	})
	_, err := svc.ListAssistants(readCtx, &gen.ListAssistantsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	// getManaged is read-scoped — with project:read but no managed assistant
	// provisioned yet, it must surface NotFound (so the dashboard can decide
	// whether to call ensureManaged or show the viewer notice) rather than
	// 403, which would conflate "missing" with "no permission".
	_, err = svc.GetManagedAssistant(readCtx, &gen.GetManagedAssistantPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestServiceCreateAssistantMapsInvalidToolsetToBadRequest(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID := newRBACService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectWrite,
		Selector: authz.NewSelector(authz.ScopeProjectWrite, projectID.String()),
	})

	_, err := svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             "Assistant",
		Model:            "openai/gpt-4o-mini",
		Instructions:     "",
		Toolsets: []*types.AssistantToolsetRef{
			{ToolsetSlug: "missing-toolset", EnvironmentSlug: nil},
		},
		WarmTTLSeconds: nil,
		MaxConcurrency: nil,
		Status:         nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestServiceCreateAssistantAutoEnablesMCPOnAttachedToolsets(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistants_mcp_autoenable")
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectWrite,
		Selector: authz.NewSelector(authz.ScopeProjectWrite, projectID.String()),
	})

	toolsetsQ := toolsetsRepo.New(conn)
	ts, err := toolsetsQ.CreateToolset(t.Context(), toolsetsRepo.CreateToolsetParams{
		OrganizationID:         "org-test",
		ProjectID:              projectID,
		Name:                   "Slack",
		Slug:                   "slack",
		Description:            pgtype.Text{},
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                pgtype.Text{String: "org-test-slack-xyz", Valid: true},
		McpEnabled:             false,
	})
	require.NoError(t, err)
	require.False(t, ts.McpEnabled)

	_, err = svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             "Assistant",
		Model:            "openai/gpt-4o-mini",
		Instructions:     "",
		Toolsets: []*types.AssistantToolsetRef{
			{ToolsetSlug: ts.Slug, EnvironmentSlug: nil},
		},
		WarmTTLSeconds: nil,
		MaxConcurrency: nil,
		Status:         nil,
	})
	require.NoError(t, err)

	reloaded, err := toolsetsQ.GetToolset(t.Context(), toolsetsRepo.GetToolsetParams{
		Slug:      ts.Slug,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.True(t, reloaded.McpEnabled, "attaching toolset to assistant must enable MCP")
}

func TestServiceUpdateAssistantAutoEnablesMCPOnAttachedToolsets(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistants_mcp_autoenable_update")
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectWrite,
		Selector: authz.NewSelector(authz.ScopeProjectWrite, projectID.String()),
	})

	toolsetsQ := toolsetsRepo.New(conn)
	ts, err := toolsetsQ.CreateToolset(t.Context(), toolsetsRepo.CreateToolsetParams{
		OrganizationID:         "org-test",
		ProjectID:              projectID,
		Name:                   "Slack",
		Slug:                   "slack",
		Description:            pgtype.Text{},
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                pgtype.Text{String: "org-test-slack-xyz", Valid: true},
		McpEnabled:             false,
	})
	require.NoError(t, err)

	created, err := svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             "Assistant",
		Model:            "openai/gpt-4o-mini",
		Instructions:     "",
		Toolsets:         nil,
		WarmTTLSeconds:   nil,
		MaxConcurrency:   nil,
		Status:           nil,
	})
	require.NoError(t, err)

	_, err = svc.UpdateAssistant(ctx, &gen.UpdateAssistantPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		Name:             nil,
		Model:            nil,
		Instructions:     nil,
		Toolsets: []*types.AssistantToolsetRef{
			{ToolsetSlug: ts.Slug, EnvironmentSlug: nil},
		},
		WarmTTLSeconds: nil,
		MaxConcurrency: nil,
		Status:         nil,
	})
	require.NoError(t, err)

	reloaded, err := toolsetsQ.GetToolset(t.Context(), toolsetsRepo.GetToolsetParams{
		Slug:      ts.Slug,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.True(t, reloaded.McpEnabled, "updating assistant toolsets must enable MCP on newly attached toolsets")
}

// A remote-backed MCP server (no toolset) can be attached to an assistant and
// round-trips through the API and the dispatch resolver, which points the
// runner at the server's Gram-hosted endpoint.
func TestServiceAttachRemoteMcpServerToAssistant(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistants_attach_mcp_server")
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectWrite,
		Selector: authz.NewSelector(authz.ScopeProjectWrite, projectID.String()),
	})

	// Seed a remote-backed mcp_server with a Gram-hosted endpoint, mirroring
	// how the dashboard registers an external "Remote MCP" server.
	remote, err := remotemcpRepo.New(conn).CreateServer(t.Context(), remotemcpRepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		Name:          pgtype.Text{String: "External SaaS", Valid: true},
		Slug:          pgtype.Text{String: "external-remote-src", Valid: true},
		TransportType: "streamable-http",
		Url:           "https://mcp.example.com/v1/mcp",
	})
	require.NoError(t, err)

	issuer, err := usersessionsRepo.New(conn).CreateUserSessionIssuer(t.Context(), usersessionsRepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		Slug:               "usi-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)

	server, err := mcpserversRepo.New(conn).CreateMCPServer(t.Context(), mcpserversRepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                pgtype.Text{String: "General - External SaaS", Valid: true},
		Slug:                pgtype.Text{String: "external-remote-mcp-xyz", Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{UUID: remote.ID, Valid: true},
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Visibility:          "private",
	})
	require.NoError(t, err)

	_, err = mcpendpointsRepo.New(conn).CreateMCPEndpoint(t.Context(), mcpendpointsRepo.CreateMCPEndpointParams{
		ProjectID:   projectID,
		McpServerID: uuid.NullUUID{UUID: server.ID, Valid: true},
		Slug:        "team-remote-mcp",
	})
	require.NoError(t, err)

	created, err := svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             "Assistant",
		Model:            "openai/gpt-4o-mini",
		Instructions:     "",
		Toolsets:         nil,
		McpServers: []*types.AssistantMCPServerRef{
			{McpServerSlug: server.Slug.String, EnvironmentSlug: nil},
		},
		WarmTTLSeconds: nil,
		MaxConcurrency: nil,
		Status:         nil,
	})
	require.NoError(t, err)
	require.Len(t, created.McpServers, 1)
	require.Equal(t, server.Slug.String, created.McpServers[0].McpServerSlug)

	// Round-trips through a fresh read.
	got, err := svc.GetAssistant(ctx, &gen.GetAssistantPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, got.McpServers, 1)
	require.Equal(t, server.Slug.String, got.McpServers[0].McpServerSlug)

	// The dispatch resolver turns the attachment into the runner's MCP URL,
	// pointed at the server's Gram-hosted endpoint slug (not the internal slug).
	assistantID, err := uuid.Parse(created.ID)
	require.NoError(t, err)
	rows, err := svc.core.loadAssistantMcpServers(t.Context(), projectID, []uuid.UUID{assistantID})
	require.NoError(t, err)
	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)
	resolved := resolveAssistantMCPServers(t.Context(), testenv.NewLogger(t), serverURL, nil, rows[assistantID], nil)
	require.Len(t, resolved, 1)
	require.Equal(t, "external-remote-mcp-xyz", resolved[0].ID)
	require.Equal(t, "https://gram.test/mcp/team-remote-mcp", resolved[0].URL)
}

// Attaching a server the runtime cannot reach fails the write with a
// validation error instead of silently vanishing from reads and dispatch:
// no Gram-hosted endpoint means no /mcp URL to build, and disabled servers
// 404 at the serving path.
func TestAssistantsService_AttachMCPServer_RejectsUnreachable(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistants_attach_mcp_server_reject")
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectWrite,
		Selector: authz.NewSelector(authz.ScopeProjectWrite, projectID.String()),
	})

	remote, err := remotemcpRepo.New(conn).CreateServer(t.Context(), remotemcpRepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		Name:          pgtype.Text{String: "External SaaS", Valid: true},
		Slug:          pgtype.Text{String: "external-remote-src", Valid: true},
		TransportType: "streamable-http",
		Url:           "https://mcp.example.com/v1/mcp",
	})
	require.NoError(t, err)

	issuer, err := usersessionsRepo.New(conn).CreateUserSessionIssuer(t.Context(), usersessionsRepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		Slug:               "usi-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)

	// No mcp_endpoints row for this server.
	endpointless, err := mcpserversRepo.New(conn).CreateMCPServer(t.Context(), mcpserversRepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                pgtype.Text{String: "Endpointless", Valid: true},
		Slug:                pgtype.Text{String: "endpointless-remote", Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{UUID: remote.ID, Valid: true},
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Visibility:          "private",
	})
	require.NoError(t, err)

	disabled, err := mcpserversRepo.New(conn).CreateMCPServer(t.Context(), mcpserversRepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                pgtype.Text{String: "Disabled", Valid: true},
		Slug:                pgtype.Text{String: "disabled-remote", Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{UUID: remote.ID, Valid: true},
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Visibility:          "disabled",
	})
	require.NoError(t, err)
	_, err = mcpendpointsRepo.New(conn).CreateMCPEndpoint(t.Context(), mcpendpointsRepo.CreateMCPEndpointParams{
		ProjectID:   projectID,
		McpServerID: uuid.NullUUID{UUID: disabled.ID, Valid: true},
		Slug:        "disabled-remote-endpoint",
	})
	require.NoError(t, err)

	for _, tt := range []struct {
		slug    string
		wantErr string
	}{
		{slug: endpointless.Slug.String, wantErr: "no Gram-hosted MCP endpoint"},
		{slug: disabled.Slug.String, wantErr: "is disabled"},
	} {
		// The resolver carries the reason; the endpoint maps it to a 400.
		_, resolveErr := svc.core.resolveMcpServerRefsForWrite(t.Context(), projectID, []*types.AssistantMCPServerRef{
			{McpServerSlug: tt.slug, EnvironmentSlug: nil},
		})
		require.ErrorContains(t, resolveErr, tt.wantErr)

		_, err := svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			Name:             "Assistant " + tt.slug,
			Model:            "openai/gpt-4o-mini",
			Instructions:     "",
			Toolsets:         nil,
			McpServers: []*types.AssistantMCPServerRef{
				{McpServerSlug: tt.slug, EnvironmentSlug: nil},
			},
			WarmTTLSeconds: nil,
			MaxConcurrency: nil,
			Status:         nil,
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}

func newRBACService(t *testing.T) (*Service, context.Context, uuid.UUID) {
	t.Helper()
	svc, ctx, projectID, _ := newRBACServiceWithConn(t, "assistants_rbac")
	return svc, ctx, projectID
}

func newRBACServiceWithConn(t *testing.T, dbName string) (*Service, context.Context, uuid.UUID, *pgxpool.Pool) {
	t.Helper()

	conn, err := assistantsInfra.CloneTestDatabase(t, dbName)
	require.NoError(t, err)

	proj, err := projectsRepo.New(conn).CreateProject(t.Context(), projectsRepo.CreateProjectParams{
		Name:           "Project",
		Slug:           "project-rbac-test",
		OrganizationID: "org-test",
	})
	require.NoError(t, err)
	projectID := proj.ID
	projectSlug := proj.Slug

	logger := testenv.NewLogger(t)
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	service := &Service{
		tracer:   testenv.NewTracerProvider(t).Tracer("test"),
		logger:   logger,
		auth:     nil,
		authz:    authzEngine,
		core:     NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger()),
		signaler: &stubWorkflowSignaler{signalledThreads: nil},
	}

	sessionID := "session-test"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org-test",
		UserID:                "user-test",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &sessionID,
		ProjectID:             &projectID,
		OrganizationSlug:      "org-test",
		Email:                 nil,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           &projectSlug,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	})

	return service, ctx, projectID, conn
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

// Attaching a disabled toolset lifts its wrapper out of disabled too, so the
// endpoint the assistant runtime addresses is served rather than a terminal 404.
func TestServiceCreateAssistantLiftsDisabledWrapper(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistants_mcp_wrapper")
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectWrite,
		Selector: authz.NewSelector(authz.ScopeProjectWrite, projectID.String()),
	})

	ts, err := toolsetsRepo.New(conn).CreateToolset(t.Context(), toolsetsRepo.CreateToolsetParams{
		OrganizationID: "org-test",
		ProjectID:      projectID,
		Name:           "Slack",
		Slug:           "slack",
		McpSlug:        pgtype.Text{String: "org-test-slack-wrapped", Valid: true},
		McpEnabled:     false,
	})
	require.NoError(t, err)
	issuer, err := usersessionsRepo.New(conn).CreateUserSessionIssuer(t.Context(), usersessionsRepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		OrganizationID:     pgtype.Text{String: "", Valid: false},
		Slug:               "usi-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	_, err = toolsetsRepo.New(conn).UpdateToolsetUserSessionIssuer(t.Context(), toolsetsRepo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Slug:                ts.Slug,
		ProjectID:           projectID,
	})
	require.NoError(t, err)
	// remote_session_issuers FK to organization_metadata; the fixture org is a bare id.
	now := time.Now()
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(t.Context(), testrepo.CreateOrganizationMetadataFixtureParams{
		ID: "org-test", Name: "org-test", Slug: "org-test", GramAccountType: "enterprise",
		FreeTrialStartedAt: conv.ToPGTimestamptz(now), FreeTrialEndsAt: conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
	}))
	remoteIssuerID := seedBoundRemoteSessionIssuer(t, conn, "org-test", projectID, issuer.ID)
	serverID, err := uuid.NewV7()
	require.NoError(t, err)
	wrapper, err := mcpserversRepo.New(conn).CreateMCPServer(t.Context(), mcpserversRepo.CreateMCPServerParams{
		ID:         serverID,
		ProjectID:  projectID,
		Name:       pgtype.Text{String: "Slack", Valid: true},
		Slug:       pgtype.Text{String: "slack-" + serverID.String()[:8], Valid: true},
		ToolsetID:  uuid.NullUUID{UUID: ts.ID, Valid: true},
		Visibility: "disabled",
	})
	require.NoError(t, err)
	_, err = mcpendpointsRepo.New(conn).CreateMCPEndpoint(t.Context(), mcpendpointsRepo.CreateMCPEndpointParams{
		ProjectID:   projectID,
		McpServerID: uuid.NullUUID{UUID: wrapper.ID, Valid: true},
		Slug:        ts.McpSlug.String,
	})
	require.NoError(t, err)

	_, err = svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{
		Name:         "Assistant",
		Model:        "openai/gpt-4o-mini",
		Instructions: "",
		Toolsets:     []*types.AssistantToolsetRef{{ToolsetSlug: ts.Slug, EnvironmentSlug: nil}},
	})
	require.NoError(t, err)

	lifted, err := mcpserversRepo.New(conn).GetMCPServerByToolsetID(t.Context(), mcpserversRepo.GetMCPServerByToolsetIDParams{ToolsetID: ts.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, "private", lifted.Visibility)
	// The stale wrapper issuer is repaired and its derived remote issuer resynced.
	require.Equal(t, uuid.NullUUID{UUID: issuer.ID, Valid: true}, lifted.UserSessionIssuerID)
	require.Equal(t, uuid.NullUUID{UUID: remoteIssuerID, Valid: true}, lifted.RemoteSessionIssuerID)
	endpoints, err := mcpendpointsRepo.New(conn).ListMCPEndpointsByMCPServerID(t.Context(), mcpendpointsRepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: wrapper.ID})
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	require.Equal(t, ts.McpSlug.String, endpoints[0].Slug)
}

// seedBoundRemoteSessionIssuer creates a remote session issuer with one client
// bound to the given user issuer, the shape the issuer resync derives from.
func seedBoundRemoteSessionIssuer(t *testing.T, conn *pgxpool.Pool, organizationID string, projectID, userIssuerID uuid.UUID) uuid.UUID {
	t.Helper()

	q := remotesessionsRepo.New(conn)
	suffix := uuid.NewString()[:8]
	issuer, err := q.CreateRemoteSessionIssuer(t.Context(), remotesessionsRepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(projectID),
		OrganizationID:                    conv.ToPGText(organizationID),
		Slug:                              "rsi-" + suffix,
		Issuer:                            "https://issuer-" + suffix + ".example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://issuer-" + suffix + ".example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://issuer-" + suffix + ".example.com/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
	})
	require.NoError(t, err)
	client, err := q.CreateRemoteSessionClient(t.Context(), remotesessionsRepo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(projectID),
		OrganizationID:        conv.ToPGTextEmpty(organizationID),
		RemoteSessionIssuerID: issuer.ID,
		ClientID:              "client-" + suffix,
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now()),
	})
	require.NoError(t, err)
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(t.Context(), remotesessionsRepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userIssuerID,
	}))
	return issuer.ID
}
