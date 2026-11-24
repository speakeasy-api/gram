package transcriptcrypto

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// assetColumn names the stored location of sealed asset objects in the
// encryption associated data. It is not a database column: sealed assets live
// in object storage, next to their plaintext object.
const assetColumn = "chat_transcript_assets.object"
const assetVersion = 1

// maxAssetHeader bounds the header length read from an opened asset before it
// is parsed.
const maxAssetHeader = 1024

// AssetKind says which transcript copy an asset object holds.
type AssetKind string

const (
	// AssetMessageContent is a chat message body uploaded as JSON. Large
	// messages are read only from this object.
	AssetMessageContent AssetKind = "message_content"

	// AssetContentPart is the body of a chat_content_parts row, such as a
	// prompt attachment.
	AssetContentPart AssetKind = "content_part"

	// AssetChatAttachment is a file uploaded through the asset service for a
	// chat. It belongs to a project, not to a chat.
	AssetChatAttachment AssetKind = "chat_attachment"
)

// AssetIdentity binds a sealed asset to its owner and to the exact plaintext
// bytes. Asset objects are content addressed and shared by every row of the
// same chat (or project, for chat attachments) that has the same bytes, so the
// identity names the chat and the content hash, not a message.
type AssetIdentity struct {
	Kind      AssetKind `json:"kind"`
	ProjectID uuid.UUID `json:"project_id"`
	// ChatID is uuid.Nil for AssetChatAttachment and required otherwise.
	ChatID uuid.UUID `json:"chat_id"`
	// SHA256 is the lowercase hex SHA-256 of the plaintext bytes.
	SHA256 string `json:"sha256"`
}

type assetHeader struct {
	Version int `json:"version"`
	AssetIdentity
}

// ErrAssetMismatch reports a sealed asset that opened but belongs to another
// identity or holds other bytes than its content hash names.
var ErrAssetMismatch = errors.New("sealed transcript asset does not match its identity")

func (id AssetIdentity) validate() error {
	switch id.Kind {
	case AssetMessageContent, AssetContentPart:
		if id.ChatID == uuid.Nil {
			return errors.New("transcript asset chat identity is required")
		}
	case AssetChatAttachment:
		if id.ChatID != uuid.Nil {
			return errors.New("chat attachment assets have no chat identity")
		}
	default:
		return errors.New("unknown transcript asset kind")
	}
	if id.ProjectID == uuid.Nil {
		return errors.New("transcript asset project identity is required")
	}
	if !IsContentHash(id.SHA256) {
		return errors.New("transcript asset content hash must be lowercase hex SHA-256")
	}
	return nil
}

// IsContentHash reports whether s is a lowercase hex SHA-256, as content
// addressed asset object names use.
func IsContentHash(s string) bool {
	if len(s) != sha256.Size*2 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ContentHash returns the lowercase hex SHA-256 that AssetIdentity expects.
func ContentHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// EncryptAsset seals body for identity. The body must hash to identity.SHA256,
// so a caller cannot seal bytes under another object's name.
func (c *Codec) EncryptAsset(ctx context.Context, identity AssetIdentity, body []byte) ([]byte, error) {
	if c == nil || c.encryptor == nil {
		return nil, errors.New("transcript codec is not initialized")
	}
	return encryptAsset(ctx, c.encryptor, identity, body)
}

// DecryptAsset authenticates a sealed asset and checks its identity and
// content hash. Every failure returns nil, never partial plaintext.
func (c *Codec) DecryptAsset(ctx context.Context, identity AssetIdentity, sealed []byte) ([]byte, error) {
	if c == nil || c.encryptor == nil {
		return nil, errors.New("transcript codec is not initialized")
	}
	return decryptAsset(ctx, c.encryptor, identity, sealed)
}

// The sealed plaintext is a 4-byte big-endian header length, the JSON header,
// then the raw body. Raw framing keeps binary attachments at the envelope's
// base64 overhead instead of base64 twice.
func encryptAsset(ctx context.Context, e cryptor, identity AssetIdentity, body []byte) ([]byte, error) {
	if err := identity.validate(); err != nil {
		return nil, err
	}
	if ContentHash(body) != identity.SHA256 {
		return nil, ErrAssetMismatch
	}
	header, err := json.Marshal(assetHeader{Version: assetVersion, AssetIdentity: identity})
	if err != nil || len(header) > maxAssetHeader {
		return nil, errors.New("encode transcript asset header")
	}
	plaintext := make([]byte, 0, 4+len(header)+len(body))
	plaintext = binary.BigEndian.AppendUint32(plaintext, uint32(len(header))) //nolint:gosec // bounded by maxAssetHeader
	plaintext = append(plaintext, header...)
	plaintext = append(plaintext, body...)
	ciphertext, err := e.Encrypt(ctx, assetColumn, plaintext)
	if err != nil {
		return nil, &operationError{operation: "encrypt transcript asset", cause: err}
	}
	if ciphertext == "" {
		return nil, errors.New("encrypt transcript asset: empty ciphertext")
	}
	return []byte(ciphertext), nil
}

func decryptAsset(ctx context.Context, e cryptor, identity AssetIdentity, sealed []byte) ([]byte, error) {
	if err := identity.validate(); err != nil {
		return nil, err
	}
	plaintext, err := e.Decrypt(ctx, assetColumn, string(sealed))
	if err != nil {
		return nil, &operationError{operation: "decrypt transcript asset", cause: err}
	}
	if len(plaintext) < 4 {
		return nil, errors.New("decode transcript asset")
	}
	n := binary.BigEndian.Uint32(plaintext[:4])
	if n == 0 || n > maxAssetHeader || int(n) > len(plaintext)-4 {
		return nil, errors.New("decode transcript asset")
	}
	var h assetHeader
	if err := json.Unmarshal(plaintext[4:4+n], &h); err != nil {
		return nil, errors.New("decode transcript asset")
	}
	body := plaintext[4+n:]
	if h.Version != assetVersion || h.AssetIdentity != identity || ContentHash(body) != identity.SHA256 {
		return nil, ErrAssetMismatch
	}
	return body, nil
}
