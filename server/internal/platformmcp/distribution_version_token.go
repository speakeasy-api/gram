package platformmcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrDistributionVersionTokenInvalid = errors.New("invalid platform mcp distribution version token")

type distributionVersionToken struct {
	OrganizationID string `json:"organization_id"`
	UserID         string `json:"user_id"`
	ProjectSlug    string `json:"project_slug"`
	Version        int64  `json:"version"`
}

type distributionVersionTokenCodec struct {
	key []byte
}

func newDistributionVersionTokenCodec(keyMaterial string) (*distributionVersionTokenCodec, error) {
	if keyMaterial == "" {
		return nil, ErrDistributionVersionTokenInvalid
	}
	key := sha256.Sum256([]byte("platform-mcp-distribution-version:" + keyMaterial))
	return &distributionVersionTokenCodec{key: key[:]}, nil
}

func (c *distributionVersionTokenCodec) Encode(principal Principal, projectSlug string, version int64) (string, error) {
	if c == nil || len(c.key) == 0 || principal.OrganizationID == "" || principal.UserID == "" || projectSlug == "" || version < 0 {
		return "", ErrDistributionVersionTokenInvalid
	}
	payload, err := json.Marshal(distributionVersionToken{
		OrganizationID: principal.OrganizationID,
		UserID:         principal.UserID,
		ProjectSlug:    projectSlug,
		Version:        version,
	})
	if err != nil {
		return "", fmt.Errorf("encode platform mcp distribution version token: %w", err)
	}
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	token := make([]byte, 0, len(payload)+sha256.Size)
	token = append(token, payload...)
	token = append(token, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(token), nil
}

func (c *distributionVersionTokenCodec) Decode(value string, principal Principal, projectSlug string) (int64, error) {
	if c == nil || len(c.key) == 0 || value == "" || principal.OrganizationID == "" || principal.UserID == "" || projectSlug == "" {
		return 0, ErrDistributionVersionTokenInvalid
	}
	token, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(token) <= sha256.Size {
		return 0, ErrDistributionVersionTokenInvalid
	}
	payload, signature := token[:len(token)-sha256.Size], token[len(token)-sha256.Size:]
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return 0, ErrDistributionVersionTokenInvalid
	}
	var decoded distributionVersionToken
	if err := json.Unmarshal(payload, &decoded); err != nil || decoded.Version < 0 || decoded.OrganizationID != principal.OrganizationID || decoded.UserID != principal.UserID || decoded.ProjectSlug != projectSlug {
		return 0, ErrDistributionVersionTokenInvalid
	}
	return decoded.Version, nil
}
