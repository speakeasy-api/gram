package mcp

import (
	"net/http"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/privatekeyjwt"
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
	assertion privatekeyjwt.Assertion

	// hasClientAuthParameters is true when the form carried any client
	// authentication parameter, even with an empty value. The values above cannot tell an
	// empty client_id from an absent one, and dispatch must: a request that
	// sends client_id= is attempting client authentication and has to fail it,
	// rather than be handled as a caller that presented no client at all.
	hasClientAuthParameters bool
}

// clientAuthFormParameters are the form parameters that make up client
// authentication at the token endpoint.
var clientAuthFormParameters = []string{
	oauthwire.ParamClientID,
	oauthwire.ParamClientSecret,
	oauthwire.ParamClientAssertion,
	oauthwire.ParamClientAssertionType,
}

// presented reports whether the request offered any client authentication
// parameter at all, valid or not, including one sent with an empty value.
func (c presentedClientCredentials) presented() bool {
	return c.hasClientAuthParameters || c.clientID != "" || c.secret != "" || c.assertion.Presented()
}

// extractClientCredentials reads every client authentication parameter a
// request can carry. HTTP Basic still wins for the client_id and secret when
// both it and form parameters are present, so existing clients keep their
// behavior; the "multiple" label surfaces the misconfiguration in logs.
//
// "multiple" is decided on values, not on which keys the form carried. A
// parameter sent empty alongside a real credential is not a second
// authentication method under RFC 6749 section 2.3, and refusing it would
// turn a client that pads its form into a failed exchange. Dispatch still
// reads key presence, through presented, because a caller that names any
// client is one that has to authenticate.
func extractClientCredentials(r *http.Request) presentedClientCredentials {
	formID := r.PostForm.Get(oauthwire.ParamClientID)
	formSecret := r.PostForm.Get(oauthwire.ParamClientSecret)
	assertion := privatekeyjwt.Assertion{
		Value: r.PostForm.Get(oauthwire.ParamClientAssertion),
		Type:  r.PostForm.Get(oauthwire.ParamClientAssertionType),
	}
	hasFormCredentials := formID != "" || formSecret != ""
	hasClientAuthParameters := slices.ContainsFunc(clientAuthFormParameters, func(key string) bool {
		_, ok := r.PostForm[key]
		return ok
	})

	if id, secret, ok := r.BasicAuth(); ok && id != "" {
		method := oauthwire.AuthMethodClientSecretBasic
		if hasFormCredentials || assertion.Presented() {
			method = "multiple"
		}
		return presentedClientCredentials{clientID: id, secret: secret, method: method, assertion: assertion, hasClientAuthParameters: hasClientAuthParameters}
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
	return presentedClientCredentials{clientID: formID, secret: formSecret, method: method, assertion: assertion, hasClientAuthParameters: hasClientAuthParameters}
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
	claimed, err := privatekeyjwt.UnverifiedClientID(creds.assertion.Value)
	if err != nil {
		return "", string(privatekeyjwt.ReasonOf(err))
	}
	return claimed, ""
}
