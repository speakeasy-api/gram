package remotesessions

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/go-jose/go-jose/v4/jwt"
)

func validateRFC9068Claims(claims jwt.Claims, all map[string]json.RawMessage, clientID string) error {
	// RFC 9068 §2.2: iat, jti, and client_id are required by the profile.
	if claims.IssuedAt == nil || claims.ID == "" || claimString(all, "client_id") == "" {
		return errors.New("missing required profile claims")
	}
	// Gram binding policy: the profile's client_id must name this OAuth client.
	if claimString(all, "client_id") != clientID {
		return errors.New("client mismatch")
	}
	return nil
}

// parseJWTAccessTokenScope enforces RFC 6749's exact SP-delimited syntax.
func parseJWTAccessTokenScope(scope string) ([]string, error) {
	scopes := strings.Split(scope, " ")
	for _, token := range scopes {
		if token == "" {
			return nil, errors.New("invalid scope")
		}
		// RFC 6749 §3.3: scope-token = 1*( %x21 / %x23-5B / %x5D-7E ).
		for i := 0; i < len(token); i++ {
			c := token[i]
			if c < 0x21 || c > 0x7e || c == '"' || c == '\\' {
				return nil, errors.New("invalid scope")
			}
		}
	}
	return scopes, nil
}
