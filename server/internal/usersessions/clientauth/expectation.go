package clientauth

import (
	"time"

	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

// Expectation is what the presented assertion has to satisfy, assembled by
// the caller from the resolved client row and endpoint, or from the trusted
// issuer and admitted identity a workload names.
//
// Two profiles share it. A client assertion authenticates a client to itself
// and RFC 7523 §3 makes iss, sub, and client_id one value; build one with
// ClientExpectation, which is where that rule lives. A workload assertion
// says "this issuer vouches for this machine", so iss and sub are different
// values and neither is a client id; build one field by field, and note that
// both are matched exactly rather than as patterns — an issuer trusted
// without naming subjects vouches for every workload on its platform, its
// other customers included.
type Expectation struct {
	// Issuer is the value the assertion's iss must equal: the client_id
	// for a client assertion, the platform's issuer identifier for a
	// workload assertion.
	Issuer string

	// Subject is the value the assertion's sub must equal: the same
	// client_id for a client assertion, the admitted external subject for
	// a workload assertion.
	//
	// Whether it equals Issuer is the profile's choice, which is why the
	// two are matched separately rather than compared against one value.
	Subject string

	// KeySource is where the signing party's public keys come from: the
	// source recorded when a client registered or its metadata document
	// was read, or a trusted issuer's published key set.
	KeySource jwks.Source

	// ReplayIssuer is the stable server-side identity of the authorization
	// server that scopes spent identifiers, never a value derived from the
	// request.
	ReplayIssuer string

	// ReplayParty scopes a spent jti to who minted it, within
	// ReplayIssuer: the client_id, or a stable reference to the trusted
	// issuer row. A jti is only unique per minting party, so without this
	// one party's identifiers would collide with another's.
	ReplayParty string

	// ReplaySubject narrows the identifier within ReplayParty and is empty
	// when ReplayParty already names the whole party. A workload sets it
	// to the external subject: one platform issuer vouches for many
	// workloads, and a jti is unique per issuer rather than per workload.
	ReplaySubject string

	// Audiences are the aud values this endpoint accepts, computed per
	// request from the endpoint being addressed.
	Audiences Audiences

	// MaxLifetime bounds how far this assertion's exp may sit in the
	// future. Zero means DefaultMaxLifetime.
	//
	// The bound belongs to the profile rather than the package because the
	// two disagree. DefaultMaxLifetime is a client-assertion ceiling, taken
	// from what the strictest mainstream profile permits and what real
	// implementations emit; a platform token's lifetime is set by that
	// platform. Kubernetes is the case that forces the distinction: a
	// projected service account token's expirationSeconds is routinely
	// configured past an hour, and a cluster issuing longer-lived tokens
	// would otherwise have every assertion rejected for a reason that has
	// nothing to do with workloads.
	//
	// Widening DefaultMaxLifetime is not the lever to reach for: it also
	// sets DefaultMaxReplayHold, so it widens the replay window for client
	// authentication to buy something only workloads need.
	MaxLifetime time.Duration
}

// ClientExpectation is the expectation for a client authenticating itself
// with a private_key_jwt assertion.
//
// It holds RFC 7523 §3's rule that iss, sub, and client_id are one value, so
// call sites cannot set Issuer and Subject apart: that would admit an
// assertion vouching for someone other than the client being authenticated,
// and nothing downstream would notice.
func ClientExpectation(clientID string, keySource jwks.Source, replayIssuer string, audiences Audiences) Expectation {
	return Expectation{
		Issuer:        clientID,
		Subject:       clientID,
		KeySource:     keySource,
		ReplayIssuer:  replayIssuer,
		ReplayParty:   clientID,
		ReplaySubject: "",
		Audiences:     audiences,
		MaxLifetime:   DefaultMaxLifetime,
	}
}

// validate catches an Expectation the caller assembled incompletely. Each of
// these would otherwise surface as a client-shaped failure: an empty Issuer
// or Subject matches an assertion missing that claim, an empty replay part
// makes the guard refuse every key, and empty Audiences reject every aud.
// All are wiring faults and are labelled as such, so an operator does not
// chase a client bug or a store outage that is neither.
func (e Expectation) validate() error {
	if e.Issuer == "" {
		return reject(ReasonVerifierMisconfigured, "no issuer to authenticate against")
	}
	if e.Subject == "" {
		return reject(ReasonVerifierMisconfigured, "no subject to authenticate against")
	}
	if e.ReplayIssuer == "" {
		return reject(ReasonVerifierMisconfigured, "no replay issuer configured for this endpoint")
	}
	if e.ReplayParty == "" {
		return reject(ReasonVerifierMisconfigured, "no replay party configured for this assertion")
	}
	if len(e.Audiences.accepted()) == 0 {
		return reject(ReasonVerifierMisconfigured, "no audience values configured for this endpoint")
	}
	if e.MaxLifetime < 0 {
		return reject(ReasonVerifierMisconfigured, "assertion lifetime bound is negative")
	}
	// A bound large enough to overflow its own replay hold would wrap to a
	// negative duration, which compares below every guard cap and so would
	// pass the check in Verify that exists to catch exactly this.
	if e.MaxLifetime > 0 && ReplayHoldFor(e.MaxLifetime) < e.MaxLifetime {
		return reject(ReasonVerifierMisconfigured, "assertion lifetime bound overflows its replay hold")
	}
	// iss == sub is the client profile, whose ceiling is fixed by the
	// mainstream profiles rather than chosen per endpoint. Enforced here so
	// the bound cannot be widened for a client by assembling the struct
	// directly instead of through ClientExpectation.
	if e.Issuer == e.Subject && e.MaxLifetime > DefaultMaxLifetime {
		return reject(ReasonVerifierMisconfigured, "client assertion lifetime bound exceeds %s", DefaultMaxLifetime)
	}
	// iss != sub is a workload, where one issuer vouches for many subjects.
	// Without the subject in the replay scope they share one keyspace, and
	// the first to spend a jti makes every other workload's assertion
	// carrying it fail as a replay.
	if e.Issuer != e.Subject && e.ReplaySubject == "" {
		return reject(ReasonVerifierMisconfigured, "no replay subject configured for this workload")
	}
	return nil
}

// lifetime is the ceiling this expectation puts on an assertion's exp,
// falling back to the client-assertion bound when the caller named none.
func (e Expectation) lifetime() time.Duration {
	if e.MaxLifetime == 0 {
		return DefaultMaxLifetime
	}
	return e.MaxLifetime
}

// replayHold is how long a spent identifier must be remembered under this
// expectation: the full window in which an accepted assertion can still
// verify.
func (e Expectation) replayHold() time.Duration {
	return ReplayHoldFor(e.lifetime())
}
