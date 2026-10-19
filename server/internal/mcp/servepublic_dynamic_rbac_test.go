package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	pgvector_go "github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deployments_repo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	ragrepo "github.com/speakeasy-api/gram/server/internal/rag/repo"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	tools_repo "github.com/speakeasy-api/gram/server/internal/tools/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// dynamicSearchVectorDims is the width of the tool-search embedding column.
const dynamicSearchVectorDims = 1536

// countingEmbeddings answers every query embedding with the same vector, so
// every indexed tool is an equally good search match, and counts the calls so
// a test can prove the search service was never reached.
type countingEmbeddings struct {
	openrouter.CompletionClient
	calls atomic.Int32
}

func (c *countingEmbeddings) CreateEmbeddings(_ context.Context, _ string, _ string, inputs []string, _ ...openrouter.EmbeddingOption) ([][]float32, error) {
	c.calls.Add(1)
	vectors := make([][]float32, len(inputs))
	for i := range vectors {
		vectors[i] = unitVector()
	}
	return vectors, nil
}

func unitVector() []float32 {
	v := make([]float32, dynamicSearchVectorDims)
	v[0] = 1
	return v
}

// dynamicRBACFixture is a private hosted toolset whose tools are reader
// (read-only), purger (destructive) and writer (no annotations), indexed for
// tool search, with a bearer for the mock user. Each tool carries its own tag
// (see dynamicToolTags), so a test can tell whose tags a response reveals.
type dynamicRBACFixture struct {
	toolset    toolsets_repo.Toolset
	issuer     usersessions_repo.UserSessionIssuer
	bearer     string
	embeddings *countingEmbeddings
}

var dynamicToolTags = map[string]string{
	"reader": "tag-reader",
	"purger": "tag-purger",
	"writer": "tag-writer",
}

func newDynamicRBACFixture(t *testing.T) (context.Context, *testInstance, dynamicRBACFixture) {
	t.Helper()

	embeddings := &countingEmbeddings{}
	ctx, ti := newTestMCPServiceWithPoolConfigAndTemporal(t, testenv.NewLogger(t), testenv.NewMeterProvider(t), &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{}, nil, nil, false, mcp.MetaRuntimeConfig{}, testenv.NewTracerProvider(t), nil, nil, embeddings)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "dynamic-rbac-"+uuid.NewString()[:8])

	deploymentID, err := deployments_repo.New(ti.conn).InsertDeployment(ctx, deployments_repo.InsertDeploymentParams{
		ProjectID:      toolset.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         "test-user",
		IdempotencyKey: uuid.New().String(),
	})
	require.NoError(t, err)
	require.NoError(t, deployments_repo.New(ti.conn).CreateDeploymentStatus(ctx, deployments_repo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID,
		Status:       "completed",
	}))

	toolURNs := make([]urn.Tool, 0, 3)
	for _, tool := range []struct {
		name        string
		readOnly    pgtype.Bool
		destructive pgtype.Bool
	}{
		{name: "reader", readOnly: pgtype.Bool{Bool: true, Valid: true}},
		{name: "purger", destructive: pgtype.Bool{Bool: true, Valid: true}},
		{name: "writer"},
	} {
		toolURN := urn.NewTool(urn.ToolKindHTTP, tool.name, uuid.New().String()[:8])
		toolURNs = append(toolURNs, toolURN)
		require.NoError(t, tools_repo.New(ti.conn).CreateHTTPToolDefinition(ctx, tools_repo.CreateHTTPToolDefinitionParams{
			ProjectID:       toolset.ProjectID,
			DeploymentID:    deploymentID,
			ToolUrn:         toolURN,
			Name:            tool.name,
			Summary:         tool.name + " summary",
			Description:     tool.name + " description",
			Tags:            []string{dynamicToolTags[tool.name]},
			HttpMethod:      "GET",
			Path:            "/test",
			SchemaVersion:   "3.0.0",
			Schema:          []byte(`{}`),
			ServerEnvVar:    "TEST_SERVER_URL",
			Security:        []byte(`[]`),
			HeaderSettings:  []byte(`{}`),
			QuerySettings:   []byte(`{}`),
			PathSettings:    []byte(`{}`),
			ReadOnlyHint:    tool.readOnly,
			DestructiveHint: tool.destructive,
		}))

		_, err := ragrepo.New(ti.conn).InsertToolsetEmbedding(ctx, ragrepo.InsertToolsetEmbeddingParams{
			ProjectID:      toolset.ProjectID,
			ToolsetID:      toolset.ID,
			ToolsetVersion: 1,
			EntryKey:       toolURN.String(),
			EmbeddingModel: "test-embedding-model",
			Embedding1536:  pgvector_go.NewVector(unitVector()),
			Payload:        []byte(`{"name":"` + tool.name + `","_gramIndexDeploymentId":"` + deploymentID.String() + `"}`),
			Tags:           []string{dynamicToolTags[tool.name]},
		})
		require.NoError(t, err)
	}
	_, err = toolsetsRepo.CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID:     toolset.ID,
		Version:       1,
		ToolUrns:      toolURNs,
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	issuer, err := usersessions_repo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessions_repo.CreateUserSessionIssuerParams{
		ProjectID:          toolset.ProjectID,
		Slug:               toolset.Slug + "-issuer",
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(24 * time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	toolset, err = toolsetsRepo.UpdateToolsetUserSessionIssuer(ctx, toolsets_repo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           toolset.ProjectID,
	})
	require.NoError(t, err)
	setToolsetMcpPrivate(t, ctx, ti, toolset.ID, toolset.ProjectID)

	f := dynamicRBACFixture{toolset: toolset, issuer: issuer, bearer: "", embeddings: embeddings}
	f.bearer = f.mintBearer(t, ctx, ti, nil)
	return ctx, ti, f
}

