package oauthwire

import (
	"net/http"
	"strings"

	"golang.org/x/net/http/httpguts"

	"github.com/speakeasy-api/gram/server/internal/oautherr"
)

// InvalidClientResponse selects the status and challenge for an invalid_client
// response at an OAuth token or revocation endpoint. RFC 6749 section 5.2
// requires a matching challenge for any Authorization scheme, including an
// unsupported one. Authentication outside that header uses the default 400.
// Callers classify malformed requests and multiple credentials separately.
func InvalidClientResponse(header http.Header) (status int, challenge string) {
	values := header.Values("Authorization")
	if len(values) == 1 {
		if scheme := authorizationScheme(values[0]); scheme != "" {
			if strings.EqualFold(scheme, "Basic") {
				scheme = "Basic"
			}
			return http.StatusUnauthorized, scheme + ` realm="Speakeasy"`
		}
	}
	return http.StatusBadRequest, ""
}

// ClientAuthenticationError classifies malformed or unsupported client
// authentication before credential extraction or lookup. ParseForm must have
// populated PostForm. Repeated credentials and multiple authentication methods
// are invalid_request; a single unsupported or undecodable header credential is
// invalid_client. A bare client_id is an identifier, not another method.
func ClientAuthenticationError(r *http.Request) string {
	authorization := r.Header.Values("Authorization")
	if len(authorization) > 1 {
		return oautherr.CodeInvalidRequest
	}
	for _, key := range []string{ParamClientID, ParamClientSecret, ParamClientAssertion, ParamClientAssertionType} {
		if len(r.PostForm[key]) > 1 {
			return oautherr.CodeInvalidRequest
		}
	}
	secret := r.PostForm.Get(ParamClientSecret) != ""
	assertion := r.PostForm.Get(ParamClientAssertion) != "" || r.PostForm.Get(ParamClientAssertionType) != ""
	if (len(authorization) > 0 && (secret || assertion)) || (secret && assertion) {
		return oautherr.CodeInvalidRequest
	}
	if len(authorization) == 0 {
		return ""
	}
	scheme := authorizationScheme(authorization[0])
	if scheme == "" {
		return oautherr.CodeInvalidRequest
	}
	if !strings.EqualFold(scheme, "Basic") {
		return oautherr.CodeInvalidClient
	}
	id, _, ok := r.BasicAuth()
	if !ok || id == "" {
		return oautherr.CodeInvalidClient
	}
	if formID := r.PostForm.Get(ParamClientID); formID != "" && formID != id {
		return oautherr.CodeInvalidRequest
	}
	return ""
}

// HTTP authentication schemes use the same token grammar as field names.
// Checking it before reflecting the scheme keeps challenges syntactically valid.
func authorizationScheme(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 || !httpguts.ValidHeaderFieldName(fields[0]) {
		return ""
	}
	return fields[0]
}
