package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// remoteResponse wraps a raw result payload in a remote message the way the
// proxy builds one from upstream bytes. Internal so the tests below can reach
// the unexported dirty flag.
func remoteResponse(result string) *RemoteMessage {
	return &RemoteMessage{
		UserHTTPRequest:    nil,
		RemoteHTTPRequest:  nil,
		RemoteHTTPResponse: nil,
		Message: &jsonrpc.Response{
			ID:     jsonrpc.ID{},
			Result: json.RawMessage(result),
			Error:  nil,
		},
		dirty: false,
	}
}

func resultMembers(t *testing.T, msg *RemoteMessage) map[string]json.RawMessage {
	t.Helper()

	rpcResp, ok := msg.Message.(*jsonrpc.Response)
	require.True(t, ok)
	var members map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rpcResp.Result, &members))
	return members
}

// TestApplyCacheLabelFlipsDirty pins the step that actually gets the label
// onto the wire. Without the dirty flag the proxy relays the upstream body
// untouched and the spliced result is never emitted, yet every assertion made
// against rpcResp.Result alone would still pass.
func TestApplyCacheLabelFlipsDirty(t *testing.T) {
	t.Parallel()

	msg := remoteResponse(`{"tools":[{"name":"a","inputSchema":{}}]}`)

	require.NoError(t, applyCacheLabel(msg, cacheLabelPrivateZeroTTL))
	require.True(t, msg.dirty, "the label only reaches the client if the message is re-materialized")

	body, ok, err := msg.materializedBytes()
	require.NoError(t, err)
	require.True(t, ok, "a marked message must re-encode")
	require.Contains(t, string(body), `"cacheScope":"private"`)
	require.Contains(t, string(body), `"ttlMs":0`)
}

func TestApplyCacheLabelZeroTTLOverwritesUpstreamHints(t *testing.T) {
	t.Parallel()

	// An upstream declaring itself publicly cacheable for a minute cannot
	// have accounted for the filters in front of it, so both members
	// overwrite rather than fill.
	msg := remoteResponse(`{"ttlMs":60000,"cacheScope":"public","tools":[]}`)

	require.NoError(t, applyCacheLabel(msg, cacheLabelPrivateZeroTTL))

	members := resultMembers(t, msg)
	require.JSONEq(t, `"private"`, string(members["cacheScope"]))
	require.JSONEq(t, `0`, string(members["ttlMs"]))
}

// TestApplyCacheLabelPrivateKeepsUpstreamTTL covers the label where no Gram
// filter shaped the result: the scope is overwritten, while the upstream's
// freshness hint still describes the content and is kept.
func TestApplyCacheLabelPrivateKeepsUpstreamTTL(t *testing.T) {
	t.Parallel()

	msg := remoteResponse(`{"ttlMs":60000,"cacheScope":"public","tools":[]}`)

	require.NoError(t, applyCacheLabel(msg, cacheLabelPrivate))

	members := resultMembers(t, msg)
	require.JSONEq(t, `"private"`, string(members["cacheScope"]))
	require.JSONEq(t, `60000`, string(members["ttlMs"]))
}

func TestApplyCacheLabelPrivateFillsAbsentTTL(t *testing.T) {
	t.Parallel()

	msg := remoteResponse(`{"tools":[]}`)

	require.NoError(t, applyCacheLabel(msg, cacheLabelPrivate))

	members := resultMembers(t, msg)
	require.JSONEq(t, `"private"`, string(members["cacheScope"]))
	require.JSONEq(t, `0`, string(members["ttlMs"]))
}

func TestApplyCacheLabelUpstreamLeavesMessage(t *testing.T) {
	t.Parallel()

	const result = `{"ttlMs":60000,"cacheScope":"public","tools":[]}`
	msg := remoteResponse(result)

	require.NoError(t, applyCacheLabel(msg, cacheLabelUpstream))

	require.False(t, msg.dirty)
	rpcResp, ok := msg.Message.(*jsonrpc.Response)
	require.True(t, ok)
	require.JSONEq(t, result, string(rpcResp.Result))
}

func TestApplyCacheLabelLeavesListAndUnknownMembers(t *testing.T) {
	t.Parallel()

	// Only the two caching members are rewritten: the list member keeps its
	// original bytes and members the SDK does not model relay untouched.
	msg := remoteResponse(`{"resultType":"complete","nextCursor":"c1",` +
		`"futureUnknownField":{"deep":[1,2,3]},` +
		`"tools":[{"name":"a","inputSchema":{},"x-vendor":"keep"}]}`)
	before := resultMembers(t, msg)

	require.NoError(t, applyCacheLabel(msg, cacheLabelPrivateZeroTTL))

	after := resultMembers(t, msg)
	require.Equal(t, string(before["tools"]), string(after["tools"]))
	require.JSONEq(t, `"complete"`, string(after["resultType"]))
	require.JSONEq(t, `"c1"`, string(after["nextCursor"]))
	require.JSONEq(t, `{"deep":[1,2,3]}`, string(after["futureUnknownField"]))
}

