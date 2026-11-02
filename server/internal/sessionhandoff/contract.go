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
	_, err := hex.DecodeString(token)
	return err == nil
}
