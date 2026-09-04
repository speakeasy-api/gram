package chat

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// jsonNULEscape is the only JSON spelling of U+0000 inside a string value.
// Postgres jsonb refuses it (SQLSTATE 22P05) just as text refuses the raw
// byte, so it must be neutralized before content_raw reaches the database.
var jsonNULEscape = []byte(`\u` + "0000")

// ReplaceNUL substitutes every NUL byte with U+FFFD. Postgres text columns
// reject NUL outright (SQLSTATE 22021: invalid byte sequence for encoding
// "UTF8": 0x00), and imported provider transcripts occasionally carry one
// from pasted binary or terminal output. Replacing instead of dropping keeps
// a visible marker where the byte was. Strings without NUL are returned
// unchanged without allocating.
func ReplaceNUL(s string) string {
	if strings.IndexByte(s, 0) < 0 {
		return s
	}
	return strings.ReplaceAll(s, "\x00", string(utf8.RuneError))
}

// replaceNULInJSON applies ReplaceNUL to every string value and object key in
// a JSON document so the result is safe for a jsonb column. Documents that
// carry no NUL — the overwhelming majority — are returned byte-for-byte.
// A document that carries one is decoded and re-encoded; numbers round-trip
// verbatim via json.Number so the rewrite never alters a value it did not
// need to touch. Input that is not valid JSON is returned as-is, except that
// raw NUL bytes are still substituted so the column rejection cannot recur.
func replaceNULInJSON(raw []byte) []byte {
	hasRawNUL := bytes.IndexByte(raw, 0) >= 0
	if !hasRawNUL && !bytes.Contains(raw, jsonNULEscape) {
		return raw
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		if hasRawNUL {
			return bytes.ReplaceAll(raw, []byte{0}, []byte(string(utf8.RuneError)))
		}
		return raw
	}

	v, changed := replaceNULInJSONValue(v)
	if !changed {
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

// replaceNULInJSONValue walks a decoded JSON value and reports whether any
// string or object key needed a NUL replaced.
func replaceNULInJSONValue(v any) (any, bool) {
	switch x := v.(type) {
	case string:
		if strings.IndexByte(x, 0) < 0 {
			return x, false
		}
		return ReplaceNUL(x), true
	case []any:
		changed := false
		for i, elem := range x {
			replaced, c := replaceNULInJSONValue(elem)
			if c {
				x[i] = replaced
				changed = true
			}
		}
		return x, changed
	case map[string]any:
		changed := false
		for key, elem := range x {
			replaced, c := replaceNULInJSONValue(elem)
			if c {
				x[key] = replaced
				changed = true
			}
		}
		// Keys are rewritten after the value pass so the range above never
		// observes entries it inserted.
		for key := range x {
			if strings.IndexByte(key, 0) < 0 {
				continue
			}
			x[ReplaceNUL(key)] = x[key]
			delete(x, key)
			changed = true
		}
		return x, changed
	default:
		return v, false
	}
}
