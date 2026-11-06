// Package hybrid is Option D: Option A's schema and engine reading Option B's
// tables. Mapping is the one new piece: a reversible translation between a
// principal_grants row and a relationship the engine understands.
package hybrid

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/authz"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/model"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/schema"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/xrbac"
)

// Stored resource_kind values ↔ schema types.
var kindToType = map[string]string{"org": "organization", "project": "project", "mcp": "mcp_server", "agent": "agent", "chat": "chat", "user": "user"}

var typeToKind = func() map[string]string {
	m := map[string]string{}
	for k, v := range kindToType {
		m[v] = k
	}
	return m
}()

type target struct {
	typ    *schema.Type
	grant  *schema.GrantDef // nil for a marker
	marker string
}

type Mapping struct {
	byScope map[string]target
	sc      *schema.Schema
}

// NewMapping derives the scope ↔ relation table from the schema's StoredAs
// declarations. Two relations claiming one scope is a startup error.
func NewMapping(sc *schema.Schema) (*Mapping, error) {
	m := &Mapping{byScope: map[string]target{}, sc: sc}
	for _, t := range sc.Types {
		for _, g := range t.Grants {
			if g.StoredScope == "" {
				continue
			}
			if prev, dup := m.byScope[g.StoredScope]; dup {
				return nil, fmt.Errorf("scope %q mapped twice (%s and %s.%s)", g.StoredScope, prev.typ.Name, t.Name, g.Name)
			}
			m.byScope[g.StoredScope] = target{typ: t, grant: g}
		}
		for name, scope := range t.MarkerScopes {
			m.byScope[scope] = target{typ: t, marker: name}
		}
	}
	return m, nil
}

func (m *Mapping) MarkerScopes() []string {
	var out []string
	for scope, t := range m.byScope {
		if t.marker != "" {
			out = append(out, scope)
		}
	}
	slices.Sort(out)
	return out
}

func parsePrincipal(urn string) (authz.Subject, error) {
	typ, id, ok := strings.Cut(urn, ":")
	if !ok || id == "" {
		return authz.Subject{}, fmt.Errorf("malformed principal %q", urn)
	}
	return authz.Subject{Type: typ, ID: id}, nil
}

// Decode turns a stored grant into a relationship. Any shape the schema
// cannot express fails the load (never silently dropped — D2).
func (m *Mapping) Decode(org string, g xrbac.StoredGrant) (authz.Relationship, error) {
	tg, ok := m.byScope[string(g.Scope)]
	if !ok {
		return authz.Relationship{}, fmt.Errorf("grant %s: scope %q has no schema mapping", g.ID, g.Scope)
	}
	subj, err := parsePrincipal(g.Principal)
	if err != nil {
		return authz.Relationship{}, err
	}
	sel := maps.Clone(g.Selectors)
	if kindToType[sel["resource_kind"]] != tg.typ.Name {
		return authz.Relationship{}, fmt.Errorf("grant %s: resource_kind %q does not match %s", g.ID, sel["resource_kind"], tg.typ.Name)
	}
	id := sel["resource_id"]
	delete(sel, "resource_kind")
	delete(sel, "resource_id")
	r := authz.Relationship{ID: rowUUID(g.ID), Subject: subj, ExpiresAt: g.ValidUntil, Source: g.Principal, SourceRef: g.ID}
	if g.ValidFrom != nil {
		return authz.Relationship{}, fmt.Errorf("grant %s: valid_from is not supported by this spike", g.ID)
	}

	if tg.marker != "" {
		if id == "*" || len(sel) > 0 {
			return authz.Relationship{}, fmt.Errorf("grant %s: a marker names exactly one %s", g.ID, tg.typ.Name)
		}
		r.Object, r.Relation = authz.ObjectID{Type: tg.typ.Name, ID: id}, tg.marker
		return r, nil
	}

	anchor := schema.Self
	switch {
	case id != "*":
		r.Object = authz.ObjectID{Type: tg.typ.Name, ID: id}
	case sel["project_id"] != "":
		anchor, r.Object = "project", authz.ObjectID{Type: "project", ID: sel["project_id"]}
	default:
		anchor, r.Object = "organization", authz.ObjectID{Type: "organization", ID: org}
	}
	delete(sel, "project_id")
	if !slices.Contains(tg.grant.Anchors, anchor) {
		return authz.Relationship{}, fmt.Errorf("grant %s: %s.%s cannot be anchored at %s", g.ID, tg.typ.Name, tg.grant.Name, anchor)
	}
	r.Relation = schema.StoredRelation(tg.typ.Name, tg.grant.Name, anchor)

	if subject, ok := sel["subject_principal"]; ok {
		if len(tg.grant.SubjectAttrs) == 0 {
			return authz.Relationship{}, fmt.Errorf("grant %s: %s.%s is not subject-scoped", g.ID, tg.typ.Name, tg.grant.Name)
		}
		r.SubjectScope = subject
		delete(sel, "subject_principal")
	} // no key on a subject-scoped grant means every subject, as today

	tool, hasTool := sel["tool"]
	disp, hasDisp := sel["disposition"]
	delete(sel, "tool")
	delete(sel, "disposition")
	if hasTool || hasDisp {
		if !slices.Contains(tg.grant.Conditions, model.CondMCPTools) {
			return authz.Relationship{}, fmt.Errorf("grant %s: %s.%s takes no tool constraints", g.ID, tg.typ.Name, tg.grant.Name)
		}
		p := model.MCPToolsParams{}
		if hasTool {
			p.Tools = []string{tool}
		}
		if hasDisp {
			p.Dispositions = []string{disp}
		}
		r.ConditionName = model.CondMCPTools
		r.ConditionParams, _ = json.Marshal(p)
	}
	if len(sel) > 0 {
		return authz.Relationship{}, fmt.Errorf("grant %s: unsupported selector keys %v", g.ID, slices.Sorted(maps.Keys(sel)))
	}
	return r, nil
}

