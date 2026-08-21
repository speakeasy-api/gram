// Asserts the managed-assistant platform toolset is reachable only by a
// project's managed assistant. Any other assistant token for the same project
// is rejected at the entrypoint as if the toolset did not exist, rather than
// relying on downstream tools to refuse the call.
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
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestServePlatformToolset_ManagedAssistantReachesManagedToolset(t *testing.T) {
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
	require.Contains(t, w.Body.String(), platformtools.ToolNameSearchLogs)
}

// A thread whose source kind cannot be read — deleted mid-turn, or a failed
// read — falls back to the flag alone. Reporting it as not-dashboard would
// 404 the toolset a dashboard thread is already connected to and strip the
// assistant of every tool mid-turn; the legacy toolset stays hidden either
// way, so an org that is not on the variant cannot be reached by the error.
func TestServePlatformToolset_UnreadableThreadFallsBackToVariant(t *testing.T) {
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

	ti.features.SetFlagVariant(feature.FlagAssistantPlatformMCP, authCtx.ActiveOrganizationID, feature.VariantAssistantToolsPlatformMCP)

	// mintAssistantToken carries no thread, so the source-kind read finds
	// nothing — the same position a deleted thread leaves this check in.
	token := mintAssistantToken(t, ti, authCtx, managedID)

	w, err := servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, toolsListBody(), token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "the variant's toolset must still serve: %s", w.Body.String())

	_, err = servePlatformHTTP(t, ti, platformtools.ManagedAssistantPlatformToolsetSlug, toolsListBody(), token)
	require.Error(t, err, "the legacy toolset stays hidden on the platformmcp variant")
}

// The rollout is dashboard-scoped. A Slack thread on the same managed
// assistant keeps the legacy toolset even while the org is on the platformmcp
// variant — attachment grants it, so the serve path must not 404 it.
func TestServePlatformToolset_NonDashboardThreadKeepsLegacyToolset(t *testing.T) {
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

	ti.features.SetFlagVariant(feature.FlagAssistantPlatformMCP, authCtx.ActiveOrganizationID, feature.VariantAssistantToolsPlatformMCP)

	token := mintSourceKindAssistantToken(t, ti, authCtx, managedID, "slack-thread", "slack")

	w, err := servePlatformHTTP(t, ti, platformtools.ManagedAssistantPlatformToolsetSlug, toolsListBody(), token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "a slack thread must keep the legacy toolset: %s", w.Body.String())

	_, err = servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, toolsListBody(), token)
	require.Error(t, err, "a slack thread must not reach the platform toolset")
}

func TestServePlatformToolset_ManagedToolsetRejectedOnPlatformMCPVariant(t *testing.T) {
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

	ti.features.SetFlagVariant(feature.FlagAssistantPlatformMCP, authCtx.ActiveOrganizationID, feature.VariantAssistantToolsPlatformMCP)

	// The rollout is dashboard-scoped, so the calling thread decides:
	// mint a token bound to a dashboard thread.
	token, _ := mintThreadAssistantToken(t, ti, authCtx, managedID, "rmmcpvariant")
	_, err = servePlatformHTTP(t, ti, platformtools.ManagedAssistantPlatformToolsetSlug, toolsListBody(), token)
	require.Error(t, err, "the legacy toolset must stay hidden on the platformmcp variant")
	require.Contains(t, err.Error(), "not found")
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

// The research tools are the MCP research runner's, and it holds them
// in-process. Served over HTTP they would give any assistant in any
// mcp_approval organization billable web search and arbitrary page fetch, so
// the entrypoint refuses the slug outright — including for the project's own
// managed assistant, which is the most privileged token that reaches here.
func TestServePlatformToolset_ResearchToolsetIsNotServed(t *testing.T) {
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
	_, err = servePlatformHTTP(t, ti, platformtools.ResearchToolsetSlug, toolsListBody(), token)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestServePlatformToolset_AssistantToolCallAudited(t *testing.T) {
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

	token, threadID := mintThreadAssistantToken(t, ti, authCtx, managedID, "audit-test")

	countBefore, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionAssistantToolCall)
	require.NoError(t, err)

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": platformtools.ToolNameSearchLogs,
			"arguments": map[string]any{
				"query":     "errors",
				"api_token": "super-secret",
			},
		},
	})
	require.NoError(t, err)

	w, err := servePlatformHTTP(t, ti, platformtools.ManagedAssistantPlatformToolsetSlug, body, token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "tool call dispatch must succeed: %s", w.Body.String())

	countAfter, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionAssistantToolCall)
	require.NoError(t, err)
	require.Equal(t, countBefore+1, countAfter, "an assistant tool call must record exactly one audit entry")

	record, err := audittest.LatestAuditLogByAction(t.Context(), ti.conn, audit.ActionAssistantToolCall)
	require.NoError(t, err)
	require.Equal(t, "assistant", record.SubjectType)
	require.Equal(t, platformtools.ToolNameSearchLogs, record.SubjectDisplay)
	require.Equal(t, platformtools.ManagedAssistantPlatformToolsetSlug, record.SubjectSlug)
	require.Equal(t, uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}, record.ProjectID)

	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, platformtools.ToolNameSearchLogs, metadata["tool_name"])
	require.Equal(t, platformtools.ManagedAssistantPlatformToolsetSlug, metadata["toolset_slug"])
	require.Equal(t, threadID.String(), metadata["thread_id"])

	params, ok := metadata["params"].(map[string]any)
	require.True(t, ok, "metadata must carry the tool call params: %s", string(record.Metadata))
	require.Equal(t, "errors", params["query"])
	require.Equal(t, "[REDACTED]", params["api_token"], "secret-shaped params must be scrubbed")
}

