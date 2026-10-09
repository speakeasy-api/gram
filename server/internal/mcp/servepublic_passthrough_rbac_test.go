package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	externalmcp_types "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	tools_repo "github.com/speakeasy-api/gram/server/internal/tools/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// passthroughSlug prefixes every tool the upstream lists through the proxy.
const passthroughSlug = "devices"

// countingUpstream fronts the fixture MCP server and records each tools/call
// it forwards, so a test can prove a refused call never reached it.
type countingUpstream struct {
	mu     sync.Mutex
	bodies []string
}

func (c *countingUpstream) toolCalls(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, body := range c.bodies {
		if strings.Contains(body, `"tools/call"`) && strings.Contains(body, `"name":"`+name+`"`) {
			n++
		}
	}
	return n
}

func newCountingUpstream(t *testing.T, target string) (*countingUpstream, string) {
	t.Helper()

	targetURL, err := url.Parse(target)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	counter := &countingUpstream{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		counter.mu.Lock()
		counter.bodies = append(counter.bodies, string(body))
		counter.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return counter, server.URL
}

// passthroughFixture is a private hosted toolset holding an external MCP
// proxy (whose upstream lists read_data with readOnlyHint, drop_data with
// destructiveHint and plain_data with no hints) beside a materialized
// read-only HTTP tool, lookup, with a bearer for the mock user.
type passthroughFixture struct {
	toolset toolsets_repo.Toolset
	// slug is the external MCP source slug that prefixes passthrough tools.
	slug     string
	bearer   string
	upstream *countingUpstream
}

func newPassthroughFixture(t *testing.T) (context.Context, *testInstance, passthroughFixture) {
	t.Helper()
	return newPassthroughFixtureWith(t, true)
}

// newPassthroughFixtureWith builds the fixture, leaving out the materialized
// lookup tool when withLookup is false so the toolset holds only the proxy.
func newPassthroughFixtureWith(t *testing.T, withLookup bool) (context.Context, *testInstance, passthroughFixture) {
	t.Helper()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	destructive := true
	mock := newMockExternalMCPServer(t, externalmcp_types.TransportTypeStreamableHTTP, []testmcp.Tool{
		{
			Name: "read_data", Description: "Reads data", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
			Response:    testmcp.ToolResponse{Content: []map[string]any{{"type": "text", "text": "read_data result"}}},
		},
		{
			Name: "drop_data", Description: "Drops data", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive},
			Response:    testmcp.ToolResponse{Content: []map[string]any{{"type": "text", "text": "drop_data result"}}},
		},
		{
			Name: "plain_data", Description: "Plain data", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			Response: testmcp.ToolResponse{Content: []map[string]any{{"type": "text", "text": "plain_data result"}}},
		},
	})
	t.Cleanup(mock.Close)
	upstream, upstreamURL := newCountingUpstream(t, mock.URL)

	cfg := setupToolsetWithExternalMCP(t, ctx, ti, upstreamURL, externalmcp_types.TransportTypeStreamableHTTP, passthroughSlug+"-"+uuid.NewString()[:6])
	if withLookup {
		addReadOnlyLookupTool(t, ctx, ti, cfg)
	}
	setToolsetMcpPrivate(t, ctx, ti, cfg.toolset.ID, *authCtx.ProjectID)

	issuer, err := usersessions_repo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessions_repo.CreateUserSessionIssuerParams{
		ProjectID:          cfg.toolset.ProjectID,
		Slug:               cfg.toolset.Slug + "-issuer",
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(24 * time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	toolset, err := toolsets_repo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsets_repo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Slug:                cfg.toolset.Slug,
		ProjectID:           cfg.toolset.ProjectID,
	})
	require.NoError(t, err)

	subject := urn.NewUserSubject(mockidp.MockUserID)
	bearer, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  subject,
		Audience: urn.NewToolset(toolset.ID).String(),
		Issuer:   "https://test.example",
		Lifetime: time.Hour,
		ClientID: "test-client",
	})
	require.NoError(t, err)
	now := time.Now()
	_, err = usersessions_repo.New(ti.conn).CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: issuer.ID,
		SubjectUrn:          subject,
		Jti:                 jti,
		RefreshTokenHash:    conv.ToPGText("passthrough-rbac-" + uuid.NewString()),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true},
	})
	require.NoError(t, err)

	return ctx, ti, passthroughFixture{toolset: toolset, slug: cfg.slug, bearer: bearer, upstream: upstream}
}

