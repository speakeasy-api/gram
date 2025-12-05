package auth

import (
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

func NewServerJWT(ident RunnerIdentity, claims jwt.MapClaims) (string, error) {
	sub := fmt.Sprintf("%s:%s:%s", ident.ProjectID, ident.DeploymentID, ident.FunctionID)
	clone := make(jwt.MapClaims, len(claims)+1)
	for k, v := range claims {
		clone[k] = v
	}
	clone["sub"] = sub

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, clone)
	tokenString, err := token.SignedString(ident.AuthSecret.Reveal())
	if err != nil {
		return "", fmt.Errorf("sign server token: %w", err)
	}

	return tokenString, nil
}
