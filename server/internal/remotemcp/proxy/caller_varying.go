package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

// callerVaryingMethods are the methods whose results carry the MCP 2026-07-28
// cacheScope and ttlMs hints the proxy may relabel: the list methods, and
// resources/read.
var callerVaryingMethods = []string{
	methodToolsList,
	methodResourcesList,
	mcpversions.MethodResourcesTemplatesList,
	mcpversions.MethodPromptsList,
	methodResourcesRead,
}

// callerVaryingCacheable is the cache stance the proxy stamps on a result it
// labels with [cacheLabelPrivateZeroTTL], matching the hosted surface's
// cacheHintsCallerVarying so both paths label the same property identically.
// [cacheLabelPrivate] takes only its cacheScope, plus its ttlMs where the
// upstream declared none.
var callerVaryingCacheable = mcp.Cacheable{TTLMs: 0, CacheScope: "private"}

// cacheLabel is the cache stance the proxy applies to one relayed result.
type cacheLabel string

const (
	// cacheLabelUpstream relays the upstream's own cache hints untouched.
	cacheLabelUpstream cacheLabel = "upstream"

	// cacheLabelPrivate overwrites cacheScope with "private" and keeps the
	// upstream's ttlMs, writing 0 only where the upstream declared none. The
	// retained copy then belongs to the requesting caller alone, and the
	// upstream's freshness hint still describes content no Gram filter
	// reshaped.
	cacheLabelPrivate cacheLabel = "private"

	// cacheLabelPrivateZeroTTL overwrites cacheScope with "private" and ttlMs
	// with 0. It applies where a Gram filter can reshape the result: an
	// upstream ttl would let the requesting client keep serving a filtered
	// result from cache after the grants or selection that shaped it changed.
	cacheLabelPrivateZeroTTL cacheLabel = "private-zero-ttl"
)

// callerUniform reports whether nothing Gram places between the caller and
// the upstream varies by caller, so the upstream's own cache hints describe
// the relayed result as truthfully as they would describe a direct reply.
//
// That holds only for an anonymous caller of a public server
// ([Proxy.AnonymousCaller]) when the proxy forwards no per-caller credential
// or configured pass-through header and attaches no list filter. Each of
// those is variance Gram introduces and the upstream cannot account for: an
// access gate an intermediary would bypass by serving a retained copy to
// another caller, a filter that rewrites a list per principal, or a
// caller-derived value Gram injects upstream on an admin's say-so.
//
// Caller headers forwarded verbatim do not count. The upstream receives them
// exactly as a client connecting directly would send them, so declaring
// whether its result varies on them is the upstream's to do.
func (p *Proxy) callerUniform() bool {
	return p.AnonymousCaller &&
		p.AuthorizationOverride == "" &&
		p.CallerAssertion == nil &&
		len(p.ToolsListResponseInterceptors) == 0 &&
		len(p.ResourcesListResponseInterceptors) == 0 &&
		!slices.ContainsFunc(p.Headers, func(h ConfiguredHeader) bool { return h.ValueFromRequestHeader != "" })
}

// filtersMethod reports whether a response interceptor attached for method
// can reshape its result. Only the tools/list and resources/list chains
// filter; the resources/read chain observes for usage tracking.
func (p *Proxy) filtersMethod(method string) bool {
	switch method {
	case methodToolsList:
		return len(p.ToolsListResponseInterceptors) > 0
	case methodResourcesList:
		return len(p.ResourcesListResponseInterceptors) > 0
	default:
		return false
	}
}

// requestCacheLabel picks the label for the reply to the user request req,
// carried on r. A request for none of [callerVaryingMethods] relays its reply
// untouched.
//
// The method check reads the method alone rather than requiring a decoded
// typed request view, so a request whose params fail to decode is still
// labelled. It also scans the raw body for every top-level method member,
// since the decoded method keeps only the last of duplicate members while an
// upstream may honour the first. A filter attached for any of them zeroes the
// ttl.
func (p *Proxy) requestCacheLabel(r *http.Request, req *UserRequest) cacheLabel {
	if req == nil {
		return cacheLabelUpstream
	}

	var requested, filtered bool
	decoded := userRequestMethod(req)
	for _, method := range callerVaryingMethods {
		if decoded == method || hasTopLevelJSONRPCMethod(req.body, method) {
			requested = true
			filtered = filtered || p.filtersMethod(method)
		}
	}
	if !requested {
		return cacheLabelUpstream
	}
	return p.resolveCacheLabel(r, filtered)
}

