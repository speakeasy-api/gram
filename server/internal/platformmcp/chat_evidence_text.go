package platformmcp

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"
)

// canonicalEvidenceText preserves compact JSON and decoded string leaves so
// quoted commands, newlines and nested JSON arguments have literal matches.
// Apply the same projection before and after redaction; never return raw text.
func canonicalEvidenceText(content string, calls, raw []byte) string {
	fields := []string{content, string(calls), string(raw)}
	for i, field := range fields {
		fields[i] = canonicalEvidenceField(field)
	}
	return strings.Join(fields, "\n")
}

func canonicalEvidenceField(field string) string {
	if len(field) > 131072 || !utf8.ValidString(field) {
		return "[WITHHELD]"
	}
	value, ok := decodeEvidenceJSON(field)
	if !ok {
		return field
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "[WITHHELD]"
	}
	var out strings.Builder
	out.Write(data)
	var appendLeaves func(any, int) bool
	appendLeaves = func(item any, depth int) bool {
		if depth > 8 || out.Len() > 524288 {
			return false
		}
		switch typed := item.(type) {
		case map[string]any:
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if !appendLeaves(typed[key], depth+1) {
					return false
				}
			}
		case []any:
			for _, leaf := range typed {
				if !appendLeaves(leaf, depth+1) {
					return false
				}
			}
		case string:
			out.WriteByte('\n')
			out.WriteString(typed)
			nested, ok := decodeEvidenceJSON(typed)
			if ok {
				return appendLeaves(nested, depth+1)
			}
		}
		return out.Len() <= 524288
	}
	if !appendLeaves(value, 0) {
		return "[WITHHELD]"
	}
	return out.String()
}

// UseNumber preserves identifier-sized integers and keeps valid large numbers
// from causing structured credentials to fall back to plain-text scanning.
func decodeEvidenceJSON(text string) (any, bool) {
	if !json.Valid([]byte(text)) {
		return nil, false
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return nil, false
	}
	return value, true
}