// mintBearer mints a session for the mock user whose stored consent tool
// selection is selection (nil for none).
func (f dynamicRBACFixture) mintBearer(t *testing.T, ctx context.Context, ti *testInstance, selection []byte) string {
	t.Helper()

	subject := urn.NewUserSubject(mockidp.MockUserID)
	bearer, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  subject,
		Audience: urn.NewToolset(f.toolset.ID).String(),
		Issuer:   "https://test.example",
		Lifetime: time.Hour,
		ClientID: "test-client",
	})
	require.NoError(t, err)
	now := time.Now()
	_, err = usersessions_repo.New(ti.conn).CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: f.issuer.ID,
		SubjectUrn:          subject,
		Jti:                 jti,
		RefreshTokenHash:    conv.ToPGText("dynamic-rbac-" + uuid.NewString()),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true},
		ToolSelection:       selection,
	})
	require.NoError(t, err)
	return bearer
}

var dynamicModeHeaders = map[string]string{"Gram-Mode": "dynamic"}

func dynamicToolsCall(t *testing.T, name string, arguments map[string]any) []byte {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      9,
		"method":  mcpversions.MethodToolsCall,
		"params":  map[string]any{"name": name, "arguments": arguments},
	})
	require.NoError(t, err)
	return body
}

// serveDynamic sends one request in dynamic mode and returns everything it
// said, whether as a response body or a returned error.
func serveDynamic(t *testing.T, ti *testInstance, f dynamicRBACFixture, body []byte) string {
	t.Helper()

	w, err := servePublicHTTP(t, context.Background(), ti, f.toolset.McpSlug.String, body, f.bearer, dynamicModeHeaders)
	out := w.Body.String()
	if err != nil {
		out += err.Error()
	}
	return out
}

func dynamicListNames(t *testing.T, ti *testInstance, f dynamicRBACFixture) ([]string, string) {
	t.Helper()

	w, err := servePublicHTTP(t, context.Background(), ti, f.toolset.McpSlug.String, makeToolsListBody(), f.bearer, dynamicModeHeaders)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return toolNames(parseToolsListResponse(t, w.Body.Bytes())), w.Body.String()
}

