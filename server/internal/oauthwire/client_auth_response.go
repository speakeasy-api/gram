package oauthwire

import (
	"net/http"
	"strings"
)

// InvalidClientResponse selects the status and challenge for an invalid_client
// response at an OAuth token or revocation endpoint. A Basic authentication
// attempt requires a matching challenge even when its credentials cannot be
// decoded. Other client authentication failures use the default 400 response.
// Callers classify malformed requests and multiple credentials separately.
func InvalidClientResponse(header http.Header) (status int, challenge string) {
	for _, value := range header.Values("Authorization") {
		fields := strings.Fields(value)
		if len(fields) > 0 && strings.EqualFold(fields[0], "Basic") {
			return http.StatusUnauthorized, `Basic realm="Speakeasy"`
		}
	}
	return http.StatusBadRequest, ""
}
