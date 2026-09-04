package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// spliceTopLevelKey returns object — a JSON object payload such as a
// JSON-RPC result or params document — with its top-level key member
// replaced by value, leaving every other member in place. Mutation setters
// ([ToolsListResponse.SetTools], [ToolsCallRequest.SetArguments],
// [ResourcesListResponse.SetResources], and any added later) MUST route
// payload rewrites through this splice rather than re-marshaling a typed
// SDK struct: a typed round-trip silently deletes every member the struct
// does not model, stripping protocol fields the SDK has not caught up with
// (under MCP 2026-07-28, the required resultType, ttlMs, and cacheScope
// tools/list result members) from an otherwise byte-for-byte relay.
//
// Preserved members keep their original value bytes — numbers do not lose
// precision and escape sequences are not rewritten — though insignificant
// whitespace is compacted and top-level member order is not retained,
// matching what the jsonrpc.EncodeMessage envelope re-encode already does
// to every mutated message. Unknown members of the JSON-RPC envelope itself
// live outside the result/params payload and are beyond this helper's
// reach; that envelope encoder still drops them, which JSON-RPC 2.0's
// closed envelope object makes acceptable.
//
// A zero-length value deletes the member, mirroring the omitempty encoding
// of the typed fields this splice replaces. A literal null object is
// treated as an empty object. Duplicate top-level keys in object collapse
// to the last occurrence, exactly as encoding/json decodes them — a
// property filter setters rely on, since preserving an earlier duplicate
// would let upstream smuggle the unfiltered value past the mutation.
//
// The final marshal is what validates value: caller-supplied bytes that are
// not well-formed JSON surface as an error here instead of reaching the
// wire. Do not replace the map round-trip with raw byte assembly — that
// validation would silently disappear.
func spliceTopLevelKey(object json.RawMessage, key string, value json.RawMessage) (json.RawMessage, error) {
	return spliceTopLevelKeys(object, map[string]json.RawMessage{key: value})
}

// spliceTopLevelKeys is spliceTopLevelKey for several members at once,
// applying every replacement in a single decode and re-encode. Chaining
// single-key splices instead costs one full round trip of the whole payload
// per member — on a tools/list result that means re-decoding the entire tools
// array once per key rewritten.
//
// Every rule spliceTopLevelKey documents applies unchanged to each entry.
func spliceTopLevelKeys(object json.RawMessage, replacements map[string]json.RawMessage) (json.RawMessage, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(object, &members); err != nil {
		return nil, fmt.Errorf("decode payload object: %w", err)
	}
	// A literal null decodes successfully into a nil map.
	if members == nil {
		members = make(map[string]json.RawMessage, len(replacements))
	}

	for key, value := range replacements {
		if len(value) == 0 {
			delete(members, key)
		} else {
			members[key] = value
		}
	}

	// json.Encoder rather than json.Marshal so preserved and replacement
	// bytes are not HTML-escaped, matching the SDK's envelope encoder.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(members); err != nil {
		return nil, fmt.Errorf("encode payload object: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}), nil
}

// marshalJSONNoHTMLEscape marshals v like json.Marshal but without HTML
// escaping. Replacement values handed to spliceTopLevelKey must be encoded
// this way: the splice and the SDK envelope encoder both leave literal <,
// >, and & untouched in preserved members, so an HTML-escaped replacement
// would relay <-escaped alongside neighbors that keep their literal
// characters.
func marshalJSONNoHTMLEscape(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode replacement value: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}), nil
}