// streamCacheLabel picks the label for every reply on the standalone GET
// stream opened by r. A client resuming a dropped POST stream with
// Last-Event-ID can receive a replayed reply there, and nothing on the stream
// says which request a reply answers, so the label assumes any attached
// filter shaped it. It is meaningless on a reply to any other method, but
// harmless.
func (p *Proxy) streamCacheLabel(r *http.Request) cacheLabel {
	return p.resolveCacheLabel(r, p.filtersMethod(methodToolsList) || p.filtersMethod(methodResourcesList))
}

// resolveCacheLabel resolves the label for a result relayed in answer to r,
// where filtered reports that a Gram filter may have reshaped it.
func (p *Proxy) resolveCacheLabel(r *http.Request, filtered bool) cacheLabel {
	switch {
	case p.callerUniform(), declaresRevisionWithoutCacheHints(r):
		return cacheLabelUpstream
	case filtered:
		return cacheLabelPrivateZeroTTL
	default:
		return cacheLabelPrivate
	}
}

// declaresRevisionWithoutCacheHints reports whether r names, in its
// MCP-Protocol-Version header, a recognized revision older than 2026-07-28.
// Results under those revisions define no cache hints, so no cache reads a
// label and there is nothing for one to describe.
//
// A request naming no revision, or one this package does not recognize, is
// not exempt. The declared revision is client input, but it only decides the
// label on that client's own reply, and the client and upstream already
// agreed on it between themselves.
func declaresRevisionWithoutCacheHints(r *http.Request) bool {
	if r == nil {
		return false
	}
	v := mcpversions.Sanitize(r.Header.Get(mcpversions.HTTPHeader))
	return mcpversions.Known(v) && !mcpversions.AtLeast(v, mcpversions.Version20260728)
}

// applyCacheLabel rewrites the cache hints of msg's result per label and
// flags msg for re-encode. cacheScope is overwritten rather than filled: an
// upstream declaring its own result public cannot account for the access gate
// and per-principal filters Gram puts in front of it.
//
// It works on the raw result rather than a typed view, so a result that
// fails the strict typed decode is labelled the same way. A response carrying
// both an error and a result object is labelled too, since a lenient client
// may read the result whenever one is present. Only the caching members are
// spliced; the payload itself and every other member keep their original
// values. Messages that carry no result to label are left untouched: anything
// that is not a [*jsonrpc.Response], and results that are absent or not JSON
// objects (including a literal null, which the splice would otherwise turn
// into an object).
//
// Returns a [*MutationError] when the splice fails, leaving msg unchanged.
func applyCacheLabel(msg *RemoteMessage, label cacheLabel) error {
	if label == cacheLabelUpstream {
		return nil
	}
	rpcResp, ok := msg.Message.(*jsonrpc.Response)
	if !ok || firstNonWhitespaceByte(rpcResp.Result) != '{' {
		return nil
	}

	ttlMs := json.RawMessage(strconv.Itoa(callerVaryingCacheable.TTLMs))
	replacements := map[string]json.RawMessage{
		"cacheScope": json.RawMessage(strconv.Quote(callerVaryingCacheable.CacheScope)),
	}
	defaults := map[string]json.RawMessage{"ttlMs": ttlMs}
	if label == cacheLabelPrivateZeroTTL {
		replacements["ttlMs"] = ttlMs
		defaults = nil
	}

	// Both members go in on one splice, since a chained pair would re-decode
	// the whole result, list included, a second time.
	result, err := spliceTopLevelKeysWithDefaults(rpcResp.Result, replacements, defaults)
	if err != nil {
		return &MutationError{Op: "label cache hints", Cause: fmt.Errorf("splice cache hints: %w", err)}
	}

	rpcResp.Result = result
	msg.dirty = true
	return nil
}
