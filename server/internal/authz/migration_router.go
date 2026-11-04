package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/authz/schema"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

// MigrationMode selects which evaluator decides a typed operation. Modes are
// ordered from most to least conservative, and an operation always runs in
// the most conservative mode of everything it touches.
type MigrationMode int

const (
	// MigrationModeOld lets the engine decide, exactly as before the
	// operation's service migrated. It is the default for every unit.
	MigrationModeOld MigrationMode = iota
	// MigrationModeShadow lets the engine decide and compares the registry
	// evaluator's answer to the same request, without changing the response.
	MigrationModeShadow
	// MigrationModeNew lets the registry evaluator decide.
	MigrationModeNew
)

func (m MigrationMode) String() string {
	switch m {
	case MigrationModeOld:
		return "old"
	case MigrationModeShadow:
		return "shadow"
	case MigrationModeNew:
		return "new"
	default:
		return fmt.Sprintf("mode(%d)", int(m))
	}
}

// MigrationUnit is the manifest of one independently routed part of the
// model: the declared types whose operations switch evaluators together, and
// the units whose types its permissions read. A unit never runs in a less
// conservative mode than any unit it depends on, so a dependent cannot reach
// the registry evaluator before what it traverses has.
type MigrationUnit struct {
	// Name keys the unit's routing flag and its comparison records.
	Name string
	// Types are the declared types the unit routes.
	Types []string
	// DependsOn are the units the unit's permissions traverse or offer as
	// alternatives.
	DependsOn []string
}

// ModeSource chooses a unit's mode for an organization. The router asks once
// per unit per authorization boundary and pins the answer.
type ModeSource interface {
	MigrationMode(ctx context.Context, unit, organizationID string) MigrationMode
}

// featureFlagModes reads each unit's mode from its migration feature flag.
type featureFlagModes struct {
	flags feature.Provider
}

// FeatureFlagModes routes each unit by its feature.RBACMigrationFlag. Any
// flag that is off, unknown or fails to evaluate keeps the engine deciding.
func FeatureFlagModes(flags feature.Provider) ModeSource {
	return featureFlagModes{flags: flags}
}

func (f featureFlagModes) MigrationMode(ctx context.Context, unit, organizationID string) MigrationMode {
	switch feature.RBACMigrationVariant(ctx, f.flags, unit, organizationID) {
	case feature.RBACMigrationVariantShadow:
		return MigrationModeShadow
	case feature.RBACMigrationVariantNew:
		return MigrationModeNew
	default:
		return MigrationModeOld
	}
}

type fixedModes MigrationMode

// FixedModes routes every unit to mode, for tests and local development.
func FixedModes(mode MigrationMode) ModeSource {
	return fixedModes(mode)
}

func (f fixedModes) MigrationMode(context.Context, string, string) MigrationMode {
	return MigrationMode(f)
}

// DefaultMigrationUnits are the units of the default registry. Each type the
// default registry declares belongs to exactly one unit; a migration that
// declares new types adds them to its unit here.
func DefaultMigrationUnits() []MigrationUnit {
	return []MigrationUnit{
		{Name: "organization", Types: []string{"organization"}, DependsOn: nil},
		{Name: "project", Types: []string{"project"}, DependsOn: []string{"organization"}},
		{Name: "mcp", Types: []string{"mcp_server", "mcp_auth_client"}, DependsOn: []string{"project"}},
		{Name: "skill", Types: []string{"skill"}, DependsOn: []string{"project"}},
		{Name: "assistant", Types: []string{"assistant"}, DependsOn: []string{"project"}},
		{Name: "environment", Types: []string{"environment"}, DependsOn: []string{"project"}},
	}
}

// MigrationRouter selects the evaluator for each typed operation from its
// units' modes, pinned once per authorization boundary.
type MigrationRouter struct {
	source     ModeSource
	unitOfType map[string]string
	// closure lists each unit and every unit it transitively depends on.
	closure map[string][]string
	units   []string
}

// NewMigrationRouter validates the unit manifests against registry. Every
// unit type must be declared and belong to one unit, every dependency must be
// a registered unit, and dependencies must not form a cycle.
func NewMigrationRouter(registry *schema.Registry, source ModeSource, units ...MigrationUnit) (*MigrationRouter, error) {
	if registry == nil {
		return nil, errors.New("migration router requires a registry")
	}
	if source == nil {
		return nil, errors.New("migration router requires a mode source")
	}
	router := &MigrationRouter{
		source:     source,
		unitOfType: make(map[string]string),
		closure:    make(map[string][]string, len(units)),
		units:      make([]string, 0, len(units)),
	}
	byName := make(map[string]MigrationUnit, len(units))
	for _, unit := range units {
		if !feature.ValidMigrationUnit(unit.Name) {
			return nil, fmt.Errorf("migration unit %q: name must be lower snake case", unit.Name)
		}
		if _, dup := byName[unit.Name]; dup {
			return nil, fmt.Errorf("migration unit %q is registered twice", unit.Name)
		}
		if len(unit.Types) == 0 {
			return nil, fmt.Errorf("migration unit %q routes no types", unit.Name)
		}
		for _, typeName := range unit.Types {
			if _, ok := registry.Type(typeName); !ok {
				return nil, fmt.Errorf("migration unit %q routes undeclared type %q", unit.Name, typeName)
			}
			if owner, taken := router.unitOfType[typeName]; taken {
				return nil, fmt.Errorf("type %q belongs to migration units %q and %q", typeName, owner, unit.Name)
			}
			router.unitOfType[typeName] = unit.Name
		}
		byName[unit.Name] = unit
		router.units = append(router.units, unit.Name)
	}
	for _, unit := range units {
		closure, err := dependencyClosure(byName, unit.Name, nil)
		if err != nil {
			return nil, err
		}
		slices.Sort(closure)
		router.closure[unit.Name] = slices.Compact(closure)
	}
	return router, nil
}

