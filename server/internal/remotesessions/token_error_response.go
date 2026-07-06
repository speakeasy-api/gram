package remotesessions

import (
	"encoding/json"
)

// tokenErrorResponse is the RFC 6749 §5.2 error response an OAuth 2.0 token
// endpoint returns on a failed grant (e.g. a rejected refresh_token). Only the
// three RFC-defined members are modeled; provider-specific extensions are
// ignored. error_uri is rarely populated in practice but is part of the spec.
type tokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	ErrorURI         string `json:"error_uri"`
}

// parseTokenErrorResponse decodes an RFC 6749 §5.2 token error response body. A
// body that does not decode, or that omits the required "error" member, yields
// the zero value — whose summary falls back to the HTTP status.
func parseTokenErrorResponse(body []byte) tokenErrorResponse {
	var e tokenErrorResponse
	_ = json.Unmarshal(body, &e)
	return e
}

// summary renders a short, public-safe description of the error, preferring the
// RFC error / error_description and falling back to the supplied HTTP status
// when the body carried no recognizable error. The raw body is never surfaced.
func (e tokenErrorResponse) summary(status string) string {
	switch {
	case e.Error != "" && e.ErrorDescription != "":
		return e.Error + ": " + e.ErrorDescription
	case e.Error != "" && e.ErrorURI != "":
		return e.Error + ": " + e.ErrorURI
	case e.Error != "":
		return e.Error
	default:
		return "HTTP " + status
	}
}
