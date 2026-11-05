package remotemcp

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/tooldisposition"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	catalogTestServerID  = "11111111-1111-1111-1111-111111111111"
	catalogTestProjectID = "22222222-2222-2222-2222-222222222222"
	catalogTestOrgID     = "org_catalog"
)

// fakeToolCatalogStore is an in-memory catalog deriving dispositions the way the
// production cache does for the hints these tests use.
type fakeToolCatalogStore struct {
	mu        sync.Mutex
	catalog   map[string]string
	writes    [][]tooldisposition.ToolRecord
	recordErr error
}

func (f *fakeToolCatalogStore) Dispositions(ctx context.Context, mcpServerID, projectID string) (map[string]string, error) {
	return f.ToolCatalog(ctx, mcpServerID, projectID)
}

func (f *fakeToolCatalogStore) ToolCatalog(context.Context, string, string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.catalog), nil
}

func (f *fakeToolCatalogStore) RecordMissingTools(_ context.Context, _, _, _ string, tools []tooldisposition.ToolRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recordErr != nil {
		return f.recordErr
	}
	f.writes = append(f.writes, tools)
	for _, tool := range tools {
		if _, recorded := f.catalog[tool.ToolName]; recorded {
			continue
		}
		disposition := ""
		if tool.DestructiveHint != nil && *tool.DestructiveHint {
			disposition = authz.DispositionDestructive
		}
		f.catalog[tool.ToolName] = disposition
	}
	return nil
}

func newCatalogTestEngine(t *testing.T) *authz.Engine {
	t.Helper()
	return authz.NewEngine(testenv.NewLogger(t), nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
}

func destructiveTool(name string) *mcp.Tool {
	return &mcp.Tool{Name: name, InputSchema: map[string]any{}, Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true)}}
}

func blockDestructiveContext(t *testing.T) context.Context {
	t.Helper()

	sessionID := "session_catalog"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: catalogTestOrgID,
		UserID:               "user_catalog",
		SessionID:            &sessionID,
		AccountType:          "enterprise",
	})
	return authztest.WithExactGrants(t, ctx,
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, authz.Selector{"resource_kind": "mcp", "resource_id": catalogTestServerID}),
		authz.NewGrantWithSelector(authz.ScopeMCPBlockedConnect, authz.Selector{"resource_kind": "mcp", "resource_id": catalogTestServerID, "disposition": authz.DispositionDestructive}),
	)
}

func newCatalogSync(store *fakeToolCatalogStore, list func(context.Context, *http.Request) ([]*mcp.Tool, error)) *toolCatalogSync {
	return &toolCatalogSync{
		store:          store,
		refreshes:      newToolCatalogRefreshes(),
		organizationID: catalogTestOrgID,
		mcpServerID:    catalogTestServerID,
		projectID:      catalogTestProjectID,
		listUpstream:   list,
	}
}

func newCatalogCall(t *testing.T, name string) *proxy.ToolsCallRequest {
	t.Helper()

	return &proxy.ToolsCallRequest{
		Params:      &mcp.CallToolParamsRaw{Name: name},
		UserRequest: &proxy.UserRequest{UserHTTPRequest: httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)},
	}
}

func newAuthzInterceptor(t *testing.T, store *fakeToolCatalogStore, catalog *toolCatalogSync) *ToolsCallAuthzInterceptor {
	t.Helper()

	interceptor := NewToolsCallAuthzInterceptor(newCatalogTestEngine(t), store, catalogTestServerID, catalogTestProjectID, testenv.NewLogger(t))
	interceptor.catalog = catalog
	return interceptor
}

