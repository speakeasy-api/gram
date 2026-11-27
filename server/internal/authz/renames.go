package authz

import "maps"

// scopeRename is where a renamed stored scope now points. scopeRenames, in
// tables_gen.go, is generated from registry.Registry.Renamed.
type scopeRename struct {
	To Scope
	// WildcardResource rewrites the stored resource id to the wildcard, for a
	// scope that moved from the organization onto a type inside a project.
	WildcardResource bool
}

// CanonicalScope returns the declared scope a stored scope name means: its
// new name when it was renamed, and scope itself otherwise.
func CanonicalScope(scope Scope) Scope {
	if rename, ok := scopeRenames[scope]; ok {
		return rename.To
	}
	return scope
}

// CanonicalGrant reads a grant stored under a renamed scope as a grant of the
// scope it was renamed to. Every reader of stored grant rows calls it before
// validating or evaluating a grant, so a rename ships before the data
// migration that rewrites the rows. A grant whose scope was not renamed is
// returned unchanged.
func CanonicalGrant(grant Grant) Grant {
	return canonicalGrant(scopeRenames, grant)
}

func canonicalGrant(renames map[Scope]scopeRename, grant Grant) Grant {
	rename, ok := renames[grant.Scope]
	if !ok {
		return grant
	}
	grant.Scope = rename.To
	grant.Selector = canonicalSelector(rename, grant.Selector)
	return grant
}

// canonicalSelector moves a stored selector onto the renamed scope's
// resource kind. Every other key is kept, so a project_id or tool limit
// still narrows the grant. The input is never modified.
func canonicalSelector(rename scopeRename, selector Selector) Selector {
	if selector == nil {
		return nil
	}
	out := maps.Clone(selector)
	if _, ok := out[SelectorKeyResourceKind]; ok {
		out[SelectorKeyResourceKind] = ResourceKindForScope(rename.To)
	}
	if rename.WildcardResource {
		out[SelectorKeyResourceID] = WildcardResource
	}
	return out
}
