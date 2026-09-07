package mcprequests

import (
	"encoding/json"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

// maxCapabilityKeys bounds how many capability keys are retained from one
// request. Under 2026-07-28 the capabilities map is REQUIRED on every request,
// which makes it client-supplied input arriving at request rate rather than
// once per handshake; a real client advertises a handful of keys, so anything
// past this cap is noise or abuse.
const maxCapabilityKeys = 20

// WireMeta is the on-the-wire `_meta` object of an MCP request's params,
// unsanitized. It exists so a handler that already unmarshals its params can
// pick up `_meta` in the same pass — declare a `*WireMeta` field with the
// `_meta` json tag — instead of re-scanning the document with [ParseMeta].
// Decoding is tolerant and never errors, so embedding one cannot change the
// host struct's decode behavior. Consumers must go through
// [WireMeta.Sanitize] before storing or recording any field.
type WireMeta struct {
	// ProtocolVersion is the raw `io.modelcontextprotocol/protocolVersion`
	// value.
	ProtocolVersion string `json:"io.modelcontextprotocol/protocolVersion"`

	// ClientInfo is the raw `io.modelcontextprotocol/clientInfo` value.
	ClientInfo *WireClientInfo `json:"io.modelcontextprotocol/clientInfo"`

	// Capabilities is the raw `io.modelcontextprotocol/clientCapabilities`
	// object, keyed by capability name.
	Capabilities map[string]json.RawMessage `json:"io.modelcontextprotocol/clientCapabilities"`
}

// UnmarshalJSON decodes `_meta` tolerantly and never returns an error: this
// is observability metadata, so a `_meta` that is not an object, or a member
// of the wrong type, must leave the corresponding fields zero rather than
// fail the RPC whose params struct embeds a WireMeta. Each member decodes
// independently, so one mis-typed member cannot discard the others.
func (w *WireMeta) UnmarshalJSON(data []byte) error {
	*w = WireMeta{ProtocolVersion: "", ClientInfo: nil, Capabilities: nil}

	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}

	_ = json.Unmarshal(raw["io.modelcontextprotocol/protocolVersion"], &w.ProtocolVersion)

	var info map[string]json.RawMessage
	if json.Unmarshal(raw["io.modelcontextprotocol/clientInfo"], &info) == nil && info != nil {
		w.ClientInfo = &WireClientInfo{Name: "", Version: ""}
		_ = json.Unmarshal(info["name"], &w.ClientInfo.Name)
		_ = json.Unmarshal(info["version"], &w.ClientInfo.Version)
	}

	_ = json.Unmarshal(raw["io.modelcontextprotocol/clientCapabilities"], &w.Capabilities)

	return nil
}

// WireClientInfo is an MCP `Implementation` value exactly as a client
// reported it, unsanitized.
type WireClientInfo struct {
	// Name is the client's self-reported name.
	Name string `json:"name"`

	// Version is the client's self-reported version.
	Version string `json:"version"`
}

// SanitizedClientInfo is an MCP `Implementation` value as self-reported by a
// client, with both fields passed through [SanitizeClientInfoField].
// Untrusted in origin — display/analytics data, never an input to
// authorization — but safe to store and record as-is.
type SanitizedClientInfo struct {
	// Name is the client's self-reported name, e.g. "claude-code".
	Name string

	// Version is the client's self-reported version, e.g. "1.2.3".
	Version string
}

// SanitizedMeta is the per-request metadata Gram reads from an MCP request's
// params. Every field is sanitized on decode, so consumers may store or
// record them directly.
type SanitizedMeta struct {
	// ProtocolVersion is the protocol revision this request declared via the
	// `io.modelcontextprotocol/protocolVersion` `_meta` key (2026-07-28). It
	// carries the same negotiated-version semantics as the
	// MCP-Protocol-Version header, which the spec requires to mirror it.
	// Empty for handshake-era requests, whose version arrives in the header
	// instead — deliberately NOT filled from the top-level `protocolVersion`
	// field of a legacy `initialize` body, which is a *requested* version
	// with different semantics and is recorded by the initialize handlers.
	ProtocolVersion string

	// ClientInfo is the per-request client identity from the
	// `io.modelcontextprotocol/clientInfo` `_meta` key (SEP-2575 /
	// 2026-07-28). Nil when the request carried none, which is every request
	// from a handshake-era client.
	ClientInfo *SanitizedClientInfo

	// CapabilityKeys is the sorted list of top-level keys the client
	// advertised under `io.modelcontextprotocol/clientCapabilities`, capped
	// at maxCapabilityKeys. Nil when the request advertised none.
	CapabilityKeys []string
}