func TestToolsCallCatalog_UnrecordedDestructiveToolIsRecordedThenBlocked(t *testing.T) {
	t.Parallel()

	store := &fakeToolCatalogStore{catalog: map[string]string{}}
	listings := 0
	catalog := newCatalogSync(store, func(context.Context, *http.Request) ([]*mcp.Tool, error) {
		listings++
		return []*mcp.Tool{destructiveTool("delete_repository")}, nil
	})

	err := newAuthzInterceptor(t, store, catalog).InterceptToolsCallRequest(blockDestructiveContext(t), newCatalogCall(t, "delete_repository"))

	require.Error(t, err, "a block on destructive tools must apply to a tool the catalog had not seen")
	require.Equal(t, 1, listings)
	require.Equal(t, authz.DispositionDestructive, store.catalog["delete_repository"])
}

func TestToolsCallCatalog_RecordedToolDoesNotListUpstream(t *testing.T) {
	t.Parallel()

	// An entry with no behavior class is still recorded, so it must not trigger
	// a listing on every call.
	store := &fakeToolCatalogStore{catalog: map[string]string{"ping": ""}}
	catalog := newCatalogSync(store, func(context.Context, *http.Request) ([]*mcp.Tool, error) {
		t.Fatal("a recorded tool must not list the upstream")
		return nil, nil
	})

	require.NoError(t, newAuthzInterceptor(t, store, catalog).InterceptToolsCallRequest(blockDestructiveContext(t), newCatalogCall(t, "ping")))
}

func TestToolsCallCatalog_ToolMissingFromUpstreamIsRefused(t *testing.T) {
	t.Parallel()

	store := &fakeToolCatalogStore{catalog: map[string]string{}}
	catalog := newCatalogSync(store, func(context.Context, *http.Request) ([]*mcp.Tool, error) {
		return []*mcp.Tool{{Name: "other", InputSchema: map[string]any{}}}, nil
	})

	err := newAuthzInterceptor(t, store, catalog).InterceptToolsCallRequest(blockDestructiveContext(t), newCatalogCall(t, "invented"))

	var reject *proxy.RejectError
	require.ErrorAs(t, err, &reject)
	require.Equal(t, proxy.RejectCodeInvalidParams, reject.Code)
}

func TestToolsCallCatalog_FailsClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		listErr   error
		recordErr error
	}{
		{name: "upstream listing fails", listErr: errors.New("upstream down"), recordErr: nil},
		{name: "catalog write fails", listErr: nil, recordErr: errors.New("database down")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeToolCatalogStore{catalog: map[string]string{}, recordErr: tt.recordErr}
			catalog := newCatalogSync(store, func(context.Context, *http.Request) ([]*mcp.Tool, error) {
				return []*mcp.Tool{{Name: "search", InputSchema: map[string]any{}}}, tt.listErr
			})

			err := newAuthzInterceptor(t, store, catalog).InterceptToolsCallRequest(blockDestructiveContext(t), newCatalogCall(t, "search"))
			require.Error(t, err, "a tool the rules could not be checked against must not be called")
		})
	}
}

func TestToolsCallCatalog_InventedNamesAreBoundedByCooldown(t *testing.T) {
	t.Parallel()

	store := &fakeToolCatalogStore{catalog: map[string]string{}}
	var listings atomic.Int64
	catalog := newCatalogSync(store, func(context.Context, *http.Request) ([]*mcp.Tool, error) {
		listings.Add(1)
		return nil, nil
	})
	now := time.Unix(1_700_000_000, 0)
	catalog.refreshes.now = func() time.Time { return now }
	ctx := blockDestructiveContext(t)

	for _, name := range []string{"a", "b", "c"} {
		_, err := catalog.callDisposition(ctx, newCatalogCall(t, name))
		require.ErrorIs(t, err, errToolNotOnUpstream)
	}
	require.EqualValues(t, 1, listings.Load(), "different names must share one listing within the cooldown")

	now = now.Add(toolCatalogRefreshCooldown)
	_, err := catalog.callDisposition(ctx, newCatalogCall(t, "d"))
	require.ErrorIs(t, err, errToolNotOnUpstream)
	require.EqualValues(t, 2, listings.Load())
}

