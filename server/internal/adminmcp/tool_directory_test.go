package adminmcp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

func TestToolDirectoryMatchesRegisteredToolsAcrossPages(t *testing.T) {
	t.Parallel()
	operations := map[WriteOperation]bool{}
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
	runtime.ToolDirectoryHandler(session).ServeHTTP(response, request)
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
	require.Equal(t, directoryWrite, byName["create_image_upload_url"].Access)
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
		{name: "expired session", cookie: "expired", err: errors.New("session expired")},
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
			runtime.ToolDirectoryHandler(session).ServeHTTP(response, request)
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
	runtime.ToolDirectoryHandler(nil).ServeHTTP(response, request)
	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
	require.Equal(t, http.MethodGet, response.Header().Get("Allow"))
}

func TestToolDirectoryListsOnlyEnabledWriteOperations(t *testing.T) {
	t.Parallel()
	runtime := NewRuntime(nil, "https://admin.example.test/admin-mcp")
	deps := WriteDependencies{Features: &productfeatures.Client{}, Registry: mcpregistry.New(nil, nil)}
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{}}
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