// DeclaredProtocolVersion resolves the protocol revision a request declared:
// the MCP-Protocol-Version header value where present, otherwise the
// 2026-07-28 per-request `_meta` key the header is required to mirror. Both
// carry the same negotiated-version semantics, so the precedence is purely
// "header first, body as its mirror" — headerValue may be raw; it is
// sanitized here. Callers that can use this precedence must; the one
// sanctioned re-implementation is the meta surface's validator, which needs
// the raw bytes to tell an absent declaration from a malformed one.
func (m SanitizedMeta) DeclaredProtocolVersion(headerValue string) string {
	if v := mcpversions.Sanitize(headerValue); v != "" {
		return v
	}
	return m.ProtocolVersion
}

// DeclaredProtocolVersion is the params-level form of
// [SanitizedMeta.DeclaredProtocolVersion] for callers that need only the
// version: the params are decoded lazily, so a request whose header already
// supplies the version pays no JSON scan.
func DeclaredProtocolVersion(headerValue string, params json.RawMessage) string {
	if v := mcpversions.Sanitize(headerValue); v != "" {
		return v
	}
	return ParseMeta(params).ProtocolVersion
}

// Sanitize bounds every wire field into a [SanitizedMeta]: the version is
// passed through mcpversions.Sanitize, the client info fields through
// [SanitizeClientInfoField], and the capability keys are sanitized, sorted,
// and capped. A nil receiver yields the zero SanitizedMeta, so callers may
// chain off an absent `_meta` field directly.
func (w *WireMeta) Sanitize() SanitizedMeta {
	if w == nil {
		return SanitizedMeta{ProtocolVersion: "", ClientInfo: nil, CapabilityKeys: nil}
	}

	var info *SanitizedClientInfo
	if w.ClientInfo != nil {
		info = &SanitizedClientInfo{
			Name:    SanitizeClientInfoField(w.ClientInfo.Name),
			Version: SanitizeClientInfoField(w.ClientInfo.Version),
		}
	}

	// Sanitize before sorting: sanitization can reorder keys (control
	// characters sort before printable ones and are then dropped) and can
	// collapse distinct raw keys into duplicates, so sorting the raw keys
	// first would break the sorted-and-unique contract on adversarial input.
	var keys []string
	for k := range w.Capabilities {
		if k = SanitizeClientInfoField(k); k != "" {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	keys = slices.Compact(keys)
	if len(keys) > maxCapabilityKeys {
		keys = keys[:maxCapabilityKeys]
	}

	return SanitizedMeta{
		ProtocolVersion: mcpversions.Sanitize(w.ProtocolVersion),
		ClientInfo:      info,
		CapabilityKeys:  keys,
	}
}

// ParseMeta decodes the per-request metadata from a request's raw params.
// Malformed params must never fail an RPC, so any decode error yields the
// zero SanitizedMeta rather than an error. The legacy `initialize` top-level
// fields (protocolVersion, clientInfo, capabilities) are deliberately not
// read here — those are requested/handshake values with different semantics,
// owned by the initialize handlers.
//
// This scans the entire params document to reach `_meta`; a handler that
// already unmarshals its params should embed a [WireMeta] field instead and
// call [WireMeta.Sanitize], paying a single scan.
func ParseMeta(params json.RawMessage) SanitizedMeta {
	if len(params) == 0 {
		return (*WireMeta)(nil).Sanitize()
	}

	var raw struct {
		Meta *WireMeta `json:"_meta"`
	}
	if err := json.Unmarshal(params, &raw); err != nil {
		return (*WireMeta)(nil).Sanitize()
	}

	return raw.Meta.Sanitize()
}
