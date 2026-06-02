package mcpmetadata_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	assets_repo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environments_repo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projects_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func createTestToolset(t *testing.T, ctx context.Context, ti *testInstance, slug string) toolsets_repo.Toolset {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := toolsets_repo.New(ti.conn).CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Test MCP Server",
		Slug:                   slug,
		Description:            conv.ToPGText("A test MCP server"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             false,
	})
	require.NoError(t, err)

	return toolset
}

func TestService_SetMcpMetadata(t *testing.T) {
	t.Parallel()

	t.Run("creates metadata for toolset", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPMetadataService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Test MCP Server",
			Slug:                   "test-mcp",
			Description:            conv.ToPGText("A test MCP server"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                pgtype.Text{String: "", Valid: false},
			McpEnabled:             false,
		})
		require.NoError(t, err)

		payload := &gen.SetMcpMetadataPayload{
			ToolsetSlug:              conv.PtrEmpty(types.Slug(toolset.Slug)),
			LogoAssetID:              nil,
			ExternalDocumentationURL: new("https://docs.example.com"),
			SessionToken:             nil,
			ProjectSlugInput:         nil,
		}

		result, err := ti.service.SetMcpMetadata(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.NotEmpty(t, result.ID)
		require.NotNil(t, result.ToolsetID)
		require.Equal(t, toolset.ID.String(), *result.ToolsetID)
		require.NotNil(t, result.ExternalDocumentationURL)
		require.Equal(t, "https://docs.example.com", *result.ExternalDocumentationURL)
		require.Nil(t, result.LogoAssetID)
	})

	t.Run("updates existing metadata", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPMetadataService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Test MCP Server",
			Slug:                   "test-mcp-update",
			Description:            conv.ToPGText("A test MCP server"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                pgtype.Text{String: "", Valid: false},
			McpEnabled:             false,
		})
		require.NoError(t, err)

		firstPayload := &gen.SetMcpMetadataPayload{
			ToolsetSlug:              conv.PtrEmpty(types.Slug(toolset.Slug)),
			LogoAssetID:              nil,
			ExternalDocumentationURL: new("https://docs.example.com/v1"),
			SessionToken:             nil,
			ProjectSlugInput:         nil,
		}

		firstResult, err := ti.service.SetMcpMetadata(ctx, firstPayload)
		require.NoError(t, err)
		require.NotNil(t, firstResult)

		secondPayload := &gen.SetMcpMetadataPayload{
			ToolsetSlug:              conv.PtrEmpty(types.Slug(toolset.Slug)),
			LogoAssetID:              nil,
			ExternalDocumentationURL: new("https://docs.example.com/v2"),
			SessionToken:             nil,
			ProjectSlugInput:         nil,
		}

		secondResult, err := ti.service.SetMcpMetadata(ctx, secondPayload)
		require.NoError(t, err)
		require.NotNil(t, secondResult)

		require.Equal(t, firstResult.ID, secondResult.ID)
		require.NotNil(t, secondResult.ToolsetID)
		require.Equal(t, toolset.ID.String(), *secondResult.ToolsetID)
		require.NotNil(t, secondResult.ExternalDocumentationURL)
		require.Equal(t, "https://docs.example.com/v2", *secondResult.ExternalDocumentationURL)
	})

	t.Run("sets logo asset ID", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPMetadataService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Test MCP Server",
			Slug:                   "test-mcp-logo",
			Description:            conv.ToPGText("A test MCP server"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                pgtype.Text{String: "", Valid: false},
			McpEnabled:             false,
		})
		require.NoError(t, err)

		assetsRepo := assets_repo.New(ti.conn)
		asset, err := assetsRepo.CreateAsset(ctx, assets_repo.CreateAssetParams{
			Name:          "test-logo.png",
			Url:           "https://example.com/logo.png",
			ProjectID:     *authCtx.ProjectID,
			Sha256:        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			Kind:          "image",
			ContentType:   "image/png",
			ContentLength: 1024,
		})
		require.NoError(t, err)

		logoAssetID := asset.ID.String()

		payload := &gen.SetMcpMetadataPayload{
			ToolsetSlug:              conv.PtrEmpty(types.Slug(toolset.Slug)),
			LogoAssetID:              &logoAssetID,
			ExternalDocumentationURL: nil,
			SessionToken:             nil,
			ProjectSlugInput:         nil,
		}

		result, err := ti.service.SetMcpMetadata(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.NotEmpty(t, result.ID)
		require.NotNil(t, result.ToolsetID)
		require.Equal(t, toolset.ID.String(), *result.ToolsetID)
		require.NotNil(t, result.LogoAssetID)
		require.Equal(t, logoAssetID, *result.LogoAssetID)
		require.Nil(t, result.ExternalDocumentationURL)
	})

	t.Run("sets server instructions", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPMetadataService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Test MCP Server",
			Slug:                   "test-mcp-instructions",
			Description:            conv.ToPGText("A test MCP server"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                pgtype.Text{String: "", Valid: false},
			McpEnabled:             false,
		})
		require.NoError(t, err)

		instructions := "You have tools for searching the Test Hub. Use them wisely."

		payload := &gen.SetMcpMetadataPayload{
			ToolsetSlug:              conv.PtrEmpty(types.Slug(toolset.Slug)),
			LogoAssetID:              nil,
			ExternalDocumentationURL: nil,
			Instructions:             &instructions,
			SessionToken:             nil,
			ProjectSlugInput:         nil,
		}

		result, err := ti.service.SetMcpMetadata(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.NotEmpty(t, result.ID)
		require.NotNil(t, result.ToolsetID)
		require.Equal(t, toolset.ID.String(), *result.ToolsetID)
		require.NotNil(t, result.Instructions)
		require.Equal(t, instructions, *result.Instructions)
		require.Nil(t, result.LogoAssetID)
		require.Nil(t, result.ExternalDocumentationURL)
	})
}

