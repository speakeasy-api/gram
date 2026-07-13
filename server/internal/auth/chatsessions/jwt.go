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
	// UserID is the authenticated dashboard user's ID. When present, allows
	// RBAC enforcement at the MCP gateway. Empty for API-key-only sessions.
	UserID string `json:"user_id,omitempty"`
	// SessionID is the dashboard session ID. When present (non-nil), the authz
	// engine will load and enforce RBAC grants for the user.
	SessionID *string `json:"session_id,omitempty"`
	// AccountType is the organization's billing tier (e.g. "enterprise").
	// Required for RBAC enforcement — the authz engine only loads grants
	// for enterprise accounts.
	AccountType string `json:"account_type,omitempty"`
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

// ValidateToken validates a JWT token and returns the claims. invalidToken
// reports that the token itself was rejected (malformed, bad signature,
// expired, or revoked) and the caller should be treated as unauthenticated;
// an error without invalidToken is a server fault, such as a failed
// revocation lookup.
func (m *Manager) ValidateToken(ctx context.Context, tokenString string) (claims *ChatSessionClaims, invalidToken bool, err error) {
	//nolint:exhaustruct // no point in setting all the claims fields
	token, err := jwt.ParseWithClaims(tokenString, &ChatSessionClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(m.jwtSecret), nil
	})

	if err != nil {
		return nil, true, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := token.Claims.(*ChatSessionClaims)
	if !ok || !token.Valid {
		return nil, true, fmt.Errorf("invalid token")
	}

	isRevoked, err := m.IsTokenRevoked(ctx, claims.ID)
	if err != nil {
		return nil, false, fmt.Errorf("check token revocation: %w", err)
	}
	if isRevoked {
		return nil, true, fmt.Errorf("token has been revoked")
	}

	return claims, false, nil
}
