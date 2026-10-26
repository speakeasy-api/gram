package codemode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type fixtureBackend struct {
	mu        sync.Mutex
	servers   []Server
	catalogs  map[string]*Catalog
	failures  map[string]error
	listed    []string
	calls     []string
	result    json.RawMessage
	callErr   error
	revoked   bool
	refreshes int
}

func (b *fixtureBackend) Members(context.Context) ([]Server, Lister, error) {
	b.mu.Lock()
	b.refreshes++
	b.mu.Unlock()
	return b.servers, b.catalog, nil
}

func (b *fixtureBackend) List(ctx context.Context, server string) (*Catalog, error) {
	b.mu.Lock()
	b.refreshes++
	b.mu.Unlock()
	return b.catalog(ctx, server)
}

func (b *fixtureBackend) catalog(ctx context.Context, server string) (*Catalog, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listed = append(b.listed, server)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("fixture canceled: %w", err)
	}
	if err := b.failures[server]; err != nil {
		return nil, err
	}
	catalog := b.catalogs[server]
	if catalog == nil {
		return nil, ErrToolUnavailable
	}
	return catalog, nil
}

func (b *fixtureBackend) Describe(ctx context.Context, path string) (*Tool, error) {
	b.mu.Lock()
	b.refreshes++
	b.mu.Unlock()
	server, _, err := splitPath(path)
	if err != nil {
		return nil, err
	}
	catalog, err := b.catalog(ctx, server)
	if err != nil {
		return nil, err
	}
	for _, tool := range catalog.Tools {
		if tool.Path == path {
			return &tool, nil
		}
	}
	return nil, ErrToolUnavailable
}

// Call never lists: like a member, it answers an unknown tool itself.
func (b *fixtureBackend) Call(_ context.Context, path string, _ json.RawMessage) (json.RawMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refreshes++
	if b.revoked {
		return nil, ErrExecutionRevoked
	}
	b.calls = append(b.calls, path)
	if b.callErr != nil {
		return nil, b.callErr
	}
	return b.result, nil
}

func (b *fixtureBackend) refreshCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.refreshes
}

func fixtureTool(path string) Tool {
	_, name, _ := splitPath(path)
	return Tool{Path: path, Name: name, Description: "List customer invoices", Fingerprint: path + "-v1", Definition: json.RawMessage(`{"name":"list","inputSchema":{"type":"object","properties":{"customer_id":{"type":"string"}},"required":["customer_id"],"additionalProperties":false},"outputSchema":{"type":"object"},"_meta":{"private":"secret"}}`)}
}

func TestSearchLoadsLiveCatalogsAndReportsPartialFailures(t *testing.T) {
	t.Parallel()
	backend := &fixtureBackend{servers: []Server{{Slug: "crm", Name: "CRM"}, {Slug: "offline", Name: "Offline"}}, catalogs: map[string]*Catalog{"crm": {Tools: []Tool{fixtureTool("crm--listInvoices")}}}, failures: map[string]error{"offline": fmt.Errorf("private upstream error")}}
	host, err := NewHost(t.Context(), backend, "scope", nil)
	require.NoError(t, err)
	page, err := host.Search(t.Context(), SearchArgs{Query: "unpaid invoices"})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "crm--listInvoices", page.Items[0].Path)
	require.True(t, page.Incomplete)
	require.Equal(t, []FailedMember{{Server: "offline", Reason: "catalog_unavailable"}}, page.FailedMembers)
	backend.catalogs["crm"].Tools = []Tool{fixtureTool("crm--newInvoices")}
	page, err = host.Search(t.Context(), SearchArgs{Query: "invoices", Server: "crm"})
	require.NoError(t, err)
	require.Equal(t, "crm--newInvoices", page.Items[0].Path)
	require.False(t, page.Incomplete)
	require.Len(t, backend.listed, 3, "each request must fetch live catalogs, with server-scoped fan-out")
}

