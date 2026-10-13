package platformmcp

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// A preview token is "<nonce>.<mac>". The nonce carries the preview's issue
// time and a random part, so every preview is distinct, a confirmed preview
// can be held to one idempotency key, and a preview expires with the receipt
// that would hold it. The mac binds the preview to its actor, project,
// operation, version and proposal; each tool family signs its own composition
// and holds a confirmation to it through verifyPreviewToken.

// The refusals a confirmation earns past the mac check, as every
// preview-and-confirm tool reports them.
const (
	previewUsedCode       = "preview_used"
	previewUsedMessage    = "This preview was already confirmed under another idempotency_key. Retry with that key to see its result, or preview again to make another change."
	previewExpiredCode    = "preview_expired"
	previewExpiredMessage = "This preview has expired. Preview the change again and confirm what that preview shows."
)

var (
	// errPreviewMismatch marks a confirmation whose token does not sign the
	// proposal, version, actor and project it carries.
	errPreviewMismatch = errors.New("preview token does not match the confirmation")

	// errPreviewExpired marks a confirmation of a preview older than the
	// receipt that would hold it to one key.
	errPreviewExpired = errors.New("preview expired")

	// errPreviewUsed marks a confirmation of a preview that already committed
	// under another idempotency key.
	errPreviewUsed = errors.New("preview already confirmed under another idempotency key")
)

// previewNonce starts a preview token: the time it was issued and a random
// part.
func previewNonce(now time.Time) string {
	return strconv.FormatInt(now.Unix(), 10) + "-" + rand.Text()
}

// joinPreviewToken makes the token a preview returns from its nonce and mac.
func joinPreviewToken(nonce string, mac []byte) string {
	return nonce + "." + hex.EncodeToString(mac)
}

// verifyPreviewToken holds a confirmation to its preview: resign signs the
// nonce the token carries with what the confirmation claims, and the result
// must be the token. Only then is the issue time in the nonce trusted for
// expiry, so a forged prefix cannot steer the check. A preview lives no
// longer than a receipt, so the receipt that holds it to one key is never
// swept while the preview can still be confirmed.
func verifyPreviewToken(token string, now time.Time, resign func(nonce string) (string, error)) error {
	nonce, _, _ := strings.Cut(token, ".")
	expected, err := resign(nonce)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(expected), []byte(token)) {
		return errPreviewMismatch
	}
	issued, _, _ := strings.Cut(nonce, "-")
	issuedAt, err := strconv.ParseInt(issued, 10, 64)
	if err != nil || now.Sub(time.Unix(issuedAt, 0)) > receiptLifetime {
		return errPreviewExpired
	}
	return nil
}