func TestService_SetMcpMetadata_DefaultEnvironmentID_Valid(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolset := createTestToolset(t, ctx, ti, "test-mcp-env-valid")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	envRepo := environments_repo.New(ti.conn)
	env, err := envRepo.CreateEnvironment(ctx, environments_repo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "Production",
		Slug:           "production",
		Description:    pgtype.Text{},
	})
	require.NoError(t, err)

	envID := env.ID.String()
	result, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:          conv.PtrEmpty(types.Slug(toolset.Slug)),
		DefaultEnvironmentID: &envID,
		SessionToken:         nil,
		ProjectSlugInput:     nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.DefaultEnvironmentID)
	require.Equal(t, envID, *result.DefaultEnvironmentID)
}

func TestService_SetMcpMetadata_DefaultEnvironmentID_WrongProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolset := createTestToolset(t, ctx, ti, "test-mcp-env-wrong-proj")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Create a second project to own the environment
	projRepo := projects_repo.New(ti.conn)
	otherProject, err := projRepo.CreateProject(ctx, projects_repo.CreateProjectParams{
		Name:           "Other Project",
		Slug:           "other-project",
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	otherProjectID := otherProject.ID

	envRepo := environments_repo.New(ti.conn)
	env, err := envRepo.CreateEnvironment(ctx, environments_repo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      otherProjectID,
		Name:           "Other Env",
		Slug:           "other-env",
		Description:    pgtype.Text{},
	})
	require.NoError(t, err)

	envID := env.ID.String()
	result, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:          conv.PtrEmpty(types.Slug(toolset.Slug)),
		DefaultEnvironmentID: &envID,
		SessionToken:         nil,
		ProjectSlugInput:     nil,
	})
	require.Nil(t, result)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_SetMcpMetadata_AuditLogCountOnCreate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolset := createTestToolset(t, ctx, ti, "test-mcp-audit-create")

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPMetadataUpdate)
	require.NoError(t, err)

	result, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:              conv.PtrEmpty(types.Slug(toolset.Slug)),
		ExternalDocumentationURL: new("https://docs.example.com/create"),
		SessionToken:             nil,
		ProjectSlugInput:         nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMCPMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionMCPMetadataUpdate), record.Action)
	require.Equal(t, "toolset", record.SubjectType)
	require.Equal(t, toolset.Name, record.SubjectDisplay)
	require.Equal(t, toolset.Slug, record.SubjectSlug)
	require.Empty(t, string(record.BeforeSnapshot))
	require.NotNil(t, record.AfterSnapshot)
	require.Nil(t, record.Metadata)

	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, result.ID, afterSnapshot["ID"])
	require.Equal(t, toolset.ID.String(), afterSnapshot["ToolsetID"])
	require.Equal(t, "https://docs.example.com/create", afterSnapshot["ExternalDocumentationURL"])
}

func TestService_SetMcpMetadata_AuditLogSnapshotsOnUpdate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolset := createTestToolset(t, ctx, ti, "test-mcp-audit-update")

	firstResult, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:              conv.PtrEmpty(types.Slug(toolset.Slug)),
		ExternalDocumentationURL: new("https://docs.example.com/before"),
		SessionToken:             nil,
		ProjectSlugInput:         nil,
	})
	require.NoError(t, err)
	require.NotNil(t, firstResult)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPMetadataUpdate)
	require.NoError(t, err)

	instructions := "Updated MCP installation instructions"
	secondResult, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:              conv.PtrEmpty(types.Slug(toolset.Slug)),
		ExternalDocumentationURL: new("https://docs.example.com/after"),
		Instructions:             &instructions,
		SessionToken:             nil,
		ProjectSlugInput:         nil,
	})
	require.NoError(t, err)
	require.NotNil(t, secondResult)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMCPMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionMCPMetadataUpdate), record.Action)
	require.Equal(t, "toolset", record.SubjectType)
	require.Equal(t, toolset.Name, record.SubjectDisplay)
	require.Equal(t, toolset.Slug, record.SubjectSlug)
	require.NotNil(t, record.BeforeSnapshot)
	require.NotNil(t, record.AfterSnapshot)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, firstResult.ID, beforeSnapshot["ID"])
	require.Equal(t, "https://docs.example.com/before", beforeSnapshot["ExternalDocumentationURL"])
	require.Nil(t, beforeSnapshot["Instructions"])

	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, secondResult.ID, afterSnapshot["ID"])
	require.Equal(t, "https://docs.example.com/after", afterSnapshot["ExternalDocumentationURL"])
	require.Equal(t, instructions, afterSnapshot["Instructions"])
}