func dependencyClosure(units map[string]MigrationUnit, name string, path []string) ([]string, error) {
	if slices.Contains(path, name) {
		return nil, fmt.Errorf("migration units depend on each other in a cycle: %s", strings.Join(append(path, name), " -> "))
	}
	unit, ok := units[name]
	if !ok {
		return nil, fmt.Errorf("migration unit %q depends on unregistered unit %q", path[len(path)-1], name)
	}
	out := []string{name}
	for _, dep := range unit.DependsOn {
		deps, err := dependencyClosure(units, dep, append(slices.Clone(path), name))
		if err != nil {
			return nil, err
		}
		out = append(out, deps...)
	}
	return out, nil
}

// Units lists the registered unit names in registration order.
func (r *MigrationRouter) Units() []string {
	if r == nil {
		return nil
	}
	return slices.Clone(r.units)
}

// route is the evaluator choice for one client call.
type route struct {
	mode  MigrationMode
	units []string
	// organizationID is the organization the route was chosen for. It is
	// set when a loaded decision routes by its own organization outside an
	// admitted request; otherwise the request's organization applies.
	organizationID string
}

// routeFor chooses the evaluator for a call. Every operation in the call
// runs in one mode: the most conservative mode of any unit its permissions
// or options touch, including their dependencies. A type that belongs to no
// unit, a context with no pinned routes, or a missing router keeps the
// engine deciding.
func (r *MigrationRouter) routeFor(ctx context.Context, ops []Operation) route {
	old := route{mode: MigrationModeOld, units: nil, organizationID: ""}
	if r == nil || len(ops) == 0 {
		return old
	}
	pins := routePinsFromContext(ctx)
	if pins == nil {
		return old
	}
	return r.routeWith(ctx, ops, pins, requestOrganizationID(ctx))
}

// routeForLoaded chooses the evaluator for a decision over grants a service
// loaded for organizationID. It reuses the request's pins when the request
// was admitted; otherwise the call is its own boundary and reads its units'
// modes for organizationID afresh, so background work that never passed
// admission still follows its organization's rollout.
func (r *MigrationRouter) routeForLoaded(ctx context.Context, ops []Operation, organizationID string) route {
	if r == nil || len(ops) == 0 {
		return route{mode: MigrationModeOld, units: nil, organizationID: ""}
	}
	pins := routePinsFromContext(ctx)
	if pins == nil {
		pins = &routePins{mu: sync.Mutex{}, modes: make(map[string]MigrationMode)}
	} else if requested := requestOrganizationID(ctx); requested != "" {
		organizationID = requested
	}
	return r.routeWith(ctx, ops, pins, organizationID)
}

func (r *MigrationRouter) routeWith(ctx context.Context, ops []Operation, pins *routePins, organizationID string) route {
	old := route{mode: MigrationModeOld, units: nil, organizationID: ""}
	var units []string
	for _, op := range ops {
		for _, typeName := range operationTypes(op) {
			unit, ok := r.unitOfType[typeName]
			if !ok {
				return old
			}
			units = append(units, unit)
		}
	}
	slices.Sort(units)
	units = slices.Compact(units)

	mode := MigrationModeNew
	pins.mu.Lock()
	defer pins.mu.Unlock()
	for _, unit := range units {
		for _, dep := range r.closure[unit] {
			pinned, ok := pins.modes[dep]
			if !ok {
				pinned = MigrationModeOld
				if organizationID != "" {
					pinned = r.source.MigrationMode(ctx, dep, organizationID)
				}
				pins.modes[dep] = pinned
			}
			mode = min(mode, pinned)
		}
	}
	return route{mode: mode, units: units, organizationID: organizationID}
}

func operationTypes(op Operation) []string {
	if !op.alternatives {
		return []string{op.permission.Type}
	}
	types := make([]string, 0, len(op.options))
	for _, option := range op.options {
		types = append(types, operationTypes(option)...)
	}
	return types
}

func requestOrganizationID(ctx context.Context) string {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return ""
	}
	return authCtx.ActiveOrganizationID
}

// routePins holds the modes a boundary has chosen. Modes are read lazily, the
// first time a call touches a unit, and never change afterwards, so a flag
// that flips mid-request cannot mix evaluators within the request.
type routePins struct {
	mu    sync.Mutex
	modes map[string]MigrationMode
}

type routePinsKey struct{}

// withRoutePins starts a new authorization boundary's pinned routes, keeping
// any the context already carries so a refresh within the boundary reuses
// them.
func withRoutePins(ctx context.Context) context.Context {
	if routePinsFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, routePinsKey{}, &routePins{mu: sync.Mutex{}, modes: make(map[string]MigrationMode)})
}

func routePinsFromContext(ctx context.Context) *routePins {
	pins, _ := ctx.Value(routePinsKey{}).(*routePins)
	return pins
}
