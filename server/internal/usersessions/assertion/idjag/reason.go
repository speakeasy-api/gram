package idjag

import "errors"

// Reason identifies a rejected assertion or an unavailable validation input.
type Reason string

const (
	ReasonMalformed              Reason = "assertion_malformed"
	ReasonTypeMismatch           Reason = "assertion_type_mismatch"
	ReasonTrustUnavailable       Reason = "trusted_issuer_unavailable"
	ReasonIssuerMismatch         Reason = "assertion_issuer_mismatch"
	ReasonKeyUnknown             Reason = "assertion_key_unknown"
	ReasonKeyUnresolvable        Reason = "assertion_key_unresolvable"
	ReasonSignatureInvalid       Reason = "assertion_signature_invalid"
	ReasonClaimsMalformed        Reason = "assertion_claims_malformed"
	ReasonAudienceMismatch       Reason = "assertion_audience_mismatch"
	ReasonResourceMismatch       Reason = "assertion_resource_mismatch"
	ReasonClientMismatch         Reason = "assertion_client_mismatch"
	ReasonSubjectMissing         Reason = "assertion_subject_missing"
	ReasonEmailMissing           Reason = "assertion_email_missing"
	ReasonExpiryMissing          Reason = "assertion_expiry_missing"
	ReasonExpired                Reason = "assertion_expired"
	ReasonNotYetValid            Reason = "assertion_not_yet_valid"
	ReasonLifetimeTooLong        Reason = "assertion_lifetime_too_long"
	ReasonIDMissing              Reason = "assertion_id_missing"
	ReasonNotProvisioned         Reason = "subject_not_provisioned"
	ReasonSubjectUnavailable     Reason = "subject_resolution_unavailable"
	ReasonReplayed               Reason = "assertion_replayed"
	ReasonReplayStoreUnavailable Reason = "assertion_replay_store_unavailable"
	ReasonVerifierMisconfigured  Reason = "assertion_verifier_misconfigured"
)

// ReasonOf returns the rejection reason, or an empty value for another error.
func ReasonOf(err error) Reason {
	if typed, ok := errors.AsType[*Error](err); ok {
		return typed.Reason
	}
	return ""
}
