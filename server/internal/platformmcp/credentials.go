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

var errInvalidCredential = errors.New("platform MCP credential is invalid")

type credentialKind string

const (
	authorizationCodeCredential credentialKind = "authorization_code"
	refreshTokenCredential      credentialKind = "refresh_token"
	accessJTICredential         credentialKind = "access_jti"
)

type credentialPayload struct {
	Kind           credentialKind `json:"kind"`
	OrganizationID string         `json:"organization_id"`

	// Resource is the Platform MCP resource (RFC 8707) the credential was
	// issued for. It is empty on credentials issued before the resource
	// followed the request's platform host; those belong to the configured
	// base URL's resource.
	Resource string `json:"resource,omitempty"`

	Secret string `json:"secret"`
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

// Issue mints an opaque credential bound to organizationID and resource.
func (c *CredentialCodec) Issue(kind credentialKind, organizationID, resource string) (string, error) {
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
		Resource:       resource,
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
	payload, err := c.decode(kind, credential)
	if err != nil {
		return "", err
	}
	return payload.OrganizationID, nil
}

// decode verifies and opens a credential of the given kind.
func (c *CredentialCodec) decode(kind credentialKind, credential string) (credentialPayload, error) {
	if c == nil || c.encryption == nil || credential == "" || len(credential) > maxPlatformCredentialLength {
		return credentialPayload{}, errInvalidCredential
	}
	raw, err := base64.RawURLEncoding.DecodeString(credential)
	if err != nil {
		return credentialPayload{}, errInvalidCredential
	}
	plaintext, err := c.encryption.Decrypt(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		return credentialPayload{}, errInvalidCredential
	}
	var payload credentialPayload
	if err := json.Unmarshal([]byte(plaintext), &payload); err != nil || payload.Kind != kind || payload.OrganizationID == "" || payload.Secret == "" {
		return credentialPayload{}, errInvalidCredential
	}
	return payload, nil
}
