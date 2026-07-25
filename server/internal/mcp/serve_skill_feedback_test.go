package mcp_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestServeSkillFeedbackListsAndCallsSingleDevTool(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)
	require.NotEmpty(t, authCtx.UserID)
	key := createGeneratedHooksKey(t, ti, authCtx, "hooks")
	startedAt := time.Now().Add(-time.Second)

	listed, err := serveSkillFeedbackHTTP(t, ti, toolsListBody(), key, *authCtx.ProjectSlug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code)
	require.Contains(t, listed.Body.String(), platformtools.ToolNameSkillFeedback)
	require.NotContains(t, listed.Body.String(), platformtools.ToolNameSkillsLoad)
	require.NotContains(t, listed.Body.String(), platformtools.ToolNamePlatformSkillFeedback)

	injectedChatID := uuid.NewString()
	called, err := serveSkillFeedbackHTTP(t, ti, skillFeedbackCallBody("dev-feedback", "helped", nil), key, *authCtx.ProjectSlug, injectedChatID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, called.Code)
	require.Contains(t, called.Body.String(), `"recorded":true`)

	rows, err := skillsrepo.New(ti.conn).ListRecentSkillFeedback(ctx, skillsrepo.ListRecentSkillFeedbackParams{
		ProjectID: *authCtx.ProjectID,
		SkillName: "dev-feedback",
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "dev", rows[0].Source)
	require.False(t, rows[0].SessionID.Valid)
	require.False(t, rows[0].UserID.Valid)
	require.False(t, rows[0].UserEmail.Valid)

	chClient, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	var telemetryRows []telemetryrepo.TelemetryLog
	require.Eventually(t, func() bool {
		telemetryRows, err = telemetryrepo.New(chClient).ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     startedAt.UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs: []string{
				urn.NewTool(urn.ToolKindPlatform, "skills", "feedback").String(),
			},
			SortOrder: "desc",
			Limit:     10,
		})
		return err == nil && len(telemetryRows) == 1
	}, 2*time.Second, 50*time.Millisecond)
	require.Nil(t, telemetryRows[0].GramChatID)
	require.NotContains(t, telemetryRows[0].Attributes, string(attr.GenAIConversationIDKey))
	require.NotContains(t, telemetryRows[0].Attributes, injectedChatID)
	require.NotContains(t, telemetryRows[0].Attributes, authCtx.UserID)
	if authCtx.Email != nil {
		require.NotContains(t, telemetryRows[0].Attributes, *authCtx.Email)
	}
}

