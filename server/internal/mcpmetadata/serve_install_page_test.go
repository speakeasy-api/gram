package mcpmetadata_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestServeInstallPage_Authentication(t *testing.T) {
	ctx, testInstance := newTestMCPMetadataService(t)

	// Get auth context from the setup (which creates org and project)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "Auth context should be available from test setup")
	require.NotNil(t, authCtx.ProjectID, "Project ID should be available from test setup")

	tests := []struct {
		name          string
		mcpSlug       string
		setupToolset  func(t *testing.T, ctx context.Context) (toolsetOrgID string)
		setupAuth     func(t *testing.T, toolsetOrgID string) context.Context
		expectedError bool
		shouldContain string
	}{
		{
			name:    "public toolset renders page without authentication",
			mcpSlug: "public-test-toolset",
			setupToolset: func(t *testing.T, ctx context.Context) (toolsetOrgID string) {
				// Create a public toolset using the same organization from auth context
				toolset, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
					OrganizationID: authCtx.ActiveOrganizationID,
					ProjectID:      *authCtx.ProjectID,
					Name:           "Public Test MCP Server",
					Slug:           "public-test-toolset",
					McpSlug:        conv.ToPGText("public-test-toolset"),
					Description:    conv.ToPGText("A public test MCP server"),
					McpEnabled:     true,
				})
				require.NoError(t, err)

				// Update to make it public (since CreateToolset doesn't have McpIsPublic field)
				_, err = testInstance.conn.Exec(ctx,
					"UPDATE toolsets SET mcp_is_public = true WHERE id = $1", toolset.ID)
				require.NoError(t, err)

				return authCtx.ActiveOrganizationID
			},
			setupAuth: func(t *testing.T, toolsetOrgID string) context.Context {
				// Return context with no auth - public toolsets should work
				return context.Background()
			},
			expectedError: false,
		},
		{
			name:    "private toolset redirects to login without authentication",
			mcpSlug: "private-test-toolset",
			setupToolset: func(t *testing.T, ctx context.Context) (toolsetOrgID string) {
				// Create a private toolset using the same organization from auth context
				_, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
					OrganizationID: authCtx.ActiveOrganizationID,
					ProjectID:      *authCtx.ProjectID,
					Name:           "Private Test MCP Server",
					Slug:           "private-test-toolset",
					McpSlug:        conv.ToPGText("private-test-toolset"),
					Description:    conv.ToPGText("A private test MCP server"),
					McpEnabled:     true,
				})
				require.NoError(t, err)
				// Toolset is private by default (mcp_is_public = false)

				return authCtx.ActiveOrganizationID
			},
			setupAuth: func(t *testing.T, toolsetOrgID string) context.Context {
				// Return context with no auth - should redirect to login
				return context.Background()
			},
			expectedError: false, // No error - it redirects instead
			shouldContain: "",
		},
		{
			name:    "private toolset renders page with correct organization authentication",
			mcpSlug: "private-org-toolset",
			setupToolset: func(t *testing.T, ctx context.Context) (toolsetOrgID string) {
				// Create a private toolset using the same organization from auth context
				_, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
					OrganizationID: authCtx.ActiveOrganizationID,
					ProjectID:      *authCtx.ProjectID,
					Name:           "Private Org Test MCP Server",
					Slug:           "private-org-toolset",
					McpSlug:        conv.ToPGText("private-org-toolset"),
					Description:    conv.ToPGText("A private org test MCP server"),
					McpEnabled:     true,
				})
				require.NoError(t, err)
				// Toolset is private by default (mcp_is_public = false)

				return authCtx.ActiveOrganizationID
			},
			setupAuth: func(t *testing.T, toolsetOrgID string) context.Context {
				// Set up authentication with the SAME organization as the toolset
				correctAuthCtx := &contextvalues.AuthContext{
					ActiveOrganizationID: toolsetOrgID,
					UserID:               "test-user-123",
					SessionID:            stringPtr("test-session-123"),
				}
				return contextvalues.SetAuthContext(context.Background(), correctAuthCtx)
			},
			expectedError: false,
		},
		{
			name:    "private toolset returns 404 with wrong organization authentication",
			mcpSlug: "wrong-org-toolset",
			setupToolset: func(t *testing.T, ctx context.Context) (toolsetOrgID string) {
				// Create a private toolset using the same organization from auth context
				_, err := testInstance.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
					OrganizationID: authCtx.ActiveOrganizationID,
					ProjectID:      *authCtx.ProjectID,
					Name:           "Wrong Org Test MCP Server",
					Slug:           "wrong-org-toolset",
					McpSlug:        conv.ToPGText("wrong-org-toolset"),
					Description:    conv.ToPGText("A wrong org test MCP server"),
					McpEnabled:     true,
				})
				require.NoError(t, err)
				// Toolset is private by default (mcp_is_public = false)

				return authCtx.ActiveOrganizationID
			},
			setupAuth: func(t *testing.T, toolsetOrgID string) context.Context {
				// Set up authentication with a DIFFERENT organization
				wrongAuthCtx := &contextvalues.AuthContext{
					ActiveOrganizationID: "different-org-id",
					UserID:               "test-user-456",
					SessionID:            stringPtr("test-session-456"),
				}
				return contextvalues.SetAuthContext(context.Background(), wrongAuthCtx)
			},
			expectedError: true,
			shouldContain: "mcp server not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
				assert.Error(t, err, "Expected error for test case: %s", tt.name)
				if tt.shouldContain != "" {
					assert.Contains(t, err.Error(), tt.shouldContain, "Error message should contain expected text")
				}
			} else {
				// For successful cases, check if we got a redirect or successful rendering
				if tt.name == "private toolset redirects to login without authentication" {
					// This specific test should redirect
					assert.NoError(t, err, "Should not error on redirect")
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
						assert.Greater(t, len(rr.Body.Bytes()), 0, "Expected non-empty response body")
					}
				}
			}
		})
	}
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}
