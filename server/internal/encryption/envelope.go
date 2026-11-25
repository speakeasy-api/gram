package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// EnvelopePrefix opens every ciphertext sealed with a data encryption key
// (DEK). '$' is outside the base64 alphabet, so an envelope can never be
// mistaken for a Client ciphertext, which is bare base64.
const EnvelopePrefix = "$sk1$"

// DataKeyBytes is the size of a DEK: an AES-256 key.
const DataKeyBytes = 32

// ErrNotEnvelope is returned when a value is not a DEK envelope.
var ErrNotEnvelope = errors.New("value is not a data encryption key envelope")

// ErrEnvelopeOpen is returned when an envelope does not decrypt under the given
// DEK and associated data: wrong key, wrong associated data, or tampering.
var ErrEnvelopeOpen = errors.New("data encryption key envelope did not open")

// Envelope is a ciphertext sealed with one DEK, encoded as
// "$sk1$<dek id>$<base64(nonce || AES-256-GCM ciphertext)>". The DEK id is in
// the clear so the reader knows which key to unwrap; changing it only selects a
// key that fails to open the ciphertext.
type Envelope struct {
	// DataKeyID identifies the DEK that sealed the value.
	DataKeyID uuid.UUID

	// Sealed is the GCM nonce followed by the ciphertext and tag.
	Sealed []byte
}

// IsEnvelope reports whether value is encoded as an Envelope.
func IsEnvelope(value string) bool {
	return strings.HasPrefix(value, EnvelopePrefix)
}

// ParseEnvelope decodes an envelope string.
func ParseEnvelope(value string) (Envelope, error) {
	rest, ok := strings.CutPrefix(value, EnvelopePrefix)
	if !ok {
		return Envelope{}, ErrNotEnvelope
	}

	rawID, encoded, ok := strings.Cut(rest, "$")
	if !ok {
		return Envelope{}, fmt.Errorf("%w: missing data key id separator", ErrNotEnvelope)
	}

	id, err := uuid.Parse(rawID)
	if err != nil {
		return Envelope{}, fmt.Errorf("%w: invalid data key id: %w", ErrNotEnvelope, err)
	}

	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Envelope{}, fmt.Errorf("%w: invalid ciphertext encoding: %w", ErrNotEnvelope, err)
	}

	return Envelope{DataKeyID: id, Sealed: sealed}, nil
}

// String encodes the envelope for storage.
func (e Envelope) String() string {
	return EnvelopePrefix + e.DataKeyID.String() + "$" + base64.StdEncoding.EncodeToString(e.Sealed)
}

// SealEnvelope encrypts plaintext with dataKey, binding aad as associated data
// that OpenEnvelope must be given again.
func SealEnvelope(dataKey []byte, dataKeyID uuid.UUID, plaintext, aad []byte) (Envelope, error) {
	aead, err := newDataKeyAEAD(dataKey)
	if err != nil {
		return Envelope{}, err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, newEncryptionError(errCodeNonceRead, err)
	}

	return Envelope{DataKeyID: dataKeyID, Sealed: aead.Seal(nonce, nonce, plaintext, aad)}, nil
}

// OpenEnvelope decrypts an envelope sealed with dataKey and aad. It does not
// check that dataKey is the key the envelope names; the caller selects the key
// by Envelope.DataKeyID.
func OpenEnvelope(dataKey []byte, envelope Envelope, aad []byte) ([]byte, error) {
	aead, err := newDataKeyAEAD(dataKey)
	if err != nil {
		return nil, err
	}

	if len(envelope.Sealed) < aead.NonceSize() {
		return nil, fmt.Errorf("%w: ciphertext too short", ErrEnvelopeOpen)
	}

	nonce, sealed := envelope.Sealed[:aead.NonceSize()], envelope.Sealed[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, sealed, aad)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEnvelopeOpen, err)
	}

	return plaintext, nil
}

func newDataKeyAEAD(dataKey []byte) (cipher.AEAD, error) {
	if len(dataKey) != DataKeyBytes {
		return nil, fmt.Errorf("invalid data encryption key size: %d bytes", len(dataKey))
	}

	block, err := aes.NewCipher(dataKey)
	if err != nil {
		return nil, newEncryptionError(errCodeAESNewCipher, err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, newEncryptionError(errCodeNewGCM, err)
	}

	return aead, nil
}
