package sessionhandoff

import (
	"encoding/hex"
)

const (
	PublicRoutePrefix  = "/shared/handoffs/"
	PublicRoutePattern = PublicRoutePrefix + "{token}"
	TokenBytes         = 32
)

func ValidToken(token string) bool {
	if len(token) != hex.EncodedLen(TokenBytes) {
		return false
	}
	decoded, err := hex.DecodeString(token)
	return err == nil && hex.EncodeToString(decoded) == token
}
