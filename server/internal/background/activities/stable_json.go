package activities

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func stableJSONHash(raw json.RawMessage) (string, error) {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
