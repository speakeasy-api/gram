package proxy_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// upstreamToolsListWithUnmodelledMembers is a tools/list result from an upstream
// on a protocol revision newer than the vendored SDK: it carries `resultType`
// (required of every result from 2026-07-28 onward), the `ttlMs`/`cacheScope`
// caching hints that revision added to list results, and a per-tool member
// nothing in this repo models.
const upstreamToolsListWithUnmodelledMembers = `{"jsonrpc":"2.0","id":9,"result":{` +
	`"resultType":"complete",` +
	`"tools":[` +
	`{"name":"keep","inputSchema":{"type":"object"},"x-vendor-extension":"kept"},` +
	`{"name":"drop","inputSchema":{"type":"object"}}` +
	`],` +
	`"nextCursor":"cursor-1",` +
	`"ttlMs":60000,` +
	`"cacheScope":"public"` +
	`}}`

const toolsListRequestBody = `{"jsonrpc":"2.0","id":9,"method":"tools/list","params":{}}`

func postJSON(t *testing.T, p interface {
	Post(http.ResponseWriter, *http.Request) error
}, body string,
) (*httptest.ResponseRecorder, error) {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()
	if err := p.Post(rr, req); err != nil {
		return rr, fmt.Errorf("proxy post: %w", err)
	}
	return rr, nil
}

func upstreamServing(t *testing.T, body string) *httptest.Server {
	t.Helper()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(upstream.Close)

	return upstream
}

// keepNamed returns a filter that keeps the tools whose name appears in names.
func keepNamed(names ...string) func([]*proxy.Tool) []*proxy.Tool {
	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}

	return func(tools []*proxy.Tool) []*proxy.Tool {
		kept := make([]*proxy.Tool, 0, len(tools))
		for _, tool := range tools {
			if wanted[tool.Name] {
				kept = append(kept, tool)
			}
		}
		return kept
	}
}

func resultMembers(t *testing.T, body string) map[string]json.RawMessage {
	t.Helper()

	var envelope struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &envelope), "body: %s", body)

	return envelope.Result
}

func filteringProxy(t *testing.T, upstreamBody string, keep func([]*proxy.Tool) []*proxy.Tool) *proxy.Proxy {
	t.Helper()

	p := newProxyForTest(t, upstreamServing(t, upstreamBody).URL)
	p.ToolsListResponseInterceptors = []proxy.ToolsListResponseInterceptor{
		&mutatingToolsListResponseInterceptor{name: "filter", toolsFn: keep, err: nil},
	}

	return p
}

// TestProxy_Post_ToolsListFilterKeepsUnmodelledMembers is the regression test for
// a client rejecting a relayed tools/list with "missing required resultType".
// Filtering the tool array must not disturb the members around it, nor the
// members of the tools that survive.
func TestProxy_Post_ToolsListFilterKeepsUnmodelledMembers(t *testing.T) {
	t.Parallel()

	p := filteringProxy(t, upstreamToolsListWithUnmodelledMembers, keepNamed("keep"))

	rr, err := postJSON(t, p, toolsListRequestBody)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rr.Code)

	members := resultMembers(t, rr.Body.String())
	require.JSONEq(t, `"complete"`, string(members["resultType"]), "resultType must survive the filter")
	require.JSONEq(t, `"cursor-1"`, string(members["nextCursor"]), "the pagination cursor must survive the filter")

	var tools []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(members["tools"], &tools))
	require.Len(t, tools, 1, "the filter must still have dropped the unauthorized tool")
	require.JSONEq(t, `"keep"`, string(tools[0]["name"]))
	require.JSONEq(t, `{"type":"object"}`, string(tools[0]["inputSchema"]),
		"a member Gram does not model must survive on the tool that carries it")
	require.JSONEq(t, `"kept"`, string(tools[0]["x-vendor-extension"]),
		"a vendor member must survive on the tool that carries it")
}

