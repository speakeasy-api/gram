// Asserts the managed-assistant platform toolset (carrying the dashboard egress
// tool) is reachable only by a project's managed assistant. Any other assistant
// token for the same project is rejected at the entrypoint as if the toolset did
// not exist, rather than relying on downstream tools to refuse the call.
package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assistants"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
)

func TestServePlatformToolset_ManagedAssistantReachesDashboardToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	managedID := createAssistant(t, ti, authCtx, "Managed")
	err := assistantsrepo.New(ti.conn).CreateProjectManagedAssistant(t.Context(), assistantsrepo.CreateProjectManagedAssistantParams{
		ProjectID:   *authCtx.ProjectID,
		AssistantID: managedID,
	})
	require.NoError(t, err)

	token := mintAssistantToken(t, ti, authCtx, managedID)
	w, err := servePlatformHTTP(t, ti, platformtools.ManagedAssistantPlatformToolsetSlug, toolsListBody(), token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "managed assistant must reach the managed toolset: %s", w.Body.String())
	require.Contains(t, w.Body.String(), platformtools.ToolNameDashboardSendMessage)
}

func TestServePlatformToolset_NonManagedAssistantRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// A managed assistant exists for the project, but the caller is a different
	// assistant in the same project — it must not reach the managed toolset.
	managedID := createAssistant(t, ti, authCtx, "Managed")
	err := assistantsrepo.New(ti.conn).CreateProjectManagedAssistant(t.Context(), assistantsrepo.CreateProjectManagedAssistantParams{
		ProjectID:   *authCtx.ProjectID,
		AssistantID: managedID,
	})
	require.NoError(t, err)

	otherID := createAssistant(t, ti, authCtx, "Other")
	token := mintAssistantToken(t, ti, authCtx, otherID)

	_, err = servePlatformHTTP(t, ti, platformtools.ManagedAssistantPlatformToolsetSlug, toolsListBody(), token)
	require.Error(t, err, "a non-managed assistant must be rejected at the entrypoint")
	require.Contains(t, err.Error(), "not found")
}

func createAssistant(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext, name string) uuid.UUID {
	t.Helper()
	a, err := assistantsrepo.New(ti.conn).CreateAssistant(t.Context(), assistantsrepo.CreateAssistantParams{
		ProjectID:       *authCtx.ProjectID,
		OrganizationID:  authCtx.ActiveOrganizationID,
		CreatedByUserID: pgtype.Text{String: authCtx.UserID, Valid: true},
		Name:            name + " " + uuid.NewString()[:8],
		Model:           "openai/gpt-4o-mini",
		Instructions:    "",
		WarmTtlSeconds:  300,
		MaxConcurrency:  1,
		Status:          assistants.StatusActive,
	})
	require.NoError(t, err)
	return a.ID
}

func mintAssistantToken(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext, assistantID uuid.UUID) string {
	t.Helper()
	token, err := assistanttokens.New("test-jwt-secret", ti.conn, ti.authzEngine).Generate(assistanttokens.GenerateInput{
		OrgID:       authCtx.ActiveOrganizationID,
		ProjectID:   *authCtx.ProjectID,
		UserID:      authCtx.UserID,
		AssistantID: assistantID,
		ThreadID:    uuid.Nil,
		TTL:         time.Hour,
	})
	require.NoError(t, err)
	return token
}

func toolsListBody() []byte {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	return body
}

func servePlatformHTTP(t *testing.T, ti *testInstance, slug string, body []byte, token string) (*httptest.ResponseRecorder, error) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/platform/mcp/"+slug, bytes.NewReader(body))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("toolsetSlug", slug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	if err := ti.service.ServePlatformToolset(w, req); err != nil {
		return w, fmt.Errorf("serve platform toolset: %w", err)
	}
	return w, nil
}
