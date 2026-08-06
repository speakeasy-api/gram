// Package semantic is a declarative semantic layer for Gram telemetry: a
// versioned model definition (dimension catalog + models + physical bindings),
// a planner that picks bindings by precedence, and a compiler that emits the
// same ClickHouse SQL the legacy telemetry.query repo path produces today.
//
// The package is standalone by design: the telemetry package imports it, never
// the other way around. While the legacy Go registries in
// internal/telemetry/repo still exist, a sync test in that package pins the
// embedded definition to them so the two cannot drift.
package semantic

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
)

//go:embed definition.json
var definitionJSON []byte

// Dimension types in the catalog.
const (
	DimTypeString      = "string"
	DimTypeStringArray = "string_array"
	DimTypeID          = "id"
)

// Binding time kinds.
const (
	TimeKindHourBucket = "hour_bucket"
	TimeKindUnixNano   = "unix_nano"
)

// Measure scan types.
const (
	MeasureTypeFloat64 = "float64"
	MeasureTypeInt64   = "int64"
	MeasureTypeUint64  = "uint64"
)

// Definition is the root of the semantic model definition.
type Definition struct {
	Version    int                `json:"version"`
	Dimensions []CatalogDimension `json:"dimensions"`
	Models     []Model            `json:"models"`
}

// CatalogDimension is one conformed dimension in the shared catalog.
type CatalogDimension struct {
	Name string `json:"name"`
	Type string `json:"type"` // string | string_array | id
	// LegacyKey is the public telemetry.query dimension key this catalog
	// dimension aliases, when one exists.
	LegacyKey string `json:"legacy_key,omitempty"`
	// EmptyMeans set to "not_applicable" marks dimensions where an empty
	// value means the attribute does not apply to the row at all, rather
	// than an unclassified population worth surfacing; dimension_values
	// drops '' for these.
	EmptyMeans  string `json:"empty_means,omitempty"`
	Description string `json:"description"`
}

// Model is one queryable fact model (e.g. usage).
type Model struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Internal marks models that exist to declare a population precisely
	// (e.g. provider_reports) rather than to be queried by end users. The
	// planner serves them like any other model; the flag's contract is that
	// internal models are excluded from any future public catalog/endpoint
	// (phase-3 enforcement).
	Internal bool `json:"internal,omitempty"`
	// RollupOf names the finer-grained model this model aggregates (e.g.
	// sessions rolls up usage). Documentation for consumers; the planner
	// does not rewrite across it.
	RollupOf string `json:"rollup_of,omitempty"`
	// Dimensions is the allowlist of catalog dimensions this model carries.
	Dimensions []string  `json:"dimensions"`
	Time       ModelTime `json:"time"`
	// ExclusiveWith names models whose measures must never be combined with
	// this model's in one query (they can describe the same underlying
	// activity from two authorities).
	ExclusiveWith []string  `json:"exclusive_with"`
	Measures      []Measure `json:"measures"`
	Bindings      []Binding `json:"bindings"`
}

// ModelTime declares the model's finest supported time granularity.
type ModelTime struct {
	MinGranularitySeconds int64 `json:"min_granularity_seconds"`
}

// Measure is one declared measure of a model. The declaration order in the
// JSON is the canonical SELECT order the compiler emits.
type Measure struct {
	Name string `json:"name"`
	// LegacyKey is the public telemetry.query measure key this aliases.
	LegacyKey string `json:"legacy_key,omitempty"`
	// Description disambiguates measures whose name alone is not enough
	// (e.g. charged_usd vs the computed cost_usd).
	Description string `json:"description,omitempty"`
	Unit        string `json:"unit"`
	Aggregation string `json:"aggregation"`
	Additivity  string `json:"additivity"`
	// Type is the Go scan type of the aggregated value: float64|int64|uint64.
	Type string `json:"type"`
	// NullSemantics documents what an absent value means (e.g. "unavailable").
	NullSemantics string `json:"null_semantics,omitempty"`
	// UnavailableFor documents dimension values for which this measure is not
	// reported (metadata only; no compiler behavior).
	UnavailableFor map[string][]string `json:"unavailable_for,omitempty"`
}

