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
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// grantProjectLogRead gives the turn's user log:read on the project. No system
// role holds it, so the tools that return individual tool calls and raw log
// records are hidden from the assistant without it.
func grantProjectLogRead(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext) {
	t.Helper()
	seedPrincipalGrant(t, t.Context(), ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), authz.ScopeLogRead, authCtx.ProjectID.String())
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

	otherID, otherAgentID := createAssistantWithAgent(t, ti, authCtx, "Other")
	grantProjectRead(t, ti, authCtx)
	token := mintAgentTurnCredential(t, ti, authCtx, otherID, otherAgentID)

	_, err = servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, toolsListBody(), token)
	require.Error(t, err, "a non-managed assistant must be rejected at the entrypoint")
	require.Contains(t, err.Error(), "not found")
}

func TestServePlatformToolset_RejectsAssistantRuntimeJWT(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	managedID, _ := managedAssistantWithAgent(t, ti, authCtx)
	forged := mintForgedAssistantRuntimeJWT(t, authCtx, managedID)

	_, err := servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, toolsListBody(), forged)
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnauthorized, shareable.Code)
}

func TestServePlatformToolset_DeniedAndMissingInvalidBodiesMatch(t *testing.T) {
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
	otherID, otherAgentID := createAssistantWithAgent(t, ti, authCtx, "Other")
	grantProjectRead(t, ti, authCtx)
	token := mintAgentTurnCredential(t, ti, authCtx, otherID, otherAgentID)

	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)

	for _, revision := range []string{"", mcpversions.Version20251125, mcpversions.Version20260728} {
		for _, tc := range []struct {
			name   string
			body   string
			status int
			code   oops.MCPCode
		}{
			{name: "malformed JSON", body: "{", status: http.StatusBadRequest, code: oops.MCPCodeParseError},
			{name: "empty body", status: http.StatusBadRequest, code: oops.MCPCodeParseError},
			{name: "oversized body", body: strings.Repeat(" ", (1<<20)+1), status: http.StatusRequestEntityTooLarge, code: oops.MCPCodeInternalError},
			{name: "invalid envelope", body: `{"jsonrpc":"1.0","id":0,"method":"tools/list"}`, status: http.StatusBadRequest, code: oops.MCPCodeInvalidRequest},
			{name: "invalid method type", body: `{"jsonrpc":"2.0","id":0,"method":42}`, status: http.StatusBadRequest, code: oops.MCPCodeInvalidRequest},
		} {
			t.Run(revision+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				responses := make([]*httptest.ResponseRecorder, 0, 2)
				for _, slug := range []string{"missing-platform-toolset", platformtools.PlatformMCPReadToolsetSlug} {
					req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/platform/mcp/"+slug, strings.NewReader(tc.body))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Authorization", "Bearer "+token)
					req.Header.Set(httpheaders.MethodHeader, mcpversions.MethodToolsList)
					if revision != "" {
						req.Header.Set(mcpversions.HTTPHeader, revision)
					}
					w := httptest.NewRecorder()
					router.ServeHTTP(w, req)
					responses = append(responses, w)
				}

				missing, denied := responses[0], responses[1]
				require.Equal(t, missing.Code, denied.Code)
				require.Equal(t, missing.Header(), denied.Header())
				require.Equal(t, missing.Body.String(), denied.Body.String())

				wantStatus, wantCode := http.StatusNotFound, oops.MCPCodeResourceNotFound
				if revision == mcpversions.Version20260728 {
					wantStatus, wantCode = tc.status, tc.code
				}
				require.Equal(t, wantStatus, missing.Code, "body=%s", missing.Body.String())
				var response struct {
					Error struct {
						Code oops.MCPCode `json:"code"`
					} `json:"error"`
				}
				require.NoError(t, json.Unmarshal(missing.Body.Bytes(), &response))
				require.Equal(t, wantCode, response.Error.Code)
			})
		}
	}
}

