package exclusioncore

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"

	"github.com/speakeasy-api/gram/server/internal/inv"
)

const (
	exclusionRedactionDomain = "gram/risk/exclusion-redaction/v1"
	redactedValuePrefix      = "redacted:hmac-sha256:"
)

// Redactor produces stable project-scoped fingerprints without exposing values
// to offline dictionary attacks. Field names domain-separate match values from
// filters even when their literal content is identical.
type Redactor struct {
	key []byte
}

func NewRedactor(keyMaterial string) Redactor {
	inv.Require("risk exclusion redactor", "key material is configured", keyMaterial != "")

	derive := hmac.New(sha256.New, []byte(keyMaterial))
	_, _ = derive.Write([]byte(exclusionRedactionDomain))
	return Redactor{key: derive.Sum(nil)}
}

func (r Redactor) Configured() bool {
	return len(r.key) > 0
}

func (r Redactor) Redact(projectID, field, value string) string {
	if value == "" {
		return ""
	}
	inv.Require("risk exclusion redactor", "derived key is configured", len(r.key) > 0)

	mac := hmac.New(sha256.New, r.key)
	_, _ = mac.Write([]byte(projectID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(field))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(value))
	return redactedValuePrefix + hex.EncodeToString(mac.Sum(nil))[:16]
}
