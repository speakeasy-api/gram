package jwtclaims

import (
	"time"

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

// UnsafeExtractExpiry parses the token as an unverified JWT and returns the
// "exp" claim. The boolean is false when the token is not a valid JWT, has no
// exp claim, or carries one that is not a numeric date.
//
// UNSAFE: The token signature is not verified. The returned time comes from
// untrusted data and MUST NOT be used for authentication or authorization
// decisions. It is suitable only for scheduling, such as deciding when to
// attempt a refresh, where a forged value can at worst move that attempt
// earlier or later.
func UnsafeExtractExpiry(token string) (time.Time, bool) {
	parser := jwt.NewParser()

	claims := jwt.MapClaims{}
	_, _, err := parser.ParseUnverified(token, claims)
	if err != nil {
		return time.Time{}, false
	}

	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return time.Time{}, false
	}
	return exp.Time, true
}
