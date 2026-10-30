package xrbac

import (
	"maps"
	"slices"
	"time"
)

// Everything in this file is pure: no database access. Backends load facts
// and policies; this decides.

type tri uint8

const (
	no tri = iota
	unknown
	yes
)

// facts are the check-side values a selector is compared against.
type facts map[string][]string

func requestFacts(req Request, subjectPrincipals []string) facts {
	f := facts{"resource_kind": {req.Target.Resource.Kind}, "resource_id": {req.Target.Resource.ID}}
	if p, ok := req.Target.ancestor("project"); ok {
		f["project_id"] = []string{p.ID}
	}
	if req.Kind == ToolCall && req.Tool != nil {
		f["tool"] = []string{req.Tool.Name}
		f["disposition"] = []string{req.Tool.Classification}
	}
	if subjectPrincipals != nil {
		f["subject_principal"] = subjectPrincipals
	}
	return f
}

// match is strict: a selector key the check cannot answer is unknown, never
// skipped. Unknown classification never proves a disposition.
func match(sel map[string]string, f facts) tri {
	out := yes
	for k, v := range sel {
		vals, ok := f[k]
		if !ok {
			out = unknown
			continue
		}
		if v == "*" || slices.Contains(vals, v) {
			continue
		}
		if k == "disposition" && slices.Contains(vals, ClassificationUnknown) {
			out = unknown
			continue
		}
		return no
	}
	return out
}

func allowScopes(s Scope) []Scope {
	d := byScope[s]
	return append([]Scope{s}, d.ImpliedBy...)
}

// evalLimit applies one required policy: a matching denial wins over every
// allow, including direct grants and relationship rules.
func evalLimit(l PolicyLimit, rels []Rule, req Request, f facts, now time.Time) Decision {
	rules := append(slices.Clone(l.Rules), rels...)
	scopes := allowScopes(req.Scope)
	denyUnknown := false
	for _, r := range rules {
		if r.Effect != EffectDeny || r.Scope != req.Scope || !r.validAt(now) {
			continue
		}
		switch match(r.Selector, f) {
		case yes:
			return Decision{Outcome: Denied, Reason: ReasonDenied, Limit: l.Kind, Source: r.Source}
		case unknown:
			denyUnknown = true
		}
	}
	allowSource, allowUnknown := "", false
	for _, r := range rules {
		if r.Effect != EffectAllow || !slices.Contains(scopes, r.Scope) || !r.validAt(now) {
			continue
		}
		switch match(r.Selector, f) {
		case yes:
			allowSource = r.Source
		case unknown:
			allowUnknown = true
		}
		if allowSource != "" {
			break
		}
	}
	switch {
	case denyUnknown:
		return Decision{Outcome: Unresolved, Reason: ReasonFactsMissing, Limit: l.Kind}
	case allowSource != "":
		return Decision{Outcome: Allowed, Reason: ReasonAllowed, Source: allowSource}
	case allowUnknown:
		return Decision{Outcome: Unresolved, Reason: ReasonFactsMissing, Limit: l.Kind}
	default:
		return Decision{Outcome: Denied, Reason: ReasonNoGrant, Limit: l.Kind}
	}
}

// evaluate requires every limit to allow.
func evaluate(a Authority, req Request, f facts, now time.Time) Decision {
	var last Decision
	for _, l := range a.Limits {
		d := evalLimit(l, a.Relationships[l.Kind], req, f, now)
		if d.Outcome != Allowed {
			return d
		}
		last = d
	}
	if len(a.Limits) == 0 {
		return Decision{Outcome: Denied, Reason: ReasonNoGrant}
	}
	return last
}

// toolSet is what one limit permits on one server, before a tool is chosen.
type toolSet struct {
	all    bool
	in     map[string]bool
	except map[string]bool
}

func (t toolSet) empty() bool { return !t.all && len(t.in) == 0 }

func intersect(a, b toolSet) toolSet {
	out := toolSet{in: map[string]bool{}, except: map[string]bool{}}
	maps.Copy(out.except, a.except)
	maps.Copy(out.except, b.except)
	switch {
	case a.all && b.all:
		out.all = true
	case a.all:
		for t := range b.in {
			out.in[t] = true
		}
	case b.all:
		for t := range a.in {
			out.in[t] = true
		}
	default:
		for t := range a.in {
			if b.in[t] {
				out.in[t] = true
			}
		}
	}
	for t := range out.except {
		delete(out.in, t)
	}
	return out
}