// structuredToolNames reads the tool names out of a search_tools or
// describe_tools result.
func structuredToolNames(t *testing.T, body string) []string {
	t.Helper()

	var resp struct {
		Result struct {
			StructuredContent struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &resp), body)
	names := make([]string, 0, len(resp.Result.StructuredContent.Tools))
	for _, tool := range resp.Result.StructuredContent.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

// executionFailed is how the fixture's HTTP tools fail once authorization has
// let a call through: they have no upstream address configured, so execution
// itself fails. A refused call never gets that far.
const executionFailed = "failed to execute tool call"

func TestServePublic_PrivateDynamic_DispositionGrantRevealsOnlyClassifiedTools(t *testing.T) {
	t.Parallel()

	ctx, ti, f := newDynamicRBACFixture(t)
	seedMockUserToolsetGrant(t, ctx, ti, f.toolset.ID, map[string]string{authz.SelectorKeyDisposition: authz.DispositionReadOnly})

	names, listBody := dynamicListNames(t, ti, f)
	require.ElementsMatch(t, []string{"search_tools", "describe_tools", "execute_tool"}, names)
	// The facade's examples name only the tool this caller may call.
	require.Contains(t, listBody, "reader")
	require.NotContains(t, listBody, "purger")
	require.NotContains(t, listBody, "writer")

	// Every tool is an equally good match, so the search returns all three
	// before the caller's view is applied.
	search := serveDynamic(t, ti, f, dynamicToolsCall(t, "search_tools", map[string]any{"query": "tools"}))
	require.Equal(t, []string{"reader"}, structuredToolNames(t, search))
	require.NotContains(t, search, "purger")
	require.NotContains(t, search, "writer")

	describe := serveDynamic(t, ti, f, dynamicToolsCall(t, "describe_tools", map[string]any{"tool_names": []string{"reader", "purger", "writer", "missing"}}))
	require.Equal(t, []string{"reader"}, structuredToolNames(t, describe))
	require.NotContains(t, describe, "purger")
	require.NotContains(t, describe, "writer")

	allowed := serveDynamic(t, ti, f, dynamicToolsCall(t, "execute_tool", map[string]any{"name": "reader", "arguments": map[string]any{}}))
	require.Contains(t, allowed, executionFailed, "an allowed execution reaches the tool")
	for _, denied := range []string{"purger", "writer"} {
		out := serveDynamic(t, ti, f, dynamicToolsCall(t, "execute_tool", map[string]any{"name": denied, "arguments": map[string]any{}}))
		require.Contains(t, out, "permission", "execute_tool %s must be refused", denied)
		require.NotContains(t, out, executionFailed)

		direct := serveDynamic(t, ti, f, dynamicToolsCall(t, denied, map[string]any{}))
		require.Contains(t, direct, "permission", "a direct call to %s must be refused", denied)
	}
}

// A caller who may enter the server but call none of its tools gets no
// facade, and a guessed facade call is refused before the search service is
// reached.
func TestServePublic_PrivateDynamic_NoAuthorizedToolsMeansNoFacade(t *testing.T) {
	t.Parallel()

	ctx, ti, f := newDynamicRBACFixture(t)
	seedMockUserToolsetGrant(t, ctx, ti, f.toolset.ID, map[string]string{authz.SelectorKeyTool: "not_a_tool"})

	names, _ := dynamicListNames(t, ti, f)
	require.Empty(t, names)

	for _, call := range [][]byte{
		dynamicToolsCall(t, "search_tools", map[string]any{"query": "tools"}),
		dynamicToolsCall(t, "describe_tools", map[string]any{"tool_names": []string{"reader"}}),
		dynamicToolsCall(t, "execute_tool", map[string]any{"name": "reader", "arguments": map[string]any{}}),
	} {
		out := serveDynamic(t, ti, f, call)
		require.Contains(t, out, "permission")
		require.NotContains(t, out, "reader description")
	}
	require.Zero(t, f.embeddings.calls.Load(), "the search service must not be reached")
}

// Naming an unannotated tool gives a usable dynamic session for it, and
// naming only a facade tool reveals nothing.
func TestServePublic_PrivateDynamic_NamedGrants(t *testing.T) {
	t.Parallel()

	t.Run("unannotated tool", func(t *testing.T) {
		t.Parallel()

		ctx, ti, f := newDynamicRBACFixture(t)
		seedMockUserToolsetGrant(t, ctx, ti, f.toolset.ID, map[string]string{authz.SelectorKeyTool: "writer"})

		names, _ := dynamicListNames(t, ti, f)
		require.ElementsMatch(t, []string{"search_tools", "describe_tools", "execute_tool"}, names)

		describe := serveDynamic(t, ti, f, dynamicToolsCall(t, "describe_tools", map[string]any{"tool_names": []string{"reader", "writer"}}))
		require.Equal(t, []string{"writer"}, structuredToolNames(t, describe))

		allowed := serveDynamic(t, ti, f, dynamicToolsCall(t, "execute_tool", map[string]any{"name": "writer", "arguments": map[string]any{}}))
		require.Contains(t, allowed, executionFailed)
	})

	t.Run("facade tool only", func(t *testing.T) {
		t.Parallel()

		ctx, ti, f := newDynamicRBACFixture(t)
		seedMockUserToolsetGrant(t, ctx, ti, f.toolset.ID, map[string]string{authz.SelectorKeyTool: "search_tools"})

		names, _ := dynamicListNames(t, ti, f)
		require.Empty(t, names)

		out := serveDynamic(t, ti, f, dynamicToolsCall(t, "search_tools", map[string]any{"query": "tools"}))
		require.Contains(t, out, "permission")
		require.Zero(t, f.embeddings.calls.Load())
	})
}

// dynamicListBody lists tools in dynamic mode at the server's address plus
// query, with bearer, and returns the raw response.
func dynamicListBody(t *testing.T, ti *testInstance, f dynamicRBACFixture, query string, bearer string) string {
	t.Helper()

	body := makeToolsListBody()
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+f.toolset.McpSlug.String+query, bytes.NewReader(body))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	for k, v := range dynamicModeHeaders {
		req.Header.Set(k, v)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", f.toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.ServePublic(w, req))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return w.Body.String()
}

// Search tag suggestions come from the whole toolset's index, so they must
// not appear when anything narrowed this caller's catalog, including filters
// that ran before the per-tool checks and left only allowed tools behind.
func TestServePublic_PrivateDynamic_TagSuggestionsNeverRevealWithheldTools(t *testing.T) {
	t.Parallel()

	ctx, ti, f := newDynamicRBACFixture(t)
	seedMockUserToolsetGrant(t, ctx, ti, f.toolset.ID, map[string]string{authz.SelectorKeyDisposition: authz.DispositionReadOnly})
	readerOnly := fmt.Appendf(nil, `{"resource":"toolset:%s","grant_id":"%s","allow":[{"type":"tool","name":"reader"}]}`, f.toolset.ID, uuid.NewString())

	for name, list := range map[string]string{
		"rbac":          dynamicListBody(t, ti, f, "", f.bearer),
		"url tags":      dynamicListBody(t, ti, f, "?tags="+dynamicToolTags["reader"], f.bearer),
		"consent":       dynamicListBody(t, ti, f, "", f.mintBearer(t, ctx, ti, readerOnly)),
		"url + consent": dynamicListBody(t, ti, f, "?tags="+dynamicToolTags["reader"], f.mintBearer(t, ctx, ti, readerOnly)),
	} {
		require.Contains(t, list, "search_tools", name)
		require.Contains(t, list, "reader", name)
		for _, withheld := range []string{"purger", "writer"} {
			require.NotContains(t, list, dynamicToolTags[withheld], "%s: the search tool's description and schema must not name a withheld tool's tag", name)
		}
	}
}

// A caller who may call every tool still gets the toolset's tag suggestions.
func TestServePublic_PrivateDynamic_TagSuggestionsForUnrestrictedCaller(t *testing.T) {
	t.Parallel()

	ctx, ti, f := newDynamicRBACFixture(t)
	seedMockUserToolsetGrant(t, ctx, ti, f.toolset.ID, nil)

	list := dynamicListBody(t, ti, f, "", f.bearer)
	for _, tag := range dynamicToolTags {
		require.Contains(t, list, tag)
	}
}
