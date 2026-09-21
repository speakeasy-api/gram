package privatekeyjwt

import "github.com/speakeasy-api/gram/server/internal/usersessions/jwks"

// Expectation binds a client assertion to the registered client and the
// authorization server endpoint receiving it.
type Expectation struct {
	// ClientID is the authenticated client identifier required in both iss
	// and sub by RFC 7523 §3.
	ClientID string

	// KeySource is the key set registered by this client.
	KeySource jwks.Source

	// ReplayIssuer scopes spent identifiers to this authorization server.
	ReplayIssuer string

	// Audiences names the issuer and addressed endpoint accepted for aud.
	Audiences Audiences
}

// ClientExpectation builds the RFC 7523 client-authentication expectation.
func ClientExpectation(clientID string, keySource jwks.Source, replayIssuer string, audiences Audiences) Expectation {
	return Expectation{ClientID: clientID, KeySource: keySource, ReplayIssuer: replayIssuer, Audiences: audiences}
}

func (e Expectation) validate() error {
	if e.ClientID == "" {
		return reject(ReasonVerifierMisconfigured, "no client_id to authenticate")
	}
	if e.ReplayIssuer == "" {
		return reject(ReasonVerifierMisconfigured, "no replay issuer configured for this endpoint")
	}
	if len(e.Audiences.accepted()) == 0 {
		return reject(ReasonVerifierMisconfigured, "no audience values configured for this endpoint")
	}
	return nil
}
