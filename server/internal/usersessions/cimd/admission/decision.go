package admission

// Decision is the result of evaluating an issuer's policy against a
// presented client_id. Exactly one of Admit and Denial carries a value, and
// which one is determined by Outcome:
//
//   - OutcomeAdmit: Admit is set, Denial is empty
//   - OutcomeDeny: Denial is set, Admit is empty
//   - OutcomeCheckCustom: neither is set — the decision is not final yet,
//     and the caller must consult the issuer's custom URLs
type Decision struct {
	Outcome Outcome
	Admit   AdmitReason
	Denial  DenialReason
}

func admitDecision(reason AdmitReason) Decision {
	return Decision{Outcome: OutcomeAdmit, Admit: reason, Denial: ""}
}

func denyDecision(reason DenialReason) Decision {
	return Decision{Outcome: OutcomeDeny, Admit: "", Denial: reason}
}

// checkCustomDecision defers the decision to the caller's database lookup.
// It carries no reason: only the caller can tell a custom-URL hit from a
// miss, and only the caller knows whether a miss refuses the client or
// merely records that nothing covered it.
func checkCustomDecision() Decision {
	return Decision{Outcome: OutcomeCheckCustom, Admit: "", Denial: ""}
}