func TestService_SetMcpMetadata_NoAuditLogOnFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolset := createTestToolset(t, ctx, ti, "test-mcp-audit-failure")

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPMetadataUpdate)
	require.NoError(t, err)

	invalidLogoID := "not-a-uuid"
	result, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:      conv.PtrEmpty(types.Slug(toolset.Slug)),
		LogoAssetID:      &invalidLogoID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Nil(t, result)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestService_SetMcpMetadata_ByMcpServerID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	server, _ := createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{})

	docURL := "https://docs.example.com/remote-mcp"
	instructions := "Authenticate with your remote MCP credentials."
	serverID := server.ID.String()

	result, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		McpServerID:              &serverID,
		ExternalDocumentationURL: &docURL,
		Instructions:             &instructions,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.ToolsetID)
	require.NotNil(t, result.McpServerID)
	require.Equal(t, serverID, *result.McpServerID)
	require.NotNil(t, result.Instructions)
	require.Equal(t, instructions, *result.Instructions)
}

func TestService_SetMcpMetadata_McpServer_EmitsAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	server, _ := createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{
		name: "Audit Log Server",
	})

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPMetadataUpdate)
	require.NoError(t, err)

	serverID := server.ID.String()
	docURL := "https://docs.example.com/remote-mcp-audit"

	_, err = ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		McpServerID:              &serverID,
		ExternalDocumentationURL: &docURL,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount, "expected an mcp_metadata:update audit event")

	// Both backends share the action constant; verify the latest entry
	// carries the mcp_server subject so /auditlogs filtering can still
	// distinguish the two backends downstream.
	latest, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMCPMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, "mcp_server", latest.SubjectType, "latest entry should carry the mcp_server subject")
}

func TestService_SetMcpMetadata_RejectsDefaultEnvironmentIDForMcpServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	server, _ := createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{})

	// Make an environment in the same project so the rejection has to come
	// from the explicit guard rather than from the existence check.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	env, err := environments_repo.New(ti.conn).CreateEnvironment(ctx, environments_repo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "Test Env",
		Slug:           "test-env",
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	serverID := server.ID.String()
	envID := env.ID.String()
	_, err = ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		McpServerID:          &serverID,
		DefaultEnvironmentID: &envID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestService_SetMcpMetadata_RejectsBothBackends(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolset := createTestToolset(t, ctx, ti, "xor-both-toolset-set")
	server, _ := createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{})

	serverID := server.ID.String()
	_, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug: conv.PtrEmpty(types.Slug(toolset.Slug)),
		McpServerID: &serverID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestService_SetMcpMetadata_RejectsCrossProjectMcpServerID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	// IDOR guard: a UUID that doesn't exist in the caller's project must look
	// like a foreign-project id to the lookup; verify the BadRequest path.
	foreignID, err := uuid.NewV7()
	require.NoError(t, err)

	idStr := foreignID.String()
	_, err = ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		McpServerID: &idStr,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestService_SetMcpMetadata_RejectsForeignProjectMcpServer is the real-world
// IDOR scenario: a caller knows the UUID of an mcp_server that exists, but
// lives in a different project. The lookup must reject it as not-found rather
// than treating it as the caller's own.
func TestService_SetMcpMetadata_RejectsForeignProjectMcpServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	otherProject, err := projects_repo.New(ti.conn).CreateProject(ctx, projects_repo.CreateProjectParams{
		Name:           "Foreign Project",
		Slug:           "foreign-project-" + uuid.NewString()[:8],
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	// Create a toolset + mcp_server entirely owned by the foreign project so
	// the row genuinely exists in the DB; the IDOR check has to keep the
	// caller out by project scoping, not by row existence.
	foreignToolset, err := toolsets_repo.New(ti.conn).CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              otherProject.ID,
		Name:                   "Foreign Toolset",
		Slug:                   "foreign-toolset-" + uuid.NewString()[:8],
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             false,
	})
	require.NoError(t, err)

	foreignServerID, err := uuid.NewV7()
	require.NoError(t, err)
	foreignServer, err := mcpservers_repo.New(ti.conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  foreignServerID,
		ProjectID:           otherProject.ID,
		Name:                conv.ToPGText("Foreign MCP Server"),
		Slug:                conv.ToPGText("foreign-mcp-server-" + uuid.NewString()[:8]),
		EnvironmentID:       uuid.NullUUID{},
		UserSessionIssuerID: uuid.NullUUID{},
		RemoteMcpServerID:   uuid.NullUUID{},
		ToolsetID:           uuid.NullUUID{UUID: foreignToolset.ID, Valid: true},
		Visibility:          mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)

	idStr := foreignServer.ID.String()
	_, err = ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		McpServerID: &idStr,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