func TestRunRestrictionNarrowsDiscoveryAndCalls(t *testing.T) {
	t.Parallel()
	backend := &fixtureBackend{servers: []Server{{Slug: "crm", Name: "CRM"}, {Slug: "other", Name: "Other"}}, catalogs: map[string]*Catalog{"crm": {Tools: []Tool{fixtureTool("crm--allowed"), fixtureTool("crm--hidden")}}}, result: json.RawMessage(`{"structuredContent":{},"content":[]}`)}
	host, err := NewHost(t.Context(), backend, "scope", []string{"crm--allowed"})
	require.NoError(t, err)
	servers, err := host.Servers(t.Context(), PageArgs{})
	require.NoError(t, err)
	require.Equal(t, []Server{{Slug: "crm", Name: "CRM"}}, servers.Items)
	page, err := host.Search(t.Context(), SearchArgs{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "crm--allowed", page.Items[0].Path)
	_, err = host.Describe(t.Context(), "crm--hidden")
	require.ErrorIs(t, err, ErrToolUnavailable)
	_, err = host.Call(t.Context(), "crm--hidden", json.RawMessage(`{"customer_id":"c1"}`))
	require.ErrorIs(t, err, ErrToolUnavailable)
	require.Empty(t, backend.calls)
	result, err := host.Call(t.Context(), "crm--allowed", json.RawMessage(`{"customer_id":"c1"}`))
	require.NoError(t, err)
	require.True(t, result.OK)
	require.JSONEq(t, `{}`, string(result.Data))
}

func TestSeparatorInMemberSlugIsNotAddressable(t *testing.T) {
	t.Parallel()
	backend := &fixtureBackend{servers: []Server{{Slug: "crm", Name: "CRM"}, {Slug: "crm--billing", Name: "Ambiguous"}}, catalogs: map[string]*Catalog{"crm": {Tools: []Tool{fixtureTool("crm--billing--list")}}, "crm--billing": {Tools: []Tool{fixtureTool("crm--billing--list")}}}}
	for _, tools := range [][]string{nil, {"crm--billing--list"}} {
		host, err := NewHost(t.Context(), backend, "scope", tools)
		require.NoError(t, err)
		servers, err := host.Servers(t.Context(), PageArgs{})
		require.NoError(t, err)
		require.Equal(t, []Server{{Slug: "crm", Name: "CRM"}}, servers.Items)
		page, err := host.Search(t.Context(), SearchArgs{})
		require.NoError(t, err)
		require.Len(t, page.Items, 1)
		require.Equal(t, "crm--billing--list", page.Items[0].Path)
		require.False(t, page.Incomplete)
		_, err = host.Search(t.Context(), SearchArgs{Server: "crm--billing"})
		require.ErrorIs(t, err, ErrToolUnavailable)
	}
	require.NotContains(t, backend.listed, "crm--billing")
}

func TestEmptyRestrictionNeedsNoToolEnumeration(t *testing.T) {
	t.Parallel()
	backend := &fixtureBackend{servers: []Server{{Slug: "crm", Name: "CRM"}}}
	host, err := NewHost(t.Context(), backend, "scope", []string{})
	require.NoError(t, err)
	page, err := host.Search(t.Context(), SearchArgs{})
	require.NoError(t, err)
	require.Empty(t, page.Items)
	_, err = host.Call(t.Context(), "crm--list", json.RawMessage(`{}`))
	require.ErrorIs(t, err, ErrToolUnavailable)
	require.Empty(t, backend.listed)
}

func TestDescribeReturnsExactDefinitionAndStripsMetadata(t *testing.T) {
	t.Parallel()
	tool := fixtureTool("crm--orders.list")
	backend := &fixtureBackend{catalogs: map[string]*Catalog{"crm": {Tools: []Tool{tool, fixtureTool("crm--orders-list")}}}}
	host, err := NewHost(t.Context(), backend, "scope", nil)
	require.NoError(t, err)
	described, err := host.Describe(t.Context(), tool.Path)
	require.NoError(t, err)
	require.NotContains(t, string(described.Definition), "secret")
	require.Contains(t, described.CallTemplate, `"crm--orders.list"`)
	other, err := host.Describe(t.Context(), "crm--orders-list")
	require.NoError(t, err)
	require.NotEqual(t, described.Path, other.Path)
	backend.catalogs["crm"].Tools[0].Fingerprint = "new-destination"
	again, err := host.Describe(t.Context(), tool.Path)
	require.NoError(t, err, "a run may rediscover a changed tool and correct itself")
	require.Equal(t, "new-destination", again.Fingerprint)
	require.Empty(t, backend.calls)
}

func TestCallObservesRevocationWithoutListing(t *testing.T) {
	t.Parallel()
	backend := &fixtureBackend{catalogs: map[string]*Catalog{"crm": {Tools: []Tool{fixtureTool("crm--list")}}}, revoked: true}
	host, err := NewHost(t.Context(), backend, "scope", nil)
	require.NoError(t, err)
	_, err = host.Call(t.Context(), "crm--list", json.RawMessage(`{"customer_id":"c1"}`))
	require.ErrorIs(t, err, ErrExecutionRevoked)
	require.Empty(t, backend.calls)
	require.Empty(t, backend.listed)
}

// TestCallNeverListsTheMember pins one dispatch and no catalog read per call.
func TestCallNeverListsTheMember(t *testing.T) {
	t.Parallel()
	backend := &fixtureBackend{catalogs: map[string]*Catalog{"crm": {Tools: []Tool{fixtureTool("crm--list")}}}, result: json.RawMessage(`{"structuredContent":{"n":1},"content":[]}`)}
	host, err := NewHost(t.Context(), backend, "scope", nil)
	require.NoError(t, err)
	for range 2 {
		result, err := host.Call(t.Context(), "crm--list", json.RawMessage(`{"customer_id":"c1"}`))
		require.NoError(t, err)
		require.True(t, result.OK)
	}
	require.Empty(t, backend.listed)
	require.Equal(t, []string{"crm--list", "crm--list"}, backend.calls)
}

func TestArgumentsMustBeABoundedObjectBeforeDispatch(t *testing.T) {
	t.Parallel()
	backend := &fixtureBackend{catalogs: map[string]*Catalog{"crm": {Tools: []Tool{fixtureTool("crm--list")}}}}
	host, err := NewHost(t.Context(), backend, "scope", nil)
	require.NoError(t, err)
	for _, arguments := range []string{`[]`, `null`, `"x"`, `{`, `{"a":"` + strings.Repeat("x", MaxResultBytes) + `"}`} {
		_, err = host.Call(t.Context(), "crm--list", json.RawMessage(arguments))
		require.ErrorContains(t, err, "bounded JSON object")
	}
	require.Empty(t, backend.calls)
}

// TestMemberRejectionIsACompletedFailure: schema checks are the member's, and
// its refusal reaches the program as a fixable failed call, not an unknown outcome.
func TestMemberRejectionIsACompletedFailure(t *testing.T) {
	t.Parallel()
	backend := &fixtureBackend{callErr: &DispatchError{BeforeInvoke: false, Code: "tool_call_rejected", Rejection: `server "crm" rejected the call: invalid params: customer_id must be a string`}}
	host, err := NewHost(t.Context(), backend, "scope", nil)
	require.NoError(t, err)
	result, err := host.Call(t.Context(), "crm--list", json.RawMessage(`{"customer_id":123}`))
	require.NoError(t, err)
	require.Equal(t, "completed", result.Outcome)
	require.False(t, result.OK)
	require.Equal(t, "tool_call_rejected", result.Error)
	require.Contains(t, string(result.Content), "customer_id must be a string")

	backend.callErr = &DispatchError{BeforeInvoke: false, Code: "tool_call_rejected", Rejection: strings.Repeat("é", maxRejectionBytes)}
	result, err = host.Call(t.Context(), "crm--list", json.RawMessage(`{}`))
	require.NoError(t, err)
	var content []map[string]string
	require.NoError(t, json.Unmarshal(result.Content, &content))
	require.LessOrEqual(t, len(content[0]["text"]), maxRejectionBytes)

	backend.callErr = &DispatchError{BeforeInvoke: false, Code: "tool_call_outcome_unknown", Rejection: ""}
	result, err = host.Call(t.Context(), "crm--list", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Equal(t, "unknown", result.Outcome)

	backend.callErr = &DispatchError{BeforeInvoke: true, Code: "tool_unavailable", Rejection: ""}
	_, err = host.Call(t.Context(), "crm--list", json.RawMessage(`{}`))
	var refusal *DispatchError
	require.ErrorAs(t, err, &refusal)
	require.True(t, refusal.BeforeInvoke)
}

func TestAllowlistAdmissionReadsNoCatalogs(t *testing.T) {
	t.Parallel()
	backend := &fixtureBackend{servers: []Server{{Slug: "crm", Name: "CRM"}}, result: json.RawMessage(`{"content":[]}`)}
	host, err := NewHost(t.Context(), backend, "scope", []string{"crm--list", "erp--sync"})
	require.NoError(t, err)
	require.Empty(t, backend.listed)
	require.Zero(t, backend.refreshCount())
	_, err = NewHost(t.Context(), backend, "scope", []string{"crm--list", "crm--list"})
	require.Error(t, err)
	_, err = NewHost(t.Context(), backend, "scope", []string{"no-separator"})
	require.Error(t, err)
	_, err = host.Call(t.Context(), "crm--other", json.RawMessage(`{}`))
	require.ErrorIs(t, err, ErrToolUnavailable)
	_, err = host.Call(t.Context(), "crm--list", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Equal(t, []string{"crm--list"}, backend.calls)
}

func TestSearchCursorRejectsScopeCatalogAndFailureChanges(t *testing.T) {
	t.Parallel()
	docs := []Document{{Candidate: Candidate{Path: "crm--b", Description: "B"}, Name: "b", Fingerprint: "b1"}, {Candidate: Candidate{Path: "crm--a", Description: "A"}, Name: "a", Fingerprint: "a1"}}
	page, err := Rank("scope", SearchArgs{Limit: 1}, docs, nil)
	require.NoError(t, err)
	require.Equal(t, "crm--a", page.Items[0].Path)
	require.NotEmpty(t, page.NextCursor)
	args := SearchArgs{Limit: 1, Cursor: page.NextCursor}
	second, err := Rank("scope", args, docs, nil)
	require.NoError(t, err)
	require.Equal(t, "crm--b", second.Items[0].Path)
	_, err = Rank("other-caller", args, docs, nil)
	require.ErrorContains(t, err, "catalog changed")
	_, err = Rank("scope", args, docs, []FailedMember{{Server: "offline", Reason: "catalog_unavailable"}})
	require.ErrorContains(t, err, "catalog changed")
	docs[0].Fingerprint = "changed"
	_, err = Rank("scope", args, docs, nil)
	require.ErrorContains(t, err, "catalog changed")
}

func TestRankPrefersExactNamesAndTokenizedToolNames(t *testing.T) {
	t.Parallel()
	docs := []Document{{Candidate: Candidate{Path: "crm--listCustomerInvoices"}, Name: "listCustomerInvoices"}, {Candidate: Candidate{Path: "crm--other", Description: "List customer invoices"}, Name: "other"}}
	page, err := Rank("scope", SearchArgs{Query: "customer invoices"}, docs, nil)
	require.NoError(t, err)
	require.Equal(t, "crm--listCustomerInvoices", page.Items[0].Path)
	page, err = Rank("scope", SearchArgs{Query: "crm--other"}, docs, nil)
	require.NoError(t, err)
	require.Equal(t, "crm--other", page.Items[0].Path)
}

func TestTokensSplitAcronymsFromFollowingWords(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"get", "http", "status"}, tokens("getHTTPStatus"))
	require.Equal(t, []string{"http", "server"}, tokens("HTTPServer"))
	require.Equal(t, []string{"list", "customer", "invoices"}, tokens("listCustomerInvoices"))
	require.Equal(t, []string{"read", "url"}, tokens("readURL"))
	require.Equal(t, []string{"crm", "get", "http", "status"}, tokens("crm--get_HTTP_status"))
	docs := []Document{{Candidate: Candidate{Path: "web--getHTTPStatus"}, Name: "getHTTPStatus"}}
	page, err := Rank("scope", SearchArgs{Query: "http status"}, docs, nil)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "web--getHTTPStatus", page.Items[0].Path)
}

