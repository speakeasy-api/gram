package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// NewServerJWT provisions a JWT that can be used to make API calls to the
// functions service on the Gram API.
func NewServerJWT(ident RunnerIdentity, claims jwt.MapClaims) (string, error) {
	sub := fmt.Sprintf("%s:%s:%s", ident.ProjectID, ident.DeploymentID, ident.FunctionID)
	clone := make(jwt.MapClaims, len(claims)+1)
	for k, v := range claims {
		clone[k] = v
	}
	clone["sub"] = sub
	clone["exp"] = time.Now().Add(10 * time.Minute).Unix()

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, clone)
	tokenString, err := token.SignedString(ident.AuthSecret.Reveal())
	if err != nil {
		return "", fmt.Errorf("sign server token: %w", err)
	}

	return tokenString, nil
}
