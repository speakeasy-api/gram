package zedspike

import (
	"fmt"
	"slices"
	"strings"
)

// checkTables reports every place where rbac.zed and the Go tables disagree.
// These are the checks SpiceDB cannot make, because they are about how our
// rows map onto its relations.
func checkTables(reg *Registry) []error {
	var errs []error
	usedScopes := map[Scope]bool{}
	usedEdges := map[RelationKey]bool{}
	usedFacts := map[RelationKey]bool{}
	selectorKeys := map[string]bool{}
	for _, info := range ScopeCatalogue {
		for _, key := range info.Selectors {
			selectorKeys[key] = true
		}
	}

	for _, def := range reg.Types {
		for _, rel := range def.Relations {
			key := RelationKey{def.Name, rel.Name}
			switch rel.Kind {
			case RelationEdge:
				if rel.Name == "organization" {
					continue
				}
				if _, ok := Edges[key]; !ok {
					errs = append(errs, fmt.Errorf("%s#%s is an edge with no Edges entry", def.Name, rel.Name))
				}
				usedEdges[key] = true
			case RelationFact:
				usedFacts[key] = true
			case RelationGrant, RelationBlock:
				usedScopes[rel.Scope] = true
			case RelationDirect, RelationOwnBlock:
				for _, s := range rel.Subjects {
					if s.Type != "user" || s.Wildcard || s.Relation != "" {
						errs = append(errs, fmt.Errorf("%s#%s: only a named user can hold a _direct or _own row", def.Name, rel.Name))
					}
				}
				// Land writes a _direct row next to its grant and an _own row in
				// place of its block, so each needs that base relation here.
				base, want := strings.TrimSuffix(rel.Name, "_direct"), RelationGrant
				if rel.Kind == RelationOwnBlock {
					base, want = strings.TrimSuffix(rel.Name, "_own"), RelationBlock
				}
				if b := def.Relations[base]; b == nil || b.Kind != want {
					errs = append(errs, fmt.Errorf("%s#%s has no %s relation beside it", def.Name, rel.Name, base))
				}
			case RelationMembership:
			}
		}

		for _, perm := range def.Permissions {
			if perm.Internal {
				continue
			}
			key := PermissionKey{def.Name, perm.Name}
			scopes, composite := Composites[key]
			if scope, answers := Answers[key]; answers {
				scopes = []Scope{scope}
			} else if !composite {
				errs = append(errs, fmt.Errorf("%s#%s is public but neither in Answers nor in Composites", def.Name, perm.Name))
				continue
			}
			for _, scope := range scopes {
				errs = append(errs, checkBlocksSubtracted(reg, def, perm, scope)...)
			}
		}
	}

	for scope := range ScopeCatalogue {
		if !usedScopes[scope] {
			errs = append(errs, fmt.Errorf("scope %s has no relation in rbac.zed", scope))
		}
	}
	for key := range Edges {
		if !usedEdges[key] {
			errs = append(errs, fmt.Errorf("Edges entry %s#%s matches no edge relation", key.Type, key.Relation))
		}
	}
	for key := range Facts {
		if !usedFacts[key] {
			errs = append(errs, fmt.Errorf("Facts entry %s#%s matches no relation", key.Type, key.Relation))
		}
	}
	for key, scope := range Answers {
		if def := reg.Types[key.Type]; def == nil || def.Permissions[key.Permission] == nil {
			errs = append(errs, fmt.Errorf("Answers names missing permission %s#%s", key.Type, key.Permission))
		}
		if _, ok := ScopeCatalogue[scope]; !ok {
			errs = append(errs, fmt.Errorf("Answers maps %s#%s to unknown scope %s", key.Type, key.Permission, scope))
		}
	}
	errs = append(errs, checkSelectorsConsumed(reg)...)
	for _, caveat := range reg.Caveats {
		for _, param := range caveat.GrantedParams {
			if !selectorKeys[strings.TrimPrefix(param, "granted_")] {
				errs = append(errs, fmt.Errorf("caveat %s: %s names no selector key in ScopeCatalogue", caveat.Name, param))
			}
		}
	}
	return errs
}

// checkBlocksSubtracted fails a permission answering x:verb when x:blocked_verb
// exists and the permission never reaches it: writing "read = mcp_read"
// without the block is a compile error, not a silent widening.
func checkBlocksSubtracted(reg *Registry, def *TypeDef, perm *PermissionDef, scope Scope) []error {
	parts := strings.SplitN(string(scope), ":", 2)
	if len(parts) != 2 {
		return nil
	}
	blockScope := Scope(parts[0] + ":blocked_" + parts[1])
	if _, ok := ScopeCatalogue[blockScope]; !ok {
		return nil
	}
	blockRelation := strings.Replace(string(blockScope), ":", "_", 1)
	reached := map[string]bool{}
	collectSubtracted(reg, def.Name, perm.Expr, false, map[string]bool{}, reached)
	var errs []error
	for _, name := range []string{blockRelation, blockRelation + "_own"} {
		if !reached[name] {
			errs = append(errs, fmt.Errorf("%s#%s answers %s but never subtracts %s", def.Name, perm.Name, scope, name))
		}
	}
	return errs
}

// collectSubtracted records every relation reached on the subtracted side of
// an exclusion, following same-type references and arrows.
func collectSubtracted(reg *Registry, typeName string, expr Expr, subtracted bool, seen, out map[string]bool) {
	def := reg.Types[typeName]
	switch {
	case expr.Ref != "":
		key := fmt.Sprintf("%s#%s/%t", typeName, expr.Ref, subtracted)
		if seen[key] {
			return
		}
		seen[key] = true
		if subtracted {
			out[expr.Ref] = true
		}
		if perm := def.Permissions[expr.Ref]; perm != nil {
			collectSubtracted(reg, typeName, perm.Expr, subtracted, seen, out)
		}
	case expr.Arrow != nil:
		for _, s := range def.Relations[expr.Arrow.Edge].Subjects {
			collectSubtracted(reg, s.Type, Expr{Ref: expr.Arrow.Target}, subtracted, seen, out)
		}
	case expr.Exclude != nil:
		collectSubtracted(reg, typeName, expr.Exclude.Base, subtracted, seen, out)
		collectSubtracted(reg, typeName, expr.Exclude.Subtracted, true, seen, out)
	default:
		for _, child := range append(slices.Clone(expr.Union), expr.Intersect...) {
			collectSubtracted(reg, typeName, child, subtracted, seen, out)
		}
	}
}

// checkSelectorsConsumed fails when a selector key a scope's rows may carry is
// neither a project edge nor a granted_ parameter of the caveat on every
// subject of every relation holding that scope. A key nothing reads would be
// dropped when the row lands, silently widening the grant.
func checkSelectorsConsumed(reg *Registry) []error {
	var errs []error
	for _, def := range reg.Types {
		for _, rel := range def.Relations {
			if rel.Scope == "" {
				continue
			}
			resourceType := resourceTypeForScope(rel.Scope)
			for _, key := range ScopeCatalogue[rel.Scope].Selectors {
				if key == SelectorProjectID && landsOnProject(resourceType) {
					continue
				}
				for _, subject := range rel.Subjects {
					// a key policy carries only resource and project keys,
					// enforced when it is written
					if subject.Type == "api_key" {
						continue
					}
					caveat := reg.Caveats[subject.Caveat]
					if caveat == nil || !slices.Contains(caveat.GrantedParams, "granted_"+key) {
						errs = append(errs, fmt.Errorf("%s#%s: selector key %s is not read for subject %s", def.Name, rel.Name, key, subject.Type))
						break
					}
				}
			}
		}
	}
	return errs
}
