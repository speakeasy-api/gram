package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

// cacheLabel is the cache stance the proxy applies to one relayed result.
type cacheLabel string

const (
	// cacheLabelUpstream relays the upstream's own cache hints untouched.
	cacheLabelUpstream cacheLabel = "upstream"

	// cacheLabelPrivate sets cacheScope "private" and keeps the upstream's
	// ttlMs, or 0 when it declared none.
	cacheLabelPrivate cacheLabel = "private"

	// cacheLabelPrivateZeroTTL sets cacheScope "private" and ttlMs 0, for
	// results a Gram filter can reshape: an upstream ttl would let a client keep
	// serving a filtered result after the grants that shaped it changed.
	cacheLabelPrivateZeroTTL cacheLabel = "private-zero-ttl"
)

// callerUniform reports whether nothing Gram places between the caller and
// the upstream varies by caller, so the upstream's own cache hints describe
// the relayed result as truthfully as they would a direct reply.
//
// That holds only for an anonymous caller of a public server when the proxy
// forwards no per-caller credential or configured pass-through header and
// attaches no list filter. Each of those is variance the upstream cannot see:
// an access gate an intermediary would bypass by serving a retained copy to
// another caller, a filter that rewrites a list per principal, or a
// caller-derived value Gram injects upstream. Caller headers forwarded
// verbatim do not count, since the upstream sees them exactly as on a direct
// connection and can label for them itself.
func (p *Proxy) callerUniform() bool {
	return p.AnonymousCaller &&
		p.AuthorizationOverride == "" &&
		p.CallerAssertion == nil &&
		len(p.ToolsListResponseInterceptors) == 0 &&
		len(p.ResourcesListResponseInterceptors) == 0 &&
		!slices.ContainsFunc(p.Headers, func(h ConfiguredHeader) bool { return h.ValueFromRequestHeader != "" })
}

// requestCacheLabel picks the label for the reply to req. The raw body is
// scanned for every top-level method member as well, since decoding keeps only
// the last of duplicate members while an upstream may honour the first.
func (p *Proxy) requestCacheLabel(r *http.Request, req *UserRequest) cacheLabel {
	// The methods whose results carry MCP 2026-07-28 cache hints, and whether
	// an attached Gram filter can reshape each.
	methods := []struct {
		name     string
		filtered bool
	}{
		{name: mcpversions.MethodToolsList, filtered: len(p.ToolsListResponseInterceptors) > 0},
		{name: mcpversions.MethodResourcesList, filtered: len(p.ResourcesListResponseInterceptors) > 0},
		{name: mcpversions.MethodResourcesTemplatesList, filtered: false},
		{name: mcpversions.MethodPromptsList, filtered: false},
		{name: mcpversions.MethodResourcesRead, filtered: false},
	}

	var requested, filtered bool
	decoded := userRequestMethod(req)
	for _, method := range methods {
		if decoded == method.name || hasTopLevelJSONRPCMethod(req.body, method.name) {
			requested = true
			filtered = filtered || method.filtered
		}
	}
	if !requested {
		return cacheLabelUpstream
	}
	return p.resolveCacheLabel(r, filtered)
}

// resolveCacheLabel picks the label for a result relayed in answer to r,
// where filtered reports that a Gram filter may have reshaped it.
func (p *Proxy) resolveCacheLabel(r *http.Request, filtered bool) cacheLabel {
	// Revisions before 2026-07-28 define no cache hints. An absent or
	// unrecognized revision is not exempt.
	revision := mcpversions.Sanitize(r.Header.Get(mcpversions.HTTPHeader))
	predatesCacheHints := mcpversions.Known(revision) && !mcpversions.AtLeast(revision, mcpversions.Version20260728)

	switch {
	case p.callerUniform(), predatesCacheHints:
		return cacheLabelUpstream
	case filtered:
		return cacheLabelPrivateZeroTTL
	default:
		return cacheLabelPrivate
	}
}

// applyCacheLabel splices label's cache hints into msg's result and flags msg
// for re-encode. cacheScope is overwritten rather than filled, because an
// upstream declaring its own result public cannot account for the access gate
// and filters Gram puts in front of it.
//
// It works on the raw result rather than a typed view, so results that fail
// the typed decode or arrive alongside an error are labelled too. Messages
// with no result object, including a literal null, are left untouched.
func applyCacheLabel(msg *RemoteMessage, label cacheLabel) error {
	if label == cacheLabelUpstream {
		return nil
	}
	rpcResp, ok := msg.Message.(*jsonrpc.Response)
	if !ok || firstNonWhitespaceByte(rpcResp.Result) != '{' {
		return nil
	}

	replacements := map[string]json.RawMessage{"cacheScope": json.RawMessage(`"private"`)}
	if label == cacheLabelPrivateZeroTTL {
		replacements["ttlMs"] = json.RawMessage(`0`)
	}
	result, err := spliceTopLevelKeys(rpcResp.Result, replacements, map[string]json.RawMessage{"ttlMs": json.RawMessage(`0`)})
	if err != nil {
		return &MutationError{Op: "label cache hints", Cause: fmt.Errorf("splice cache hints: %w", err)}
	}

	rpcResp.Result = result
	msg.dirty = true
	return nil
}
