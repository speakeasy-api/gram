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
	mu       sync.Mutex
	servers  []Server
	catalogs map[string]*Catalog
	failures map[string]error
	listed   []string
	calls    []string
	result   json.RawMessage
}

func (b *fixtureBackend) Members(context.Context) ([]Server, error) { return b.servers, nil }
func (b *fixtureBackend) List(ctx context.Context, server string) (*Catalog, error) {
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
func (b *fixtureBackend) Invoke(ctx context.Context, path, fingerprint string, _ json.RawMessage) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("fixture canceled: %w", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	server, _, err := splitPath(path)
	if err != nil {
		return nil, err
	}
	for _, tool := range b.catalogs[server].Tools {
		if tool.Path == path && tool.Fingerprint == fingerprint {
			b.calls = append(b.calls, path)
			return b.result, nil
		}
	}
	return nil, ErrToolUnavailable
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

func TestDescribePinsExactDefinitionAndStripsMetadata(t *testing.T) {
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
	_, err = host.Call(t.Context(), tool.Path, json.RawMessage(`{"customer_id":"c1"}`))
	require.ErrorContains(t, err, "tool_changed")
	require.Empty(t, backend.calls)
}

func TestCallObservesRevocationAfterDescription(t *testing.T) {
	t.Parallel()
	backend := &fixtureBackend{catalogs: map[string]*Catalog{"crm": {Tools: []Tool{fixtureTool("crm--list")}}}}
	host, err := NewHost(t.Context(), backend, "scope", nil)
	require.NoError(t, err)
	_, err = host.Describe(t.Context(), "crm--list")
	require.NoError(t, err)
	backend.catalogs["crm"].Tools = nil
	_, err = host.Call(t.Context(), "crm--list", json.RawMessage(`{"customer_id":"c1"}`))
	require.ErrorIs(t, err, ErrToolUnavailable)
	require.Empty(t, backend.calls)
}

func TestInvalidArgumentsNeverDispatch(t *testing.T) {
	t.Parallel()
	backend := &fixtureBackend{catalogs: map[string]*Catalog{"crm": {Tools: []Tool{fixtureTool("crm--list")}}}}
	host, err := NewHost(t.Context(), backend, "scope", nil)
	require.NoError(t, err)
	_, err = host.Call(t.Context(), "crm--list", json.RawMessage(`{"customer_id":123}`))
	require.Error(t, err)
	require.Empty(t, backend.calls)
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

func TestSchemaLocalReferencesAndExternalReferenceRejection(t *testing.T) {
	t.Parallel()
	definition := json.RawMessage(`{"inputSchema":{"type":"object","properties":{"id":{"$ref":"#/$defs/id"}},"required":["id"],"$defs":{"id":{"type":"string"}}}}`)
	require.NoError(t, ValidateArguments(definition, json.RawMessage(`{"id":"one"}`)))
	require.Error(t, ValidateArguments(definition, json.RawMessage(`{"id":1}`)))
	for _, ref := range []string{"https://example.invalid/schema", "file:///etc/passwd"} {
		raw, err := json.Marshal(map[string]any{"inputSchema": map[string]string{"$ref": ref}})
		require.NoError(t, err)
		require.ErrorContains(t, ValidateArguments(raw, json.RawMessage(`{}`)), "no URLLoader")
	}
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

func TestOutputSchemaMismatchDoesNotDiscardExecutedResult(t *testing.T) {
	t.Parallel()
	backend := &fixtureBackend{catalogs: map[string]*Catalog{"crm": {Tools: []Tool{fixtureTool("crm--write")}}}, result: json.RawMessage(`{"structuredContent":[1,2],"content":[]}`)}
	host, err := NewHost(t.Context(), backend, "scope", nil)
	require.NoError(t, err)
	result, err := host.Call(t.Context(), "crm--write", json.RawMessage(`{"customer_id":"c1"}`))
	require.NoError(t, err)
	require.True(t, result.OK)
	require.Equal(t, "completed", result.Outcome)
	require.JSONEq(t, `[1,2]`, string(result.Data))
	require.Len(t, result.Warnings, 1)
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
	docs := []Document{{Candidate: Candidate{Path: "crm--invoices"}, Name: "invoices"}}
	page, err := Rank("scope", SearchArgs{Query: strings.Repeat("invoices ", 100)}, docs, nil)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	terms := make([]string, 33)
	for i := range terms {
		terms[i] = fmt.Sprintf("term%d", i)
	}
	_, err = Rank("scope", SearchArgs{Query: strings.Join(terms, " ")}, docs, nil)
	require.ErrorContains(t, err, "too many query terms")
}
