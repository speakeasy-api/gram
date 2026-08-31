package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ValidateStrictJSONRPCBody enforces wire-level unambiguity for request
// bodies whose authorization depends on the decoded message matching the
// bytes forwarded upstream: exactly one top-level JSON value with no
// trailing data, and no duplicate object member names at any depth.
//
// Parsers disagree on duplicate members — Go's encoding/json is last-wins
// while first-wins parsers are common — so a body like
// `{"method":"ping","method":"tools/call"}` can pass a method check against
// the decoded message here yet execute tools/call on the upstream. A body
// accepted by this function decodes to the same message under either
// convention. An empty body is rejected: zero messages would pass any
// per-message check vacuously while still being forwarded authenticated.
func ValidateStrictJSONRPCBody(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if errors.Is(err, io.EOF) {
		return errors.New("empty JSON-RPC message body")
	}
	if err != nil {
		return fmt.Errorf("read JSON token: %w", err)
	}
	if err := validateStrictFrom(dec, tok); err != nil {
		return err
	}
	// dec.More() reports false when the next byte is ] or } even outside a
	// container, so it would accept `{...}]`. Only a clean EOF proves there
	// is no trailing data.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data after JSON-RPC message")
	}
	return nil
}

// hasTopLevelJSONRPCMethod reports whether any top-level method member names
// target. It intentionally preserves duplicate members so strict private
// requests cannot hide a tools/call from method preflight by placing another
// method later in the object.
func hasTopLevelJSONRPCMethod(raw []byte, target string) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return false
	}

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return false
		}
		key, ok := keyTok.(string)
		if !ok {
			return false
		}

		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return false
		}
		if key != "method" {
			continue
		}

		var method string
		if json.Unmarshal(value, &method) == nil && method == target {
			return true
		}
	}

	return false
}

// validateStrictValue consumes one complete JSON value from dec, failing on
// duplicate object member names at any depth. EOF here is mid-structure —
// truncated input — and reads as an error, unlike the empty-body case the
// entry point handles.
func validateStrictValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("read JSON token: %w", err)
	}
	return validateStrictFrom(dec, tok)
}

// validateStrictFrom walks the JSON value opened by tok.
func validateStrictFrom(dec *json.Decoder, tok json.Token) error {
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}

	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			keyTok, kerr := dec.Token()
			if kerr != nil {
				return fmt.Errorf("read object member name: %w", kerr)
			}
			key, isString := keyTok.(string)
			if !isString {
				return fmt.Errorf("object member name is %T, not string", keyTok)
			}
			if seen[key] {
				return fmt.Errorf("duplicate object member %q", key)
			}
			seen[key] = true
			if verr := validateStrictValue(dec); verr != nil {
				return verr
			}
		}
		if _, cerr := dec.Token(); cerr != nil {
			return fmt.Errorf("read object close: %w", cerr)
		}
		return nil
	case '[':
		for dec.More() {
			if verr := validateStrictValue(dec); verr != nil {
				return verr
			}
		}
		if _, cerr := dec.Token(); cerr != nil {
			return fmt.Errorf("read array close: %w", cerr)
		}
		return nil
	default:
		return nil
	}
}
