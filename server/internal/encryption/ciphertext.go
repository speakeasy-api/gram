package encryption

import (
	"encoding/base64"
	"errors"
	"fmt"
)

const (
	// gcmNonceBytes is the nonce size of cipher.NewGCM. Client.Encrypt and
	// SealEnvelope put the nonce in front of every ciphertext they produce.
	gcmNonceBytes = 12

	// gcmTagBytes is the authentication tag size of cipher.NewGCM, appended
	// after the ciphertext.
	gcmTagBytes = 16

	// emptySealedBytes is the decoded size of a sealed empty plaintext. AES-GCM
	// does not pad, so any non-empty plaintext seals to more bytes.
	emptySealedBytes = gcmNonceBytes + gcmTagBytes
)

// ErrMalformedCiphertext is returned when a value is not shaped like a
// ciphertext from Client.Encrypt or SealEnvelope.
var ErrMalformedCiphertext = errors.New("malformed ciphertext")

// IsEmptyCiphertext reports whether value, a ciphertext from Client.Encrypt or
// an encoded Envelope, seals an empty plaintext.
//
// It reads only the ciphertext length, which AES-GCM already reveals to anyone
// holding the ciphertext, so it needs no key. It also does not authenticate
// value: a ciphertext sealed under another key, or tampered with, is judged by
// its length alone.
//
// Envelopes are accepted so a column moving from Client.Encrypt to envelopes
// can hold both formats while it migrates.
func IsEmptyCiphertext(value string) (bool, error) {
	var sealed []byte
	if IsEnvelope(value) {
		envelope, err := ParseEnvelope(value)
		if err != nil {
			return false, fmt.Errorf("%w: %w", ErrMalformedCiphertext, err)
		}
		sealed = envelope.Sealed
	} else {
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return false, fmt.Errorf("%w: decode base64: %w", ErrMalformedCiphertext, err)
		}
		sealed = decoded
	}

	if len(sealed) < emptySealedBytes {
		return false, fmt.Errorf("%w: %d bytes is shorter than a GCM nonce and tag", ErrMalformedCiphertext, len(sealed))
	}

	return len(sealed) == emptySealedBytes, nil
}
