package assistants

import (
	"context"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpendpointsRepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcpRepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
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

	server, err := mcpserversRepo.New(conn).CreateMCPServer(t.Context(), mcpserversRepo.CreateMCPServerParams{
		ID:                uuid.New(),
		ProjectID:         projectID,
		Name:              pgtype.Text{String: "General - External SaaS", Valid: true},
		Slug:              pgtype.Text{String: "external-remote-mcp-xyz", Valid: true},
		RemoteMcpServerID: uuid.NullUUID{UUID: remote.ID, Valid: true},
		Visibility:        "private",
	})
	require.NoError(t, err)

	_, err = mcpendpointsRepo.New(conn).CreateMCPEndpoint(t.Context(), mcpendpointsRepo.CreateMCPEndpointParams{
		ProjectID:   projectID,
		McpServerID: server.ID,
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
	chConn, err := assistantsInfra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	service := &Service{
		tracer:   testenv.NewTracerProvider(t).Tracer("test"),
		logger:   logger,
		auth:     nil,
		authz:    authzEngine,
		core:     NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(logger), nil),
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