// TestProxy_Post_ToolsListWithoutMutationRelaysVerbatim pins the untouched path:
// with no mutating interceptor the proxy must not re-encode at all.
func TestProxy_Post_ToolsListWithoutMutationRelaysVerbatim(t *testing.T) {
	t.Parallel()

	p := newProxyForTest(t, upstreamServing(t, upstreamToolsListWithUnmodelledMembers).URL)

	rr, err := postJSON(t, p, toolsListRequestBody)
	require.NoError(t, err)
	//nolint:testifylint // Byte identity is the invariant, not JSON equivalence: JSONEq would also pass a re-encode, which is what this rules out.
	require.Equal(t, upstreamToolsListWithUnmodelledMembers, rr.Body.String(),
		"an unmutated response must relay byte-for-byte")
}

// TestProxy_Post_ToolsListInjectedToolCarriesNothing pins that the setter still
// takes tools, not a filter mask: a caller may hand back a tool it constructed,
// which simply carries no members of its own.
func TestProxy_Post_ToolsListInjectedToolCarriesNothing(t *testing.T) {
	t.Parallel()

	p := filteringProxy(t, upstreamToolsListWithUnmodelledMembers, func(tools []*proxy.Tool) []*proxy.Tool {
		return append(keepNamed("keep")(tools), &proxy.Tool{Name: "injected"})
	})

	rr, err := postJSON(t, p, toolsListRequestBody)
	require.NoError(t, err)

	var tools []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(resultMembers(t, rr.Body.String())["tools"], &tools))
	require.Len(t, tools, 2)
	require.JSONEq(t, `"keep"`, string(tools[0]["name"]))
	require.JSONEq(t, `"kept"`, string(tools[0]["x-vendor-extension"]), "the upstream's tool keeps its members")
	require.JSONEq(t, `"injected"`, string(tools[1]["name"]))
	require.Len(t, tools[1], 1, "a constructed tool carries only what it was given")
}

// TestProxy_Post_ToolsListRenamedToolKeepsItsOtherMembers pins that editing a
// modeled field rewrites that member and leaves the rest of the tool alone —
// the property that makes the carried members live on the object.
func TestProxy_Post_ToolsListRenamedToolKeepsItsOtherMembers(t *testing.T) {
	t.Parallel()

	p := filteringProxy(t, upstreamToolsListWithUnmodelledMembers, func(tools []*proxy.Tool) []*proxy.Tool {
		kept := keepNamed("keep")(tools)
		kept[0].Name = "renamed"
		return kept
	})

	rr, err := postJSON(t, p, toolsListRequestBody)
	require.NoError(t, err)

	var tools []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(resultMembers(t, rr.Body.String())["tools"], &tools))
	require.Len(t, tools, 1)
	require.JSONEq(t, `"renamed"`, string(tools[0]["name"]), "the edit must reach the client")
	require.JSONEq(t, `"kept"`, string(tools[0]["x-vendor-extension"]),
		"editing one member must not drop the others")
}

// TestProxy_Post_ToolsListFilterConfinesCacheToCaller covers the one member the
// filter rewrites beyond the tool array. Relaying an upstream's public cache
// scope on a per-caller list would let any cache between Gram and the model serve
// one caller's tools to another.
func TestProxy_Post_ToolsListFilterConfinesCacheToCaller(t *testing.T) {
	t.Parallel()

	p := filteringProxy(t, upstreamToolsListWithUnmodelledMembers, keepNamed("keep"))

	rr, err := postJSON(t, p, toolsListRequestBody)
	require.NoError(t, err)

	members := resultMembers(t, rr.Body.String())
	require.JSONEq(t, `"private"`, string(members["cacheScope"]),
		"a per-caller filtered list must not stay publicly cacheable")
	require.JSONEq(t, `0`, string(members["ttlMs"]),
		"Gram cannot promise freshness for a list it filtered on policy it can revoke")
}

// TestProxy_Post_ToolsListFilterInventsNoCacheHints pins the other half of that
// rule: an upstream on a revision without caching hints must not have one
// introduced on its behalf.
func TestProxy_Post_ToolsListFilterInventsNoCacheHints(t *testing.T) {
	t.Parallel()

	p := filteringProxy(t,
		`{"jsonrpc":"2.0","id":9,"result":{"tools":[{"name":"keep","inputSchema":{"type":"object"}}]}}`,
		keepNamed("keep"))

	rr, err := postJSON(t, p, toolsListRequestBody)
	require.NoError(t, err)

	members := resultMembers(t, rr.Body.String())
	require.NotContains(t, members, "cacheScope")
	require.NotContains(t, members, "ttlMs")
}