func TestServePlatformToolset_PreservesShareableToolError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	assistantID := createAssistant(t, ti, authCtx, "Feedback")
	token, _ := mintThreadAssistantToken(t, ti, authCtx, assistantID, "feedback-error")
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": platformtools.ToolNamePlatformSkillFeedback,
			"arguments": map[string]any{
				"skill":   "missing-skill",
				"outcome": "did_not_help",
			},
		},
	})
	require.NoError(t, err)

	w, err := servePlatformHTTP(t, ti, platformtools.AssistantsPlatformToolsetSlug, body, token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "call skills_load for this skill before submitting feedback")
	require.NotContains(t, w.Body.String(), "Internal error")
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

func mintThreadAssistantToken(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext, assistantID uuid.UUID, correlationPrefix string) (string, uuid.UUID) {
	t.Helper()

	return mintSourceKindThreadToken(t, ti, authCtx, assistantID, correlationPrefix, "dashboard")
}

// mintSourceKindAssistantToken is mintSourceKindThreadToken without the thread
// id, for callers that only need the token.
func mintSourceKindAssistantToken(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext, assistantID uuid.UUID, correlationPrefix, sourceKind string) string {
	t.Helper()

	token, _ := mintSourceKindThreadToken(t, ti, authCtx, assistantID, correlationPrefix, sourceKind)
	return token
}

func mintSourceKindThreadToken(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext, assistantID uuid.UUID, correlationPrefix, sourceKind string) (string, uuid.UUID) {
	t.Helper()

	chatID := uuid.New()
	err := assistantsrepo.New(ti.conn).UpsertAssistantChat(t.Context(), assistantsrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         pgtype.Text{String: authCtx.UserID, Valid: true},
		Title:          pgtype.Text{},
	})
	require.NoError(t, err)
	threadID, err := assistantsrepo.New(ti.conn).UpsertAssistantThread(t.Context(), assistantsrepo.UpsertAssistantThreadParams{
		AssistantID:   assistantID,
		ProjectID:     *authCtx.ProjectID,
		CorrelationID: correlationPrefix + "-" + uuid.NewString()[:8],
		ChatID:        chatID,
		SourceKind:    sourceKind,
		SourceRefJson: []byte("{}"),
	})
	require.NoError(t, err)
	token, err := assistanttokens.New("test-jwt-secret", ti.conn, ti.authzEngine).Generate(assistanttokens.GenerateInput{
		OrgID:       authCtx.ActiveOrganizationID,
		ProjectID:   *authCtx.ProjectID,
		UserID:      authCtx.UserID,
		AssistantID: assistantID,
		ThreadID:    threadID,
		TTL:         time.Hour,
	})
	require.NoError(t, err)
	return token, threadID
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