// addReadOnlyLookupTool adds a materialized HTTP tool with a stored
// readOnlyHint beside the proxy, so classification of ordinary tools is
// shown to be unaffected.
func addReadOnlyLookupTool(t *testing.T, ctx context.Context, ti *testInstance, cfg *externalMCPConfig) {
	t.Helper()

	toolURN := urn.NewTool(urn.ToolKindHTTP, "lookup-fixture", uuid.NewString()[:8])
	require.NoError(t, tools_repo.New(ti.conn).CreateHTTPToolDefinition(ctx, tools_repo.CreateHTTPToolDefinitionParams{
		ProjectID:      cfg.toolset.ProjectID,
		DeploymentID:   cfg.deploymentID,
		ToolUrn:        toolURN,
		Name:           "lookup",
		Summary:        "Lookup",
		Description:    "Looks something up",
		Tags:           []string{},
		HttpMethod:     http.MethodGet,
		Path:           "/lookup",
		SchemaVersion:  "3.0.0",
		Schema:         []byte(`{}`),
		ServerEnvVar:   "TEST_SERVER_URL",
		Security:       []byte(`[]`),
		HeaderSettings: []byte(`{}`),
		QuerySettings:  []byte(`{}`),
		PathSettings:   []byte(`{}`),
		ReadOnlyHint:   pgtype.Bool{Bool: true, Valid: true},
	}))

	proxyURN, err := urn.ParseTool(cfg.toolURN)
	require.NoError(t, err)
	latest, err := toolsets_repo.New(ti.conn).GetLatestToolsetVersion(ctx, cfg.toolset.ID)
	require.NoError(t, err)
	_, err = toolsets_repo.New(ti.conn).CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID:     cfg.toolset.ID,
		Version:       latest.Version + 1,
		ToolUrns:      []urn.Tool{toolURN, proxyURN},
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{UUID: latest.ID, Valid: true},
	})
	require.NoError(t, err)
}

func seedMockUserToolsetBlock(t *testing.T, ctx context.Context, ti *testInstance, toolsetID uuid.UUID, narrowing map[string]string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	selector := authz.NewSelector(authz.ScopeMCPBlockedConnect, toolsetID.String())
	maps.Copy(selector, narrowing)
	selectors, err := selector.MarshalJSON()
	require.NoError(t, err)

	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, mockidp.MockUserID),
		Scope:          string(authz.ScopeMCPBlockedConnect),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

func passthroughName(f passthroughFixture, tool string) string {
	return f.slug + "--" + tool
}

