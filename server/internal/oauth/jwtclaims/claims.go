package jwtclaims

import (
	"github.com/golang-jwt/jwt/v5"
)

// ExtractSubject parses the token as an unverified JWT and returns the "sub"
// claim. It returns "" when the token is not a valid JWT or has no sub claim.
func ExtractSubject(token string) string {
	parser := jwt.NewParser()

	claims := jwt.MapClaims{}
	_, _, err := parser.ParseUnverified(token, claims)
	if err != nil {
		return ""
	}

	sub, _ := claims.GetSubject()
	return sub
}
