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

	// ModeOpen skips admission entirely. Every document validation rule
	// still applies — open means "any spec-valid client", not "any client".
	ModeOpen Mode = "open"
)

// ResolveMode maps a stored user_session_issuers.client_id_metadata_admission_mode
// value onto the effective policy. This is the ONLY place either resolution
// rule lives, so the default can move without a migration or a backfill.
//
// The column is nullable with no database default precisely so the two
// states stay distinguishable: an operator who explicitly chose a mode, and
// an issuer that never had one. Both currently resolve to ModePresets and
// are treated identically everywhere downstream.
//
// stored/valid mirror a nullable text column (pgtype.Text's String/Valid)
// without dragging a pgx dependency into a policy package. The returned
// bool reports whether the stored value was RECOGNIZED, not whether it was
// present: false means the column held something outside the enum, which
// resolves to ModeDisabled — fail closed — and the caller must log it.
func ResolveMode(stored string, valid bool) (Mode, bool) {
	if !valid || stored == "" {
		return ModePresets, true
	}
	switch mode := Mode(stored); mode {
	case ModeDisabled, ModePresets, ModeOpen:
		return mode, true
	default:
		return ModeDisabled, false
	}
}

// IsValidMode reports whether a caller-supplied string is a writable mode.
// Used by the management API to validate input in app code — the column
// carries no CHECK constraint, by convention.
func IsValidMode(value string) bool {
	switch Mode(value) {
	case ModeDisabled, ModePresets, ModeOpen:
		return true
	default:
		return false
	}
}

// Modes returns every writable mode, for the management API's enum
// documentation and validation error messages.
func Modes() []Mode {
	return []Mode{ModeDisabled, ModePresets, ModeOpen}
}
