package clientauth

import "errors"

// Reason is the machine-readable label for one assertion rejection. Values
// are stable: they name failure reasons in logs, so operators diagnosing a
// client that cannot authenticate can tell "wrong audience" from "unknown
// key" from "already used" without reproducing the request.
type Reason string

const (
	// ReasonAssertionMissing reports a client that owes an assertion and
	// presented none.
	ReasonAssertionMissing Reason = "assertion_missing"

	// ReasonTypeUnsupported reports a client_assertion_type that is
	// not RFC 7523's jwt-bearer URN.
	ReasonTypeUnsupported Reason = "assertion_type_unsupported"

	// ReasonMalformed reports an assertion that is not a parseable compact
	// JWS, which includes one signed with an algorithm outside the
	// allowlist: the algorithm is fixed at parse time, so `none` and every
	// HS* land here rather than reaching signature verification.
	ReasonMalformed Reason = "assertion_malformed"

	// ReasonKeyUnresolvable reports a key set that could not be consulted at
	// all: unreachable, unparseable, or refused by the refresh limiter.
	ReasonKeyUnresolvable Reason = "assertion_key_unresolvable"

	// ReasonKeyUnknown reports an assertion whose kid names no key in the
	// client's current key set, after any permitted refresh.
	ReasonKeyUnknown Reason = "assertion_key_unknown"

	// ReasonSignatureInvalid reports an assertion whose signature does not
	// verify under the resolved key.
	ReasonSignatureInvalid Reason = "assertion_signature_invalid"

	// ReasonSubjectMismatch reports an iss or sub that is not what the
	// expectation named: for a client assertion, both must be the client_id
	// the request is authenticating, as RFC 7523 §3 requires; for a
	// workload assertion, iss must be the trusted issuer and sub the
	// admitted external subject.
	ReasonSubjectMismatch Reason = "assertion_subject_mismatch"

	// ReasonAudienceMismatch reports an aud naming neither this endpoint's issuer
	// identifier nor its own URL. It is the single most common
	// private_key_jwt interop failure, so it has its own label.
	ReasonAudienceMismatch Reason = "assertion_audience_mismatch"

	// ReasonExpiryMissing reports an assertion with no exp claim, which RFC
	// 7523 §3 requires and without which the replay hold has no end.
	ReasonExpiryMissing Reason = "assertion_expiry_missing"

	// ReasonExpired reports an exp already past, beyond the tolerated skew.
	ReasonExpired Reason = "assertion_expired"

	// ReasonLifetimeTooLong reports an exp further out than the server
	// accepts, which bounds how long the replay guard must remember it.
	ReasonLifetimeTooLong Reason = "assertion_lifetime_too_long"

	// ReasonNotYetValid reports an nbf or iat in the future, beyond the
	// tolerated skew.
	ReasonNotYetValid Reason = "assertion_not_yet_valid"

	// ReasonIDMissing reports an assertion with no jti, which cannot be held
	// against replay and so cannot be accepted.
	ReasonIDMissing Reason = "assertion_id_missing"

	// ReasonReplayed reports a jti this client already spent inside its
	// validity window.
	ReasonReplayed Reason = "assertion_replayed"

	// ReasonReplayStoreUnavailable reports a replay guard that could not be
	// consulted. The assertion is refused rather than admitted: an
	// identifier whose status is unknown has to be treated as already seen,
	// or an outage would suspend replay protection exactly when it matters.
	ReasonReplayStoreUnavailable Reason = "assertion_replay_store_unavailable"

	// ReasonVerifierMisconfigured reports an Expectation the caller
	// assembled without an issuer or subject to match, without a replay
	// scope, or without any accepted audience — or one whose assertions
	// could outlive what the replay guard remembers. It is a server wiring
	// fault, never a client error, and has its own label so it is not
	// mistaken for the client-shaped failure each of those would otherwise
	// produce.
	ReasonVerifierMisconfigured Reason = "assertion_verifier_misconfigured"
)

// ReasonOf extracts the rejection label from an assertion failure, or "" when
// the error did not come from this package.
func ReasonOf(err error) Reason {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Reason
	}
	return ""
}
