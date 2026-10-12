package platformmcp

import (
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
	key signedCursorKey
}

func newDistributionVersionTokenCodec(keyMaterial string) (*distributionVersionTokenCodec, error) {
	if keyMaterial == "" {
		return nil, ErrDistributionVersionTokenInvalid
	}
	return &distributionVersionTokenCodec{key: newSignedCursorKey("platform-mcp-distribution-version", keyMaterial)}, nil
}

func (c *distributionVersionTokenCodec) Encode(principal Principal, projectSlug string, version int64) (string, error) {
	if c == nil || len(c.key) == 0 || principal.OrganizationID == "" || principal.UserID == "" || projectSlug == "" || version < 0 {
		return "", ErrDistributionVersionTokenInvalid
	}
	token, err := sealCursor(c.key, distributionVersionToken{
		OrganizationID: principal.OrganizationID,
		UserID:         principal.UserID,
		ProjectSlug:    projectSlug,
		Version:        version,
	})
	if err != nil {
		return "", fmt.Errorf("encode platform mcp distribution version token: %w", err)
	}
	return token, nil
}

func (c *distributionVersionTokenCodec) Decode(value string, principal Principal, projectSlug string) (int64, error) {
	if c == nil || len(c.key) == 0 || value == "" || principal.OrganizationID == "" || principal.UserID == "" || projectSlug == "" {
		return 0, ErrDistributionVersionTokenInvalid
	}
	decoded, ok := openCursor[distributionVersionToken](c.key, value)
	if !ok || decoded.Version < 0 || decoded.OrganizationID != principal.OrganizationID || decoded.UserID != principal.UserID || decoded.ProjectSlug != projectSlug {
		return 0, ErrDistributionVersionTokenInvalid
	}
	return decoded.Version, nil
}
