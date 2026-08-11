package platformmcp

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/encryption"
)

const maxPlatformCredentialLength = 4096

type credentialKind string

const (
	authorizationCodeCredential credentialKind = "authorization_code"
	refreshTokenCredential      credentialKind = "refresh_token"
	accessJTICredential         credentialKind = "access_jti"
)

type credentialPayload struct {
	Kind           credentialKind `json:"kind"`
	OrganizationID string         `json:"organization_id"`
	Secret         string         `json:"secret"`
}

// CredentialCodec makes Platform MCP credentials opaque while preserving a verified
// organization routing hint for organization-scoped persistence queries.
type CredentialCodec struct {
	encryption *encryption.Client
}

func NewCredentialCodec(encryptionClient *encryption.Client) (*CredentialCodec, error) {
	if encryptionClient == nil {
		return nil, errors.New("platform MCP credential codec requires encryption")
	}
	return &CredentialCodec{encryption: encryptionClient}, nil
}

func (c *CredentialCodec) Issue(kind credentialKind, organizationID string) (string, error) {
	if c == nil || c.encryption == nil || organizationID == "" {
		return "", errors.New("platform MCP credential input is incomplete")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("read platform MCP credential entropy: %w", err)
	}
	payload, err := json.Marshal(credentialPayload{
		Kind:           kind,
		OrganizationID: organizationID,
		Secret:         base64.RawURLEncoding.EncodeToString(secret),
	})
	if err != nil {
		return "", fmt.Errorf("encode platform MCP credential: %w", err)
	}
	encoded, err := c.encryption.Encrypt(payload)
	if err != nil {
		return "", fmt.Errorf("encrypt platform MCP credential: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode encrypted platform MCP credential: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (c *CredentialCodec) OrganizationID(kind credentialKind, credential string) (string, error) {
	if c == nil || c.encryption == nil || credential == "" || len(credential) > maxPlatformCredentialLength {
		return "", errors.New("platform MCP credential is invalid")
	}
	raw, err := base64.RawURLEncoding.DecodeString(credential)
	if err != nil {
		return "", errors.New("platform MCP credential is invalid")
	}
	plaintext, err := c.encryption.Decrypt(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		return "", errors.New("platform MCP credential is invalid")
	}
	var payload credentialPayload
	if err := json.Unmarshal([]byte(plaintext), &payload); err != nil || payload.Kind != kind || payload.OrganizationID == "" || payload.Secret == "" {
		return "", errors.New("platform MCP credential is invalid")
	}
	return payload.OrganizationID, nil
}
