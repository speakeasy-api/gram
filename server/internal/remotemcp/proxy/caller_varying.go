package proxy

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// callerVaryingCacheable is the cache stance the proxy stamps on every
// tools/list and resources/list result it relays, matching the hosted
// surface's cacheHintsCallerVarying so both paths label the same property
// identically. The zero ttlMs is part of the stance: an upstream's own ttl
// would let the requesting client keep serving a list from cache after the
// grants or upstream credentials that shaped it changed.
var callerVaryingCacheable = mcp.Cacheable{TTLMs: 0, CacheScope: "private"}

// isCallerVaryingListRequest reports whether req is a single tools/list or
// resources/list request, whose result the proxy labels caller-varying.
//
// The label is unconditional because a proxied list varies by caller on
// axes no interceptor sees: header pass-through forwards caller-supplied
// header values upstream, the caller's own OAuth token or a signed caller
// assertion can reach the upstream, and RBAC and consent selection filters
// rewrite the list per principal. Server visibility gates none of the first
// three, so a public server is no more shareable than a private one. MCP
// reads an absent cacheScope as public, so an unlabelled result would let a
// shared intermediary serve one caller's list to another.
//
// The check reads the method alone rather than requiring a decoded typed
// request view, so a list request whose params fail to decode is still
// labelled. It also scans the raw body for every top-level method member,
// since the decoded method keeps only the last of duplicate members while an
// upstream may honour the first.
func isCallerVaryingListRequest(req *UserRequest) bool {
	switch userRequestMethod(req) {
	case methodToolsList, methodResourcesList:
		return true
	}
	if req == nil {
		return false
	}
	return hasTopLevelJSONRPCMethod(req.body, methodToolsList) ||
		hasTopLevelJSONRPCMethod(req.body, methodResourcesList)
}

// markCallerVarying overwrites the cacheScope and ttlMs members of msg's
// result with [callerVaryingCacheable] and flags msg for re-encode. The
// members are overwritten rather than filled: an upstream declaring its own
// result public and long-lived cannot account for the caller-supplied inputs
// and per-principal filters Gram puts in front of it.
//
// It works on the raw result rather than a typed view, so a result that
// fails the strict typed decode is labelled the same way. A response carrying
// both an error and a result object is labelled too, since a lenient client
// may read the result whenever one is present. Only the two caching members
// are spliced; the list itself and every other member keep their original
// values. Messages that carry no result to label are left untouched: anything
// that is not a [*jsonrpc.Response], and results that are absent or not JSON
// objects (including a literal null, which the splice would otherwise turn
// into an object).
//
// Returns a [*MutationError] when the splice fails, leaving msg unchanged.
func markCallerVarying(msg *RemoteMessage) error {
	rpcResp, ok := msg.Message.(*jsonrpc.Response)
	if !ok || firstNonWhitespaceByte(rpcResp.Result) != '{' {
		return nil
	}

	// Both members go in on one splice, since a chained pair would re-decode
	// the whole result, list included, a second time.
	result, err := spliceTopLevelKeys(rpcResp.Result, map[string]json.RawMessage{
		"cacheScope": json.RawMessage(strconv.Quote(callerVaryingCacheable.CacheScope)),
		"ttlMs":      json.RawMessage(strconv.Itoa(callerVaryingCacheable.TTLMs)),
	})
	if err != nil {
		return &MutationError{Op: "mark caller-varying", Cause: fmt.Errorf("splice cache hints: %w", err)}
	}

	rpcResp.Result = result
	msg.dirty = true
	return nil
}
