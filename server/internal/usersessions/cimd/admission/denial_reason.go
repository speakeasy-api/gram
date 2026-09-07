package admission

// DenialReason is the low-cardinality label recorded on the
// cimd.admission.decisions metric and the denial log line. It never carries
// the presented client_id — that URL is attacker-chosen and unbounded, so
// it belongs in a log attribute, never a metric dimension.
type DenialReason string

const (
	// DenialDisabled: the issuer admits no CIMD clients.
	DenialDisabled DenialReason = "denied_disabled"

	// DenialNotListed: presets mode, and the client_id is neither an
	// enabled catalog entry nor one of the issuer's custom URLs. This is
	// what a missing catalog entry looks like on an issuer that enforces.
	// Alerting wants AdmitOpenNotListed too: it carries the same gap from
	// the issuers that admit rather than refuse, which is most of them.
	DenialNotListed DenialReason = "denied_not_listed"

	// DenialOversized: presets mode, and the client_id exceeds
	// MaxClientIDLength. Denied before the custom-URL lookup rather than
	// after, so an oversized value never reaches the database.
	DenialOversized DenialReason = "denied_oversized"

	// DenialUnknownMode: the stored mode is not a recognized value. A data
	// error, not an implicit allow — fail closed and alert.
	DenialUnknownMode DenialReason = "denied_unknown_mode"
)