func TestToolsCallCatalog_FailedRefreshDoesNotStartTheCooldown(t *testing.T) {
	t.Parallel()

	store := &fakeToolCatalogStore{catalog: map[string]string{}}
	var listErr atomic.Pointer[error]
	refused := errors.New("expired credential")
	listErr.Store(&refused)
	catalog := newCatalogSync(store, func(context.Context, *http.Request) ([]*mcp.Tool, error) {
		return []*mcp.Tool{{Name: "search", InputSchema: map[string]any{}}}, *listErr.Load()
	})
	ctx := blockDestructiveContext(t)

	_, err := catalog.callDisposition(ctx, newCatalogCall(t, "search"))
	require.ErrorIs(t, err, refused)

	var repaired error
	listErr.Store(&repaired)
	_, err = catalog.callDisposition(ctx, newCatalogCall(t, "search"))
	require.NoError(t, err, "the caller repaired their credential, so the tool must be found straight away")
}

func TestToolsCallCatalog_ConcurrentCallsShareOneListing(t *testing.T) {
	t.Parallel()

	store := &fakeToolCatalogStore{catalog: map[string]string{}}
	release := make(chan struct{})
	var listings atomic.Int64
	catalog := newCatalogSync(store, func(context.Context, *http.Request) ([]*mcp.Tool, error) {
		listings.Add(1)
		<-release
		return []*mcp.Tool{{Name: "search", InputSchema: map[string]any{}}}, nil
	})
	ctx := blockDestructiveContext(t)

	const callers = 5
	results := make(chan error, callers)
	for range callers {
		go func() {
			_, err := catalog.callDisposition(ctx, newCatalogCall(t, "search"))
			results <- err
		}()
	}

	require.Eventually(t, func() bool { return listings.Load() == 1 }, 5*time.Second, time.Millisecond)
	close(release)
	for range callers {
		require.NoError(t, <-results, "every caller must see the tool the shared listing recorded")
	}
	require.EqualValues(t, 1, listings.Load())
}

func newListedResponse(t *testing.T, payload string) *proxy.ToolsListResponse {
	t.Helper()

	result := &mcp.ListToolsResult{}
	require.NoError(t, json.Unmarshal([]byte(payload), result))
	return &proxy.ToolsListResponse{
		RemoteMessage: &proxy.RemoteMessage{Message: &jsonrpc.Response{Result: json.RawMessage(payload)}},
		Result:        result,
	}
}

func TestToolsListCatalog_RecordsMissingToolsThenFilters(t *testing.T) {
	t.Parallel()

	store := &fakeToolCatalogStore{catalog: map[string]string{"list_issues": authz.DispositionReadOnly}}
	interceptor := NewToolsListMCPConnectFilterInterceptor(newCatalogTestEngine(t), store, catalogTestServerID, catalogTestProjectID, testenv.NewLogger(t))
	interceptor.catalog = newCatalogSync(store, nil)

	resp := newListedResponse(t, `{"tools":[
		{"name":"list_issues","inputSchema":{"type":"object"}},
		{"name":"delete_repository","inputSchema":{"type":"object"},"annotations":{"destructiveHint":true,"readOnlyHint":false}}
	]}`)
	require.NoError(t, interceptor.InterceptToolsListResponse(blockDestructiveContext(t), resp))

	require.Len(t, resp.Result.Tools, 1, "the newly recorded destructive tool must be filtered in the same response")
	require.Equal(t, "list_issues", resp.Result.Tools[0].Name)

	require.Len(t, store.writes, 1)
	require.Len(t, store.writes[0], 1, "only the tool with no entry is written")
	written := store.writes[0][0]
	require.Equal(t, "delete_repository", written.ToolName)
	require.NotNil(t, written.ReadOnlyHint, "an explicit false must stay distinct from unset")
	require.False(t, *written.ReadOnlyHint)
}

func TestToolsListCatalog_NoWriteWhenEveryToolIsRecorded(t *testing.T) {
	t.Parallel()

	store := &fakeToolCatalogStore{catalog: map[string]string{"list_issues": ""}}

	resp := newListedResponse(t, `{"tools":[{"name":"list_issues","inputSchema":{"type":"object"}}]}`)
	require.NoError(t, newCatalogSync(store, nil).recordListed(t.Context(), resp))
	require.Empty(t, store.writes)
}