func TestServePlatformToolset_UnsupportedVersionPrecedesTokenAuthentication(t *testing.T) {
	t.Parallel()

	_, ti := newTestMCPService(t)
	slug := platformtools.PlatformMCPReadToolsetSlug
	req := httptest.NewRequest(http.MethodPost, "/platform/mcp/"+slug, bytes.NewReader(toolsListBody()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(mcpversions.HTTPHeader, unservedProtocolVersion)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("toolsetSlug", slug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err := ti.service.ServePlatformToolset(w, req)
	require.NoError(t, err)
	requireUnsupportedProtocolVersionResponse(t, w, unservedProtocolVersion, mcpversions.SupportedPlatformToolset())
	require.Empty(t, w.Header().Get("WWW-Authenticate"))
}

// Failures that escape the platform handler reach the client as JSON-RPC
// errors, as on the other MCP surfaces, with the HTTP status the failure
// carries. A handshake-era revision is declared deliberately to verify that
// an unknown toolset retains its legacy response without reading the body.
func TestServePlatformToolset_AttachedFailuresAreJSONRPCErrors(t *testing.T) {
	t.Parallel()

	_, ti := newTestMCPService(t)
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)

	for _, tc := range []struct {
		name   string
		slug   string
		status int
		code   oops.MCPCode
		id     string
	}{
		{name: "missing token", slug: platformtools.PlatformMCPReadToolsetSlug, status: http.StatusUnauthorized, code: oops.MCPCodeUnauthorized, id: `1`},
		// The slug is resolved before the body is read, so the request id is
		// not known yet.
		{name: "unknown toolset", slug: "unknown-platform-toolset", status: http.StatusNotFound, code: oops.MCPCodeResourceNotFound, id: `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/platform/mcp/"+tc.slug, bytes.NewReader(toolsListBody()))
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(mcpversions.HTTPHeader, mcpversions.Version20251125)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, tc.status, w.Code, "body=%s", w.Body.String())
			require.Equal(t, "application/json", w.Header().Get("Content-Type"))
			var response struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      json.RawMessage `json:"id"`
				Error   struct {
					Code oops.MCPCode `json:"code"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response), "body=%s", w.Body.String())
			require.Equal(t, "2.0", response.JSONRPC)
			require.JSONEq(t, tc.id, string(response.ID))
			require.Equal(t, tc.code, response.Error.Code)
		})
	}
}

func TestServePlatformToolset_EmptyBodyRequiresTokenAuthentication(t *testing.T) {
	t.Parallel()

	_, ti := newTestMCPService(t)
	_, err := servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, nil, "")
	require.Error(t, err)

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnauthorized, shareable.Code)
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

	managedID, managedAgentID := managedAssistantWithAgent(t, ti, authCtx)

	grantProjectRead(t, ti, authCtx)
	token := mintAgentTurnCredential(t, ti, authCtx, managedID, managedAgentID)
	_, err := servePlatformHTTP(t, ti, platformtools.ResearchToolsetSlug, toolsListBody(), token)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

// platformMCPContextTool is a read-only Platform MCP tool that needs no seeded
// data, so tool-call plumbing tests can dispatch it.
const platformMCPContextTool = "get_platform_context"

func TestServePlatformToolset_AssistantToolCallAudited(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithPlatformMCPReadTools(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	managedID, managedAgentID := managedAssistantWithAgent(t, ti, authCtx)

	grantProjectRead(t, ti, authCtx)
	grantLiveOrgAdmin(t, ti, authCtx)
	token, threadID := mintAgentTurnCredentialWithThread(t, ti, authCtx, managedID, managedAgentID)

	countBefore, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionAssistantToolCall)
	require.NoError(t, err)

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": platformMCPContextTool,
			"arguments": map[string]any{
				"query":     "errors",
				"api_token": "super-secret",
			},
		},
	})
	require.NoError(t, err)

	w, err := servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, body, token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "tool call dispatch must succeed: %s", w.Body.String())

	countAfter, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionAssistantToolCall)
	require.NoError(t, err)
	require.Equal(t, countBefore+1, countAfter, "an assistant tool call must record exactly one audit entry")

	record, err := audittest.LatestAuditLogByAction(t.Context(), ti.conn, audit.ActionAssistantToolCall)
	require.NoError(t, err)
	require.Equal(t, "assistant", record.SubjectType)
	require.Equal(t, platformMCPContextTool, record.SubjectDisplay)
	require.Equal(t, platformtools.PlatformMCPReadToolsetSlug, record.SubjectSlug)
	require.Equal(t, uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}, record.ProjectID)

	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, platformMCPContextTool, metadata["tool_name"])
	require.Equal(t, platformtools.PlatformMCPReadToolsetSlug, metadata["toolset_slug"])
	require.Equal(t, threadID.String(), metadata["thread_id"])

	params, ok := metadata["params"].(map[string]any)
	require.True(t, ok, "metadata must carry the tool call params: %s", string(record.Metadata))
	require.Equal(t, "errors", params["query"])
	require.Equal(t, "[REDACTED]", params["api_token"], "secret-shaped params must be scrubbed")
}

