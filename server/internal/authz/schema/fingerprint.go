package schema

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
)

// Fingerprint identifies the compiled model: two registries with the same
// types, guards, scopes, permissions, alternatives and runtime projection
// share it, and any change to them changes it. Shadow comparisons record it
// so a disagreement can be traced to the exact model that produced it.
func (r *Registry) Fingerprint() string {
	h := sha256.New()
	write := func(v any) { _, _ = fmt.Fprintf(h, "%#v\n", v) }
	write(r.types)
	typeNames := make([]string, 0, len(r.guards))
	for name := range r.guards {
		typeNames = append(typeNames, name)
	}
	slices.Sort(typeNames)
	for _, name := range typeNames {
		write(name)
		write(r.guards[name])
	}
	write(r.scopes)
	write(r.permissions)
	refs := make([]PermissionRef, 0, len(r.alternatives))
	for ref := range r.alternatives {
		refs = append(refs, ref)
	}
	slices.SortFunc(refs, func(a, b PermissionRef) int {
		if a.Type != b.Type {
			return cmp.Compare(a.Type, b.Type)
		}
		return cmp.Compare(a.Permission, b.Permission)
	})
	for _, ref := range refs {
		write(r.alternatives[ref])
	}
	write(r.runtime)
	return hex.EncodeToString(h.Sum(nil))[:16]
}