// Binding maps a model onto one physical table.
type Binding struct {
	Source string `json:"source"`
	// Precedence orders binding selection; higher wins.
	Precedence int `json:"precedence"`
	// RowFilter is ANDed into the WHERE clause first, scoping the physical
	// rows to the model's population.
	RowFilter string      `json:"row_filter,omitempty"`
	Time      BindingTime `json:"time"`
	// MinWindowSeconds restricts the binding to queries whose time window
	// (TimeEnd - TimeStart) spans at least this many seconds. Used by
	// pre-aggregated sources whose bucket snapping is only acceptable over
	// wide windows; narrow windows fall through to lower-precedence bindings.
	MinWindowSeconds int64 `json:"min_window_seconds,omitempty"`
	// Dimensions and Measures map catalog names to physical SQL expressions.
	Dimensions map[string]BindingExpr `json:"dimensions"`
	Measures   map[string]BindingExpr `json:"measures"`
	// Caveats document known population/semantics quirks of the binding.
	Caveats []string `json:"caveats,omitempty"`
}

// BindingTime declares how the physical table is time-addressed.
type BindingTime struct {
	Kind                  string `json:"kind"` // hour_bucket | unix_nano
	Column                string `json:"column"`
	MinGranularitySeconds int64  `json:"min_granularity_seconds"`
}

// BindingExpr is a physical SQL expression for one dimension or measure.
type BindingExpr struct {
	SQL string `json:"sql"`
}

// Dimension returns the catalog dimension by name.
func (d *Definition) Dimension(name string) (*CatalogDimension, bool) {
	for i := range d.Dimensions {
		if d.Dimensions[i].Name == name {
			return &d.Dimensions[i], true
		}
	}
	return nil, false
}

// Model returns the model by name.
func (d *Definition) Model(name string) (*Model, bool) {
	for i := range d.Models {
		if d.Models[i].Name == name {
			return &d.Models[i], true
		}
	}
	return nil, false
}

// Measure returns the model's measure by name.
func (m *Model) Measure(name string) (*Measure, bool) {
	for i := range m.Measures {
		if m.Measures[i].Name == name {
			return &m.Measures[i], true
		}
	}
	return nil, false
}

var loadOnce = sync.OnceValues(func() (*Definition, error) {
	return Parse(definitionJSON)
})

// Load returns the embedded, validated definition. The result is shared;
// callers must not mutate it.
func Load() (*Definition, error) {
	return loadOnce()
}

// Parse decodes and validates a definition document.
func Parse(data []byte) (*Definition, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var def Definition
	if err := dec.Decode(&def); err != nil {
		return nil, fmt.Errorf("decode semantic definition: %w", err)
	}
	if err := def.Validate(); err != nil {
		return nil, fmt.Errorf("validate semantic definition: %w", err)
	}
	return &def, nil
}

// Validate checks the definition's internal referential integrity.
func (d *Definition) Validate() error {
	if d.Version != 1 {
		return fmt.Errorf("unsupported definition version %d (want 1)", d.Version)
	}

	dimNames := make(map[string]bool, len(d.Dimensions))
	dimLegacyKeys := make(map[string]string, len(d.Dimensions))
	for _, dim := range d.Dimensions {
		if dim.Name == "" {
			return fmt.Errorf("catalog dimension with empty name")
		}
		if dimNames[dim.Name] {
			return fmt.Errorf("duplicate catalog dimension %q", dim.Name)
		}
		dimNames[dim.Name] = true
		switch dim.Type {
		case DimTypeString, DimTypeStringArray, DimTypeID:
		default:
			return fmt.Errorf("catalog dimension %q has unknown type %q", dim.Name, dim.Type)
		}
		if dim.EmptyMeans != "" && dim.EmptyMeans != "not_applicable" {
			return fmt.Errorf("catalog dimension %q has unknown empty_means %q", dim.Name, dim.EmptyMeans)
		}
		if dim.LegacyKey != "" {
			if prev, ok := dimLegacyKeys[dim.LegacyKey]; ok {
				return fmt.Errorf("catalog dimensions %q and %q share legacy_key %q", prev, dim.Name, dim.LegacyKey)
			}
			dimLegacyKeys[dim.LegacyKey] = dim.Name
		}
	}

	modelNames := make(map[string]bool, len(d.Models))
	for _, m := range d.Models {
		if m.Name == "" {
			return fmt.Errorf("model with empty name")
		}
		if modelNames[m.Name] {
			return fmt.Errorf("duplicate model %q", m.Name)
		}
		modelNames[m.Name] = true
	}

	for _, m := range d.Models {
		if m.RollupOf != "" && !modelNames[m.RollupOf] {
			return fmt.Errorf("model %q declares rollup_of unknown model %q", m.Name, m.RollupOf)
		}
		if err := d.validateModel(&m); err != nil {
			return err
		}
	}

	// exclusive_with must reference existing models and be symmetric.
	for _, m := range d.Models {
		for _, other := range m.ExclusiveWith {
			peer, ok := d.Model(other)
			if !ok {
				return fmt.Errorf("model %q declares exclusive_with unknown model %q", m.Name, other)
			}
			if !slices.Contains(peer.ExclusiveWith, m.Name) {
				return fmt.Errorf("exclusive_with is not symmetric: %q lists %q but not vice versa", m.Name, other)
			}
		}
	}

	return nil
}

