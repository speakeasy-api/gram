package activities

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

var emptyJSONObject = json.RawMessage(`{}`)

func stableJSONHash(raw json.RawMessage) ([]byte, string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, "", err
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(normalized)
	return normalized, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func stableJSONObjectHash(raw json.RawMessage) ([]byte, string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return stableJSONHash(emptyJSONObject)
	}
	return stableJSONHash(trimmed)
}
