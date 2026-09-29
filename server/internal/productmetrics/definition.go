// Package productmetrics publishes tenant-scoped analytical contributions.
// These are duplicate-inclusive measurements, not billing or audit records.
package productmetrics

import (
	"fmt"
	"regexp"
	"sync"
	"unicode/utf8"
)

// Instrument identifies the supported measurement operation.
type Instrument string

const (
	// Counter contributes a nonnegative monotonic delta.
	Counter Instrument = "counter"
	// Histogram contributes one observation, without distribution buckets.
	Histogram Instrument = "histogram"
)

// Definition is a code-owned metric descriptor. Event time always means the
// source observation time; each input is one delta increment or observation.
type Definition struct {
	// ScopeName identifies the instrumentation library.
	ScopeName string `json:"scope_name"`

	// ScopeVersion identifies the instrumentation release.
	ScopeVersion string `json:"scope_version"`

	// Name is an OTel metric name, without unit or instrument suffixes.
	Name string `json:"name"`

	// Description is metadata, not series identity.
	Description string `json:"description"`

	// Unit uses UCUM notation (including annotations such as {evaluation}).
	Unit string `json:"unit"`

	// Instrument selects monotonic delta Sum or observation aggregation.
	Instrument Instrument `json:"instrument"`
}

var metricName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.\-/]{0,254}$`)

type definitionKey struct {
	scope   string
	version string
	name    string
}

// Registry registers definitions independently of attribute cardinality. Build
// one at startup from producer-owned definitions; it retains no series state.
type Registry struct {
	mu          sync.RWMutex
	definitions map[definitionKey]Definition
}

// NewRegistry creates a registry and rejects incompatible duplicate definitions.
func NewRegistry(definitions ...Definition) (*Registry, error) {
	r := &Registry{mu: sync.RWMutex{}, definitions: make(map[definitionKey]Definition)}
	for _, d := range definitions {
		if err := r.Register(d); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Register permits description changes but rejects incompatible descriptors.
// A scope version change is not a substitute for renaming incompatible metrics.
func (r *Registry) Register(d Definition) error {
	if !utf8.ValidString(d.ScopeName) || !utf8.ValidString(d.ScopeVersion) || !utf8.ValidString(d.Unit) || !utf8.ValidString(d.Description) {
		return fmt.Errorf("metric descriptor strings must be UTF-8")
	}
	if d.ScopeName == "" || !metricName.MatchString(d.Name) || d.Unit == "" {
		return fmt.Errorf("metric scope, valid name and UCUM unit are required")
	}
	if d.Instrument != Counter && d.Instrument != Histogram {
		return fmt.Errorf("unsupported instrument %q", d.Instrument)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := definitionKey{scope: d.ScopeName, version: d.ScopeVersion, name: d.Name}
	for key, previous := range r.definitions {
		if key.scope == k.scope && key.name == k.name && (previous.Unit != d.Unit || previous.Instrument != d.Instrument) {
			return fmt.Errorf("conflicting metric descriptor %q", d.Name)
		}
	}
	r.definitions[k] = d
	return nil
}

func (r *Registry) contains(d Definition) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	registered, ok := r.definitions[definitionKey{scope: d.ScopeName, version: d.ScopeVersion, name: d.Name}]
	return ok && registered.Unit == d.Unit && registered.Instrument == d.Instrument
}
