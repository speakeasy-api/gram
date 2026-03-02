package jwtclaims

import (
	"github.com/golang-jwt/jwt/v5"
)

// UnsafeExtractSubject parses the token as an unverified JWT and returns the
// "sub" claim. It returns "" when the token is not a valid JWT or has no sub
// claim.
//
// UNSAFE: The token signature is not verified. The returned subject comes from
// untrusted data and MUST NOT be used for authentication or authorization
// decisions.
func UnsafeExtractSubject(token string) string {
	parser := jwt.NewParser()

	claims := jwt.MapClaims{}
	_, _, err := parser.ParseUnverified(token, claims)
	if err != nil {
		return ""
	}

	sub, _ := claims.GetSubject()
	return sub
}
