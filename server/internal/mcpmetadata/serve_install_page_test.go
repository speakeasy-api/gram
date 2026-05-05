package mcpmetadata_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomains_repo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	deployments_repo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	externalmcp_repo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	externalmcp_types "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	organizations_repo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projects_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	tools_repo "github.com/speakeasy-api/gram/server/internal/tools/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestServeInstallPage_Authentication(t *testing.T) {
	t.Parallel()
	ctx, testInstance := newTestMCPMetadataService(t)

	// Get auth context from the setup (which creates org and project)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "Auth context should be available from test setup")
	require.NotNil(t, authCtx.ProjectID, "Project ID should be available from test setup")

	tests := []struct {
		name             string
		mcpSlug          string
		setupToolset     func(t *testing.T, ctx context.Context) (toolsetOrgID string)
		setupAuth        func(t *testing.T, toolsetOrgID string) context.Context
		expectedError    bool
		expectedNotFound bool
		shouldContain    string
	}{
		{
			name:          "public toolset renders page without authentication",
			mcpSlug:       "public-test-toolset",
			shouldContain: "",
			setupToolset: func(t *testing.T, ctx context.Context) (toolsetOrgID string) {
				t.Helper()
				// Create a public toolset using the same organization from auth context
				toolset, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
					OrganizationID:         authCtx.ActiveOrganizationID,
					ProjectID:              *authCtx.ProjectID,
					Name:                   "Public Test MCP Server",
					Slug:                   "public-test-toolset",
					McpSlug:                conv.ToPGText("public-test-toolset"),
					Description:            conv.ToPGText("A public test MCP server"),
					DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
					McpEnabled:             true,
				})
				require.NoError(t, err)

				// Update to make it public (since CreateToolset doesn't have McpIsPublic field)
				err = toolsets_repo.New(testInstance.conn).SetToolsetMCPPublicByID(ctx, toolsets_repo.SetToolsetMCPPublicByIDParams{
					McpIsPublic: true,
					ID:          toolset.ID,
					ProjectID:   toolset.ProjectID,
				})
				require.NoError(t, err)

				return authCtx.ActiveOrganizationID
			},
			setupAuth: func(t *testing.T, toolsetOrgID string) context.Context {
				t.Helper()
				// Return context with no auth - public toolsets should work
				return context.Background()
			},
			expectedError: false,
		},
		{
			name:    "private toolset redirects to login without authentication",
			mcpSlug: "private-test-toolset",
			setupToolset: func(t *testing.T, ctx context.Context) (toolsetOrgID string) {
				t.Helper()
				// Create a private toolset using the same organization from auth context
				_, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
					OrganizationID:         authCtx.ActiveOrganizationID,
					ProjectID:              *authCtx.ProjectID,
					Name:                   "Private Test MCP Server",
					Slug:                   "private-test-toolset",
					McpSlug:                conv.ToPGText("private-test-toolset"),
					Description:            conv.ToPGText("A private test MCP server"),
					DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
					McpEnabled:             true,
				})
				require.NoError(t, err)
				// Toolset is private by default (mcp_is_public = false)

				return authCtx.ActiveOrganizationID
			},
			setupAuth: func(t *testing.T, toolsetOrgID string) context.Context {
				t.Helper()
				// Return context with no auth - should redirect to login
				return context.Background()
			},
			expectedError: false, // No error - it redirects instead
			shouldContain: "",
		},
		{
			name:          "private toolset renders page with correct organization authentication",
			mcpSlug:       "private-org-toolset",
			shouldContain: "",
			setupToolset: func(t *testing.T, ctx context.Context) (toolsetOrgID string) {
				t.Helper()
				// Create a private toolset using the same organization from auth context
				_, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
					OrganizationID:         authCtx.ActiveOrganizationID,
					ProjectID:              *authCtx.ProjectID,
					Name:                   "Private Org Test MCP Server",
					Slug:                   "private-org-toolset",
					McpSlug:                conv.ToPGText("private-org-toolset"),
					Description:            conv.ToPGText("A private org test MCP server"),
					DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
					McpEnabled:             true,
				})
				require.NoError(t, err)
				// Toolset is private by default (mcp_is_public = false)

				return authCtx.ActiveOrganizationID
			},
			setupAuth: func(t *testing.T, toolsetOrgID string) context.Context {
				t.Helper()
				// Set up authentication with the SAME organization as the toolset
				correctAuthCtx := &contextvalues.AuthContext{
					ActiveOrganizationID: toolsetOrgID,
					UserID:               "test-user-123",
					SessionID:            new("test-session-123"),
					ProjectID:            nil,
					OrganizationSlug:     "",
					Email:                nil,
					AccountType:          "",
					ProjectSlug:          nil,
					APIKeyScopes:         nil,
				}
				return contextvalues.SetAuthContext(context.Background(), correctAuthCtx)
			},
			expectedError: false,
		},
		{
			name:    "private toolset returns 404 with wrong organization authentication",
			mcpSlug: "wrong-org-toolset",
			setupToolset: func(t *testing.T, ctx context.Context) (toolsetOrgID string) {
				t.Helper()
				// Create a private toolset using the same organization from auth context
				_, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
					OrganizationID:         authCtx.ActiveOrganizationID,
					ProjectID:              *authCtx.ProjectID,
					Name:                   "Wrong Org Test MCP Server",
					Slug:                   "wrong-org-toolset",
					McpSlug:                conv.ToPGText("wrong-org-toolset"),
					Description:            conv.ToPGText("A wrong org test MCP server"),
					DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
					McpEnabled:             true,
				})
				require.NoError(t, err)
				// Toolset is private by default (mcp_is_public = false)

				return authCtx.ActiveOrganizationID
			},
			setupAuth: func(t *testing.T, toolsetOrgID string) context.Context {
				t.Helper()
				// Set up authentication with a DIFFERENT organization
				wrongAuthCtx := &contextvalues.AuthContext{
					ActiveOrganizationID: "different-org-id",
					UserID:               "test-user-456",
					SessionID:            new("test-session-456"),
					ProjectID:            nil,
					OrganizationSlug:     "",
					Email:                nil,
					AccountType:          "",
					ProjectSlug:          nil,
					APIKeyScopes:         nil,
				}
				return contextvalues.SetAuthContext(context.Background(), wrongAuthCtx)
			},
			expectedNotFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Setup toolset data
			toolsetOrgID := tt.setupToolset(t, ctx)

			// Create request
			req := httptest.NewRequest("GET", "/mcp/"+tt.mcpSlug+"/install", nil)

			// Add URL param for mcpSlug
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("mcpSlug", tt.mcpSlug)

			// Setup authentication context
			authCtx := tt.setupAuth(t, toolsetOrgID)
			req = req.WithContext(context.WithValue(authCtx, chi.RouteCtxKey, rctx))

			// Create response recorder
			rr := httptest.NewRecorder()

			// Call the handler
			err := testInstance.service.ServeInstallPage(rr, req)

			if tt.expectedError {
				require.Error(t, err, "Expected error for test case: %s", tt.name)
				if tt.shouldContain != "" {
					assert.Contains(t, err.Error(), tt.shouldContain, "Error message should contain expected text")
				}
			} else if tt.expectedNotFound {
				require.NoError(t, err, "serveNotFoundPage returns nil after writing HTML")
				assert.Equal(t, http.StatusNotFound, rr.Code, "Expected 404 status code")
				assert.Contains(t, rr.Body.String(), "Server Not Found", "Expected not found page content")
			} else {
				// For successful cases, check if we got a redirect or successful rendering
				if tt.name == "private toolset redirects to login without authentication" {
					// This specific test should redirect
					require.NoError(t, err, "Should not error on redirect")
					assert.Equal(t, http.StatusFound, rr.Code, "Should redirect with 302")
					location := rr.Header().Get("Location")
					assert.Contains(t, location, "/login", "Should redirect to login page")
				} else {
					// Other successful cases might get errors due to incomplete test data setup
					// but the important thing is we didn't get an auth error
					if err != nil {
						// Check that it's NOT an auth error
						assert.NotContains(t, err.Error(), "mcp server not found",
							"Should not get auth error for valid access: %v", err)
						t.Logf("Non-auth error (may be due to incomplete test setup): %v", err)
					} else {
						// If no error, verify we got a successful response
						assert.Equal(t, http.StatusOK, rr.Code, "Expected successful response")
						assert.Equal(t, "text/html", rr.Header().Get("Content-Type"), "Expected HTML content type")
						assert.NotEmpty(t, rr.Body.Bytes(), "Expected non-empty response body")
					}
				}
			}
		})
	}
}

