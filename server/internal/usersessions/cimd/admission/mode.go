package admission

// Mode is a user_session_issuer's CIMD admission policy.
type Mode string

const (
	// ModeDisabled admits no CIMD client at all. The issuer additionally
	// omits client_id_metadata_document_supported from its RFC 8414
	// metadata: advertising support while admitting nothing would route
	// spec-compliant clients into a guaranteed-failure flow instead of
	// letting them degrade to dynamic client registration.
	ModeDisabled Mode = "disabled"

	// ModePresets admits the enabled catalog entries plus the issuer's own
	// custom URLs. This is the default for an issuer whose mode was never
	// explicitly set.
	ModePresets Mode = "presets"

	// ModeReporting evaluates exactly what ModePresets would decide, records
	// it, and then admits regardless. It is the migration step onto
	// ModePresets: an operator turns it on, waits, and reads
	// cimd.admission.decisions to learn which client_ids WOULD have been
	// refused, adding catalog entries or custom URLs until that number is
	// zero. Only then is switching to ModePresets safe.
	//
	// It exists because a presets denial is unrecoverable for the end user
	// (MCP clients do not fall back to dynamic registration after an
	// authorize rejection), so the cost of discovering a missing entry the
	// hard way is a support ticket. This mode makes that discovery free.
	//
	// Because it admits everything, it is exactly as permissive as ModeOpen
	// while it is on. It is a temporary measurement state, not a resting
	// place.
	ModeReporting Mode = "reporting"

	// ModeOpen skips admission entirely. Every document validation rule
	// still applies — open means "any spec-valid client", not "any client".
	ModeOpen Mode = "open"
)

// Enforces reports whether a denial in this mode actually refuses the
// client. False only for ModeReporting, where the decision is recorded and
// then discarded.
//
// Callers MUST consult this before acting on OutcomeDeny. Evaluate returns
// the same decision for ModeReporting as for ModePresets on purpose: the
// whole point is that the recorded outcome is directly comparable to what
// ModePresets will produce after the switch, so the same query answers
// "what would happen" before and "what is happening" after.
func (m Mode) Enforces() bool {
	return m != ModeReporting
}

// ResolveMode maps a stored user_session_issuers.client_id_metadata_admission_mode
// value onto the effective policy. This is the ONLY place either resolution
// rule lives, so the default can move without a migration or a backfill.
//
// The column is nullable with no database default precisely so the two
// states stay distinguishable: an operator who explicitly chose a mode, and
// an issuer that never had one.
//
// THE NULL BRANCH IS THE ROLLOUT LEVER, and it currently resolves to
// ModeReporting, not ModePresets. Because no issuer has the default written
// to its row, changing the line below changes the behaviour of every
// unconfigured issuer with no migration and no backfill.
//
// Shipping ModeReporting first is deliberate. An unconfigured issuer today
// behaves exactly as it did before admission control existed (it admits
// every spec-valid document), so nothing regresses, while every decision
// ModePresets would have made is recorded. The exit condition is:
//
//  1. Wait for the AIS-212 flag flip, which is what first exposes CIMD to
//     the whole customer base and therefore produces the only meaningful
//     sample.
//  2. Watch cimd.admission.decisions for outcome denied_not_listed with
//     mode reporting. Each one names a client the catalog is missing; add a
//     catalog entry, or have the operator add a custom URL.
//  3. Change this branch to ModePresets once that count is zero and stable.
//     Enforcement then begins with evidence rather than a guess.
//
// The default moves in one direction, once. Operators who want enforcement
// before then set ModePresets explicitly through the management API; only
// the default is deferred, not the capability.
//
// stored/valid mirror a nullable text column (pgtype.Text's String/Valid)
// without dragging a pgx dependency into a policy package. The returned
// bool reports whether the stored value was RECOGNIZED, not whether it was
// present: false means the column held something outside the enum, which
// resolves to ModeDisabled — fail closed — and the caller must log it.
//
// Only a genuine NULL takes the default. A non-NULL empty string is NOT
// treated as unset: nothing writes one (IsValidMode rejects it), so it can
// only arrive from a direct database write, which makes it a data error
// rather than an absent choice. It fails closed like any other
// unrecognized value.
func ResolveMode(stored string, valid bool) (Mode, bool) {
	if !valid {
		return ModeReporting, true
	}
	switch mode := Mode(stored); mode {
	case ModeDisabled, ModePresets, ModeReporting, ModeOpen:
		return mode, true
	default:
		return ModeDisabled, false
	}
}

// IsValidMode reports whether a caller-supplied string is a WRITABLE mode.
// Used by the management API to validate input in app code — the column
// carries no CHECK constraint, by convention.
//
// ModeReporting is deliberately excluded. It is as permissive as ModeOpen
// for as long as it is on, so exposing it as a per-issuer choice would put
// a reassuringly-named permissive state in front of operators, with nothing
// to stop it being left there. It is a deployment-time default (see
// ResolveMode), not a setting. ResolveMode still recognizes it, so a value
// written by that lever resolves correctly.
func IsValidMode(value string) bool {
	switch Mode(value) {
	case ModeDisabled, ModePresets, ModeOpen:
		return true
	case ModeReporting:
		return false
	default:
		return false
	}
}

// Modes returns every writable mode, for the management API's enum
// documentation and validation error messages. ModeReporting is absent for
// the reason given on IsValidMode.
func Modes() []Mode {
	return []Mode{ModeDisabled, ModePresets, ModeOpen}
}
