package mcp

import (
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/usersessions/clientauth"
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

// presentedClientCredentials is everything a token or revocation request
// offered as client authentication, before any of it is checked.
type presentedClientCredentials struct {
	// clientID is the client_id from the form or the Basic username, or
	// empty when the request carried neither.
	clientID string

	// secret is the client_secret from the form or the Basic password.
	secret string

	// method names what was presented, in the vocabulary the credential
	// event logs use: a token_endpoint_auth_method value, or "multiple" when
	// the request mixed more than one.
	method string

	// assertion is the client_assertion pair, zero when absent.
	assertion clientauth.Assertion
}

// extractClientCredentials reads every client authentication parameter a
// request can carry. HTTP Basic still wins for the client_id and secret when
// both it and form parameters are present, so existing clients keep their
// behavior; the "multiple" label surfaces the misconfiguration in logs.
func extractClientCredentials(r *http.Request) presentedClientCredentials {
	formID := r.PostForm.Get("client_id")
	formSecret := r.PostForm.Get("client_secret")
	assertion := clientauth.Assertion{
		Value: r.PostForm.Get("client_assertion"),
		Type:  r.PostForm.Get("client_assertion_type"),
	}
	hasFormCredentials := formID != "" || formSecret != ""

	if id, secret, ok := r.BasicAuth(); ok && id != "" {
		method := oauthwire.AuthMethodClientSecretBasic
		if hasFormCredentials || assertion.Presented() {
			method = "multiple"
		}
		return presentedClientCredentials{clientID: id, secret: secret, method: method, assertion: assertion}
	}

	method := oauthwire.AuthMethodNone
	switch {
	case formSecret != "" && assertion.Presented():
		method = "multiple"
	case formSecret != "":
		method = oauthwire.AuthMethodClientSecretPost
	case assertion.Presented():
		method = oauthwire.AuthMethodPrivateKeyJWT
	}
	return presentedClientCredentials{clientID: formID, secret: formSecret, method: method, assertion: assertion}
}

// resolvePresentedClientID returns the client_id a request is authenticating
// as. RFC 7521 §4.2 makes the parameter optional when a client assertion is
// present, since the assertion's sub already names the client; in that case
// the identifier is read from the unverified assertion purely to select a
// row, and the signature check against that row's own key set is what makes
// it authoritative. Returns a failure reason when no identifier can be
// determined.
func resolvePresentedClientID(creds presentedClientCredentials) (clientID, failureReason string) {
	if creds.clientID != "" {
		return creds.clientID, ""
	}
	if !creds.assertion.Presented() {
		return "", "missing_client_id"
	}
	claimed, err := clientauth.UnverifiedClientID(creds.assertion.Value)
	if err != nil {
		return "", string(clientauth.ReasonOf(err))
	}
	return claimed, ""
}
