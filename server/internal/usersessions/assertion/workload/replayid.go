package workload

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// replayIDClaims carries Entra's per-token identifier alongside the
// registered jti decoded by jwt.Claims.
type replayIDClaims struct {
	UTI optionalString `json:"uti"`
}

// An unreadable uti is absent, allowing a platform's unrelated claim of the
// same name to fall through to the assertion digest.
type optionalString string

func (o *optionalString) UnmarshalJSON(data []byte) error {
	var value string
	if json.Unmarshal(data, &value) == nil {
		*o = optionalString(value)
	}
	return nil
}

// resolveReplayID prefers jti, then uti, then a digest of the exact JWT.
// A repeated minted identifier is a replay; a repeated digest denotes the
// same platform-cached token and remains acceptable within its validity.
func resolveReplayID(jti string, extra replayIDClaims, raw string) (id string, exact bool) {
	if jti != "" {
		return jti, true
	}
	if extra.UTI != "" {
		return string(extra.UTI), true
	}
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:]), false
}