func TestNormalizePreservesStructuredContentAndContent(t *testing.T) {
	t.Parallel()
	result, err := NormalizeResult(json.RawMessage(`{"structuredContent":{},"content":[{"type":"text","text":"not json","_meta":{"secret":true}},{"type":"image","data":"AA==","mimeType":"image/png"}],"_meta":{"secret":true}}`))
	require.NoError(t, err)
	require.True(t, result.OK)
	require.JSONEq(t, `{}`, string(result.Data))
	require.JSONEq(t, `[{"type":"text","text":"not json"},{"type":"image","data":"AA==","mimeType":"image/png"}]`, string(result.Content))
	result, err = NormalizeResult(json.RawMessage(`{"isError":true,"content":[{"type":"text","text":"denied"}]}`))
	require.NoError(t, err)
	require.False(t, result.OK)
	require.Equal(t, "null", string(result.Data))
	require.Contains(t, string(result.Content), "denied")
}

func TestMissingOrNullContentIsAnUnavailableResult(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`{}`, `{"content":null}`, `{"structuredContent":{"id":1}}`, `{"structuredContent":{},"content":null,"isError":false}`} {
		_, err := NormalizeResult(json.RawMessage(raw))
		require.ErrorContains(t, err, "invalid MCP tool result", raw)
	}
	_, err := NormalizeResult(json.RawMessage(`{"content":[null]}`))
	require.ErrorContains(t, err, "invalid MCP content block")
	result, err := NormalizeResult(json.RawMessage(`{"content":[]}`))
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(result.Content))

	backend := &fixtureBackend{catalogs: map[string]*Catalog{"crm": {Tools: []Tool{fixtureTool("crm--write")}}}, result: json.RawMessage(`{"structuredContent":{"id":1}}`)}
	host, err := NewHost(t.Context(), backend, "scope", nil)
	require.NoError(t, err)
	result, err = host.Call(t.Context(), "crm--write", json.RawMessage(`{"customer_id":"c1"}`))
	require.NoError(t, err)
	require.Equal(t, "completed", result.Outcome)
	require.False(t, result.OK)
	require.Equal(t, "tool_result_unavailable", result.Error)
	require.Equal(t, []string{"crm--write"}, backend.calls)
}

