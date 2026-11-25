package classifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// Entry contains a JSON string, object, array, or null. Nested objects and arrays
// may contain any JSON value. Its zero value represents explicit JSON null, not
// an omitted field. Entries retain their JSON representation without converting
// numbers to float64 and can be copied by value.
type Entry struct {
	raw json.RawMessage
}

var _ json.Marshaler = Entry{raw: nil}
var _ json.Unmarshaler = (*Entry)(nil)

// Text constructs a string entry, escaping its contents as JSON.
func Text(value string) Entry {
	// Marshaling a string cannot fail.
	data, _ := json.Marshal(value)
	return Entry{raw: data}
}

// ParseEntry validates encoded JSON and takes an owned copy of its bytes. Only
// strings, objects, arrays, and null are accepted at the top level. Invalid input
// returns a zero-valued entry and an error.
func ParseEntry(data []byte) (Entry, error) {
	data = bytes.Trim(data, " \t\r\n")
	if !utf8.Valid(data) || !json.Valid(data) {
		return Entry{raw: nil}, fmt.Errorf("parse classifier entry: invalid JSON")
	}
	switch data[0] {
	case '"', '{', '[':
		return Entry{raw: bytes.Clone(data)}, nil
	case 'n':
		return Entry{raw: nil}, nil // Valid JSON beginning with n is null.
	default:
		return Entry{raw: nil}, fmt.Errorf("parse classifier entry: expected string, object, array, or null")
	}
}

// NewEntry encodes value using encoding/json and validates the resulting entry.
// Go values follow encoding/json conventions, including custom marshalers and
// byte slices encoded as base64 strings. Top-level numbers and booleans are
// rejected; they are permitted inside objects and arrays.
func NewEntry(value any) (Entry, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return Entry{raw: nil}, fmt.Errorf("encode classifier entry: %w", err)
	}

	return ParseEntry(data)
}

// MarshalJSON returns an owned copy of the encoded entry. Mutating the returned
// bytes cannot change this entry or any value copied from it.
func (e Entry) MarshalJSON() ([]byte, error) {
	if len(e.raw) == 0 {
		return []byte("null"), nil
	}
	return bytes.Clone(e.raw), nil
}

// UnmarshalJSON validates and copies an encoded entry. On failure the receiver
// remains unchanged, including when it previously held a valid entry.
func (e *Entry) UnmarshalJSON(data []byte) error {
	entry, err := ParseEntry(data)
	if err != nil {
		return err
	}

	*e = entry
	return nil
}
