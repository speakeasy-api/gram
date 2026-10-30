package xrbac

import "time"

// StoredGrant is a principal_grants row as it exists today, plus the planned
// additive validity columns (D3: start-inclusive, end-exclusive).
type StoredGrant struct {
	ID         string
	Principal  string // principal_urn
	Scope      Scope
	Selectors  map[string]string
	ValidFrom  *time.Time
	ValidUntil *time.Time
}

// Decode normalizes a stored grant into an internal rule. Legacy blocked
// scopes become explicit access denials of the scope they exclude, through
// catalogue metadata; risk exemptions would never decode to a denial.
func Decode(g StoredGrant) Rule {
	r := Rule{ID: g.ID, Scope: g.Scope, Effect: EffectAllow, Selector: g.Selectors, ValidFrom: g.ValidFrom, ValidUntil: g.ValidUntil, Source: g.Principal}
	if denied, ok := denies[g.Scope]; ok {
		r.Scope, r.Effect = denied, EffectDeny
	}
	return r
}

func DecodeAll(gs []StoredGrant) []Rule {
	out := make([]Rule, len(gs))
	for i, g := range gs {
		out[i] = Decode(g)
	}
	return out
}