// Encode is Decode's inverse, used by every write path (role editor, agent
// policy, temporary access) so stored rows keep today's shape.
func (m *Mapping) Encode(org string, r authz.Relationship) (xrbac.StoredGrant, error) {
	g := xrbac.StoredGrant{ID: r.SourceRef, Principal: r.Subject.Type + ":" + r.Subject.ID, ValidUntil: r.ExpiresAt, Selectors: map[string]string{}}
	if r.Subject.Relation != "" {
		return g, fmt.Errorf("%s: subject sets are stored as principals, not %q", r, r.Subject)
	}
	typeName, grantName, anchored := strings.Cut(r.Relation, ".")
	if !anchored {
		typeName, grantName = r.Object.Type, r.Relation
	}
	t, ok := m.sc.Types[typeName]
	if !ok {
		return g, fmt.Errorf("%s: unknown type %q", r, typeName)
	}
	if _, isMarker := t.Markers[grantName]; isMarker {
		g.Scope = xrbac.Scope(t.MarkerScopes[grantName])
		g.Selectors["resource_kind"], g.Selectors["resource_id"] = typeToKind[t.Name], r.Object.ID
		return g, nil
	}
	gd, ok := t.Grants[grantName]
	if !ok || gd.StoredScope == "" {
		return g, fmt.Errorf("%s: relation has no stored scope", r)
	}
	g.Scope = xrbac.Scope(gd.StoredScope)
	g.Selectors["resource_kind"] = typeToKind[t.Name]
	switch r.Object.Type {
	case t.Name:
		g.Selectors["resource_id"] = r.Object.ID
	case "project":
		g.Selectors["resource_id"], g.Selectors["project_id"] = "*", r.Object.ID
	case "organization":
		g.Selectors["resource_id"] = "*"
	default:
		return g, fmt.Errorf("%s: anchor %s has no stored shape", r, r.Object.Type)
	}
	if r.SubjectScope != "" {
		g.Selectors["subject_principal"] = r.SubjectScope
	}
	if r.ConditionName == model.CondMCPTools {
		var p model.MCPToolsParams
		if err := json.Unmarshal(r.ConditionParams, &p); err != nil {
			return g, err
		}
		if len(p.Tools) > 1 || len(p.Dispositions) > 1 {
			return g, fmt.Errorf("%s: a stored selector holds one tool and one disposition", r)
		}
		if len(p.Tools) == 1 {
			g.Selectors["tool"] = p.Tools[0]
		}
		if len(p.Dispositions) == 1 {
			g.Selectors["disposition"] = p.Dispositions[0]
		}
	}
	return g, nil
}

func rowUUID(id string) uuid.UUID { return uuid.NewSHA1(uuid.NameSpaceOID, []byte(id)) }
