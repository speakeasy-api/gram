package httpheaders

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

// MCP 2026-07-28 standard request headers that mirror JSON-RPC body fields.
// These are the wire-format primitives shared by every component that reads
// them; what a component does with a missing or mismatched value (a
// terminating server rejects, an intermediary forwards) is its own policy.
const (
	// MethodHeader mirrors the JSON-RPC `method` of every request.
	MethodHeader = "Mcp-Method"

	// NameHeader mirrors `params.name` or `params.uri` of the requests
	// [MirroredNameField] names.
	NameHeader = "Mcp-Name"

	// ParamHeaderPrefix starts each Mcp-Param-{Name} header, which mirrors a
	// tool argument annotated with `x-mcp-header`.
	ParamHeaderPrefix = "Mcp-Param-"

	// base64SentinelPrefix opens a Base64-encoded value. The specification
	// requires it exactly as written (lowercase); anything else is a literal
	// value.
	base64SentinelPrefix = "=?base64?"

	// base64SentinelSuffix closes a Base64-encoded value opened by
	// base64SentinelPrefix.
	base64SentinelSuffix = "?="
)

var (
	// ErrRepeatedHeader reports a mirrored header that appears more than
	// once, so no single value can be compared against the body.
	ErrRepeatedHeader = errors.New("mirrored header is repeated")

	// ErrInvalidBase64 reports a value wrapped in the Base64 sentinel whose
	// payload is not valid standard Base64.
	ErrInvalidBase64 = errors.New("mirrored header has invalid base64 encoding")
)

// MirroredValue returns the decoded value of the mirrored header name, and
// whether the header is present at all. A value wrapped in the
// `=?base64?…?=` sentinel is decoded before it is returned, except for
// Mcp-Method, which does not permit Base64 encoding. Callers can compare
// the result with the request body directly. The error is
// [ErrRepeatedHeader] or [ErrInvalidBase64].
func MirroredValue(header http.Header, name string) (string, bool, error) {
	values := header.Values(name)
	switch len(values) {
	case 0:
		return "", false, nil
	case 1:
		if strings.EqualFold(name, MethodHeader) {
			return values[0], true, nil
		}
		decoded, err := DecodeMirroredValue(values[0])
		if err != nil {
			return "", true, err
		}
		return decoded, true, nil
	default:
		return "", true, ErrRepeatedHeader
	}
}

// DecodeMirroredValue decodes a mirrored header value wrapped in the
// `=?base64?…?=` sentinel and returns any other value unchanged.
func DecodeMirroredValue(value string) (string, error) {
	encoded, ok := strings.CutPrefix(value, base64SentinelPrefix)
	if !ok {
		return value, nil
	}
	encoded, ok = strings.CutSuffix(encoded, base64SentinelSuffix)
	if !ok {
		return value, nil
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidBase64, err)
	}
	return string(decoded), nil
}

// MirroredNameField returns the `params` member that the Mcp-Name header
// mirrors for method, and false for methods that carry no Mcp-Name header.
func MirroredNameField(method string) (string, bool) {
	switch method {
	case mcpversions.MethodToolsCall, mcpversions.MethodPromptsGet:
		return "name", true
	case mcpversions.MethodResourcesRead:
		return "uri", true
	default:
		return "", false
	}
}