// TestApplyCacheLabelLabelsUndecodableResult covers results the typed views
// reject: the label works on the raw object, so a type mismatch inside the
// list does not leave the result unlabelled.
func TestApplyCacheLabelLabelsUndecodableResult(t *testing.T) {
	t.Parallel()

	payload := `{"tools":[{"name":"a","inputSchema":{},"annotations":{"readOnlyHint":"true"}}]}`
	require.Error(t, json.Unmarshal([]byte(payload), new(mcp.ListToolsResult)),
		"fixture must fail the typed decode")
	msg := remoteResponse(payload)

	require.NoError(t, applyCacheLabel(msg, cacheLabelPrivateZeroTTL))

	members := resultMembers(t, msg)
	require.JSONEq(t, `"private"`, string(members["cacheScope"]))
	require.Contains(t, string(members["tools"]), `"readOnlyHint":"true"`)
}

// TestApplyCacheLabelLabelsResultAlongsideError covers a malformed response
// carrying both members: a lenient client may read the result whenever one is
// present, so it must not relay unlabelled.
func TestApplyCacheLabelLabelsResultAlongsideError(t *testing.T) {
	t.Parallel()

	msg := remoteResponse(`{"cacheScope":"public","tools":[{"name":"a","inputSchema":{}}]}`)
	rpcResp, ok := msg.Message.(*jsonrpc.Response)
	require.True(t, ok)
	rpcResp.Error = &jsonrpc.Error{Code: 1, Message: "x", Data: nil}

	require.NoError(t, applyCacheLabel(msg, cacheLabelPrivateZeroTTL))

	require.True(t, msg.dirty)
	members := resultMembers(t, msg)
	require.JSONEq(t, `"private"`, string(members["cacheScope"]))
	require.JSONEq(t, `0`, string(members["ttlMs"]))
}

func TestApplyCacheLabelLeavesMessagesWithoutObjectResult(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		msg  jsonrpc.Message
	}{
		{
			name: "error response",
			msg: &jsonrpc.Response{
				ID:     jsonrpc.ID{},
				Result: nil,
				Error:  &jsonrpc.Error{Code: -32601, Message: "method not found", Data: nil},
			},
		},
		{name: "absent result", msg: &jsonrpc.Response{ID: jsonrpc.ID{}, Result: nil, Error: nil}},
		{name: "null result", msg: &jsonrpc.Response{ID: jsonrpc.ID{}, Result: json.RawMessage(`null`), Error: nil}},
		{name: "array result", msg: &jsonrpc.Response{ID: jsonrpc.ID{}, Result: json.RawMessage(` [1]`), Error: nil}},
		{name: "string result", msg: &jsonrpc.Response{ID: jsonrpc.ID{}, Result: json.RawMessage(`"ok"`), Error: nil}},
		{name: "notification", msg: &jsonrpc.Request{ID: jsonrpc.ID{}, Method: "notifications/progress", Params: json.RawMessage(`{}`)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			msg := &RemoteMessage{
				UserHTTPRequest:    nil,
				RemoteHTTPRequest:  nil,
				RemoteHTTPResponse: nil,
				Message:            tc.msg,
				dirty:              false,
			}
			before, err := jsonrpc.EncodeMessage(tc.msg)
			require.NoError(t, err)

			require.NoError(t, applyCacheLabel(msg, cacheLabelPrivateZeroTTL))

			require.False(t, msg.dirty, "a message with no result object must relay verbatim")
			after, err := jsonrpc.EncodeMessage(tc.msg)
			require.NoError(t, err)
			require.Equal(t, string(before), string(after))
		})
	}
}

// TestApplyCacheLabelLeavesMessageCleanOnFailure covers the atomicity
// contract: a failed mark must not leave the message flagged for re-encode,
// or the proxy would emit a body no splice successfully produced.
func TestApplyCacheLabelLeavesMessageCleanOnFailure(t *testing.T) {
	t.Parallel()

	msg := remoteResponse(`{"tools":`)

	err := applyCacheLabel(msg, cacheLabelPrivateZeroTTL)

	var mutErr *MutationError
	require.ErrorAs(t, err, &mutErr)
	require.False(t, msg.dirty, "a failed mark must not flag a re-encode")
	rpcResp, ok := msg.Message.(*jsonrpc.Response)
	require.True(t, ok)
	require.Equal(t, `{"tools":`, string(rpcResp.Result), "a failed mark must not rewrite the result")
}

// TestCallerVaryingHintsDerivedFromCacheable guards the coupling between the
// declared stance and the bytes spliced onto the wire: the two must not be
// independently maintained literals that can drift apart.
func TestCallerVaryingHintsDerivedFromCacheable(t *testing.T) {
	t.Parallel()

	msg := remoteResponse(`{"tools":[]}`)
	require.NoError(t, applyCacheLabel(msg, cacheLabelPrivateZeroTTL))

	rpcResp, ok := msg.Message.(*jsonrpc.Response)
	require.True(t, ok)
	var wire mcp.Cacheable
	require.NoError(t, json.Unmarshal(rpcResp.Result, &wire))
	require.Equal(t, callerVaryingCacheable, wire, "spliced bytes must decode back to the declared stance")
}

