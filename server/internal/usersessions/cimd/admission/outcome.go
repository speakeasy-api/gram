package admission

// Outcome is the in-memory admission decision. OutcomeCheckCustom is not a
// denial — it means the catalog did not match and the caller must consult
// the issuer's user_session_issuer_cimd_clients rows before deciding.
type Outcome string

const (
	// OutcomeAdmit permits the client_id with no further checks.
	OutcomeAdmit Outcome = "admitted"

	// OutcomeCheckCustom means the catalog did not match. The caller must
	// query the issuer's custom URLs; a miss there is a final denial with
	// reason DenialNotListed.
	OutcomeCheckCustom Outcome = "check_custom"

	// OutcomeDeny is a final denial. Pair it with a DenialReason.
	OutcomeDeny Outcome = "denied"
)
