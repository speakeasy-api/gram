package sessions

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

// TransferTokenTTL is the maximum time a transfer token is valid. Transfer
// tokens are one-time-use; this TTL is a safety net for abandoned flows.
const TransferTokenTTL = 60 * time.Second

// TransferTokenClaims represents the claims for a cross-domain session transfer JWT.
type TransferTokenClaims struct {
	SessionID            string `json:"sid"`
	UserID               string `json:"uid"`
	ActiveOrganizationID string `json:"oid"`
	WorkOSSessionID      string `json:"wos,omitempty"`
	ImpersonatorEmail    string `json:"imp,omitempty"`
	SourceHost           string `json:"src"`
	TargetHost           string `json:"tgt"`
	jwt.RegisteredClaims
}

// transferTokenUsedKey returns the cache key marking a transfer token JTI as consumed.
func transferTokenUsedKey(jti string) string {
	return "session_transfer_used:" + jti
}

// TransferManager handles cross-domain session transfer tokens.
type TransferManager struct {
	secret []byte
	cache  cache.Cache
}

// NewTransferManager creates a new transfer token manager.
func NewTransferManager(secret []byte, c cache.Cache) *TransferManager {
	return &TransferManager{
		secret: secret,
		cache:  c,
	}
}

// CreateTransferToken creates a signed, one-time-use transfer token for moving
// a session from sourceHost to targetHost.
func (m *TransferManager) CreateTransferToken(ctx context.Context, session Session, sourceHost, targetHost string) (string, error) {
	jti, err := generateJTI()
	if err != nil {
		return "", fmt.Errorf("generate jti: %w", err)
	}

	now := time.Now()
	claims := TransferTokenClaims{
		SessionID:            session.SessionID,
		UserID:               session.UserID,
		ActiveOrganizationID: session.ActiveOrganizationID,
		WorkOSSessionID:      session.WorkOSSessionID,
		ImpersonatorEmail:    session.ImpersonatorEmail,
		SourceHost:           sourceHost,
		TargetHost:           targetHost,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Issuer:    "",
			Subject:   "",
			Audience:  nil,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(TransferTokenTTL)),
			NotBefore: nil,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("sign transfer token: %w", err)
	}

	return signed, nil
}

// ValidateTransferToken validates a transfer token, checking its signature,
// expiration, target host match, and one-time-use constraint. Returns the
// claims if valid. The token can only be validated once.
func (m *TransferManager) ValidateTransferToken(ctx context.Context, tokenString, expectedTargetHost string) (*TransferTokenClaims, error) {
	//nolint:exhaustruct // jwt.ParseWithClaims populates the claims during parsing
	token, err := jwt.ParseWithClaims(tokenString, &TransferTokenClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse transfer token: %w", err)
	}

	claims, ok := token.Claims.(*TransferTokenClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid transfer token")
	}

	if claims.TargetHost != expectedTargetHost {
		return nil, fmt.Errorf("transfer token target host mismatch: got %s, expected %s", claims.TargetHost, expectedTargetHost)
	}

	// Check one-time-use: atomically mark the token as used.
	// Add returns true if this call created the key (token not yet used),
	// false if the key already existed (token already used).
	usedKey := transferTokenUsedKey(claims.ID)
	created, err := m.cache.Add(ctx, usedKey, TransferTokenTTL+10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("check transfer token usage: %w", err)
	}
	if !created {
		return nil, fmt.Errorf("transfer token already used")
	}

	return claims, nil
}

// generateJTI generates a cryptographically secure random token ID.
func generateJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