func TestOversizedResultPreservesCompletedDispatch(t *testing.T) {
	t.Parallel()
	backend := &fixtureBackend{catalogs: map[string]*Catalog{"crm": {Tools: []Tool{fixtureTool("crm--write")}}}, result: json.RawMessage(strings.Repeat(" ", MaxResultBytes+1))}
	host, err := NewHost(t.Context(), backend, "scope", nil)
	require.NoError(t, err)
	result, err := host.Call(t.Context(), "crm--write", json.RawMessage(`{"customer_id":"c1"}`))
	require.NoError(t, err)
	require.Equal(t, "completed", result.Outcome)
	require.False(t, result.OK)
	require.Equal(t, "tool_result_unavailable", result.Error)
	require.Equal(t, []string{"crm--write"}, backend.calls)
}

func TestStructuredOutputPassesThroughUnchanged(t *testing.T) {
	t.Parallel()
	backend := &fixtureBackend{catalogs: map[string]*Catalog{"crm": {Tools: []Tool{fixtureTool("crm--write")}}}, result: json.RawMessage(`{"structuredContent":[1,2],"content":[]}`)}
	host, err := NewHost(t.Context(), backend, "scope", nil)
	require.NoError(t, err)
	result, err := host.Call(t.Context(), "crm--write", json.RawMessage(`{"customer_id":"c1"}`))
	require.NoError(t, err)
	require.True(t, result.OK)
	require.Equal(t, "completed", result.Outcome)
	require.JSONEq(t, `[1,2]`, string(result.Data))
	require.Empty(t, result.Warnings)
}

