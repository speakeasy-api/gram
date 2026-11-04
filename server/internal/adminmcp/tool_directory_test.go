package adminmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestToolDirectoryMatchesRegisteredToolsAcrossPages(t *testing.T) {
	t.Parallel()
	operations := make(map[WriteOperation]bool)
	for _, op := range AllWriteOperations {
		if op.implemented() {
			operations[op] = true
		}
	}
	runtime := NewRuntime(nil, "https://admin.example.test/admin-mcp")
	runtime.catalogPromotions = &catalogpromotion.Service{}
	deps := WriteDependencies{Features: &productfeatures.Client{}, Registry: mcpregistry.New(nil, nil), ImageUploads: &fakeImageUploads{}}
	require.NoError(t, AttachWrites(runtime, &StaffOAuth{Approval: &StaffProposalApproval{}}, deps, WriteConfig{Enabled: true, Operations: operations}))
	verifier := &fakeAdminVerifier{result: &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: "staff@example.test"}}
	session := &StaffOAuthAuthorization{verifier: verifier}
	request := httptest.NewRequest(http.MethodGet, Path+"/tools", nil)
	request.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "browser-session"})
	response := httptest.NewRecorder()
	runtime.ToolDirectoryHandler(session, testenv.NewLogger(t)).ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.Equal(t, 1, verifier.calls)
	require.NotContains(t, response.Body.String(), "inputSchema")
	require.NotContains(t, response.Body.String(), "outputSchema")
	var directory toolDirectory
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &directory))
	expected := listAllAdminTools(t)
	require.Len(t, directory.Tools, len(expected))
	require.Greater(t, len(directory.Tools), 32, "exercise MCP pagination")
	byName := map[string]directoryTool{}
	previous := ""
	for _, tool := range directory.Tools {
		require.Greater(t, tool.Name, previous)
		previous = tool.Name
		byName[tool.Name] = tool
	}
	for _, raw := range expected {
		var tool directoryTool
		require.NoError(t, json.Unmarshal(raw, &tool))
		actual, ok := byName[tool.Name]
		require.True(t, ok, tool.Name)
		require.Equal(t, tool.Title, actual.Title)
		require.Equal(t, tool.Description, actual.Description)
	}
	require.Equal(t, directoryApproval, byName["prepare_registry_promotion_curation"].Access)
	require.Equal(t, directoryApproval, byName["execute_admin_proposal"].Access)
	require.Equal(t, directoryRead, byName["get_admin_proposal_status"].Access)
	require.Equal(t, directoryRead, byName["list_audit_log_entries"].Access)
	require.Contains(t, byName["list_audit_log_entries"].Description, "confidential")
	require.Equal(t, directoryWrite, byName["create_image_upload_url"].Access)
	require.Contains(t, byName["create_image_upload_url"].Description, "direct write without browser approval")
	require.Contains(t, byName["create_image_upload_url"].Description, "publicly served immediately and stays stored")
	require.Contains(t, byName["create_image_upload_url"].Description, "local clip paths")
}

func TestToolDirectoryReflectsDisabledWritesAndRegistryChanges(t *testing.T) {
	t.Parallel()
	runtime := NewRuntime(nil, "https://admin.example.test/admin-mcp")
	require.NoError(t, AttachWrites(runtime, &StaffOAuth{Approval: &StaffProposalApproval{}}, WriteDependencies{Features: &productfeatures.Client{}, Registry: mcpregistry.New(nil, nil)}, WriteConfig{}))
	directory, err := runtime.toolDirectory(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, directory.Tools)
	for _, tool := range directory.Tools {
		require.Equal(t, directoryRead, tool.Access, tool.Name)
	}
	removed := directory.Tools[0].Name
	runtime.server.RemoveTools(removed)
	updated, err := runtime.toolDirectory(t.Context())
	require.NoError(t, err)
	require.Len(t, updated.Tools, len(directory.Tools)-1)
	for _, tool := range updated.Tools {
		require.NotEqual(t, removed, tool.Name)
	}
}

