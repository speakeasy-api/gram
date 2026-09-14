// Package workload verifies assertions issued by an external platform for a
// separately admitted machine identity.
package workload

import (
	"slices"
	"time"

	assertioncore "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

// Expectation binds a platform assertion to the trusted issuer and exact
// external subject admitted by the authorization server.
type Expectation struct {
	// Issuer is the trusted platform's issuer identifier.
	Issuer string

	// Subject is the exact admitted external workload identity.
	Subject string

	// KeySource is the trusted issuer's published public key set.
	KeySource jwks.Source

	// ReplayIssuer scopes spent identifiers to this authorization server.
	ReplayIssuer string

	// ReplayParty is a stable server-side reference to the trusted issuer.
	ReplayParty string

	// Audiences are the endpoint identifiers this grant accepts.
	Audiences []string

	// MaxLifetime is the platform-specific assertion lifetime ceiling.
	MaxLifetime time.Duration
}

func (e Expectation) validate() error {
	if e.Issuer == "" || e.Subject == "" || e.ReplayIssuer == "" || e.ReplayParty == "" || len(e.Audiences) == 0 {
		return reject(ReasonVerifierMisconfigured, "issuer, subject, replay scope, and audience are required")
	}
	if slices.Contains(e.Audiences, "") {
		return reject(ReasonVerifierMisconfigured, "accepted audience cannot be empty")
	}
	if e.MaxLifetime <= 0 || e.MaxLifetime > time.Duration(1<<63-1)-2*assertioncore.MaxSkew {
		return reject(ReasonVerifierMisconfigured, "invalid workload assertion lifetime bound")
	}
	return nil
}