func TestEmbeddedResourceMetadataStaysHostSide(t *testing.T) {
	t.Parallel()
	result, err := NormalizeResult(json.RawMessage(`{"content":[{"type":"resource","resource":{"uri":"test://resource","text":"public","_meta":{"private":"secret"}},"_meta":{"private":"secret"}}]}`))
	require.NoError(t, err)
	require.NotContains(t, string(result.Content), "secret")
	require.Contains(t, string(result.Content), "public")
}

func TestRepeatedQueryTermsDoNotAmplifyRanking(t *testing.T) {
	t.Parallel()
	docs := []Document{
		{Candidate: Candidate{Path: "crm--invoices"}, Name: "invoices"},
		{Candidate: Candidate{Path: "crm--refunds", Description: "customers"}, Name: "refunds"},
	}
	page, err := Rank("scope", SearchArgs{Query: strings.Repeat("invoices ", 100) + "refunds customers"}, docs, nil)
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.Equal(t, "crm--refunds", page.Items[0].Path)
	require.Equal(t, "crm--invoices", page.Items[1].Path)
	terms := make([]string, 33)
	for i := range terms {
		terms[i] = fmt.Sprintf("term%d", i)
	}
	_, err = Rank("scope", SearchArgs{Query: strings.Join(terms, " ")}, docs, nil)
	require.ErrorContains(t, err, "too many query terms")
}

