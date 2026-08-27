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
	// custom URLs, and refuses everything else. It is opt-in: an operator
	// chooses it deliberately, because a denial under it is unrecoverable
	// for the end user.
	ModePresets Mode = "presets"

	// ModeReporting evaluates exactly what ModePresets would decide,
	// records it, and then admits regardless. It is no longer reachable:
	// nothing writes it, and the unconfigured default is ModeOpen, which
	// carries the same measurement. ResolveMode still recognizes it so a
	// row written while it was the default resolves rather than failing
	// closed.
	ModeReporting Mode = "reporting"

	// ModeOpen admits every spec-valid client. Document validation rules
	// all still apply — open means "any spec-valid client", not "any
	// client" — and the decision ModePresets would have made is computed
	// and recorded alongside the admission, so a catalog gap stays visible
	// on an issuer that refuses nobody.
	ModeOpen Mode = "open"
)

// Enforces reports whether a denial in this mode actually refuses the
// client. False only for ModeReporting, where the decision is recorded and
// then discarded.
//
// Callers MUST consult this before acting on OutcomeDeny.
//
// ModeOpen enforces, and that is not a contradiction to relax: Evaluate
// never denies in open mode, so the only OutcomeDeny a caller can hold
// under it is one that caller built itself, deliberately, as a real
// refusal. The shadow measurement never produces one — EvaluateShadow's
// result is translated into an AdmitReason and recorded, never returned as
// a decision.
func (m Mode) Enforces() bool {
	return m != ModeReporting
}

// ResolveMode maps a stored user_session_issuers.client_id_metadata_admission_mode
// value onto the effective policy. This is the ONLY place the resolution
// rules live.
//
// NULL resolves to ModeOpen. Open is the resting state rather than a
// waypoint toward enforcement: MCP clients pick CIMD over dynamic client
// registration once, at metadata-discovery time, and do not fall back when
// /authorize refuses the client_id, so a presets denial is a dead end with
// no client-side recovery. Applying a curated allowlist to an issuer whose
// operator never chose one would impose that dead end on customers who
// never asked for it, so ModePresets is opt-in.
//
// Nothing is given up by defaulting open. The measurement that would gate
// an enforcement decision rides along with it: an open-mode admission also
// computes what ModePresets would have decided (EvaluateShadow) and records
// that, so catalog gaps stay discoverable from cimd.admission.decisions
// without anyone being refused to find them.
//
// New rows are written 'open' explicitly, so this branch covers only rows
// created before that and any row left unset by a direct database write.
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
		return ModeOpen, true
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
// while it is on, so exposing it as a per-issuer choice would put a
// reassuringly-named permissive state in front of operators with nothing to
// stop it being left there. ModeOpen is the honest name for that behaviour
// and is writable. ResolveMode still recognizes ModeReporting so rows
// written while it was the default keep resolving.
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
