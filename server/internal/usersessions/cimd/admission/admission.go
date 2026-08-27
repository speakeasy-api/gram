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
// ModeReporting returns the identical decision to ModePresets, so the
// outcome it records is directly comparable to what ModePresets produces.
// The caller consults Mode.Enforces to learn that a denial should be
// recorded and then discarded.
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

// EvaluateShadow computes the decision ModePresets WOULD make for a
// presented client_id, for a caller that is admitting it regardless. It is
// how an open-mode issuer keeps producing the catalog-gap signal that
// enforcement would otherwise be needed to discover: the client is let
// through, and what a curated allowlist would have said about it is
// recorded anyway.
//
// It is deliberately NOT reachable through Evaluate, and that separation is
// load-bearing rather than stylistic. Evaluate is the enforcement path: what
// it returns decides whether a client is refused. This function's result
// decides nothing, so the two must not share a return value that a caller
// could act on by mistake.
//
// The result is telemetry. Callers translate it into an AdmitReason — a
// metric label — and must never surface it as a refusal. OutcomeCheckCustom
// still asks for the custom-URL lookup, and an OutcomeDeny here means only
// that no shadow verdict is available, never that the client should be
// turned away.
func EvaluateShadow(clientID string) Decision {
	return Evaluate(ModePresets, clientID)
}