func TestToolDirectoryRequiresLiveStaffSession(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		cookie string
		err    error
		staff  *contextvalues.AdminAuthContext
	}{
		{name: "missing cookie"},
		{name: "expired session", cookie: "expired", err: oops.C(oops.CodeUnauthorized)},
		{name: "nonstaff session", cookie: "browser-session"},
		{name: "mismatched session", cookie: "browser-session", staff: &contextvalues.AdminAuthContext{SessionID: "other", OIDCSubject: "staff-subject", Email: "staff@example.test"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runtime := NewRuntime(nil, "https://admin.example.test/admin-mcp")
			session := &StaffOAuthAuthorization{verifier: &fakeAdminVerifier{result: tc.staff, err: tc.err}}
			request := httptest.NewRequest(http.MethodGet, Path+"/tools", nil)
			if tc.cookie != "" {
				request.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: tc.cookie})
			}
			response := httptest.NewRecorder()
			runtime.ToolDirectoryHandler(session, testenv.NewLogger(t)).ServeHTTP(response, request)
			require.Equal(t, http.StatusUnauthorized, response.Code)
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			require.NotContains(t, response.Body.String(), "tools")
		})
	}
}

func TestToolDirectoryRejectsMutationRequests(t *testing.T) {
	t.Parallel()
	runtime := NewRuntime(nil, "https://admin.example.test/admin-mcp")
	request := httptest.NewRequest(http.MethodPost, Path+"/tools", nil)
	response := httptest.NewRecorder()
	runtime.ToolDirectoryHandler(nil, testenv.NewLogger(t)).ServeHTTP(response, request)
	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
	require.Equal(t, http.MethodGet, response.Header().Get("Allow"))
}

func TestToolDirectoryListsOnlyEnabledWriteOperations(t *testing.T) {
	t.Parallel()
	runtime := NewRuntime(nil, "https://admin.example.test/admin-mcp")
	deps := WriteDependencies{Features: &productfeatures.Client{}, Registry: mcpregistry.New(nil, nil)}
	writes := WriteConfig{Enabled: true, Operations: make(map[WriteOperation]bool)}
	writes.Operations[OperationSetOrganizationFeature] = true
	require.NoError(t, AttachWrites(runtime, &StaffOAuth{Approval: &StaffProposalApproval{}}, deps, writes))
	directory, err := runtime.toolDirectory(t.Context())
	require.NoError(t, err)
	mutatingNames := []string{}
	for _, tool := range directory.Tools {
		if tool.Access != directoryRead {
			mutatingNames = append(mutatingNames, tool.Name)
		}
	}
	require.Equal(t, []string{"execute_admin_proposal", "prepare_set_organization_feature"}, mutatingNames)
}

func TestToolDirectoryVerifierUnavailable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "provider outage", err: oops.E(oops.CodeUnexpected, errors.New("provider unavailable"), "verify staff")},
		{name: "unexpected verifier failure", err: errors.New("verifier unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runtime := NewRuntime(nil, "https://admin.example.test/admin-mcp")
			session := &StaffOAuthAuthorization{verifier: &fakeAdminVerifier{err: tc.err}}
			request := httptest.NewRequest(http.MethodGet, Path+"/tools", nil)
			request.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "browser-session"})
			response := httptest.NewRecorder()
			runtime.ToolDirectoryHandler(session, testenv.NewLogger(t)).ServeHTTP(response, request)
			require.Equal(t, http.StatusServiceUnavailable, response.Code)
			require.Equal(t, "staff verification unavailable\n", response.Body.String())
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			require.Empty(t, response.Header().Values("Set-Cookie"))
		})
	}
}

func TestToolDirectoryLogsDiscoveryFailure(t *testing.T) {
	t.Parallel()
	runtime := NewRuntime(nil, "https://admin.example.test/admin-mcp")
	runtime.server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/list" {
				return nil, errors.New("directory discovery failed")
			}
			return next(ctx, method, req)
		}
	})
	session := &StaffOAuthAuthorization{verifier: &fakeAdminVerifier{result: &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: "staff@example.test"}}}
	request := httptest.NewRequest(http.MethodGet, Path+"/tools", nil)
	request.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "browser-session"})
	response := httptest.NewRecorder()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	runtime.ToolDirectoryHandler(session, logger).ServeHTTP(response, request)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Equal(t, "tool directory unavailable\n", response.Body.String())
	require.Contains(t, logs.String(), "list admin MCP tool directory")
	require.Contains(t, logs.String(), "directory discovery failed")
	require.NotContains(t, logs.String(), "browser-session")
	require.NotContains(t, logs.String(), "staff-subject")
	require.NotContains(t, logs.String(), "staff@example.test")
}
