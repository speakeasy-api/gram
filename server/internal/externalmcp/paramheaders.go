package externalmcp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// Bound work on untrusted schemas, including unannotated properties. The byte
// bound applies before parsing; depth and node bounds cap repeated parsing and
// path copying during traversal. Schemas exceeding a budget fail closed.
const (
	// 1 MiB of schema JSON caps allocation before decoding, including metadata.
	maxParameterHeaderSchemaBytes = 1 << 20
	// 32 property edges cap recursive decoding and path copying.
	maxParameterHeaderDepth = 32
	// 4,096 visited schemas (including the root) cap wide property traversal.
	maxParameterHeaderNodes = 4096
	// The placement walk spends two JSON levels per property edge, plus the root.
	maxParameterHeaderJSONDepth = 2*maxParameterHeaderDepth + 1
)

// ParameterHeaders implements the x-mcp-header rules in go-sdk v1.7.0's
// mcp/streamable_headers.go (SEP-2243). It validates annotations before reading
// arguments, and never rewrites the argument bytes or includes their values in
// errors. Like the SDK, it follows properties, not $ref or schema combinators.
// An annotation anywhere else (the root, items, a combinator, $defs) makes the
// tool definition invalid, as MCP 2026-07-28 requires.
func ParameterHeaders(schema json.RawMessage, arguments json.RawMessage) (http.Header, error) {
	if len(schema) > maxParameterHeaderSchemaBytes {
		return nil, errors.New("MCP tool input schema exceeds byte limit")
	}
	var bindings []parameterHeaderBinding
	remainingNodes := maxParameterHeaderNodes
	if err := collectParameterHeaders(schema, nil, make(map[string]bool), &bindings, &remainingNodes); err != nil {
		return nil, err
	}
	if MayDeclareParameterHeaders(schema) {
		count, err := countSchemaAnnotations(schema)
		if err != nil {
			return nil, err
		}
		if count != len(bindings) {
			return nil, errors.New("x-mcp-header outside a chain of properties keys")
		}
	}
	if len(bindings) == 0 || len(bytes.TrimSpace(arguments)) == 0 {
		return nil, nil
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &args); err != nil {
		return nil, parameterHeaderJSONError("invalid MCP tool arguments", err)
	}
	headers := make(http.Header)
	for _, binding := range bindings {
		obj := args
		var raw json.RawMessage
		for i, part := range binding.path {
			raw = obj[part]
			if i < len(binding.path)-1 {
				obj = nil
				if err := json.Unmarshal(raw, &obj); err != nil {
					raw = nil
					break
				}
			}
		}
		if value, ok := parameterHeaderValue(raw); ok {
			headers.Set("Mcp-Param-"+binding.name, value)
		}
	}
	return headers, nil
}

type parameterHeaderBinding struct {
	path []string
	name string
}

func collectParameterHeaders(raw json.RawMessage, path []string, seen map[string]bool, bindings *[]parameterHeaderBinding, remainingNodes *int) error {
	if len(path) > maxParameterHeaderDepth {
		return errors.New("MCP tool input schema exceeds depth limit")
	}
	if *remainingNodes <= 0 {
		return errors.New("MCP tool input schema exceeds node limit")
	}
	*remainingNodes -= 1
	raw = bytes.TrimSpace(raw)
	// Boolean schemas are valid JSON schemas with no annotations.
	if bytes.Equal(raw, []byte("true")) || bytes.Equal(raw, []byte("false")) {
		return nil
	}
	var schema struct {
		Type       json.RawMessage `json:"type"`
		Header     json.RawMessage `json:"x-mcp-header"`
		Properties json.RawMessage `json:"properties"`
	}
	if len(raw) == 0 || raw[0] != '{' {
		return errors.New("invalid MCP tool input schema (category=shape)")
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return parameterHeaderJSONError("invalid MCP tool input schema", err)
	}
	if len(path) > 0 && schema.Header != nil {
		// This adapter is not a general schema validator. Only annotated
		// properties require a scalar type; unrelated types stay untouched.
		var typ string
		if err := json.Unmarshal(schema.Type, &typ); err != nil {
			return parameterHeaderJSONError("x-mcp-header requires an integer, string, or boolean type", err)
		}
		if typ != "integer" && typ != "string" && typ != "boolean" {
			return errors.New("x-mcp-header requires an integer, string, or boolean type")
		}
		var name string
		if err := json.Unmarshal(schema.Header, &name); err != nil {
			return parameterHeaderJSONError("x-mcp-header must be a non-empty string", err)
		}
		if name == "" {
			return errors.New("x-mcp-header must be a non-empty string")
		}
		for _, c := range name {
			valid := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)
			if !valid {
				return errors.New("x-mcp-header must be an HTTP token")
			}
		}
		lower := strings.ToLower(name)
		if seen[lower] {
			return errors.New("duplicate x-mcp-header name (case-insensitive)")
		}
		seen[lower] = true
		*bindings = append(*bindings, parameterHeaderBinding{path: path, name: name})
	}
	if schema.Properties != nil {
		var properties map[string]json.RawMessage
		if err := json.Unmarshal(schema.Properties, &properties); err != nil {
			return parameterHeaderJSONError("invalid MCP tool input schema properties", err)
		}
		if properties == nil {
			return errors.New("invalid MCP tool input schema properties (category=shape)")
		}
		for name, property := range properties {
			childPath := append(append([]string(nil), path...), name)
			if err := collectParameterHeaders(property, childPath, seen, bindings, remainingNodes); err != nil {
				return err
			}
		}
	}
	return nil
}

