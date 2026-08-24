package platformmcp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SubjectReferenceTTL bounds how long an opaque reference stays usable. A
// reference is a handle for one investigation, not a durable identifier: a
// short life keeps it from accumulating in a caller's notes or being replayed
// against a later session.
const SubjectReferenceTTL = 10 * time.Minute

// Subject reference kinds. A reference minted for one kind cannot be presented
// as another, so a trace handle can never be spent where a person is expected.
const (
	subjectKindUser  = "user"
	subjectKindTrace = "trace"
)

// Identity kinds carried inside a user reference. Telemetry records a person
// under one of three different columns, and the value alone does not say which:
// filtering an external user id as though it were an email matches nothing and
// reports an active person as inactive. The minting side states the column, so
// the reading side can filter the right one.
const (
	SubjectIdentityEmail    = "email"
	SubjectIdentityExternal = "external"
	SubjectIdentityUser     = "user"
)

// FormatSubjectIdentity builds the value a user reference carries. Summary
// tools mint references through this so the kind travels with the identifier.
func FormatSubjectIdentity(identityKind, identifier string) string {
	return identityKind + ":" + identifier
}

// parseSubjectIdentity splits a reference value back into its column and
// identifier. An unrecognized kind is refused rather than guessed, because
// guessing wrong reports an active person as inactive.
func parseSubjectIdentity(value string) (string, string, error) {
	identityKind, identifier, ok := strings.Cut(value, ":")
	if !ok || identifier == "" {
		return "", "", ErrSubjectReferenceNotFound
	}
	switch identityKind {
	case SubjectIdentityEmail, SubjectIdentityExternal, SubjectIdentityUser:
		return identityKind, identifier, nil
	default:
		return "", "", ErrSubjectReferenceNotFound
	}
}

// ErrSubjectReferenceNotFound is what an unknown, expired, cross-generation, or
// cross-organization reference resolves to. It is deliberately a single
// not-found rather than a set of distinguishable failures: telling a caller
// that a reference is "expired" rather than "unknown" confirms the reference
// once existed, which is itself information about another organization.
var ErrSubjectReferenceNotFound = errors.New("platform mcp subject reference not found")

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
//
// The payload is encrypted, not merely signed. A signed-but-plaintext token is
// not opaque — base64 is an encoding, not confidentiality — and the value
// inside is frequently an email address, so anyone holding the reference could
// read the identity it was minted to hide.
type subjectReferenceCodec struct {
	aead cipher.AEAD
}

func newSubjectReferenceCodec(keyMaterial string) (*subjectReferenceCodec, error) {
	if keyMaterial == "" {
		return nil, ErrSubjectReferenceNotFound
	}
	key := sha256.Sum256([]byte("platform-mcp-subject-reference:" + keyMaterial))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("build platform mcp subject reference cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build platform mcp subject reference aead: %w", err)
	}
	return &subjectReferenceCodec{aead: aead}, nil
}

// referenceAAD binds a reference to its organization, session, and kind through
// the AEAD's associated data. The same facts are inside the ciphertext, but
// carrying them here means a token minted for another organization, session, or
// kind fails to open at all rather than being decrypted and then rejected.
func referenceAAD(organizationID, binding, kind string) []byte {
	return []byte("platform-mcp-subject-reference|" + organizationID + "|" + binding + "|" + kind)
}

func (c *subjectReferenceCodec) Encode(principal Principal, kind, value string, now time.Time) (string, error) {
	binding := principalCursorBinding(principal)
	if c == nil || c.aead == nil || principal.OrganizationID == "" || binding == "" || kind == "" || value == "" {
		return "", ErrSubjectReferenceNotFound
	}
	payload, err := json.Marshal(subjectReference{
		OrganizationID: principal.OrganizationID,
		Binding:        binding,
		Kind:           kind,
		Value:          value,
		ExpiresAt:      now.Add(SubjectReferenceTTL).UnixNano(),
	})
	if err != nil {
		return "", fmt.Errorf("encode platform mcp subject reference: %w", err)
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate platform mcp subject reference nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, payload, referenceAAD(principal.OrganizationID, binding, kind))
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (c *subjectReferenceCodec) Decode(token string, principal Principal, kind string, now time.Time) (string, error) {
	binding := principalCursorBinding(principal)
	if c == nil || c.aead == nil || token == "" || principal.OrganizationID == "" || binding == "" || kind == "" {
		return "", ErrSubjectReferenceNotFound
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) <= c.aead.NonceSize() {
		return "", ErrSubjectReferenceNotFound
	}
	nonce, ciphertext := raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():]
	payload, err := c.aead.Open(nil, nonce, ciphertext, referenceAAD(principal.OrganizationID, binding, kind))
	if err != nil {
		return "", ErrSubjectReferenceNotFound
	}
	var reference subjectReference
	if err := json.Unmarshal(payload, &reference); err != nil {
		return "", ErrSubjectReferenceNotFound
	}
	// The AAD already bound these, so a mismatch here means the ciphertext and
	// its associated data disagree — re-checked rather than assumed.
	if reference.OrganizationID != principal.OrganizationID ||
		reference.Binding != binding ||
		reference.Kind != kind ||
		reference.Value == "" ||
		// Rejected at the boundary, so the advertised lifetime is a limit
		// rather than a floor.
		now.UnixNano() >= reference.ExpiresAt {
		return "", ErrSubjectReferenceNotFound
	}
	return reference.Value, nil
}
