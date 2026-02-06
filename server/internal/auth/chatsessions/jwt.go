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
	ExternalUserID   *string `json:"external_user_id,omitempty"`
	APIKeyID         string  `json:"api_key_id"`
	jwt.RegisteredClaims
}

// GenerateToken creates a new JWT token for a chat session
func (m *Manager) GenerateToken(ctx context.Context, claims ChatSessionClaims, embedOrigin string, expiresAfter int) (string, string, error) {
	now := time.Now()
	expiry := now.Add(time.Duration(expiresAfter) * time.Second)
	jti := uuid.New().String()

	claims.RegisteredClaims = jwt.RegisteredClaims{
		ID:        jti,
		Audience:  jwt.ClaimStrings{embedOrigin},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiry),
		Issuer:    "",
		Subject:   "",
		NotBefore: nil,
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