// listedTools returns the listed tools by name with their raw annotations.
func listedTools(t *testing.T, ti *testInstance, f passthroughFixture) map[string]json.RawMessage {
	t.Helper()

	w, err := servePublicHTTP(t, context.Background(), ti, f.toolset.McpSlug.String, makeToolsListBody(), f.bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		Result struct {
			Tools []struct {
				Name        string          `json:"name"`
				Annotations json.RawMessage `json:"annotations"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	out := make(map[string]json.RawMessage, len(resp.Result.Tools))
	for _, tool := range resp.Result.Tools {
		out[tool.Name] = tool.Annotations
	}
	return out
}

func passthroughCall(t *testing.T, ti *testInstance, f passthroughFixture, name string) string {
	t.Helper()
	return hostedCallOutcome(t, ti, f.toolset.McpSlug.String, f.bearer, name)
}

func requirePassthroughDenied(t *testing.T, ti *testInstance, f passthroughFixture, tool string) {
	t.Helper()

	before := f.upstream.toolCalls(tool)
	out := passthroughCall(t, ti, f, passthroughName(f, tool))
	require.Contains(t, out, "permission", "%s must be refused", tool)
	require.Equal(t, before, f.upstream.toolCalls(tool), "a refused %s call must never reach the upstream", tool)
}

func requirePassthroughAllowed(t *testing.T, ti *testInstance, f passthroughFixture, tool string) {
	t.Helper()

	before := f.upstream.toolCalls(tool)
	out := passthroughCall(t, ti, f, passthroughName(f, tool))
	require.Contains(t, out, tool+" result", "%s must run upstream", tool)
	require.Equal(t, before+1, f.upstream.toolCalls(tool))
}

// Live passthrough tools are unclassified for authorization whatever the
// upstream advertises, so tools/list and tools/call agree on them: annotation
// rules neither list nor call them, while rules naming them or covering the
// whole server do. A materialized tool keeps its stored classification.
func TestServePublic_PrivatePassthrough_AuthorizedAsUnclassified(t *testing.T) {
	t.Parallel()

	t.Run("read-only rule", func(t *testing.T) {
		t.Parallel()

		ctx, ti, f := newPassthroughFixture(t)
		seedMockUserToolsetGrant(t, ctx, ti, f.toolset.ID, map[string]string{authz.SelectorKeyDisposition: authz.DispositionReadOnly})

		listed := listedTools(t, ti, f)
		require.Contains(t, listed, "lookup", "the materialized read-only tool stays classified")
		for _, tool := range []string{"read_data", "drop_data", "plain_data"} {
			require.NotContains(t, listed, passthroughName(f, tool))
			requirePassthroughDenied(t, ti, f, tool)
		}
		require.Contains(t, passthroughCall(t, ti, f, "lookup"), executionFailed)
	})

	t.Run("rule naming one passthrough tool", func(t *testing.T) {
		t.Parallel()

		ctx, ti, f := newPassthroughFixture(t)
		seedMockUserToolsetGrant(t, ctx, ti, f.toolset.ID, map[string]string{authz.SelectorKeyTool: passthroughName(f, "read_data")})

		listed := listedTools(t, ti, f)
		require.Len(t, listed, 1)
		annotations, ok := listed[passthroughName(f, "read_data")]
		require.True(t, ok)
		require.Contains(t, string(annotations), `"readOnlyHint":true`, "the upstream's annotations are still shown")

		requirePassthroughAllowed(t, ti, f, "read_data")
		requirePassthroughDenied(t, ti, f, "drop_data")
		requirePassthroughDenied(t, ti, f, "plain_data")
	})

	t.Run("whole-server rule", func(t *testing.T) {
		t.Parallel()

		ctx, ti, f := newPassthroughFixture(t)
		seedMockUserToolsetGrant(t, ctx, ti, f.toolset.ID, nil)

		listed := listedTools(t, ti, f)
		for _, tool := range []string{"read_data", "drop_data", "plain_data"} {
			require.Contains(t, listed, passthroughName(f, tool))
		}
		requirePassthroughAllowed(t, ti, f, "drop_data")
	})

	t.Run("rule naming a passthrough tool and an annotation", func(t *testing.T) {
		t.Parallel()

		ctx, ti, f := newPassthroughFixture(t)
		seedMockUserToolsetGrant(t, ctx, ti, f.toolset.ID, map[string]string{
			authz.SelectorKeyTool:        passthroughName(f, "read_data"),
			authz.SelectorKeyDisposition: authz.DispositionReadOnly,
		})

		require.Empty(t, listedTools(t, ti, f))
		requirePassthroughDenied(t, ti, f, "read_data")
	})

	t.Run("annotation exclusion does not match, name exclusion does", func(t *testing.T) {
		t.Parallel()

		ctx, ti, f := newPassthroughFixture(t)
		seedMockUserToolsetGrant(t, ctx, ti, f.toolset.ID, nil)
		seedMockUserToolsetBlock(t, ctx, ti, f.toolset.ID, map[string]string{authz.SelectorKeyDisposition: authz.DispositionDestructive})

		require.Contains(t, listedTools(t, ti, f), passthroughName(f, "drop_data"))
		requirePassthroughAllowed(t, ti, f, "drop_data")

		seedMockUserToolsetBlock(t, ctx, ti, f.toolset.ID, map[string]string{authz.SelectorKeyTool: passthroughName(f, "drop_data")})

		require.NotContains(t, listedTools(t, ti, f), passthroughName(f, "drop_data"))
		requirePassthroughDenied(t, ti, f, "drop_data")
	})
}

// dynamicExecute calls tool through the dynamic-mode execute_tool wrapper and
// returns everything the request said.
func dynamicExecute(t *testing.T, ti *testInstance, f passthroughFixture, tool string) string {
	t.Helper()

	body := dynamicToolsCall(t, "execute_tool", map[string]any{"name": passthroughName(f, tool), "arguments": map[string]any{}})
	w, err := servePublicHTTP(t, context.Background(), ti, f.toolset.McpSlug.String, body, f.bearer, dynamicModeHeaders)
	out := w.Body.String()
	if err != nil {
		out += err.Error()
	}
	return out
}

// Dynamic discovery can't count passthrough tools, but execute_tool is not a
// discovery tool: it unwraps to its target, which is authorized like a direct
// call. An authorized passthrough target runs; an unauthorized one is refused
// without reaching the upstream.
func TestServePublic_PrivatePassthrough_DynamicExecuteAuthorizesTheTarget(t *testing.T) {
	t.Parallel()

	t.Run("proxy-only toolset, whole-server rule", func(t *testing.T) {
		t.Parallel()

		ctx, ti, f := newPassthroughFixtureWith(t, false)
		seedMockUserToolsetGrant(t, ctx, ti, f.toolset.ID, nil)

		before := f.upstream.toolCalls("read_data")
		require.Contains(t, dynamicExecute(t, ti, f, "read_data"), "read_data result")
		require.Equal(t, before+1, f.upstream.toolCalls("read_data"))
	})

	t.Run("mixed toolset, rule naming one passthrough tool", func(t *testing.T) {
		t.Parallel()

		ctx, ti, f := newPassthroughFixture(t)
		seedMockUserToolsetGrant(t, ctx, ti, f.toolset.ID, map[string]string{authz.SelectorKeyTool: passthroughName(f, "read_data")})

		before := f.upstream.toolCalls("read_data")
		require.Contains(t, dynamicExecute(t, ti, f, "read_data"), "read_data result")
		require.Equal(t, before+1, f.upstream.toolCalls("read_data"))

		beforeDenied := f.upstream.toolCalls("drop_data")
		require.Contains(t, dynamicExecute(t, ti, f, "drop_data"), "permission")
		require.Equal(t, beforeDenied, f.upstream.toolCalls("drop_data"), "a refused call must never reach the upstream")
	})

	t.Run("annotation rule does not reach a passthrough target", func(t *testing.T) {
		t.Parallel()

		ctx, ti, f := newPassthroughFixtureWith(t, false)
		seedMockUserToolsetGrant(t, ctx, ti, f.toolset.ID, map[string]string{authz.SelectorKeyDisposition: authz.DispositionReadOnly})

		before := f.upstream.toolCalls("read_data")
		require.Contains(t, dynamicExecute(t, ti, f, "read_data"), "permission")
		require.Equal(t, before, f.upstream.toolCalls("read_data"))
	})
}

// dynamicListOutcome lists tools in dynamic mode and returns the listed names
// and the JSON-RPC error message, if any.
func dynamicListOutcome(t *testing.T, ti *testInstance, f passthroughFixture) ([]string, string) {
	t.Helper()

	w, err := servePublicHTTP(t, context.Background(), ti, f.toolset.McpSlug.String, makeToolsListBody(), f.bearer, dynamicModeHeaders)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		Result *struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	if resp.Error != nil {
		return nil, resp.Error.Message
	}
	require.NotNil(t, resp.Result, w.Body.String())
	names := make([]string, 0, len(resp.Result.Tools))
	for _, tool := range resp.Result.Tools {
		names = append(names, tool.Name)
	}
	return names, ""
}

// Dynamic discovery does not support catalogs that still hold external-MCP
// passthrough tools, as on main: main's facade build rejects a passthrough
// placeholder (or reports the search index unavailable first), so no dynamic
// facade is ever listed for them. Under private per-tool enforcement the list
// is empty when no materialized tool is authorized; otherwise the same
// discovery error as main remains. execute_tool still authorizes its target
// like a direct call, so list and call deliberately disagree here, exactly as
// they did on main.
func TestServePublic_PrivatePassthrough_DynamicDiscoveryIsUnsupported(t *testing.T) {
	t.Parallel()

	type role struct {
		name      string
		narrowing func(f passthroughFixture) map[string]string
	}
	wholeServer := role{"whole server", func(passthroughFixture) map[string]string { return nil }}
	named := role{"named passthrough tool", func(f passthroughFixture) map[string]string {
		return map[string]string{authz.SelectorKeyTool: passthroughName(f, "read_data")}
	}}
	readOnly := role{"read-only annotation", func(passthroughFixture) map[string]string {
		return map[string]string{authz.SelectorKeyDisposition: authz.DispositionReadOnly}
	}}

	for _, tc := range []struct {
		catalog     string
		withLookup  bool
		role        role
		listErrors  bool
		readAllowed bool
	}{
		{"proxy-only", false, wholeServer, false, true},
		{"proxy-only", false, named, false, true},
		{"proxy-only", false, readOnly, false, false},
		// lookup is authorized, so discovery is attempted and fails on the
		// passthrough placeholder as on main.
		{"mixed", true, wholeServer, true, true},
		{"mixed", true, named, false, true},
		{"mixed", true, readOnly, true, false},
	} {
		t.Run(tc.catalog+", "+tc.role.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti, f := newPassthroughFixtureWith(t, tc.withLookup)
			seedMockUserToolsetGrant(t, ctx, ti, f.toolset.ID, tc.role.narrowing(f))

			names, listErr := dynamicListOutcome(t, ti, f)
			require.Empty(t, names, "no dynamic facade is listed for a passthrough catalog")
			if tc.listErrors {
				require.NotEmpty(t, listErr, "an authorized materialized tool leaves main's discovery error in place")
			} else {
				require.Empty(t, listErr)
			}

			before := f.upstream.toolCalls("read_data")
			out := dynamicExecute(t, ti, f, "read_data")
			if tc.readAllowed {
				require.Contains(t, out, "read_data result")
				require.Equal(t, before+1, f.upstream.toolCalls("read_data"))
			} else {
				require.Contains(t, out, "permission")
				require.Equal(t, before, f.upstream.toolCalls("read_data"), "a refused call must never reach the upstream")
			}

			beforeDrop := f.upstream.toolCalls("drop_data")
			drop := dynamicExecute(t, ti, f, "drop_data")
			if tc.role.name == wholeServer.name {
				require.Contains(t, drop, "drop_data result")
				require.Equal(t, beforeDrop+1, f.upstream.toolCalls("drop_data"))
			} else {
				require.Contains(t, drop, "permission")
				require.Equal(t, beforeDrop, f.upstream.toolCalls("drop_data"), "a refused call must never reach the upstream")
			}
		})
	}
}
