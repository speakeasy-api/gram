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

// SubjectReferenceTTL bounds how long an opaque reference stays usable. A
// reference is a handle for one investigation, not a durable identifier: a
// short life keeps it from accumulating in a caller's notes or being replayed
// against a later session.
const SubjectReferenceTTL = 15 * time.Minute

// Subject reference kinds. A reference minted for one kind cannot be presented
// as another, so a trace handle can never be spent where a person is expected.
const (
	subjectKindUser  = "user"
	subjectKindTrace = "trace"
)

var ErrSubjectReferenceInvalid = errors.New("invalid platform mcp subject reference")

type subjectReference struct {
	OrganizationID string `json:"organization_id"`
	// Binding ties the reference to the caller's session, exactly as a
	// pagination cursor is bound. A reference handed to another connection, or
	// surviving a reauthorization, stops resolving.
	Binding   string `json:"binding"`
	Kind      string `json:"kind"`
	Value     string `json:"value"`
	ExpiresAt int64  `json:"expires_at"`
}

// subjectReferenceCodec mints and resolves opaque references. It exists so a
// diagnostic can point at a person or an occurrence across two calls without
// ever naming one: the identity stays server-side, and what the caller holds is
// meaningless outside its organization, session, and TTL.
type subjectReferenceCodec struct {
	key []byte
}

func newSubjectReferenceCodec(keyMaterial string) (*subjectReferenceCodec, error) {
	if keyMaterial == "" {
		return nil, ErrSubjectReferenceInvalid
	}
	key := sha256.Sum256([]byte("platform-mcp-subject-reference:" + keyMaterial))
	return &subjectReferenceCodec{key: key[:]}, nil
}

func (c *subjectReferenceCodec) Encode(principal Principal, kind, value string, now time.Time) (string, error) {
	binding := principalCursorBinding(principal)
	if c == nil || len(c.key) == 0 || principal.OrganizationID == "" || binding == "" || kind == "" || value == "" {
		return "", ErrSubjectReferenceInvalid
	}
	payload, err := json.Marshal(subjectReference{
		OrganizationID: principal.OrganizationID,
		Binding:        binding,
		Kind:           kind,
		Value:          value,
		ExpiresAt:      now.Add(SubjectReferenceTTL).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("encode platform mcp subject reference: %w", err)
	}
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	token := make([]byte, 0, len(payload)+sha256.Size)
	token = append(token, payload...)
	token = append(token, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(token), nil
}

func (c *subjectReferenceCodec) Decode(token string, principal Principal, kind string, now time.Time) (string, error) {
	binding := principalCursorBinding(principal)
	if c == nil || len(c.key) == 0 || token == "" || principal.OrganizationID == "" || binding == "" || kind == "" {
		return "", ErrSubjectReferenceInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) <= sha256.Size {
		return "", ErrSubjectReferenceInvalid
	}
	payload, signature := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	// Compared before the payload is trusted, so a forged reference is rejected
	// on its signature rather than on the contents it claims.
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return "", ErrSubjectReferenceInvalid
	}
	var reference subjectReference
	if err := json.Unmarshal(payload, &reference); err != nil {
		return "", ErrSubjectReferenceInvalid
	}
	if reference.OrganizationID != principal.OrganizationID ||
		reference.Binding != binding ||
		reference.Kind != kind ||
		reference.Value == "" ||
		now.Unix() > reference.ExpiresAt {
		return "", ErrSubjectReferenceInvalid
	}
	return reference.Value, nil
}