func TestServeInstallPage_Instructions(t *testing.T) {
	t.Parallel()
	ctx, testInstance := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "Auth context should be available from test setup")
	require.NotNil(t, authCtx.ProjectID, "Project ID should be available from test setup")

	// Create a public toolset
	toolset, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Test MCP Server with Instructions",
		Slug:                   "test-instructions-toolset",
		McpSlug:                conv.ToPGText("test-instructions-toolset"),
		Description:            conv.ToPGText("A test MCP server with instructions"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	// Make it public
	err = toolsets_repo.New(testInstance.conn).SetToolsetMCPPublicByID(ctx, toolsets_repo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	})
	require.NoError(t, err)

	// Set metadata with instructions
	instructions := "Test Hub - Search and analyze test data\n\n## Key Capabilities\n\n- Search test records\n- Filter by status\n\n## Usage Patterns\n\nUse search before filtering"
	_, err = testInstance.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:      types.Slug(toolset.Slug),
		Instructions:     &instructions,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest("GET", "/mcp/test-instructions-toolset/install", nil)

	// Add URL param for mcpSlug
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", "test-instructions-toolset")
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	// Create response recorder
	rr := httptest.NewRecorder()

	// Call the handler
	err = testInstance.service.ServeInstallPage(rr, req)
	require.NoError(t, err)

	// Verify response
	assert.Equal(t, http.StatusOK, rr.Code, "Expected successful response")
	assert.Equal(t, "text/html", rr.Header().Get("Content-Type"), "Expected HTML content type")

	// Verify instructions are in the HTML
	body := rr.Body.String()
	assert.Contains(t, body, "Server Instructions", "Should contain instructions section header")
	assert.Contains(t, body, "Test Hub - Search and analyze test data", "Should contain instructions content")
}

