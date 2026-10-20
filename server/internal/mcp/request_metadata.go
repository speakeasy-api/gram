package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Members of a request's params-level `_meta` object that MCP 2026-07-28
// requires on every request.
const (
	// metaProtocolVersionKey carries the request's protocol revision, which
	// the MCP-Protocol-Version header mirrors.
	metaProtocolVersionKey = "io.modelcontextprotocol/protocolVersion"

	// metaClientCapabilitiesKey carries the client's ClientCapabilities
	// object.
	metaClientCapabilitiesKey = "io.modelcontextprotocol/clientCapabilities"
)

// declarationError is a protocol-version declaration failure together with
// the revision whose rules govern the response. A request whose declarations
// contradict each other names no revision of its own, so the response follows
// [mcpversions.Latest]. A request with one malformed declaration follows the
// other declaration when it names a recognized revision
// ([declarationRevision]).
type declarationError struct {
	// revision selects the wire code and HTTP status of the response.
	revision string

	// err is the JSON-RPC error answered to the client.
	err *oops.MCPError
}

func (e *declarationError) Error() string { return e.err.Error() }

func (e *declarationError) Unwrap() error { return e.err }

// declarationRevision returns the revision that governs the response to a
// malformed declaration: the request's other declaration when it names a
// recognized revision, otherwise [mcpversions.Latest].
func declarationRevision(other string) string {
	if mcpversions.Known(other) {
		return other
	}
	return mcpversions.Latest()
}

