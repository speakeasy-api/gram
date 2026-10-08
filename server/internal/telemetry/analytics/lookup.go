package analytics

import (
	"context"
	"fmt"
	"slices"
)

// LookupLoader fetches a lookup's map for one tenant: a raw value to the
// value it reads as.
type LookupLoader func(ctx context.Context, tenant Tenant) (map[string]string, error)

// Lookup is a per-tenant map a dimension reads through at query time: a
// value with an entry becomes its target, the rest stay as reported.
type Lookup struct {
	Name        string
	Description string
	// Load is nil on a declaration; WithLoaders binds one.
	Load LookupLoader
}

// LookupMaps is the loaded maps a query reads fields through, by lookup
// name. A missing or empty map leaves the field as reported.
type LookupMaps map[string]map[string]string

// Lookup finds a lookup by name.
func (c *Catalog) Lookup(name string) (*Lookup, bool) {
	for _, l := range c.lookups {
		if l.Name == name {
			return l, true
		}
	}
	return nil, false
}

// Lookups lists every lookup in declaration order.
func (c *Catalog) Lookups() []*Lookup {
	return slices.Clone(c.lookups)
}

// WithLoaders returns a copy of the catalog with a loader bound to each
// lookup; every declared lookup needs one and every loader a declared lookup.
func (c *Catalog) WithLoaders(loaders map[string]LookupLoader) (*Catalog, error) {
	bound := make([]*Lookup, 0, len(c.lookups))
	for _, l := range c.lookups {
		load, ok := loaders[l.Name]
		if !ok || load == nil {
			return nil, fmt.Errorf("catalog: lookup %q has no loader", l.Name)
		}
		bound = append(bound, &Lookup{Name: l.Name, Description: l.Description, Load: load})
	}
	for name := range loaders {
		if _, ok := c.Lookup(name); !ok {
			return nil, fmt.Errorf("catalog: loader for undeclared lookup %q", name)
		}
	}
	return &Catalog{datasets: c.datasets, lookups: bound}, nil
}

// LoadLookups fetches each lookup the read fields declare, once. A lookup
// without a loader is skipped; an unknown dataset is left for the compiler.
func (c *Catalog) LoadLookups(ctx context.Context, tenant Tenant, dataset string, reads []string) (LookupMaps, error) {
	ds, ok := c.Dataset(dataset)
	if !ok {
		return nil, nil
	}
	maps := make(LookupMaps)
	for _, f := range ds.Fields {
		if f.Lookup == "" || !slices.Contains(reads, f.Name) {
			continue
		}
		if _, loaded := maps[f.Lookup]; loaded {
			continue
		}
		lookup, ok := c.Lookup(f.Lookup)
		if !ok || lookup.Load == nil {
			continue
		}
		m, err := lookup.Load(ctx, tenant)
		if err != nil {
			return nil, fmt.Errorf("load lookup %s: %w", f.Lookup, err)
		}
		maps[f.Lookup] = m
	}
	return maps, nil
}

// lookupPairs lays a map out as transform's two arrays, sorted for stable
// binds. An entry with an empty side is skipped: an empty raw value matches
// nothing, and an empty target would fold a value into the one never offered.
func lookupPairs(m map[string]string) ([]string, []string) {
	raws := make([]string, 0, len(m))
	for raw, target := range m {
		if raw == "" || target == "" {
			continue
		}
		raws = append(raws, raw)
	}
	slices.Sort(raws)
	targets := make([]string, len(raws))
	for i, raw := range raws {
		targets[i] = m[raw]
	}
	return raws, targets
}