func TestServeInstallPage_ToolDetails(t *testing.T) {
	t.Parallel()
	ctx, testInstance := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "Auth context should be available from test setup")
	require.NotNil(t, authCtx.ProjectID, "Project ID should be available from test setup")

	// Create a public toolset
	toolset, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Tool Details Test Server",
		Slug:                   "tool-details-test",
		McpSlug:                conv.ToPGText("tool-details-test"),
		Description:            conv.ToPGText("A test MCP server with tools"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	// Make it public
	err = toolsets_repo.New(testInstance.conn).SetToolsetMCPPublicByID(ctx, toolsets_repo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	})
	require.NoError(t, err)

	deploymentID, err := deployments_repo.New(testInstance.conn).InsertDeployment(ctx, deployments_repo.InsertDeploymentParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         "test-user",
		IdempotencyKey: uuid.New().String(),
	})
	require.NoError(t, err)

	err = deployments_repo.New(testInstance.conn).CreateDeploymentStatus(ctx, deployments_repo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID,
		Status:       "completed",
	})
	require.NoError(t, err)

	toolURN := urn.NewTool(urn.ToolKindHTTP, "test-api", uuid.New().String()[:8])
	err = tools_repo.New(testInstance.conn).CreateHTTPToolDefinition(ctx, tools_repo.CreateHTTPToolDefinitionParams{
		ProjectID:       *authCtx.ProjectID,
		DeploymentID:    deploymentID,
		ToolUrn:         toolURN,
		Name:            "search_records",
		UntruncatedName: pgtype.Text{},
		Summary:         "Search records",
		Description:     "Search and filter records by various criteria",
		Tags:            []string{},
		HttpMethod:      "GET",
		Path:            "/api/records",
		SchemaVersion:   "3.0.0",
		Schema:          []byte(`{}`),
		ServerEnvVar:    "TEST_SERVER_URL",
		Security:        []byte(`[]`),
		HeaderSettings:  []byte(`{}`),
		QuerySettings:   []byte(`{}`),
		PathSettings:    []byte(`{}`),
		ReadOnlyHint:    pgtype.Bool{Bool: true, Valid: true},
		IdempotentHint:  pgtype.Bool{Bool: true, Valid: true},
		DestructiveHint: pgtype.Bool{},
		OpenWorldHint:   pgtype.Bool{},
	})
	require.NoError(t, err)

	_, err = toolsets_repo.New(testInstance.conn).CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID:     toolset.ID,
		Version:       1,
		ToolUrns:      []urn.Tool{toolURN},
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest("GET", "/mcp/tool-details-test/install", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", "tool-details-test")
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	// Create response recorder
	rr := httptest.NewRecorder()

	// Call the handler
	err = testInstance.service.ServeInstallPage(rr, req)
	require.NoError(t, err)

	// Verify response basics
	assert.Equal(t, http.StatusOK, rr.Code, "Expected successful response")
	assert.Equal(t, "text/html", rr.Header().Get("Content-Type"), "Expected HTML content type")

	body := rr.Body.String()

	// Verify new table markup is present
	assert.Contains(t, body, `class="tools-table"`, "Should contain tools-table class")
	assert.Contains(t, body, `class="tool-name"`, "Should contain tool-name class")
	assert.Contains(t, body, "search_records", "Should contain the tool name")
	assert.Contains(t, body, "Search and filter records by various criteria", "Should contain the tool description")

	// Verify annotation badges are rendered
	assert.Contains(t, body, `class="annotation-badges"`, "Should contain annotation-badges container")
	assert.Contains(t, body, "Read-only", "Should contain Read-only annotation badge")
	assert.Contains(t, body, "Idempotent", "Should contain Idempotent annotation badge")

	// Verify the old badge markup is gone (no tool-names class)
	assert.NotContains(t, body, `class="tool-names"`, "Should not contain old tool-names class")
}