func TestServePlatformToolset_RiskScanUsesStableToolsetIdentityAndAnnotations(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithPlatformMCPReadTools(t)
	scanner := consumeRiskScanPayloads(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	grantLiveOrgAdmin(t, ti, authCtx)

	managedID, managedAgentID := managedAssistantWithAgent(t, ti, authCtx)

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": platformMCPContextTool,
			"arguments": map[string]any{
				"query": "errors",
			},
		},
	})
	require.NoError(t, err)

	grantProjectRead(t, ti, authCtx)
	token := mintAgentTurnCredential(t, ti, authCtx, managedID, managedAgentID)
	w, err := servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, body, token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, scanner.events, 2)

	event := scanner.events[0]
	require.Equal(t, mcpriskscan.SurfacePlatformMCP, event.Surface)
	require.Equal(t, platformtools.PlatformToolsetID(platformtools.PlatformMCPReadToolsetSlug).String(), event.ServerID)
	require.Empty(t, event.ToolsetID, "platform toolsets have no persisted toolset id")
	require.Equal(t, platformMCPContextTool, event.ToolName)
	require.NotNil(t, event.ToolAnnotations)
	require.NotNil(t, event.ToolAnnotations.ReadOnlyHint)
	require.True(t, *event.ToolAnnotations.ReadOnlyHint)
	require.Equal(t, mcpriskscan.PhaseRequest, event.Phase())
	require.Equal(t, mcpriskscan.PhaseResponse, scanner.events[1].Phase())
	require.Equal(t, event.ExecutionID(), scanner.events[1].ExecutionID())
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

func createAssistantWithAgent(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext, name string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	assistantID := createAssistant(t, ti, authCtx, name)
	agent, err := agentrepo.New(ti.conn).CreateAgent(t.Context(), agentrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Name:           name + " agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	return assistantID, agent.ID
}

func toolsListBody() []byte {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	return body
}

func servePlatformHTTP(t *testing.T, ti *testInstance, slug string, body []byte, token string, projectSlug ...string) (*httptest.ResponseRecorder, error) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/platform/mcp/"+slug, bytes.NewReader(body))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if len(projectSlug) > 0 {
		req.Header.Set("Gram-Project", projectSlug[0])
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("toolsetSlug", slug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	if err := ti.service.ServePlatformToolset(w, req); err != nil {
		return w, fmt.Errorf("serve platform toolset: %w", err)
	}
	return w, nil
}

// The managed assistant reaches the Platform MCP read toolset.
func TestServePlatformToolset_PlatformMCPReadVariantListsTools(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithPlatformMCPReadTools(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	managedID, managedAgentID := managedAssistantWithAgent(t, ti, authCtx)

	grantProjectRead(t, ti, authCtx)
	token := mintAgentTurnCredential(t, ti, authCtx, managedID, managedAgentID)
	w, err := servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, toolsListBody(), token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "managed assistant must reach the platform toolset: %s", w.Body.String())
	// Names come through as the catalogue declares them: the assistant is
	// served that catalogue rather than a parallel platformtools set, so there
	// is nothing to disambiguate and no platform_ prefix.
	body := w.Body.String()
	require.Contains(t, body, `"get_platform_context"`)
	require.Contains(t, body, `"list_projects"`)
	require.Contains(t, body, `"find_mcp"`)
	require.Contains(t, body, `"get_mcp"`)
	require.NotContains(t, body, platformtools.ToolNameListProjects, "the prefixed platformtools names must not be served")

	// The assistant only ever acts in its own project, so the project the
	// policy supplies is not advertised as an argument for a model to choose.
	require.NotContains(t, body, `"project_id"`, "project arguments are injected, not requested")

	// The catalogue is narrowed by the active organization's product features
	// and embeds that organization's project in each tool, so it is never
	// shareable with another caller.
	var envelope map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	requireCacheHints(t, envelope["result"], "private")
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

	otherID, otherAgentID := createAssistantWithAgent(t, ti, authCtx, "Other")
	grantProjectRead(t, ti, authCtx)
	token := mintAgentTurnCredential(t, ti, authCtx, otherID, otherAgentID)

	_, err = servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, toolsListBody(), token)
	require.Error(t, err, "a non-managed assistant must not reach the platform toolset")
	require.Contains(t, err.Error(), "not found")
}

