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
// value with an entry becomes its target, the rest stay as reported. The
// catalog declares a lookup the way it declares a field, so describe can
// say which map a dimension reads through; the column itself stays the raw
// fact, since the map is a mutable tenant setting applied per request.
type Lookup struct {
	Name        string
	Description string
	// Load fetches the tenant's map. A declaration carries none: the service
	// attaches loaders with WithLoaders, and a catalog without them folds
	// nothing, which is what widget validation plans against.
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

// WithLoaders returns a catalog whose lookups load through the given
// loaders, keyed by lookup name. Every declared lookup needs one and every
// loader must name a declared lookup, so a dimension never silently reads
// raw values because its loader was forgotten. The receiver is unchanged.
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

// LoadLookups fetches the maps the fields a request reads go through, one
// load per lookup however many of those fields share it. A field the request
// does not read costs nothing and cannot fail it, so a bare count never
// touches a lookup's store. A lookup without a loader is skipped, so the
// field reads raw values. An unknown dataset loads nothing; the compiler
// reports it.
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

// lookupPairs lays a map out as the two parallel arrays ClickHouse transform
// takes, in raw-value order, so one map always renders one SQL string with
// one set of binds. An entry with an empty side is skipped: an empty raw
// value matches nothing, and an empty target would fold a value into the one
// the values picker never offers.
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
