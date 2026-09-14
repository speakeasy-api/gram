package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

const toolSearchUnavailableMessage = "tool search is temporarily unavailable; try again later"

func TestMCPWithoutTemporalServesStaticRequestsAndFailsFastOnStaleDynamicIndex(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithoutTemporal(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, "no-temporal-"+uuid.NewString()[:8])
	addHTTPTools(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "static_tool")

	initialize, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, initialize.Code)

	staticList, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"static_tool"}, toolNames(parseToolsListResponse(t, staticList.Body.Bytes())))

	dynamicList, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), "", map[string]string{"Gram-Mode": "dynamic"})
	require.NoError(t, err)
	requireMCPToolSearchUnavailable(t, dynamicList)

	dynamicSearch, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsCallBody("search_tools"), "", map[string]string{"Gram-Mode": "dynamic"})
	require.NoError(t, err)
	requireMCPToolSearchUnavailable(t, dynamicSearch)
}

func TestOAuthWithoutTemporalServesAuthorizationMetadata(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithoutTemporal(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	external := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug:     "no-temporal-oauth-" + uuid.NewString()[:8],
		IsPublic: true,
		Metadata: nil,
	})
	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, external.Toolset.McpSlug.String)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestSkillsLoadWithoutTemporalRemainsSuccessfulAcrossTrailingSignal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithoutTemporal(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	assistantID := createAssistant(t, ti, authCtx, "No Temporal")
	token := mintAssistantToken(t, ti, authCtx, assistantID)
	skillName := "no-temporal-skill-" + uuid.NewString()[:8]
	queries := skillsrepo.New(ti.conn)
	skill, err := queries.CreateSkill(ctx, skillsrepo.CreateSkillParams{
		ProjectID:   *authCtx.ProjectID,
		Name:        skillName,
		DisplayName: skillName,
		Summary:     pgtype.Text{},
	})
	require.NoError(t, err)
	content := "---\nname: " + skillName + "\ndescription: nil Temporal coverage\n---\n\nbody\n"
	_, err = queries.CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content:          content,
		CanonicalSha256:  uuid.NewString(),
		RawSha256:        uuid.NewString(),
		Description:      pgtype.Text{String: "nil Temporal coverage", Valid: true},
		Metadata:         []byte(`{}`),
		SpecValid:        true,
		ValidationErrors: []byte(`[]`),
		CreatedByUserID:  authCtx.UserID,
		ProjectID:        *authCtx.ProjectID,
		SkillID:          skill.ID,
	})
	require.NoError(t, err)
	_, err = queries.CreateSkillDistribution(ctx, skillsrepo.CreateSkillDistributionParams{
		PluginID:        uuid.NullUUID{},
		AssistantID:     uuid.NullUUID{UUID: assistantID, Valid: true},
		PinnedVersionID: uuid.NullUUID{},
		Channel:         "assistant",
		CreatedByUserID: authCtx.UserID,
		ProjectID:       *authCtx.ProjectID,
		SkillID:         skill.ID,
	})
	require.NoError(t, err)

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      platformtools.ToolNameSkillsLoad,
			"arguments": map[string]any{"name": skillName},
		},
	})
	require.NoError(t, err)
	callLoad := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/platform/mcp/"+platformtools.AssistantsPlatformToolsetSlug, bytes.NewReader(body))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Gram-Chat-ID", uuid.NewString())
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("toolsetSlug", platformtools.AssistantsPlatformToolsetSlug)
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		require.NoError(t, ti.service.ServePlatformToolset(w, req))
		return w
	}

	first := callLoad()
	require.Equal(t, http.StatusOK, first.Code)
	require.Contains(t, first.Body.String(), "body")
	second := callLoad()
	require.Equal(t, http.StatusOK, second.Code)
	require.Contains(t, second.Body.String(), "body")

	// The second call is coalesced into a trailing signal. Flushing exercises
	// that callback synchronously so a nil Temporal environment cannot leave a
	// process-crashing timer behind.
	require.NoError(t, ti.efficacySignaler.Shutdown(t.Context()))
}

func requireMCPToolSearchUnavailable(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code)

	var response struct {
		Error struct {
			Code    oops.MCPCode `json:"code"`
			Message string       `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, oops.MCPCodeInternalError, response.Error.Code)
	require.Equal(t, toolSearchUnavailableMessage, response.Error.Message)
}