// tools/call must round-trip through the re-served reader against the seeded
// org: list_projects returns only projects the caller has permission to read.
func TestServePlatformToolset_PlatformMCPReadListProjectsCall(t *testing.T) {
	t.Parallel()

	ctx, ti, recorder := newTestMCPServiceWithPlatformMCPReadTools(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	managedID, managedAgentID := managedAssistantWithAgent(t, ti, authCtx)

	// Assistant calls are authorized live on every request, not carried from
	// an earlier decision, so the grant has to exist for real.
	grantProjectRead(t, ti, authCtx)
	grantLiveOrgAdmin(t, ti, authCtx)

	token := mintAgentTurnCredential(t, ti, authCtx, managedID, managedAgentID)

	otherProject, err := projectsrepo.New(ti.conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           "other-readable-" + uuid.NewString()[:8],
		Slug:           "other-readable-" + uuid.NewString()[:8],
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

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
	require.NotContains(t, w.Body.String(), `"error"`)
	require.Contains(t, w.Body.String(), authCtx.ProjectID.String())
	require.NotContains(t, w.Body.String(), otherProject.ID.String(), "org:admin alone does not grant project:read on another project")

	require.NoError(t, authz.PatchPrincipalGrants(
		t.Context(), ti.conn, authCtx.ActiveOrganizationID,
		urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		[]*authz.RoleGrant{{Scope: string(authz.ScopeProjectRead), Selectors: []authz.Selector{authz.NewSelector(authz.ScopeProjectRead, otherProject.ID.String())}}},
		nil,
	))
	w, err = servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, body, token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "list_projects call must succeed: %s", w.Body.String())
	require.NotContains(t, w.Body.String(), `"error"`)
	require.Contains(t, w.Body.String(), otherProject.ID.String(), "the caller's newly readable project must appear in the listing")

	events := scanAttributes(recorder, mcpriskscan.SurfacePlatformMCP)
	require.Len(t, events, 4)
	for _, event := range events {
		require.Equal(t, authCtx.ActiveOrganizationID, event[attr.OrganizationIDKey])
		require.Equal(t, authCtx.ProjectID.String(), event[attr.ProjectIDKey])
		require.Equal(t, "list_projects", event[attr.ToolNameKey])
		require.Equal(t, platformtools.PlatformToolsetID(platformtools.PlatformMCPReadToolsetSlug).String(), event[attr.McpServerIDKey])
		require.Empty(t, event[attr.ToolsetIDKey])
		require.Equal(t, mcpriskscan.MethodToolsCall, event["gram.mcp.risk.scan.method"])
		require.Equal(t, "false", event["gram.mcp.risk.scan.identity_stamped"], "platform auth does not fabricate MCP principal provenance from AuthContext.UserID")
	}
	for i := 0; i < len(events); i += 2 {
		require.Equal(t, mcpriskscan.PhaseRequest, events[i]["gram.mcp.risk.scan.phase"])
		require.Equal(t, mcpriskscan.PhaseResponse, events[i+1]["gram.mcp.risk.scan.phase"])
		require.Equal(t, events[i]["gram.mcp.risk.scan.execution_id"], events[i+1]["gram.mcp.risk.scan.execution_id"])
	}
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
