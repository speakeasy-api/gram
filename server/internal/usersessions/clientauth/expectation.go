package clientauth

import "github.com/speakeasy-api/gram/server/internal/usersessions/jwks"

// Expectation is what the presented assertion has to satisfy, assembled by
// the caller from the resolved client row and endpoint.
type Expectation struct {
	// ClientID is the identifier being authenticated. The assertion's iss
	// and sub must both equal it.
	ClientID string

	// KeySource is where this client's public keys come from, built from the
	// key source recorded when the client registered or its metadata
	// document was read.
	KeySource jwks.Source

	// ReplayIssuer is the stable server-side identity of the authorization
	// server that scopes spent identifiers, never a value derived from the
	// request.
	ReplayIssuer string

	// Audiences are the aud values this endpoint accepts, computed per
	// request from the endpoint being addressed.
	Audiences Audiences
}

// validate catches an Expectation the caller assembled incompletely. Each of
// these would otherwise surface as a client-shaped failure: an empty ClientID
// matches an assertion with no iss or sub, an empty ReplayIssuer makes the
// replay guard refuse every key, and empty Audiences reject every aud. All
// three are wiring faults and are labelled as such, so an operator does not
// chase a client bug or a store outage that is neither.
func (e Expectation) validate() error {
	if e.ClientID == "" {
		return reject(ReasonVerifierMisconfigured, "no client_id to authenticate against")
	}
	if e.ReplayIssuer == "" {
		return reject(ReasonVerifierMisconfigured, "no replay issuer configured for this endpoint")
	}
	if len(e.Audiences.accepted()) == 0 {
		return reject(ReasonVerifierMisconfigured, "no audience values configured for this endpoint")
	}
	return nil
}