func (d *Definition) validateModel(m *Model) error {
	modelDims := make(map[string]bool, len(m.Dimensions))
	for _, name := range m.Dimensions {
		if _, ok := d.Dimension(name); !ok {
			return fmt.Errorf("model %q declares dimension %q not in the catalog", m.Name, name)
		}
		if modelDims[name] {
			return fmt.Errorf("model %q declares dimension %q twice", m.Name, name)
		}
		modelDims[name] = true
	}
	if m.Time.MinGranularitySeconds <= 0 {
		return fmt.Errorf("model %q must declare a positive time.min_granularity_seconds", m.Name)
	}

	measureNames := make(map[string]bool, len(m.Measures))
	measureLegacyKeys := make(map[string]string, len(m.Measures))
	for _, ms := range m.Measures {
		if ms.Name == "" {
			return fmt.Errorf("model %q has a measure with empty name", m.Name)
		}
		if measureNames[ms.Name] {
			return fmt.Errorf("model %q declares measure %q twice", m.Name, ms.Name)
		}
		measureNames[ms.Name] = true
		switch ms.Type {
		case MeasureTypeFloat64, MeasureTypeInt64, MeasureTypeUint64:
		default:
			return fmt.Errorf("model %q measure %q has unknown scan type %q", m.Name, ms.Name, ms.Type)
		}
		if ms.LegacyKey != "" {
			if prev, ok := measureLegacyKeys[ms.LegacyKey]; ok {
				return fmt.Errorf("model %q measures %q and %q share legacy_key %q", m.Name, prev, ms.Name, ms.LegacyKey)
			}
			measureLegacyKeys[ms.LegacyKey] = ms.Name
		}
		for dim := range ms.UnavailableFor {
			if !modelDims[dim] {
				return fmt.Errorf("model %q measure %q declares unavailable_for unknown dimension %q", m.Name, ms.Name, dim)
			}
		}
	}

	if len(m.Bindings) == 0 {
		return fmt.Errorf("model %q has no bindings", m.Name)
	}
	for _, b := range m.Bindings {
		if b.Source == "" {
			return fmt.Errorf("model %q has a binding with empty source", m.Name)
		}
		switch b.Time.Kind {
		case TimeKindHourBucket, TimeKindUnixNano:
		default:
			return fmt.Errorf("model %q binding %q has unknown time kind %q", m.Name, b.Source, b.Time.Kind)
		}
		if b.Time.Column == "" {
			return fmt.Errorf("model %q binding %q has an empty time column", m.Name, b.Source)
		}
		if b.Time.MinGranularitySeconds <= 0 {
			return fmt.Errorf("model %q binding %q must declare a positive time.min_granularity_seconds", m.Name, b.Source)
		}
		if b.MinWindowSeconds < 0 {
			return fmt.Errorf("model %q binding %q has a negative min_window_seconds", m.Name, b.Source)
		}
		for name, expr := range b.Dimensions {
			if !modelDims[name] {
				return fmt.Errorf("model %q binding %q serves dimension %q not declared on the model", m.Name, b.Source, name)
			}
			if expr.SQL == "" {
				return fmt.Errorf("model %q binding %q has empty SQL for dimension %q", m.Name, b.Source, name)
			}
		}
		for name, expr := range b.Measures {
			if !measureNames[name] {
				return fmt.Errorf("model %q binding %q serves measure %q not declared on the model", m.Name, b.Source, name)
			}
			if expr.SQL == "" {
				return fmt.Errorf("model %q binding %q has empty SQL for measure %q", m.Name, b.Source, name)
			}
		}
	}

	return nil
}
