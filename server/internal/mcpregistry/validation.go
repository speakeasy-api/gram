package mcpregistry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry/contract"
)

const RecordInputByteLimit = 8 << 20

// StoredRecordByteLimit bounds PostgreSQL jsonb::text, not compressed storage.
const StoredRecordByteLimit = 8 << 20

type Issue struct {
	Path    string
	Message string
}

type InvalidError struct{ Issues []Issue }

func (e *InvalidError) Error() string { return "invalid registry record" }

type Validator struct {
	schema *jsonschema.Schema
}

func LoadValidator() (*Validator, error) {
	s, err := contract.Compile(contract.Schema)
	if err != nil {
		return nil, fmt.Errorf("load registry validator: %w", err)
	}
	return &Validator{schema: s}, nil
}

func (v *Validator) Validate(raw json.RawMessage) []Issue {
	return v.validate(raw, RecordInputByteLimit)
}

func (v *Validator) ValidateStored(raw json.RawMessage) []Issue {
	return v.validate(raw, StoredRecordByteLimit)
}

func (v *Validator) validate(raw json.RawMessage, byteLimit int) []Issue {
	if len(raw) > byteLimit {
		return []Issue{{Path: "", Message: "record exceeds byte limit"}}
	}
	if !validJSONUnicode(raw) {
		return []Issue{{Path: "", Message: "record must contain valid Unicode text"}}
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return []Issue{{Path: "", Message: "expected exactly one JSON value"}}
	}
	if err := v.schema.Validate(value); err != nil {
		return validationIssues(err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return []Issue{{Path: "", Message: "expected object"}}
	}
	meta, _ := object["_meta"].(map[string]any)
	catalog, _ := meta["com.speakeasy.ai/catalog"].(map[string]any)
	if documentationURL, ok := catalog["documentationUrl"].(string); ok {
		parsed, err := url.Parse(documentationURL)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil {
			return []Issue{{Path: "/_meta/com.speakeasy.ai~1catalog/documentationUrl", Message: "absolute HTTP(S) URL with a host and without userinfo required"}}
		}
	}
	if issues := validateOktaMapping(meta); len(issues) > 0 {
		return issues
	}
	server, _ := object["server"].(map[string]any)
	name, _ := server["name"].(string)
	if name == "" {
		return []Issue{{Path: "/server/name", Message: "nonempty name required"}}
	}
	return nil
}

// Never use ValidationError.Error: its text can include rejected values.
func validationIssues(err error) []Issue {
	var root *jsonschema.ValidationError
	if !errors.As(err, &root) {
		return []Issue{{Path: "", Message: "record does not conform to registry schema"}}
	}
	issues := make([]Issue, 0, 20)
	var visit func(*jsonschema.ValidationError)
	visit = func(e *jsonschema.ValidationError) {
		if len(issues) == 20 {
			return
		}
		if len(e.Causes) > 0 {
			for _, cause := range e.Causes {
				visit(cause)
				if len(issues) == 20 {
					return
				}
			}
			return
		}
		path := ""
		truncated := false
		for _, segment := range e.InstanceLocation {
			escaped := strings.ReplaceAll(strings.ReplaceAll(segment, "~", "~0"), "/", "~1")
			if len(path)+len(escaped)+1 > 256 {
				truncated = true
				break
			}
			path += "/" + escaped
		}
		message := "does not satisfy schema constraint"
		switch e.ErrorKind.(type) {
		case *kind.Type:
			message = "unexpected JSON type"
		case *kind.Required:
			message = "required property missing"
		case *kind.Format:
			message = "invalid format"
		case *kind.Pattern:
			message = "does not match required pattern"
		}
		if truncated {
			message += " (path truncated)"
		}
		issues = append(issues, Issue{Path: path, Message: message})
	}
	visit(root)
	return issues
}

// encoding/json replaces malformed UTF-8 and unpaired escaped surrogates with
// U+FFFD. Reject them before decoding keys, matching PostgreSQL jsonb admission.
// JSON syntax validation remains the decoder's responsibility.
func validJSONUnicode(raw []byte) bool {
	if !utf8.Valid(raw) {
		return false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i+4 >= len(raw) || raw[i] != 'u' {
			continue
		}
		code, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			continue
		}
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code < 0xd800 || code > 0xdbff {
			continue
		}
		if i+6 >= len(raw) || string(raw[i+1:i+3]) != `\u` {
			return false
		}
		low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}

// schemaPropertyNames lists every property name the contract declares.
var schemaPropertyNames = sync.OnceValue(func() map[string]bool {
	names := map[string]bool{}
	var walk func(any)
	walk = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			if props, ok := n["properties"].(map[string]any); ok {
				for name := range props {
					names[name] = true
				}
			}
			for _, child := range n {
				walk(child)
			}
		case []any:
			for _, child := range n {
				walk(child)
			}
		}
	}
	for _, raw := range [][]byte{contract.Schema, contract.SpeakeasyRegistrySchema} {
		var doc any
		if err := json.Unmarshal(raw, &doc); err == nil {
			walk(doc)
		}
	}
	return names
})

// LogPath masks every segment that is neither a declared property name nor an
// array index, because validator paths echo submitted keys of open maps.
func LogPath(path string) string {
	if path == "" {
		return ""
	}
	segments := strings.Split(path[1:], "/")
	for i, segment := range segments {
		name := strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		if _, err := strconv.ParseUint(name, 10, 32); err != nil && !schemaPropertyNames()[name] {
			segments[i] = "*"
		}
	}
	return "/" + strings.Join(segments, "/")
}