// parameterHeaderJSONError reports decoder metadata, never the error message:
// encoding/json errors can contain raw values or schema-controlled field names.
// Offsets are byte positions within the JSON fragment passed to the decoder.
// Callers must supply a static context, not schema or argument content.
func parameterHeaderJSONError(context string, err error) error {
	if syntax, ok := errors.AsType[*json.SyntaxError](err); ok {
		return fmt.Errorf("%s (category=syntax offset=%d type=%T)", context, syntax.Offset, syntax)
	}
	if mismatch, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
		return fmt.Errorf("%s (category=type offset=%d type=%T)", context, mismatch.Offset, mismatch)
	}
	return fmt.Errorf("%s (category=decode type=%T)", context, err)
}

func parameterHeaderValue(raw json.RawMessage) (string, bool) {
	var primitive any
	if json.Unmarshal(raw, &primitive) != nil {
		return "", false
	}
	var value string
	switch v := primitive.(type) {
	case string:
		value = v
	case bool:
		value = strconv.FormatBool(v)
	case float64:
		// Deliberately match the SDK's IEEE-754 value-level integer semantics,
		// including integer-valued decimals/exponents, without remarshal.
		const maxSafeInteger = 1<<53 - 1 // 9,007,199,254,740,991: largest exactly representable interoperable integer.
		if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) || v < -maxSafeInteger || v > maxSafeInteger {
			return "", false
		}
		value = strconv.FormatInt(int64(v), 10)
	default:
		return "", false
	}
	// The SDK accepts an encoded empty string but rejects a raw empty header
	// as missing. Encode it explicitly rather than reproduce its generator bug.
	encode := value == "" || strings.HasPrefix(value, "=?base64?") && strings.HasSuffix(value, "?=")
	if len(value) > 0 && (value[0] == ' ' || value[len(value)-1] == ' ') {
		encode = true
	}
	for _, c := range value {
		if c < 0x20 || c > 0x7e {
			encode = true
			break
		}
	}
	if encode {
		value = "=?base64?" + base64.StdEncoding.EncodeToString([]byte(value)) + "?="
	}
	return value, true
}

// MayDeclareParameterHeaders reports whether schema could carry an
// x-mcp-header annotation, spelled plainly or with JSON escapes. A schema
// without either never needs headers or placement checks.
func MayDeclareParameterHeaders(schema json.RawMessage) bool {
	return bytes.Contains(schema, []byte(`"x-mcp-header"`)) || bytes.Contains(schema, []byte(`\u`))
}

// parameterHeaderDataKeywords hold instance values, not schemas, so an
// x-mcp-header key inside them is data rather than an annotation.
var parameterHeaderDataKeywords = map[string]bool{"const": true, "default": true, "enum": true, "examples": true}

// countSchemaAnnotations counts x-mcp-header annotations anywhere in schema,
// on decoded member names, within the depth and node budgets.
func countSchemaAnnotations(schema json.RawMessage) (int, error) {
	decoder := json.NewDecoder(bytes.NewReader(schema))
	// Numbers in unrelated metadata need not fit a float64.
	decoder.UseNumber()
	walk := &annotationWalk{decoder: decoder, remainingNodes: maxParameterHeaderNodes, count: 0}
	if err := walk.value(1, false, false); err != nil {
		return 0, err
	}
	return walk.count, nil
}

// annotationWalk streams a schema's tokens, counting x-mcp-header members.
type annotationWalk struct {
	decoder *json.Decoder

	// remainingNodes is the object and array budget left.
	remainingNodes int

	// count is the annotations found so far.
	count int
}

// value consumes one JSON value at depth. inProperties marks a properties
// map, whose member names are property names; data marks instance values,
// which never hold annotations.
func (w *annotationWalk) value(depth int, inProperties, data bool) error {
	token, err := w.decoder.Token()
	if err != nil {
		return parameterHeaderJSONError("invalid MCP tool input schema", err)
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	// Instance values hold no annotations; the byte limit already bounds them.
	if !data {
		if depth > maxParameterHeaderJSONDepth {
			return errors.New("MCP tool input schema exceeds depth limit")
		}
		if w.remainingNodes <= 0 {
			return errors.New("MCP tool input schema exceeds node limit")
		}
		w.remainingNodes--
	}
	for w.decoder.More() {
		if delim == '[' {
			if err := w.value(depth+1, false, data); err != nil {
				return err
			}
			continue
		}
		keyToken, err := w.decoder.Token()
		if err != nil {
			return parameterHeaderJSONError("invalid MCP tool input schema", err)
		}
		key, _ := keyToken.(string)
		schemaMember := !inProperties && !data
		if schemaMember && key == "x-mcp-header" {
			w.count++
		}
		if err := w.value(depth+1, schemaMember && key == "properties", data || (schemaMember && parameterHeaderDataKeywords[key])); err != nil {
			return err
		}
	}
	if _, err := w.decoder.Token(); err != nil {
		return parameterHeaderJSONError("invalid MCP tool input schema", err)
	}
	return nil
}
