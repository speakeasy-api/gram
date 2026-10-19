package platformmcp

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"
)

const chatEvidenceMaxDepth = 16

// canonicalEvidenceProjection preserves compact JSON and decoded leaves for
// literal matching, reporting structural omissions independently of the query.
func canonicalEvidenceProjection(content string, calls, raw []byte) (string, bool) {
	fields := []string{content, string(calls), string(raw)}
	withheld := false
	for i, field := range fields {
		text, hidden := canonicalEvidenceFieldProjection(field)
		fields[i] = text
		withheld = withheld || hidden
	}
	return strings.Join(fields, "\n"), withheld
}

func canonicalEvidenceField(field string) string {
	text, _ := canonicalEvidenceFieldProjection(field)
	return text
}

func canonicalEvidenceFieldProjection(field string) (string, bool) {
	if len(field) > 131072 || !utf8.ValidString(field) {
		return "[WITHHELD]", true
	}
	value, ok := decodeEvidenceJSON(field)
	if !ok {
		return field, false
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "[WITHHELD]", true
	}
	var out strings.Builder
	out.Write(data)
	var appendLeaves func(any, int) bool
	appendLeaves = func(item any, depth int) bool {
		if depth > chatEvidenceMaxDepth || out.Len() > 524288 {
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
	if !appendLeaves(value, 1) {
		return "[WITHHELD]", true
	}
	return out.String(), false
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
