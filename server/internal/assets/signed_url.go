package assets

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// SignedAssetClaims represents the custom claims for a signed asset URL token.
type SignedAssetClaims struct {
	AssetID   string `json:"asset_id"`
	ProjectID string `json:"project_id"`
	jwt.RegisteredClaims
}

// GenerateSignedAssetToken creates a new JWT token for accessing an asset.
func GenerateSignedAssetToken(jwtSecret string, assetID, projectID uuid.UUID, ttlSeconds time.Duration) (string, time.Time, error) {
	now := time.Now()
	expiry := now.Add(ttlSeconds * time.Second).Truncate(time.Second)
	jti := uuid.New().String()

	claims := SignedAssetClaims{
		AssetID:   assetID.String(),
		ProjectID: projectID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Issuer:    "",
			Subject:   "",
			Audience:  nil,
			ExpiresAt: jwt.NewNumericDate(expiry),
			NotBefore: nil,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}

	return signed, expiry, nil
}

// ValidateSignedAssetToken validates a JWT token and returns the claims.
func ValidateSignedAssetToken(jwtSecret string, tokenString string) (*SignedAssetClaims, error) {
	var claims SignedAssetClaims
	token, err := jwt.ParseWithClaims(tokenString, &claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return &claims, nil
}