// The Platform MCP read toolset is rollout-gated: the managed assistant
// reaches it only when the assistant-platform-mcp flag resolves to the
// platformmcp variant for the org.
func TestServePlatformToolset_PlatformMCPReadVariantListsTools(t *testing.T) {
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

	ti.features.SetFlagVariant(feature.FlagAssistantPlatformMCP, authCtx.ActiveOrganizationID, feature.VariantAssistantToolsPlatformMCP)

	// The rollout is dashboard-scoped, so the calling thread decides:
	// mint a token bound to a dashboard thread.
	token, _ := mintThreadAssistantToken(t, ti, authCtx, managedID, "ntliststools")
	w, err := servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, toolsListBody(), token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "managed assistant must reach the platform toolset when the flag is on: %s", w.Body.String())
	// Names come through as the catalogue declares them: the assistant is
	// served that catalogue rather than a parallel platformtools set, so there
	// is nothing to disambiguate and no platform_ prefix.
	body := w.Body.String()
	require.Contains(t, body, `"get_platform_context"`)
	require.Contains(t, body, `"list_projects"`)
	require.Contains(t, body, `"find_mcp"`)
	require.Contains(t, body, `"get_mcp"`)
	require.NotContains(t, body, platformtools.ToolNameListProjects, "the legacy prefixed set must not be served on this variant")

	// The assistant only ever acts in its own project, so the project the
	// policy supplies is not advertised as an argument for a model to choose.
	require.NotContains(t, body, `"project_id"`, "project arguments are injected, not requested")
}

func TestServePlatformToolset_PlatformMCPReadLegacyVariantRejected(t *testing.T) {
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
	_, err = servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, toolsListBody(), token)
	require.Error(t, err, "the platform toolset must stay hidden on the legacy variant")
	require.Contains(t, err.Error(), "not found")
}

func TestServePlatformToolset_PlatformMCPReadNonManagedAssistantRejected(t *testing.T) {
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

	ti.features.SetFlagVariant(feature.FlagAssistantPlatformMCP, authCtx.ActiveOrganizationID, feature.VariantAssistantToolsPlatformMCP)

	otherID := createAssistant(t, ti, authCtx, "Other")
	token := mintAssistantToken(t, ti, authCtx, otherID)

	_, err = servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, toolsListBody(), token)
	require.Error(t, err, "a non-managed assistant must not reach the platform toolset even on the platformmcp variant")
	require.Contains(t, err.Error(), "not found")
}

// tools/call must round-trip through the re-served reader against the seeded
// org: list_projects returns the project the auth context lives in.
func TestServePlatformToolset_PlatformMCPReadListProjectsCall(t *testing.T) {
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

	ti.features.SetFlagVariant(feature.FlagAssistantPlatformMCP, authCtx.ActiveOrganizationID, feature.VariantAssistantToolsPlatformMCP)

	// Assistant calls are authorized live on every request, not carried from
	// an earlier decision, so the grant has to exist for real.
	grantLiveOrgAdmin(t, ti, authCtx)

	// The rollout is dashboard-scoped, so the calling thread decides:
	// mint a token bound to a dashboard thread.
	token, _ := mintThreadAssistantToken(t, ti, authCtx, managedID, "projectscall")

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "list_projects",
			"arguments": map[string]any{},
		},
	})
	require.NoError(t, err)

	w, err := servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, body, token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "list_projects call must succeed: %s", w.Body.String())
	require.Contains(t, w.Body.String(), authCtx.ProjectID.String(), "the caller's project must appear in the listing")
}

// grantLiveOrgAdmin persists an org:admin grant for the auth context's user.
// The adapter rechecks authorization against the database on every call, which
// the test fixture's context-only grants do not satisfy.
func grantLiveOrgAdmin(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext) {
	t.Helper()

	err := authz.PatchPrincipalGrants(
		t.Context(),
		ti.conn,
		authCtx.ActiveOrganizationID,
		urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		[]*authz.RoleGrant{{Scope: string(authz.ScopeOrgAdmin), Selectors: nil}},
		nil,
	)
	require.NoError(t, err)
}
