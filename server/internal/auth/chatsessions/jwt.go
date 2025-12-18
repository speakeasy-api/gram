package chatsessions

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ChatSessionClaims represents the custom claims for a chat session JWT
type ChatSessionClaims struct {
	OrgID            string  `json:"org_id"`
	ProjectID        string  `json:"project_id"`
	OrganizationSlug string  `json:"organization_slug"`
	ProjectSlug      string  `json:"project_slug"`
	UserIdentifier   *string `json:"user_identifier,omitempty"`
	jwt.RegisteredClaims
}

// GenerateToken creates a new JWT token for a chat session
func (m *Manager) GenerateToken(ctx context.Context, orgID, projectID, organizationSlug, projectSlug string, userIdentifier *string, expiresAfter int) (string, string, error) {
	now := time.Now()
	expiry := now.Add(time.Duration(expiresAfter) * time.Second)
	jti := uuid.New().String()

	claims := ChatSessionClaims{
		OrgID:            orgID,
		ProjectID:        projectID,
		OrganizationSlug: organizationSlug,
		ProjectSlug:      projectSlug,
		UserIdentifier:   userIdentifier,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiry),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        jti,
			Issuer:    "",
			Subject:   "",
			Audience:  nil,
			NotBefore: nil,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(m.jwtSecret))
	if err != nil {
		return "", "", fmt.Errorf("sign token: %w", err)
	}

	return signed, jti, nil
}

// ValidateToken validates a JWT token and returns the claims
func (m *Manager) ValidateToken(ctx context.Context, tokenString string) (*ChatSessionClaims, error) {
	//nolint:exhaustruct // no point in setting all the claims fields
	token, err := jwt.ParseWithClaims(tokenString, &ChatSessionClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(m.jwtSecret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := token.Claims.(*ChatSessionClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	// Check if token is revoked
	isRevoked, _ := m.IsTokenRevoked(ctx, claims.ID)
	if isRevoked {
		return nil, fmt.Errorf("token has been revoked")
	}

	return claims, nil
}