// userRequestOf decodes body into a single-message user request the way the
// proxy builds one from an inbound POST.
func userRequestOf(t *testing.T, body string) *UserRequest {
	t.Helper()

	msg, err := jsonrpc.DecodeMessage([]byte(body))
	require.NoError(t, err)
	return &UserRequest{UserHTTPRequest: nil, JSONRPCMessages: []jsonrpc.Message{msg}, body: []byte(body), dirty: false}
}

func TestRequestCacheLabelByMethod(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want cacheLabel
	}{
		{name: "tools/list", body: `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, want: cacheLabelPrivate},
		{name: "resources/list", body: `{"jsonrpc":"2.0","id":1,"method":"resources/list","params":{}}`, want: cacheLabelPrivate},
		{name: "tools/list with undecodable params", body: `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"cursor":123}}`, want: cacheLabelPrivate},
		{name: "tools/call", body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"a"}}`, want: cacheLabelUpstream},
		{name: "resources/templates/list", body: `{"jsonrpc":"2.0","id":1,"method":"resources/templates/list"}`, want: cacheLabelPrivate},
		{name: "prompts/list", body: `{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`, want: cacheLabelPrivate},
		{name: "resources/read", body: `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"file:///a"}}`, want: cacheLabelPrivate},
		{name: "initialize", body: `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, want: cacheLabelUpstream},
		{name: "tools/list hidden by a later duplicate method", body: `{"jsonrpc":"2.0","id":1,"method":"tools/list","method":"ping"}`, want: cacheLabelPrivate},
		{name: "resources/list hidden by a later duplicate method", body: `{"jsonrpc":"2.0","id":1,"method":"resources/list","method":"ping"}`, want: cacheLabelPrivate},
		{name: "prompts/list hidden by a later duplicate method", body: `{"jsonrpc":"2.0","id":1,"method":"prompts/list","method":"ping"}`, want: cacheLabelPrivate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := &Proxy{}
			require.Equal(t, tc.want, p.requestCacheLabel(nil, userRequestOf(t, tc.body)))
		})
	}
}

// TestRequestCacheLabelZeroesTTLForFilteredMethod pins that a filter zeroes
// the ttl only on the method it filters, including when a duplicate method
// member hides that method from the decoded request.
func TestRequestCacheLabelZeroesTTLForFilteredMethod(t *testing.T) {
	t.Parallel()

	p := &Proxy{ToolsListResponseInterceptors: []ToolsListResponseInterceptor{nil}}

	require.Equal(t, cacheLabelPrivateZeroTTL, p.requestCacheLabel(nil, userRequestOf(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	require.Equal(t, cacheLabelPrivateZeroTTL, p.requestCacheLabel(nil, userRequestOf(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list","method":"prompts/list"}`)))
	require.Equal(t, cacheLabelPrivate, p.requestCacheLabel(nil, userRequestOf(t, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)))
}

func TestCallerUniform(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		configure func(p *Proxy)
		want      bool
	}{
		{name: "anonymous", configure: func(*Proxy) {}, want: true},
		{name: "not anonymous", configure: func(p *Proxy) { p.AnonymousCaller = false }, want: false},
		{name: "authorization override", configure: func(p *Proxy) { p.AuthorizationOverride = "token" }, want: false},
		{
			name: "caller assertion",
			configure: func(p *Proxy) {
				p.CallerAssertion = func(context.Context) (string, error) { return "", nil }
			},
			want: false,
		},
		{
			name: "static header",
			configure: func(p *Proxy) {
				p.Headers = []ConfiguredHeader{{Name: "X-Api-Key", StaticValue: "shared", ValueFromRequestHeader: "", IsRequired: false}}
			},
			want: true,
		},
		{
			name: "pass-through header",
			configure: func(p *Proxy) {
				p.Headers = []ConfiguredHeader{{Name: "X-Tenant", StaticValue: "", ValueFromRequestHeader: "X-Caller-Tenant", IsRequired: false}}
			},
			want: false,
		},
		{name: "tools/list filter", configure: func(p *Proxy) { p.ToolsListResponseInterceptors = []ToolsListResponseInterceptor{nil} }, want: false},
		{name: "resources/list filter", configure: func(p *Proxy) { p.ResourcesListResponseInterceptors = []ResourcesListResponseInterceptor{nil} }, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := &Proxy{AnonymousCaller: true}
			tc.configure(p)
			require.Equal(t, tc.want, p.callerUniform())
		})
	}
}

func TestDeclaresRevisionWithoutCacheHints(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		revision string
		want     bool
	}{
		{name: "2024-11-05", revision: "2024-11-05", want: true},
		{name: "2025-11-25", revision: "2025-11-25", want: true},
		{name: "2026-07-28", revision: "2026-07-28", want: false},
		{name: "unrecognized", revision: "draft", want: false},
		{name: "absent", revision: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", http.NoBody)
			if tc.revision != "" {
				r.Header.Set("MCP-Protocol-Version", tc.revision)
			}
			require.Equal(t, tc.want, declaresRevisionWithoutCacheHints(r))
		})
	}
}