// rawRequestMeta decodes the members of a request's params-level `_meta`
// without interpreting them, so callers can tell an absent member from a
// present but malformed one (the tolerant [mcprequests.WireMeta] decode
// cannot). ok is false when params is present but is not a JSON object.
func rawRequestMeta(params json.RawMessage) (meta map[string]json.RawMessage, ok bool) {
	if len(bytes.TrimSpace(params)) == 0 || bytes.Equal(bytes.TrimSpace(params), []byte("null")) {
		return nil, true
	}

	var decoded struct {
		Meta json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(params, &decoded); err != nil {
		return nil, false
	}
	// A `_meta` that is not an object carries no members; required-member
	// checks report it as missing them.
	_ = json.Unmarshal(decoded.Meta, &meta)
	return meta, true
}

// validateRequestMetadata enforces MCP 2026-07-28's server validation of
// per-request metadata on a request governed by that revision or later.
// Earlier revisions define none of these headers or `_meta` members, so a
// request governed by one of them is never checked here. The header and
// `_meta` protocol-version agreement is enforced before this, with the
// supported-version check, because the specification orders it ahead of an
// unsupported-version rejection.
//
// Failures are HeaderMismatch (-32020) for a standard header that is missing,
// repeated, malformed, or disagrees with the body, and InvalidParams (-32602)
// for a missing or malformed required `_meta` member. Both carry HTTP 400.
// Messages name only the header or member, never client-supplied values.
//
// Notifications are checked for the MCP-Protocol-Version and Mcp-Method
// headers only, matching the go-sdk v1.7.0 server: the schema makes `_meta`
// optional on notifications, and no notification carries Mcp-Name. A
// rejected notification is still acknowledged without a body. Responses
// (no method) are not validated.
//
// Header values outside visible ASCII are compared with the body rather than
// rejected as invalid characters. The go-sdk v1.7.0 client sends Mcp-Name
// unencoded even for non-ASCII resource URIs and prompt names, and net/http
// already rejects control characters in header values; a value that does not
// match the body is still rejected.
func validateRequestMetadata(header http.Header, req *rawRequest, resolution mcpversions.Resolution) error {
	if !mcpversions.AtLeast(resolution.InEffect, mcpversions.Version20260728) || req.Method == "" {
		return nil
	}

	versionValues := header.Values(mcpversions.HTTPHeader)
	switch {
	case len(versionValues) == 0:
		return headerMismatchError(req.ID, fmt.Sprintf("missing required %s header", mcpversions.HTTPHeader))
	case len(versionValues) > 1:
		return headerMismatchError(req.ID, fmt.Sprintf("%s header is repeated", mcpversions.HTTPHeader))
	case mcpversions.Sanitize(versionValues[0]) == "":
		return headerMismatchError(req.ID, fmt.Sprintf("%s header is malformed", mcpversions.HTTPHeader))
	}

	method, err := requiredMirroredHeader(header, httpheaders.MethodHeader, req.ID)
	if err != nil {
		return err
	}
	if method != req.Method {
		return headerMismatchError(req.ID, fmt.Sprintf("%s header does not match the request method", httpheaders.MethodHeader))
	}
	if !req.ID.IsSet() {
		return nil
	}

	meta, paramsIsObject := rawRequestMeta(req.Params)
	if !paramsIsObject {
		return invalidMetaError(req.ID, "request params must be a JSON object")
	}

	if field, mirrored := httpheaders.MirroredNameField(req.Method); mirrored {
		name, err := requiredMirroredHeader(header, httpheaders.NameHeader, req.ID)
		if err != nil {
			return err
		}
		var params map[string]json.RawMessage
		var want *string
		if json.Unmarshal(req.Params, &params) != nil || json.Unmarshal(params[field], &want) != nil || want == nil {
			return headerMismatchError(req.ID, fmt.Sprintf("%s header cannot be verified: request params carry no string %q", httpheaders.NameHeader, field))
		}
		if name != *want {
			return headerMismatchError(req.ID, fmt.Sprintf("%s header does not match params.%s", httpheaders.NameHeader, field))
		}
	}

	version, err := validateRequiredMeta(req.ID, meta)
	if err != nil {
		return err
	}

	// Version resolution compares sanitized declarations. Compare the
	// original wire values too: trimming must not hide a header/body mismatch.
	if versionValues[0] != version {
		return headerMismatchError(req.ID, fmt.Sprintf("%s header does not match the request _meta declaration", mcpversions.HTTPHeader))
	}

	return nil
}

// requiredMirroredHeader returns the decoded value of a standard header the
// request must carry, or the HeaderMismatch error for a missing, repeated, or
// malformed one.
func requiredMirroredHeader(header http.Header, name string, id mcpjsonrpc.ID) (string, error) {
	value, present, err := httpheaders.MirroredValue(header, name)
	switch {
	case errors.Is(err, httpheaders.ErrRepeatedHeader):
		return "", headerMismatchError(id, fmt.Sprintf("%s header is repeated", name))
	case err != nil:
		return "", headerMismatchError(id, fmt.Sprintf("%s header is malformed", name))
	case !present:
		return "", headerMismatchError(id, fmt.Sprintf("missing required %s header", name))
	default:
		return value, nil
	}
}

// validateRequiredMeta checks the `_meta` members MCP 2026-07-28 requires on
// every request: a non-empty protocol revision string and a client
// capabilities object. The optional client identity is not checked. It
// returns the protocol revision exactly as the body carries it.
func validateRequiredMeta(id mcpjsonrpc.ID, meta map[string]json.RawMessage) (string, error) {
	rawVersion, ok := meta[metaProtocolVersionKey]
	if !ok {
		return "", invalidMetaError(id, fmt.Sprintf("missing required _meta member %q", metaProtocolVersionKey))
	}
	var version string
	if json.Unmarshal(rawVersion, &version) != nil || mcpversions.Sanitize(version) == "" {
		return "", invalidMetaError(id, fmt.Sprintf("_meta member %q must be a protocol version string", metaProtocolVersionKey))
	}

	rawCapabilities, ok := meta[metaClientCapabilitiesKey]
	if !ok {
		return "", invalidMetaError(id, fmt.Sprintf("missing required _meta member %q", metaClientCapabilitiesKey))
	}
	var capabilities map[string]json.RawMessage
	if json.Unmarshal(rawCapabilities, &capabilities) != nil || capabilities == nil {
		return "", invalidMetaError(id, fmt.Sprintf("_meta member %q must be an object", metaClientCapabilitiesKey))
	}

	return version, nil
}

// headerMismatchError is the MCP 2026-07-28 HeaderMismatch (-32020) error.
// message must not contain client-supplied values.
func headerMismatchError(id mcpjsonrpc.ID, message string) *oops.MCPError {
	return &oops.MCPError{
		ID:      id,
		Code:    oops.MCPCodeHeaderMismatch,
		Message: message,
		Data:    nil,
	}
}

// invalidMetaError is the InvalidParams (-32602) error MCP 2026-07-28 assigns
// to missing or malformed required `_meta`. message must not contain
// client-supplied values.
func invalidMetaError(id mcpjsonrpc.ID, message string) *oops.MCPError {
	return &oops.MCPError{
		ID:      id,
		Code:    oops.MCPCodeInvalidParams,
		Message: message,
		Data:    nil,
	}
}
