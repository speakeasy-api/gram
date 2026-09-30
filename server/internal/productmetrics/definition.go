// Package productmetrics publishes tenant-scoped analytical contributions.
// These are duplicate-inclusive measurements, not billing or audit records.
package productmetrics

import (
	"fmt"
	"regexp"
	"sync"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
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
	contracts   map[definitionKey][]AttributeRule
}

// NewRegistry creates a registry and rejects incompatible duplicate definitions.
func NewRegistry(definitions ...Definition) (*Registry, error) {
	r := &Registry{mu: sync.RWMutex{}, definitions: make(map[definitionKey]Definition), contracts: make(map[definitionKey][]AttributeRule)}
	for _, d := range definitions {
		if err := r.Register(d); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// AttributeRule declares one producer-owned dimension. An empty Values list
// permits arbitrary values of the declared type; producers still own cardinality.
type AttributeRule struct {
	// Namespace is resource, scope, or point.
	Namespace string

	// Key is the exact attribute name.
	Key string

	// Type preserves the OTel scalar or array representation.
	Type attribute.Type

	// Values optionally restrict the dimension to canonical typed values.
	Values []attribute.Value
}

// RegisterDimensions installs an explicit dimension contract for a registered
// descriptor. An empty contract permits no producer attributes. Contracts are
// code-owned and never accepted from broker messages. Configure before publishing.
func (r *Registry) RegisterDimensions(d Definition, rules ...AttributeRule) error {
	if !r.contains(d) {
		return fmt.Errorf("metric definition is not registered")
	}
	seen := make(map[string]bool)
	copyRules := make([]AttributeRule, len(rules))
	for i, rule := range rules {
		if rule.Namespace != "resource" && rule.Namespace != "scope" && rule.Namespace != "point" {
			return fmt.Errorf("invalid dimension namespace")
		}
		key := rule.Namespace + ":" + rule.Key
		if rule.Key == "" || !utf8.ValidString(rule.Key) || seen[key] || rule.Type == attribute.INVALID {
			return fmt.Errorf("invalid or duplicate dimension rule")
		}
		if rule.Namespace == "resource" && (rule.Key == OrganizationResourceKey || rule.Key == ProjectResourceKey) {
			return fmt.Errorf("reserved dimension rule")
		}
		seen[key] = true
		for _, v := range rule.Values {
			if v.Type() != rule.Type {
				return fmt.Errorf("dimension value has incompatible type")
			}
			if _, err := CanonicalAttributes([]attribute.KeyValue{{Key: attribute.Key(rule.Key), Value: v}}); err != nil {
				return err
			}
		}
		copyRules[i] = rule
		copyRules[i].Values = append([]attribute.Value(nil), rule.Values...)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contracts[definitionKey{scope: d.ScopeName, version: d.ScopeVersion, name: d.Name}] = copyRules
	return nil
}

// ValidateDimensions rejects undeclared keys, types and values when a producer
// installed a contract. Definitions without a contract retain generic semantics.
func (r *Registry) ValidateDimensions(c Contribution) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rules, exists := r.contracts[definitionKey{scope: c.Definition.ScopeName, version: c.Definition.ScopeVersion, name: c.Definition.Name}]
	if !exists {
		return nil
	}
	for namespace, attrs := range map[string][]attribute.KeyValue{"resource": c.ResourceAttributes, "scope": c.ScopeAttributes, "point": c.PointAttributes} {
		for _, a := range attrs {
			allowed := false
			for _, rule := range rules {
				if rule.Namespace != namespace || rule.Key != string(a.Key) || rule.Type != a.Value.Type() {
					continue
				}
				if len(rule.Values) == 0 {
					allowed = true
					break
				}
				actual, err := CanonicalAttributes([]attribute.KeyValue{a})
				if err != nil {
					return err
				}
				for _, v := range rule.Values {
					expected, err := CanonicalAttributes([]attribute.KeyValue{{Key: a.Key, Value: v}})
					if err != nil {
						return err
					}
					if actual == expected {
						allowed = true
						break
					}
				}
			}
			if !allowed {
				return fmt.Errorf("attribute violates producer dimension contract")
			}
		}
	}
	return nil
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