func TestToolsListCatalog_WriteFailureRejectsTheListing(t *testing.T) {
	t.Parallel()

	store := &fakeToolCatalogStore{catalog: map[string]string{}, recordErr: errors.New("database down")}
	interceptor := NewToolsListMCPConnectFilterInterceptor(newCatalogTestEngine(t), store, catalogTestServerID, catalogTestProjectID, testenv.NewLogger(t))
	interceptor.catalog = newCatalogSync(store, nil)

	resp := newListedResponse(t, `{"tools":[{"name":"delete_repository","inputSchema":{"type":"object"}}]}`)
	require.Error(t, interceptor.InterceptToolsListResponse(blockDestructiveContext(t), resp))
}

func TestToolsListCatalog_NullToolIsRejected(t *testing.T) {
	t.Parallel()

	store := &fakeToolCatalogStore{catalog: map[string]string{}}

	resp := newListedResponse(t, `{"tools":[null]}`)
	require.Error(t, newCatalogSync(store, nil).recordListed(t.Context(), resp))
	require.Empty(t, store.writes)
}

func TestToolsCallCatalog_PaddedToolNameReadsTheStoredRule(t *testing.T) {
	t.Parallel()

	store := &fakeToolCatalogStore{catalog: map[string]string{"delete_repository": authz.DispositionDestructive}}
	catalog := newCatalogSync(store, func(context.Context, *http.Request) ([]*mcp.Tool, error) {
		t.Fatal("a recorded tool must not list the upstream")
		return nil, nil
	})

	err := newAuthzInterceptor(t, store, catalog).InterceptToolsCallRequest(blockDestructiveContext(t), newCatalogCall(t, "delete_repository "))
	require.Error(t, err, "a padded name must not get past a rule on the tool it spells")
}

func TestToolsListCatalog_BlankToolNameDoesNotWriteOnEveryListing(t *testing.T) {
	t.Parallel()

	store := &fakeToolCatalogStore{catalog: map[string]string{"list_issues": ""}}

	resp := newListedResponse(t, `{"tools":[{"name":"list_issues","inputSchema":{"type":"object"}},{"name":" ","inputSchema":{"type":"object"}}]}`)
	require.NoError(t, newCatalogSync(store, nil).recordListed(t.Context(), resp))
	require.Empty(t, store.writes)
}

func TestToolCatalogRefreshKey_SeparatesCallersWithoutAUserID(t *testing.T) {
	t.Parallel()

	base := &contextvalues.AuthContext{ActiveOrganizationID: catalogTestOrgID}
	first := contextvalues.WithAuthenticatedActor(t.Context(), base, urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()))
	second := contextvalues.WithAuthenticatedActor(t.Context(), base, urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()))

	require.NotEqual(t, toolCatalogRefreshKey(first, catalogTestServerID), toolCatalogRefreshKey(second, catalogTestServerID))
}

func TestToolsCallCatalog_PaddedToolNameMatchesAToolNameBlock(t *testing.T) {
	t.Parallel()

	sessionID := "session_catalog"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: catalogTestOrgID,
		UserID:               "user_catalog",
		SessionID:            &sessionID,
		AccountType:          "enterprise",
	})
	ctx = authztest.WithExactGrants(t, ctx,
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, authz.Selector{"resource_kind": "mcp", "resource_id": catalogTestServerID}),
		authz.NewGrantWithSelector(authz.ScopeMCPBlockedConnect, authz.Selector{"resource_kind": "mcp", "resource_id": catalogTestServerID, "tool": "delete_repository"}),
	)

	store := &fakeToolCatalogStore{catalog: map[string]string{"delete_repository": ""}}
	interceptor := newAuthzInterceptor(t, store, newCatalogSync(store, nil))

	require.Error(t, interceptor.InterceptToolsCallRequest(ctx, newCatalogCall(t, " delete_repository ")))
}
