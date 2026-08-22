package platformmcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// probeReceiptTTL bounds how long a probe receipt stays redeemable. The
// receipt attests to a live verification, so it must go stale quickly enough
// that registration cannot ride on materially outdated evidence; the remedy
// for an expired receipt is simply to re-probe.
const probeReceiptTTL = 10 * time.Minute

var (
	// ErrProbeReceiptInvalid reports a probe receipt that is malformed, carries
	// a bad signature, or was minted with unusable contents. It maps to the
	// receipt_invalid tool result code.
	ErrProbeReceiptInvalid = errors.New("invalid platform mcp probe receipt")

	// ErrProbeReceiptExpired reports an authentic probe receipt past its
	// expiry. It maps to the receipt_expired tool result code; the remedy is to
	// re-probe.
	ErrProbeReceiptExpired = errors.New("expired platform mcp probe receipt")

	// ErrProbeReceiptContextMismatch reports an authentic probe receipt
	// presented by a different caller than the one that probed. It maps to the
	// receipt_context_mismatch tool result code.
	ErrProbeReceiptContextMismatch = errors.New("platform mcp probe receipt bound to a different connection")
)

// probeReceipt is the server-issued identity a remote URL registration
// accepts in place of a raw URL. It is deliberately not project-bound: the
// probe mutates nothing project-scoped, so the same connection may redeem one
// receipt against any project it is eligible for — project eligibility is the
// registration flow's own check.
type probeReceipt struct {
	OrganizationID string `json:"organization_id"`

	// Binding identifies the probing caller. For an OAuth caller it is the
	// connection ID — unlike catalog cursors, which bind to the connection
	// generation, a receipt survives reauthorization within its TTL because it
	// attests to a probe of a URL, not to a session's walk. A connection-less
	// caller binds to its acting surface and subject instead, mirroring
	// principalCursorBinding.
	Binding string `json:"binding"`

	// NormalizedURL is the probed URL after normalizeRemoteURL. Encode enforces
	// that it is in normalized form, so redeeming code can trust it without
	// re-validating.
	NormalizedURL string `json:"normalized_url"`

	// ProbeDigest fingerprints the probe evidence the receipt was issued for,
	// binding what the user confirmed to what gets registered. The codec treats
	// it as opaque.
	ProbeDigest string `json:"probe_digest"`

	// ExpiresAt is the redemption deadline in Unix seconds.
	ExpiresAt int64 `json:"expires_at"`
}

// principalReceiptBinding is the caller identity a probe receipt binds to. An
// OAuth caller binds to its connection ID; a principal claiming a connection
// without one has an incomplete identity and gets no binding, mirroring the
// operation budget's refusal. A connection-less caller binds to its acting
// surface and subject, so the codec keeps working should the assistant
// audience ever be admitted — and so a receipt still cannot cross between the
// assistant and an OAuth connection acting for the same user.
func principalReceiptBinding(principal Principal) string {
	if principal.HasConnection() {
		return principal.ConnectionID
	}
	if principal.UserID == "" {
		return ""
	}
	return string(principal.surface()) + ":" + userSubjectURN(principal.UserID)
}

type probeReceiptCodec struct {
	key []byte
}

func newProbeReceiptCodec(keyMaterial string) (*probeReceiptCodec, error) {
	if keyMaterial == "" {
		return nil, ErrProbeReceiptInvalid
	}
	key := sha256.Sum256([]byte("platform-mcp-probe-receipt:" + keyMaterial))
	return &probeReceiptCodec{key: key[:]}, nil
}

// Encode mints a receipt for the probing principal over an already-normalized
// URL and the probe's evidence digest. The URL is re-normalized and compared
// so a receipt can never carry a URL the shape rules would refuse — the
// mutating redeemer trusts receipt contents without re-validating.
func (c *probeReceiptCodec) Encode(principal Principal, normalizedURL, probeDigest string, now time.Time) (string, error) {
	binding := principalReceiptBinding(principal)
	if c == nil || len(c.key) == 0 || principal.OrganizationID == "" || binding == "" || probeDigest == "" || now.IsZero() {
		return "", ErrProbeReceiptInvalid
	}
	if renormalized, err := normalizeRemoteURL(normalizedURL); err != nil || renormalized != normalizedURL {
		return "", ErrProbeReceiptInvalid
	}
	payload, err := json.Marshal(probeReceipt{
		OrganizationID: principal.OrganizationID,
		Binding:        binding,
		NormalizedURL:  normalizedURL,
		ProbeDigest:    probeDigest,
		ExpiresAt:      now.Add(probeReceiptTTL).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("encode platform mcp probe receipt: %w", err)
	}
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	token := make([]byte, 0, len(payload)+sha256.Size)
	token = append(token, payload...)
	token = append(token, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(token), nil
}

// Decode authenticates a receipt and returns its contents. Refusals are
// ordered: authenticity first (ErrProbeReceiptInvalid), then caller binding
// (ErrProbeReceiptContextMismatch), then expiry (ErrProbeReceiptExpired) —
// a caller the receipt was never minted for learns only that it is not
// theirs, never whether it would still redeem.
func (c *probeReceiptCodec) Decode(value string, principal Principal, now time.Time) (probeReceipt, error) {
	binding := principalReceiptBinding(principal)
	if c == nil || len(c.key) == 0 || value == "" || principal.OrganizationID == "" || binding == "" || now.IsZero() {
		return probeReceipt{}, ErrProbeReceiptInvalid
	}
	token, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(token) <= sha256.Size {
		return probeReceipt{}, ErrProbeReceiptInvalid
	}
	payload, signature := token[:len(token)-sha256.Size], token[len(token)-sha256.Size:]
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return probeReceipt{}, ErrProbeReceiptInvalid
	}
	var receipt probeReceipt
	if err := json.Unmarshal(payload, &receipt); err != nil || receipt.NormalizedURL == "" || receipt.ProbeDigest == "" || receipt.ExpiresAt <= 0 {
		return probeReceipt{}, ErrProbeReceiptInvalid
	}
	if receipt.OrganizationID != principal.OrganizationID || receipt.Binding != binding {
		return probeReceipt{}, ErrProbeReceiptContextMismatch
	}
	if !now.Before(time.Unix(receipt.ExpiresAt, 0)) {
		return probeReceipt{}, ErrProbeReceiptExpired
	}
	return receipt, nil
}