// TestServeInstallPage_CustomDomain_WrongDomainReturnsNotFound verifies that a
// toolset linked to one custom domain cannot be resolved when the request
// arrives through a different organization's custom domain. This guards against
// cross-domain toolset leakage in the install page handler.
func TestServeInstallPage_CustomDomain_WrongDomainReturnsNotFound(t *testing.T) {
	t.Parallel()
	ctx, testInstance := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	domainsRepo := customdomains_repo.New(testInstance.conn)

	// Create a custom domain for this organization and link a toolset to it.
	domain, err := domainsRepo.CreateCustomDomain(ctx, customdomains_repo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "correct-install.example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	domain, err = domainsRepo.UpdateCustomDomain(ctx, customdomains_repo.UpdateCustomDomainParams{
		ID:             domain.ID,
		Verified:       true,
		Activated:      true,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	mcpSlug := "cd-install-" + uuid.New().String()[:8]
	toolset, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Custom Domain Install Test",
		Slug:                   mcpSlug,
		McpSlug:                conv.ToPGText(mcpSlug),
		Description:            conv.ToPGText("toolset linked to a custom domain"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	// Make it public so auth isn't the reason for rejection.
	err = toolsets_repo.New(testInstance.conn).SetToolsetMCPPublicByID(ctx, toolsets_repo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	})
	require.NoError(t, err)

	// Link the toolset to the custom domain.
	_, err = testInstance.toolsetRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolset.McpEnabled,
		CustomDomainID:         uuid.NullUUID{UUID: domain.ID, Valid: true},
		ToolSelectionMode:      toolset.ToolSelectionMode,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	// Create a different domain belonging to another organization.
	otherDomain, err := domainsRepo.CreateCustomDomain(ctx, customdomains_repo.CreateCustomDomainParams{
		OrganizationID: "other-org",
		Domain:         "wrong-install.example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	otherDomain, err = domainsRepo.UpdateCustomDomain(ctx, customdomains_repo.UpdateCustomDomainParams{
		ID:             otherDomain.ID,
		Verified:       true,
		Activated:      true,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	// Request the install page through the wrong custom domain context.
	wrongCtx := customdomains.WithContext(context.Background(), &customdomains.Context{
		OrganizationID: "other-org",
		Domain:         otherDomain.Domain,
		DomainID:       otherDomain.ID,
	})

	req := httptest.NewRequest("GET", "/mcp/"+mcpSlug+"/install", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(wrongCtx, chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	err = testInstance.service.ServeInstallPage(rr, req)

	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "Server Not Found")
}

// TestServeInstallPage_CustomDomain_CorrectDomainRendersPage verifies that a
// toolset linked to a custom domain is successfully resolved and rendered when
// the request arrives through that same domain.
func TestServeInstallPage_CustomDomain_CorrectDomainRendersPage(t *testing.T) {
	t.Parallel()
	ctx, testInstance := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	domainsRepo := customdomains_repo.New(testInstance.conn)

	// Create and activate a custom domain for this organization.
	domain, err := domainsRepo.CreateCustomDomain(ctx, customdomains_repo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "correct-render.example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	domain, err = domainsRepo.UpdateCustomDomain(ctx, customdomains_repo.UpdateCustomDomainParams{
		ID:             domain.ID,
		Verified:       true,
		Activated:      true,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	mcpSlug := "cd-correct-" + uuid.New().String()[:8]
	toolset, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Correct Domain Install Test",
		Slug:                   mcpSlug,
		McpSlug:                conv.ToPGText(mcpSlug),
		Description:            conv.ToPGText("toolset linked to a custom domain"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	// Make it public and link it to the custom domain.
	err = toolsets_repo.New(testInstance.conn).SetToolsetMCPPublicByID(ctx, toolsets_repo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	})
	require.NoError(t, err)

	_, err = testInstance.toolsetRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolset.McpEnabled,
		CustomDomainID:         uuid.NullUUID{UUID: domain.ID, Valid: true},
		ToolSelectionMode:      toolset.ToolSelectionMode,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	// Request through the correct custom domain context.
	correctCtx := customdomains.WithContext(context.Background(), &customdomains.Context{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         domain.Domain,
		DomainID:       domain.ID,
	})

	req := httptest.NewRequest("GET", "/mcp/"+mcpSlug+"/install", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(correctCtx, chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	err = testInstance.service.ServeInstallPage(rr, req)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "text/html", rr.Header().Get("Content-Type"))
}

// TestServeInstallPage_CustomDomain_PlatformDomainStillWorks verifies that a
// toolset linked to a custom domain can still be accessed via the platform
// domain (i.e. when no custom domain context is present in the request).
func TestServeInstallPage_CustomDomain_PlatformDomainStillWorks(t *testing.T) {
	t.Parallel()
	ctx, testInstance := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	domainsRepo := customdomains_repo.New(testInstance.conn)

	// Create and activate a custom domain for this organization.
	domain, err := domainsRepo.CreateCustomDomain(ctx, customdomains_repo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "platform-fallback.example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	domain, err = domainsRepo.UpdateCustomDomain(ctx, customdomains_repo.UpdateCustomDomainParams{
		ID:             domain.ID,
		Verified:       true,
		Activated:      true,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	mcpSlug := "cd-platform-" + uuid.New().String()[:8]
	toolset, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Platform Domain Install Test",
		Slug:                   mcpSlug,
		McpSlug:                conv.ToPGText(mcpSlug),
		Description:            conv.ToPGText("toolset linked to a custom domain"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	// Make it public and link it to the custom domain.
	err = toolsets_repo.New(testInstance.conn).SetToolsetMCPPublicByID(ctx, toolsets_repo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	})
	require.NoError(t, err)

	_, err = testInstance.toolsetRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolset.McpEnabled,
		CustomDomainID:         uuid.NullUUID{UUID: domain.ID, Valid: true},
		ToolSelectionMode:      toolset.ToolSelectionMode,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	// Request via the platform domain — no custom domain in context.
	req := httptest.NewRequest("GET", "/mcp/"+mcpSlug+"/install", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	err = testInstance.service.ServeInstallPage(rr, req)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "text/html", rr.Header().Get("Content-Type"))
}

// TestServeInstallPage_CustomDomain_DeletedToolsetReturnsNotFound verifies that
// when two toolsets from different organizations share the same MCP slug on
// distinct custom domains and one is soft-deleted, requesting the install page
// through the deleted toolset's domain returns not-found rather than leaking
// the other organization's active toolset.
func TestServeInstallPage_CustomDomain_DeletedToolsetReturnsNotFound(t *testing.T) {
	t.Parallel()
	ctx, testInstance := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	domainsRepo := customdomains_repo.New(testInstance.conn)

	// --- Org A: the deleted toolset's org (reuse the test-provided org) ---

	domainA, err := domainsRepo.CreateCustomDomain(ctx, customdomains_repo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "deleted-org-a.example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	domainA, err = domainsRepo.UpdateCustomDomain(ctx, customdomains_repo.UpdateCustomDomainParams{
		ID:             domainA.ID,
		Verified:       true,
		Activated:      true,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	sharedMCPSlug := "shared-slug-" + uuid.New().String()[:8]

	toolsetA, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Org A Toolset",
		Slug:                   "org-a-" + sharedMCPSlug,
		McpSlug:                conv.ToPGText(sharedMCPSlug),
		Description:            conv.ToPGText("toolset on org A, will be deleted"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	err = toolsets_repo.New(testInstance.conn).SetToolsetMCPPublicByID(ctx, toolsets_repo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolsetA.ID,
		ProjectID:   toolsetA.ProjectID,
	})
	require.NoError(t, err)

	_, err = testInstance.toolsetRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolsetA.Name,
		Description:            toolsetA.Description,
		DefaultEnvironmentSlug: toolsetA.DefaultEnvironmentSlug,
		McpSlug:                toolsetA.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolsetA.McpEnabled,
		CustomDomainID:         uuid.NullUUID{UUID: domainA.ID, Valid: true},
		ToolSelectionMode:      toolsetA.ToolSelectionMode,
		Slug:                   toolsetA.Slug,
		ProjectID:              toolsetA.ProjectID,
	})
	require.NoError(t, err)

	// --- Org B: the active toolset's org ---

	orgBID := "org-b-" + uuid.New().String()[:8]
	err = organizations_repo.New(testInstance.conn).CreateOrganizationMetadata(ctx, organizations_repo.CreateOrganizationMetadataParams{
		ID:   orgBID,
		Name: "Org B",
		Slug: "org-b-" + uuid.New().String()[:8],
	})
	require.NoError(t, err)

	projectB, err := projects_repo.New(testInstance.conn).CreateProject(ctx, projects_repo.CreateProjectParams{
		Name:           "Org B Project",
		Slug:           "org-b-proj-" + uuid.New().String()[:8],
		OrganizationID: orgBID,
	})
	require.NoError(t, err)
	projectBID := projectB.ID

	domainB, err := domainsRepo.CreateCustomDomain(ctx, customdomains_repo.CreateCustomDomainParams{
		OrganizationID: orgBID,
		Domain:         "active-org-b.example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	domainB, err = domainsRepo.UpdateCustomDomain(ctx, customdomains_repo.UpdateCustomDomainParams{
		ID:             domainB.ID,
		Verified:       true,
		Activated:      true,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	toolsetB, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         orgBID,
		ProjectID:              projectBID,
		Name:                   "Org B Toolset",
		Slug:                   "org-b-" + sharedMCPSlug,
		McpSlug:                conv.ToPGText(sharedMCPSlug),
		Description:            conv.ToPGText("toolset on org B, stays active"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	err = toolsets_repo.New(testInstance.conn).SetToolsetMCPPublicByID(ctx, toolsets_repo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolsetB.ID,
		ProjectID:   toolsetB.ProjectID,
	})
	require.NoError(t, err)

	_, err = testInstance.toolsetRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolsetB.Name,
		Description:            toolsetB.Description,
		DefaultEnvironmentSlug: toolsetB.DefaultEnvironmentSlug,
		McpSlug:                toolsetB.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolsetB.McpEnabled,
		CustomDomainID:         uuid.NullUUID{UUID: domainB.ID, Valid: true},
		ToolSelectionMode:      toolsetB.ToolSelectionMode,
		Slug:                   toolsetB.Slug,
		ProjectID:              toolsetB.ProjectID,
	})
	require.NoError(t, err)

	// Soft-delete toolset A.
	_, err = testInstance.toolsetRepo.DeleteToolset(ctx, toolsets_repo.DeleteToolsetParams{
		Slug:      toolsetA.Slug,
		ProjectID: toolsetA.ProjectID,
	})
	require.NoError(t, err)

	// Request the install page through org A's custom domain — should 404
	// because toolset A is deleted, and the active toolset B on a different
	// domain must not leak through.
	domainACtx := customdomains.WithContext(context.Background(), &customdomains.Context{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         domainA.Domain,
		DomainID:       domainA.ID,
	})

	req := httptest.NewRequest("GET", "/mcp/"+sharedMCPSlug+"/install", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", sharedMCPSlug)
	req = req.WithContext(context.WithValue(domainACtx, chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	err = testInstance.service.ServeInstallPage(rr, req)

	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "Server Not Found")
}

// TestServeInstallPage_ClaudeDesktop_NoSecurityInputs verifies that a public
// MCP server with no required HTTP headers renders the simple "Add custom
// connector" Claude Desktop install flow, including the Teams & Enterprise
// admin connector footer.
func TestServeInstallPage_ClaudeDesktop_NoSecurityInputs(t *testing.T) {
	t.Parallel()
	ctx, testInstance := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mcpSlug := "claude-desktop-public-" + uuid.New().String()[:8]
	toolset, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Public Claude Desktop Toolset",
		Slug:                   mcpSlug,
		McpSlug:                conv.ToPGText(mcpSlug),
		Description:            conv.ToPGText("public toolset with no security inputs"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	err = toolsets_repo.New(testInstance.conn).SetToolsetMCPPublicByID(ctx, toolsets_repo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	})
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/mcp/"+mcpSlug+"/install", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	err = testInstance.service.ServeInstallPage(rr, req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rr.Code)

	body := rr.Body.String()
	assert.Contains(t, body, `"Add custom connector"`, "should render the simple Add custom connector flow")
	assert.Contains(t, body, "For Teams &amp; Enterprise", "should render the Teams & Enterprise admin connector footer")
	assert.NotContains(t, body, "Settings &gt; Developer &gt; Local MCP Servers", "should not render the claude_desktop_config.json workaround flow")
	assert.NotContains(t, body, "Claude Desktop does not yet support custom HTTP headers", "should not render the workaround explanation")
}

// TestServeInstallPage_ClaudeDesktop_WithSecurityInputs verifies that an MCP
// server requiring HTTP-header credentials renders the claude_desktop_config.json
// workaround flow (because Claude Desktop's custom connector UI does not yet
// support custom HTTP headers) and hides the simple Add custom connector flow
// and the Teams & Enterprise footer (which has the same UI limitation).
func TestServeInstallPage_ClaudeDesktop_WithSecurityInputs(t *testing.T) {
	t.Parallel()
	ctx, testInstance := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mcpSlug := "claude-desktop-private-" + uuid.New().String()[:8]
	_, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Private Claude Desktop Toolset",
		Slug:                   mcpSlug,
		McpSlug:                conv.ToPGText(mcpSlug),
		Description:            conv.ToPGText("private toolset producing security inputs via gram security mode"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/mcp/"+mcpSlug+"/install", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	err = testInstance.service.ServeInstallPage(rr, req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rr.Code)

	body := rr.Body.String()
	assert.Contains(t, body, "Settings &gt; Developer &gt; Local MCP Servers", "should render the claude_desktop_config.json edit instructions")
	assert.Contains(t, body, `"mcpServers"`, "should render the claude_desktop_config.json snippet")
	assert.Contains(t, body, `"mcp-remote@0.1.25"`, "should render the mcp-remote command in the snippet")
	assert.Contains(t, body, `"--header"`, "should render the --header argument in the snippet")
	assert.Contains(t, body, "does not yet support custom HTTP headers", "should explain why the workaround is needed")
	assert.NotContains(t, body, `"Add custom connector"`, "should not render the simple Add custom connector flow")
	assert.NotContains(t, body, "For Teams &amp; Enterprise", "should not render the Teams & Enterprise admin connector footer")
}

// TestServeInstallPage_PrivateWithGramOAuth_NoAuthorizationHeader regression-tests
// AGE-1962: a private MCP server with a Gram OAuth proxy attached must not render
// the GRAM_KEY Authorization header (or gram-environment) in the install snippets.
// OAuth handles identity auth at the HTTP layer, so the install command must not
// instruct users to set those headers manually.
func TestServeInstallPage_PrivateWithGramOAuth_NoAuthorizationHeader(t *testing.T) {
	t.Parallel()
	ctx, testInstance := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateProxyToolset(t, ctx, testInstance.conn, authCtx, oauthtest.ProxyToolsetOpts{
		Slug:         "private-gram-oauth",
		IsPublic:     false,
		ProviderType: "",
	})
	mcpSlug := result.Toolset.McpSlug.String

	req := httptest.NewRequest("GET", "/mcp/"+mcpSlug+"/install", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	err := testInstance.service.ServeInstallPage(rr, req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rr.Code)

	body := rr.Body.String()
	assert.NotContains(t, body, "Authorization", "OAuth-protected install command must not reference an Authorization header")
	assert.NotContains(t, body, "gram-key", "OAuth-protected install command must not reference the gram-key input")
	assert.NotContains(t, body, "gram-environment", "OAuth-protected install command must not reference the gram-environment input")
	assert.NotContains(t, body, "GRAM_KEY", "OAuth-protected install command must not reference the GRAM_KEY env var")
}

// TestServeInstallPage_NoDomain_AuthedUserWithOrgDomain verifies that a toolset
// WITHOUT a custom domain can still be loaded via the platform domain when the
// logged-in user's organization happens to have a custom domain configured. This
// guards against a regression where resolveDomainIDFromContext returning the
// org's domain from auth context would prevent the slug-only fallback.
func TestServeInstallPage_NoDomain_AuthedUserWithOrgDomain(t *testing.T) {
	t.Parallel()
	ctx, testInstance := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.SessionID)

	domainsRepo := customdomains_repo.New(testInstance.conn)

	// Create and activate a custom domain for this organization so that
	// resolveDomainIDFromContext returns non-nil via the auth context path.
	domain, err := domainsRepo.CreateCustomDomain(ctx, customdomains_repo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "org-has-domain.example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	_, err = domainsRepo.UpdateCustomDomain(ctx, customdomains_repo.UpdateCustomDomainParams{
		ID:             domain.ID,
		Verified:       true,
		Activated:      true,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	// Create a toolset WITHOUT linking it to any custom domain.
	mcpSlug := "no-domain-" + uuid.New().String()[:8]
	_, err = testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "No Domain Toolset",
		Slug:                   mcpSlug,
		McpSlug:                conv.ToPGText(mcpSlug),
		Description:            conv.ToPGText("toolset with no custom domain"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	// Make the toolset public.
	err = toolsets_repo.New(testInstance.conn).SetToolsetMCPPublicBySlug(ctx, toolsets_repo.SetToolsetMCPPublicBySlugParams{
		McpIsPublic: true,
		McpSlug:     pgtype.Text{String: mcpSlug, Valid: true},
	})
	require.NoError(t, err)

	// Build a request through the platform domain (no custom domain context)
	// but with a valid session token so that auth context is populated.
	reqCtx := contextvalues.SetSessionTokenInContext(context.Background(), *authCtx.SessionID)

	req := httptest.NewRequest("GET", "/mcp/"+mcpSlug+"/install", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(reqCtx, chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	err = testInstance.service.ServeInstallPage(rr, req)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "text/html", rr.Header().Get("Content-Type"))
}

func TestServeInstallPage_ExternalMCP_FiltersNonUserProvidedHeaders(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	mcpSlug := "external-mcp-filter-" + uuid.New().String()[:8]
	toolset, err := ti.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         orgID,
		ProjectID:              projectID,
		Name:                   "External MCP Filter Test",
		Slug:                   mcpSlug,
		McpSlug:                conv.ToPGText(mcpSlug),
		Description:            conv.ToPGText("public toolset proxying an external MCP server with header configs"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	err = toolsets_repo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsets_repo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	})
	require.NoError(t, err)

	deploymentID, err := deployments_repo.New(ti.conn).InsertDeployment(ctx, deployments_repo.InsertDeploymentParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		UserID:         "test-user",
		IdempotencyKey: uuid.New().String(),
	})
	require.NoError(t, err)

	err = deployments_repo.New(ti.conn).CreateDeploymentStatus(ctx, deployments_repo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID,
		Status:       "completed",
	})
	require.NoError(t, err)

	externalmcpRepo := externalmcp_repo.New(ti.conn)
	registryID, err := externalmcpRepo.CreateMCPRegistry(ctx, externalmcp_repo.CreateMCPRegistryParams{
		Name: "test-registry-" + mcpSlug,
		Url:  "https://mcp.example.com/glyphic",
	})
	require.NoError(t, err)
	attachmentSlug := "glyphic"
	attachment, err := externalmcpRepo.CreateExternalMCPAttachment(ctx, externalmcp_repo.CreateExternalMCPAttachmentParams{
		DeploymentID:            deploymentID,
		RegistryID:              uuid.NullUUID{UUID: registryID, Valid: true},
		Name:                    "Glyphic MCP Server",
		Slug:                    attachmentSlug,
		RegistryServerSpecifier: "test-server",
	})
	require.NoError(t, err)

	// header_definitions JSON shape matches the unexported externalMCPHeaderDefinition
	// struct in server/internal/mv/toolset.go. extractExternalMCPHeaderDefinitions
	// produces variable names by snake-casing "<attachmentSlug>_<headerName>", so the
	// resulting names here are GLYPHIC_X_API_KEY, GLYPHIC_AUTHORIZATION, GLYPHIC_TRACE_ID.
	headerDefsJSON := []byte(`[
		{"name":"X-Api-Key","isRequired":true,"isSecret":true},
		{"name":"Authorization","isRequired":true,"isSecret":true},
		{"name":"Trace-Id","isRequired":true,"isSecret":false}
	]`)

	toolURNString := "tools:externalmcp:" + attachmentSlug + ":proxy"
	_, err = externalmcpRepo.CreateExternalMCPToolDefinition(ctx, externalmcp_repo.CreateExternalMCPToolDefinitionParams{
		ExternalMcpAttachmentID:    attachment.ID,
		ToolUrn:                    toolURNString,
		Type:                       "proxy",
		RemoteUrl:                  "https://mcp.example.com/glyphic",
		TransportType:              externalmcp_types.TransportTypeStreamableHTTP,
		RequiresOauth:              false,
		OauthVersion:               "none",
		OauthAuthorizationEndpoint: pgtype.Text{},
		OauthTokenEndpoint:         pgtype.Text{},
		OauthRegistrationEndpoint:  pgtype.Text{},
		OauthScopesSupported:       []string{},
		HeaderDefinitions:          headerDefsJSON,
	})
	require.NoError(t, err)

	toolURN, err := urn.ParseTool(toolURNString)
	require.NoError(t, err)
	_, err = toolsets_repo.New(ti.conn).CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID:     toolset.ID,
		Version:       1,
		ToolUrns:      []urn.Tool{toolURN},
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{Valid: false},
	})
	require.NoError(t, err)

	mcpRepo := mcpmetadata_repo.New(ti.conn)
	metadata, err := mcpRepo.UpsertMetadata(ctx, mcpmetadata_repo.UpsertMetadataParams{
		ToolsetID: toolset.ID,
		ProjectID: projectID,
	})
	require.NoError(t, err)

	// Mark X-Api-Key as system-provided and Authorization as omitted; leave Trace-Id
	// without an env config (defaulting to user-provided) as the positive-case anchor.
	_, err = mcpRepo.UpsertEnvironmentConfig(ctx, mcpmetadata_repo.UpsertEnvironmentConfigParams{
		ProjectID:     projectID,
		McpMetadataID: metadata.ID,
		VariableName:  "GLYPHIC_X_API_KEY",
		ProvidedBy:    "system",
	})
	require.NoError(t, err)
	_, err = mcpRepo.UpsertEnvironmentConfig(ctx, mcpmetadata_repo.UpsertEnvironmentConfigParams{
		ProjectID:     projectID,
		McpMetadataID: metadata.ID,
		VariableName:  "GLYPHIC_AUTHORIZATION",
		ProvidedBy:    "none",
	})
	require.NoError(t, err)

	installReq := httptest.NewRequest("GET", "/mcp/"+mcpSlug+"/install", nil)
	installRctx := chi.NewRouteContext()
	installRctx.URLParams.Add("mcpSlug", mcpSlug)
	installReq = installReq.WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, installRctx))

	installRR := httptest.NewRecorder()
	err = ti.service.ServeInstallPage(installRR, installReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, installRR.Code)

	body := installRR.Body.String()
	assert.NotContains(t, body, "GLYPHIC_X_API_KEY",
		"system-provided external MCP header must not appear in the install snippet")
	assert.NotContains(t, body, "GLYPHIC_AUTHORIZATION",
		"omitted external MCP header must not appear in the install snippet")
	assert.Contains(t, body, "GLYPHIC_TRACE_ID",
		"user-provided external MCP header (no env config) must still appear in the install snippet")
}