// TestProxy_Post_ToolsListEmptyFilterKeepsResultMembers covers the filter that
// removes everything: the array must be present and empty, and the members
// around it intact.
func TestProxy_Post_ToolsListEmptyFilterKeepsResultMembers(t *testing.T) {
	t.Parallel()

	p := filteringProxy(t, upstreamToolsListWithUnmodelledMembers, keepNamed())

	rr, err := postJSON(t, p, toolsListRequestBody)
	require.NoError(t, err)

	members := resultMembers(t, rr.Body.String())
	require.JSONEq(t, `[]`, string(members["tools"]))
	require.JSONEq(t, `"complete"`, string(members["resultType"]))
	require.JSONEq(t, `"cursor-1"`, string(members["nextCursor"]))
}

// TestProxy_Post_ToolsListFilterCollapsesRepeatedMemberNames is the safety
// property behind carrying members by content rather than by byte identity.
// Parsers disagree on a repeated member name — Go keeps the last, first-wins
// parsers are common — so relaying both onward would let Gram authorize one tool
// while a peer downstream reads another.
func TestProxy_Post_ToolsListFilterCollapsesRepeatedMemberNames(t *testing.T) {
	t.Parallel()

	p := filteringProxy(t,
		`{"jsonrpc":"2.0","id":9,"result":{"tools":[{"name":"first","name":"second","inputSchema":{}}]}}`,
		keepNamed("second"))

	rr, err := postJSON(t, p, toolsListRequestBody)
	require.NoError(t, err)

	body := rr.Body.String()
	require.NotContains(t, body, `"first"`, "the value Gram did not authorize must not reach the client")
	require.Contains(t, body, `"second"`, "the value Gram authorized is the one relayed")
	require.Equal(t, 1, strings.Count(body, `"name"`), "the repeated member must be emitted once")
}

// TestProxy_Post_ToolsListFilterDropsCaseVariantOfModelledMember pins the other
// parser-differential case. Go matches a struct field to a member
// case-insensitively while the protocol matches keys exactly, so carrying both
// `name` and `Name` would leave Gram having authorized one and a
// case-insensitive peer reading the other.
func TestProxy_Post_ToolsListFilterDropsCaseVariantOfModelledMember(t *testing.T) {
	t.Parallel()

	p := filteringProxy(t,
		`{"jsonrpc":"2.0","id":9,"result":{"tools":[{"name":"authorized","Name":"shadow","inputSchema":{}}]}}`,
		keepNamed("authorized"))

	rr, err := postJSON(t, p, toolsListRequestBody)
	require.NoError(t, err)

	var tools []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(resultMembers(t, rr.Body.String())["tools"], &tools))
	require.Len(t, tools, 1)
	require.JSONEq(t, `"authorized"`, string(tools[0]["name"]), "the client sees the name Gram authorized")
	require.NotContains(t, tools[0], "Name", "the case variant must not be carried alongside it")
}

// TestProxy_Post_ToolsListNullResultRelaysUntouched covers a JSON null result.
// It is not a list Gram can filter, so no typed view is built, the interceptors
// do not run, and the payload relays as it arrived — rather than panicking on a
// nil member map.
func TestProxy_Post_ToolsListNullResultRelaysUntouched(t *testing.T) {
	t.Parallel()

	const nullResult = `{"jsonrpc":"2.0","id":9,"result":null}`
	p := filteringProxy(t, nullResult, keepNamed())

	rr, err := postJSON(t, p, toolsListRequestBody)
	require.NoError(t, err)
	//nolint:testifylint // Byte identity is the point: nothing decoded, so nothing re-encoded.
	require.Equal(t, nullResult, rr.Body.String())
}

