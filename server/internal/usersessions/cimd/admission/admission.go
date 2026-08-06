package admission

// MaxClientIDLength caps the URL-shaped client_id that admission will
// consider. It exists here, ahead of the resolver's own identical cap, for
// one reason: in ModePresets a catalog miss triggers a database lookup
// keyed by the presented client_id, on an unauthenticated endpoint. Without
// a bound applied first, an attacker could drive an indexed query with a
// request-sized parameter. Denying oversized input outright is both cheaper
// and stricter than the resolver's rejection, which only runs after
// admission has already passed.
//
// The value matches the resolver's own cap (the user_session_clients btree
// entry limit is what ultimately sets it); cimd derives its constant from
// this one so the two cannot drift.
const MaxClientIDLength = 2048

// Evaluate makes the in-memory admission decision for a presented
// client_id. Callers must have already established that clientID is
// URL-shaped; DCR-issued identifiers never reach admission.
//
// It never returns AdmitCustom: that outcome depends on the issuer's own
// URL rows, which is a database lookup only the caller can perform, and is
// what OutcomeCheckCustom asks for.
//
// ModeReporting returns the identical decision to ModePresets. That is the
// entire point: the recorded outcome must be comparable to what ModePresets
// will produce after the switch. The caller consults Mode.Enforces to learn
// that a denial should be recorded and then discarded.
func Evaluate(mode Mode, clientID string) Decision {
	switch mode {
	case ModeOpen:
		return admitDecision(AdmitOpen)
	case ModeDisabled:
		return denyDecision(DenialDisabled)
	case ModePresets, ModeReporting:
		if reason, ok := CatalogMatch(clientID); ok {
			return admitDecision(reason)
		}
		// Bound the input before it can become a query parameter. The
		// catalog check runs first because every enabled entry is well
		// under the cap, so a legitimate client never pays for this.
		if len(clientID) > MaxClientIDLength {
			return denyDecision(DenialOversized)
		}
		return checkCustomDecision()
	default:
		// Unreachable when the mode came from ResolveMode, which already
		// folds unrecognized values into ModeDisabled. Kept as a fail-closed
		// backstop for a mode built any other way.
		return denyDecision(DenialUnknownMode)
	}
}