func TestServeSkillFeedbackAcceptsGeneratedHooksDownloadKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	key := createGeneratedHooksKey(t, ti, authCtx, "hooks-download")

	w, err := serveSkillFeedbackHTTP(t, ti, toolsListBody(), key, *authCtx.ProjectSlug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestServeSkillFeedbackRejectsMissingConsumerAndAssistantTokens(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := serveSkillFeedbackHTTP(t, ti, toolsListBody(), "", *authCtx.ProjectSlug)
	require.ErrorContains(t, err, "unauthorized")

	consumerKey := ti.createTestAPIKey(ctx, t)
	_, err = serveSkillFeedbackHTTP(t, ti, toolsListBody(), consumerKey, *authCtx.ProjectSlug)
	require.Error(t, err)

	assistantID := createAssistant(t, ti, authCtx, "Feedback route assistant")
	assistantToken := mintAssistantToken(t, ti, authCtx, assistantID)
	_, err = serveSkillFeedbackHTTP(t, ti, toolsListBody(), assistantToken, *authCtx.ProjectSlug)
	require.Error(t, err)
}

func TestServeSkillFeedbackRejectsHooksScopedKeyWithoutGeneratedMarker(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	key := "gram_local_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	key = createHooksScopedKey(t, ti, authCtx, "developer-hooks", key)

	_, err := serveSkillFeedbackHTTP(t, ti, toolsListBody(), key, *authCtx.ProjectSlug)
	require.ErrorContains(t, err, "generated plugin hooks key")
}

func TestServeSkillFeedbackRejectsProjectMismatch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	key := createGeneratedHooksKey(t, ti, authCtx, "hooks")
	otherSlug := "feedback-other-" + uuid.NewString()[:8]
	_, err := projectrepo.New(ti.conn).CreateProject(ctx, projectrepo.CreateProjectParams{
		Name:           otherSlug,
		Slug:           otherSlug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	_, err = serveSkillFeedbackHTTP(t, ti, toolsListBody(), key, otherSlug)
	require.Error(t, err)
}

func TestServeSkillFeedbackInitializeMirrorsSessionHeader(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	key := createGeneratedHooksKey(t, ti, authCtx, "hooks")
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"})
	require.NoError(t, err)

	w, err := serveSkillFeedbackHTTP(t, ti, body, key, *authCtx.ProjectSlug)
	require.NoError(t, err)
	require.NotEmpty(t, w.Header().Get("Mcp-Session-Id"))

	req := httptest.NewRequest(http.MethodPost, "/platform/mcp/skill-feedback", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Gram-Project", *authCtx.ProjectSlug)
	req.Header.Set("Mcp-Session-Id", "client-session")
	w = httptest.NewRecorder()
	require.NoError(t, ti.service.ServeSkillFeedback(w, req))
	require.Equal(t, "client-session", w.Header().Get("Mcp-Session-Id"))
}

func TestServeSkillFeedbackRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	key := createGeneratedHooksKey(t, ti, authCtx, "hooks")

	_, err := serveSkillFeedbackHTTP(t, ti, bytes.Repeat([]byte("x"), (1<<20)+1), key, *authCtx.ProjectSlug)
	require.ErrorContains(t, err, "exceeds 1 MiB")
}

func TestServePlatformToolsetRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	assistantID := createAssistant(t, ti, authCtx, "Oversized assistant")
	token := mintAssistantToken(t, ti, authCtx, assistantID)

	_, err := servePlatformHTTP(t, ti, platformtools.AssistantsPlatformToolsetSlug, bytes.Repeat([]byte("x"), (1<<20)+1), token)
	require.ErrorContains(t, err, "exceeds 1 MiB")
}

func createGeneratedHooksKey(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext, purpose string) string {
	t.Helper()
	marker := strings.ReplaceAll(uuid.NewString(), "-", "")[:6]
	key := "gram_local_" + marker + strings.Repeat("0", 32)
	return createHooksScopedKey(t, ti, authCtx, fmt.Sprintf("plugins-%s-20260725-120000-%s", purpose, marker), key)
}

func createHooksScopedKey(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext, name, key string) string {
	t.Helper()
	require.NotNil(t, authCtx.ProjectID)
	keyHash, err := auth.GetAPIKeyHash(key)
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateAPIKey(t.Context(), keysrepo.CreateAPIKeyParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		CreatedByUserID: authCtx.UserID,
		Name:            name,
		KeyPrefix:       key[:16],
		KeyHash:         keyHash,
		Scopes:          []string{"hooks"},
	})
	require.NoError(t, err)
	return key
}

func skillFeedbackCallBody(skill, outcome string, note *string) []byte {
	arguments := map[string]any{"skill": skill, "outcome": outcome}
	if note != nil {
		arguments["note"] = *note
	}
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": platformtools.ToolNameSkillFeedback, "arguments": arguments},
	})
	return body
}

func serveSkillFeedbackHTTP(t *testing.T, ti *testInstance, body []byte, token, projectSlug string, chatID ...string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/platform/mcp/skill-feedback", bytes.NewReader(body))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Gram-Project", projectSlug)
	if len(chatID) > 0 {
		req.Header.Set("Gram-Chat-ID", chatID[0])
	}
	w := httptest.NewRecorder()
	if err := ti.service.ServeSkillFeedback(w, req); err != nil {
		return w, fmt.Errorf("serve skill feedback: %w", err)
	}
	return w, nil
}