// TestProxy_Post_ToolsListFilterDropsCaseVariantCacheHints pins that confining a
// filtered list actually confines it. A case-variant alias of a cache hint would
// be read in place of the value Gram writes by any parser that folds member
// names, leaving a per-caller result still marked publicly cacheable.
func TestProxy_Post_ToolsListFilterDropsCaseVariantCacheHints(t *testing.T) {
	t.Parallel()

	p := filteringProxy(t,
		`{"jsonrpc":"2.0","id":9,"result":{"tools":[{"name":"keep","inputSchema":{}}],`+
			`"CacheScope":"public","TtlMs":60000}}`,
		keepNamed("keep"))

	rr, err := postJSON(t, p, toolsListRequestBody)
	require.NoError(t, err)

	members := resultMembers(t, rr.Body.String())
	require.JSONEq(t, `"private"`, string(members["cacheScope"]),
		"an aliased hint still counts as the upstream having sent one")
	require.JSONEq(t, `0`, string(members["ttlMs"]))
	require.NotContains(t, members, "CacheScope", "the alias must not survive alongside the confined value")
	require.NotContains(t, members, "TtlMs")
}

// TestProxy_Post_ToolsListNullArrayRelaysUntouched covers an explicitly null tool
// array. It decodes to an empty list without error, so a mutation would emit it
// as [] — normalising the upstream's malformed payload on exactly the paths that
// filter, and relaying it untouched on the rest. Every path relays instead.
func TestProxy_Post_ToolsListNullArrayRelaysUntouched(t *testing.T) {
	t.Parallel()

	const nullArray = `{"jsonrpc":"2.0","id":9,"result":{"tools":null}}`
	p := filteringProxy(t, nullArray, keepNamed())

	rr, err := postJSON(t, p, toolsListRequestBody)
	require.NoError(t, err)
	//nolint:testifylint // Byte identity is the point: nothing decoded, so nothing re-encoded.
	require.Equal(t, nullArray, rr.Body.String())
}

