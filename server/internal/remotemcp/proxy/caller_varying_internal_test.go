package proxy

import (
	"encoding/json"
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

// TestMarkCallerVaryingFlipsDirty pins the step that actually gets the label
// onto the wire. Without the dirty flag the proxy relays the upstream body
// untouched and the spliced result is never emitted, yet every assertion made
// against rpcResp.Result alone would still pass.
func TestMarkCallerVaryingFlipsDirty(t *testing.T) {
	t.Parallel()

	msg := remoteResponse(`{"tools":[{"name":"a","inputSchema":{}}]}`)

	require.NoError(t, markCallerVarying(msg))
	require.True(t, msg.dirty, "the label only reaches the client if the message is re-materialized")

	body, ok, err := msg.materializedBytes()
	require.NoError(t, err)
	require.True(t, ok, "a marked message must re-encode")
	require.Contains(t, string(body), `"cacheScope":"private"`)
	require.Contains(t, string(body), `"ttlMs":0`)
}

func TestMarkCallerVaryingOverwritesUpstreamHints(t *testing.T) {
	t.Parallel()

	// An upstream declaring itself publicly cacheable for a minute cannot
	// have accounted for the caller-supplied inputs and filters in front of
	// it, so both members overwrite rather than fill.
	msg := remoteResponse(`{"ttlMs":60000,"cacheScope":"public","tools":[]}`)

	require.NoError(t, markCallerVarying(msg))

	members := resultMembers(t, msg)
	require.JSONEq(t, `"private"`, string(members["cacheScope"]))
	require.JSONEq(t, `0`, string(members["ttlMs"]))
}

func TestMarkCallerVaryingLeavesListAndUnknownMembers(t *testing.T) {
	t.Parallel()

	// Only the two caching members are rewritten: the list member keeps its
	// original bytes and members the SDK does not model relay untouched.
	msg := remoteResponse(`{"resultType":"complete","nextCursor":"c1",` +
		`"futureUnknownField":{"deep":[1,2,3]},` +
		`"tools":[{"name":"a","inputSchema":{},"x-vendor":"keep"}]}`)
	before := resultMembers(t, msg)

	require.NoError(t, markCallerVarying(msg))

	after := resultMembers(t, msg)
	require.Equal(t, string(before["tools"]), string(after["tools"]))
	require.JSONEq(t, `"complete"`, string(after["resultType"]))
	require.JSONEq(t, `"c1"`, string(after["nextCursor"]))
	require.JSONEq(t, `{"deep":[1,2,3]}`, string(after["futureUnknownField"]))
}

// TestMarkCallerVaryingLabelsUndecodableResult covers results the typed views
// reject: the label works on the raw object, so a type mismatch inside the
// list does not leave the result unlabelled.
func TestMarkCallerVaryingLabelsUndecodableResult(t *testing.T) {
	t.Parallel()

	payload := `{"tools":[{"name":"a","inputSchema":{},"annotations":{"readOnlyHint":"true"}}]}`
	require.Error(t, json.Unmarshal([]byte(payload), new(mcp.ListToolsResult)),
		"fixture must fail the typed decode")
	msg := remoteResponse(payload)

	require.NoError(t, markCallerVarying(msg))

	members := resultMembers(t, msg)
	require.JSONEq(t, `"private"`, string(members["cacheScope"]))
	require.Contains(t, string(members["tools"]), `"readOnlyHint":"true"`)
}

// TestMarkCallerVaryingLabelsResultAlongsideError covers a malformed response
// carrying both members: a lenient client may read the result whenever one is
// present, so it must not relay unlabelled.
func TestMarkCallerVaryingLabelsResultAlongsideError(t *testing.T) {
	t.Parallel()

	msg := remoteResponse(`{"cacheScope":"public","tools":[{"name":"a","inputSchema":{}}]}`)
	rpcResp, ok := msg.Message.(*jsonrpc.Response)
	require.True(t, ok)
	rpcResp.Error = &jsonrpc.Error{Code: 1, Message: "x", Data: nil}

	require.NoError(t, markCallerVarying(msg))

	require.True(t, msg.dirty)
	members := resultMembers(t, msg)
	require.JSONEq(t, `"private"`, string(members["cacheScope"]))
	require.JSONEq(t, `0`, string(members["ttlMs"]))
}

func TestMarkCallerVaryingLeavesMessagesWithoutObjectResult(t *testing.T) {
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

			require.NoError(t, markCallerVarying(msg))

			require.False(t, msg.dirty, "a message with no result object must relay verbatim")
			after, err := jsonrpc.EncodeMessage(tc.msg)
			require.NoError(t, err)
			require.Equal(t, string(before), string(after))
		})
	}
}

// TestMarkCallerVaryingLeavesMessageCleanOnFailure covers the atomicity
// contract: a failed mark must not leave the message flagged for re-encode,
// or the proxy would emit a body no splice successfully produced.
func TestMarkCallerVaryingLeavesMessageCleanOnFailure(t *testing.T) {
	t.Parallel()

	msg := remoteResponse(`{"tools":`)

	err := markCallerVarying(msg)

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
	require.NoError(t, markCallerVarying(msg))

	rpcResp, ok := msg.Message.(*jsonrpc.Response)
	require.True(t, ok)
	var wire mcp.Cacheable
	require.NoError(t, json.Unmarshal(rpcResp.Result, &wire))
	require.Equal(t, callerVaryingCacheable, wire, "spliced bytes must decode back to the declared stance")
}

func TestIsCallerVaryingListRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want bool
	}{
		{name: "tools/list", body: `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, want: true},
		{name: "resources/list", body: `{"jsonrpc":"2.0","id":1,"method":"resources/list","params":{}}`, want: true},
		{name: "tools/list with undecodable params", body: `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"cursor":123}}`, want: true},
		{name: "tools/call", body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"a"}}`, want: false},
		{name: "resources/templates/list", body: `{"jsonrpc":"2.0","id":1,"method":"resources/templates/list"}`, want: false},
		{name: "prompts/list", body: `{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`, want: false},
		{name: "tools/list hidden by a later duplicate method", body: `{"jsonrpc":"2.0","id":1,"method":"tools/list","method":"ping"}`, want: true},
		{name: "resources/list hidden by a later duplicate method", body: `{"jsonrpc":"2.0","id":1,"method":"resources/list","method":"ping"}`, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			msg, err := jsonrpc.DecodeMessage([]byte(tc.body))
			require.NoError(t, err)
			req := &UserRequest{UserHTTPRequest: nil, JSONRPCMessages: []jsonrpc.Message{msg}, body: []byte(tc.body), dirty: false}
			require.Equal(t, tc.want, isCallerVaryingListRequest(req))
		})
	}
}