// discover proves some tool call passes every limit — not that each matches
// the server independently.
func discover(a Authority, q AccessQuery, now time.Time) Decision {
	f := requestFacts(Request{Scope: q.Scope, Target: q.Target}, nil)
	var common *toolSet
	for _, l := range a.Limits {
		ts := toolSet{in: map[string]bool{}, except: map[string]bool{}}
		for _, r := range append(slices.Clone(l.Rules), a.Relationships[l.Kind]...) {
			if !r.validAt(now) {
				continue
			}
			base := maps.Clone(r.Selector)
			tool, hasTool := base["tool"]
			_, hasDisp := base["disposition"]
			delete(base, "tool")
			delete(base, "disposition")
			if match(base, f) != yes {
				continue
			}
			switch {
			case r.Effect == EffectDeny && r.Scope == q.Scope && !hasTool && !hasDisp:
				return Decision{Outcome: Denied, Reason: ReasonDenied, Limit: l.Kind, Source: r.Source}
			case r.Effect == EffectDeny && r.Scope == q.Scope && hasTool && !hasDisp:
				ts.except[tool] = true
			case r.Effect == EffectAllow && slices.Contains(allowScopes(q.Scope), r.Scope):
				if hasTool {
					ts.in[tool] = true
				} else {
					ts.all = true
				}
			}
		}
		if common == nil {
			common = &ts
		} else {
			c := intersect(*common, ts)
			common = &c
		}
		if common.empty() {
			return Decision{Outcome: Denied, Reason: ReasonLimitFailed, Limit: l.Kind}
		}
	}
	if common == nil {
		return Decision{Outcome: Denied, Reason: ReasonNoGrant}
	}
	return Decision{Outcome: Allowed, Reason: ReasonAllowed}
}

func covers(parent, child map[string]string) bool {
	for k, v := range parent {
		if v != "*" && child[k] != v {
			return false
		}
	}
	return true
}

func overlaps(a, b map[string]string) bool {
	for k, v := range a {
		if w, ok := b[k]; ok && v != "*" && w != "*" && v != w {
			return false
		}
	}
	return true
}

// contain checks the whole proposed policy against every parent: each allow
// must be covered (resource, constraints, validity), and no overlapping
// parent denial may be dropped.
func contain(parents []PolicyLimit, rels map[LimitKind][]Rule, proposed []Rule, now time.Time) Decision {
	for _, p := range proposed {
		if p.Effect == EffectDeny {
			continue // restrictions only reduce authority
		}
		if err := ValidateAgentRuntimeScope(CurrentAgentRuntimeScopeVersion, p.Scope); err != nil {
			return Decision{Outcome: Denied, Reason: ReasonNotDelegable, Source: string(p.Scope)}
		}
		for _, parent := range parents {
			rules := append(slices.Clone(parent.Rules), rels[parent.Kind]...)
			covered := false
			for _, a := range rules {
				if a.Effect != EffectAllow || !slices.Contains(allowScopes(p.Scope), a.Scope) || !a.validAt(now) {
					continue
				}
				if !covers(a.Selector, p.Selector) {
					continue
				}
				if a.ValidUntil != nil && (p.ValidUntil == nil || p.ValidUntil.After(*a.ValidUntil)) {
					continue
				}
				covered = true
				break
			}
			if !covered {
				return Decision{Outcome: Denied, Reason: ReasonExceedsParent, Limit: parent.Kind}
			}
			for _, d := range rules {
				if d.Effect != EffectDeny || d.Scope != p.Scope || !d.validAt(now) || !overlaps(d.Selector, p.Selector) {
					continue
				}
				carried := slices.ContainsFunc(proposed, func(q Rule) bool {
					return q.Effect == EffectDeny && q.Scope == d.Scope && maps.Equal(q.Selector, d.Selector)
				})
				if !carried {
					return Decision{Outcome: Denied, Reason: ReasonDenied, Limit: parent.Kind, Source: "proposal drops an overlapping denial"}
				}
			}
		}
	}
	return Decision{Outcome: Allowed, Reason: ReasonAllowed}
}