// TestCallbacksRefreshAuthorizationOnce pins one authorization refresh per
// callback: search shares it across members, and a call reads no catalog.
func TestCallbacksRefreshAuthorizationOnce(t *testing.T) {
	t.Parallel()
	servers := []Server{{Slug: "a", Name: "A"}, {Slug: "b", Name: "B"}, {Slug: "c", Name: "C"}}
	catalogs := map[string]*Catalog{}
	for _, server := range servers {
		catalogs[server.Slug] = &Catalog{Tools: []Tool{fixtureTool(server.Slug + "--list")}, Incomplete: false}
	}
	backend := &fixtureBackend{servers: servers, catalogs: catalogs, result: json.RawMessage(`{"structuredContent":{},"content":[]}`)}
	host, err := NewHost(t.Context(), backend, "scope", nil)
	require.NoError(t, err)

	page, err := host.Search(t.Context(), SearchArgs{})
	require.NoError(t, err)
	require.Len(t, page.Items, 3)
	require.Equal(t, 1, backend.refreshCount())
	require.Len(t, backend.listed, 3)

	result, err := host.Call(t.Context(), "a--list", json.RawMessage(`{"customer_id":"c1"}`))
	require.NoError(t, err)
	require.True(t, result.OK)
	require.Equal(t, 2, backend.refreshCount())
	require.Len(t, backend.listed, 3)
	require.Equal(t, []string{"a--list"}, backend.calls)
}

func TestCallUntypedBackendFailureHasUnknownOutcome(t *testing.T) {
	t.Parallel()
	backend := &fixtureBackend{callErr: fmt.Errorf("unclassified transport failure")}
	host, err := NewHost(t.Context(), backend, "scope", nil)
	require.NoError(t, err)
	result, err := host.Call(t.Context(), "crm--list", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Equal(t, "unknown", result.Outcome)
}