// TestProxy_Post_ToolsCallAmbiguousNameIsRejectedNotErrored pins the error class.
// An ambiguous invocation is invalid client input, so the caller gets a JSON-RPC
// rejection rather than a 5xx blamed on the proxy.
func TestProxy_Post_ToolsCallAmbiguousNameIsRejectedNotErrored(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":4,"result":{"content":[]}}`))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.ToolsCallRequestInterceptors = []proxy.ToolsCallRequestInterceptor{
		&mutatingToolsCallRequestInterceptor{
			name:   "scrub-arguments",
			argsFn: func(json.RawMessage) json.RawMessage { return json.RawMessage(`{}`) },
			err:    nil,
		},
	}

	rr, err := postJSON(t, p,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"denied","Name":"allowed","arguments":{}}}`)
	require.NoError(t, err, "invalid client input must not surface as a proxy error")
	require.Equal(t, int32(0), hits.Load(), "the ambiguous payload must never reach the upstream")
	require.Contains(t, rr.Body.String(), "-32602", "the caller gets an invalid-params rejection")
	require.Contains(t, rr.Body.String(), "ambiguous tools/call params")
}

// TestProxy_Post_ToolsListFilterKeepsUnmodelledAnnotationMembers pins that the
// guarantee holds one level down. Gram reads four annotation hints; a hint it does
// not model must survive on the annotations object that carries it, and must not
// be readable one way by Gram and another way downstream.
func TestProxy_Post_ToolsListFilterKeepsUnmodelledAnnotationMembers(t *testing.T) {
	t.Parallel()

	p := filteringProxy(t,
		`{"jsonrpc":"2.0","id":9,"result":{"tools":[{"name":"keep","inputSchema":{},`+
			`"annotations":{"readOnlyHint":true,"x-vendor-hint":"kept","destructiveHint":false}}]}}`,
		keepNamed("keep"))

	rr, err := postJSON(t, p, toolsListRequestBody)
	require.NoError(t, err)

	var tools []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(resultMembers(t, rr.Body.String())["tools"], &tools))
	require.Len(t, tools, 1)

	var annotations map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(tools[0]["annotations"], &annotations))
	require.JSONEq(t, `true`, string(annotations["readOnlyHint"]))
	require.JSONEq(t, `false`, string(annotations["destructiveHint"]),
		"an explicitly-false hint must stay explicitly false")
	require.JSONEq(t, `"kept"`, string(annotations["x-vendor-hint"]),
		"an annotation member Gram does not model must survive")
}

// TestProxy_Post_ToolsListNullToolRelaysUntouched covers `tools: [null]`. It is
// not a list Gram can authorize per tool, so no typed view is built and the
// payload relays as it arrived rather than reaching a filter that would
// dereference the null.
func TestProxy_Post_ToolsListNullToolRelaysUntouched(t *testing.T) {
	t.Parallel()

	const nullTool = `{"jsonrpc":"2.0","id":9,"result":{"tools":[null]}}`
	p := filteringProxy(t, nullTool, keepNamed())

	rr, err := postJSON(t, p, toolsListRequestBody)
	require.NoError(t, err)
	//nolint:testifylint // Byte identity is the point: nothing decoded, so nothing re-encoded.
	require.Equal(t, nullTool, rr.Body.String())
}

func TestProxy_Post_ResourcesListFilterKeepsUnmodelledMembers(t *testing.T) {
	t.Parallel()

	p := newProxyForTest(t, upstreamServing(t, `{"jsonrpc":"2.0","id":9,"result":{`+
		`"resultType":"complete",`+
		`"resources":[{"uri":"file:///keep","name":"keep","x-vendor-extension":"kept"},{"uri":"file:///drop","name":"drop"}],`+
		`"ttlMs":60000,`+
		`"cacheScope":"public"`+
		`}}`).URL)
	p.ResourcesListResponseInterceptors = []proxy.ResourcesListResponseInterceptor{
		&mutatingResourcesListResponseInterceptor{
			name: "keep-only-keep",
			resourcesFn: func(resources []*proxy.Resource) []*proxy.Resource {
				kept := make([]*proxy.Resource, 0, len(resources))
				for _, resource := range resources {
					if resource.Name == "keep" {
						kept = append(kept, resource)
					}
				}
				return kept
			},
			err: nil,
		},
	}

	rr, err := postJSON(t, p, `{"jsonrpc":"2.0","id":9,"method":"resources/list","params":{}}`)
	require.NoError(t, err)

	members := resultMembers(t, rr.Body.String())
	require.JSONEq(t, `"complete"`, string(members["resultType"]))
	require.JSONEq(t, `"private"`, string(members["cacheScope"]))
	require.JSONEq(t, `0`, string(members["ttlMs"]))

	var resources []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(members["resources"], &resources))
	require.Len(t, resources, 1)
	require.JSONEq(t, `"kept"`, string(resources[0]["x-vendor-extension"]))
}

// TestProxy_Post_ToolsCallArgumentRewriteKeepsOtherParams covers the
// request-side commit. A multi round-trip retry carries `inputResponses` and
// `requestState` alongside `arguments`; dropping either strands the round trip.
func TestProxy_Post_ToolsCallArgumentRewriteKeepsOtherParams(t *testing.T) {
	t.Parallel()

	var forwarded atomic.Pointer[string]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen := string(body)
		forwarded.Store(&seen)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":4,"result":{"content":[]}}`))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.ToolsCallRequestInterceptors = []proxy.ToolsCallRequestInterceptor{
		&mutatingToolsCallRequestInterceptor{
			name:   "scrub-arguments",
			argsFn: func(json.RawMessage) json.RawMessage { return json.RawMessage(`{"scrubbed":true}`) },
			err:    nil,
		},
	}

	_, err := postJSON(t, p, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{`+
		`"name":"do_thing",`+
		`"arguments":{"secret":"strip me"},`+
		`"requestState":"AEAD-protected blob",`+
		`"inputResponses":{"github_login":{"action":"accept"}}`+
		`}}`)
	require.NoError(t, err)

	seen := forwarded.Load()
	require.NotNil(t, seen)

	var envelope struct {
		Params map[string]json.RawMessage `json:"params"`
	}
	require.NoError(t, json.Unmarshal([]byte(*seen), &envelope))
	require.JSONEq(t, `{"scrubbed":true}`, string(envelope.Params["arguments"]), "the rewrite must reach upstream")
	require.JSONEq(t, `"do_thing"`, string(envelope.Params["name"]))
	require.JSONEq(t, `"AEAD-protected blob"`, string(envelope.Params["requestState"]),
		"multi round-trip state must survive an argument rewrite")
	require.JSONEq(t, `{"github_login":{"action":"accept"}}`, string(envelope.Params["inputResponses"]),
		"multi round-trip responses must survive an argument rewrite")
}
