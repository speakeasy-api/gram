package killswitches

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Registration contains all code-owned contracts finalized into a Registry. Registered adapter
// implementations are retained behind interfaces because they cannot be deep-copied. They must
// therefore be immutable, return a stable Kind, and be safe for concurrent use after registration.
type Registration struct {
	Definitions       []Definition
	IdentityContracts []IdentityContract
	PrincipalAdapters []PrincipalAdapterRegistration
	ResourceAdapters  []ResourceAdapterRegistration
	Surfaces          []Surface
	TransportAdapters []TransportAdapterRegistration
	Coverage          []CoverageContract
}

// Registry is a finalized snapshot of code-owned kill-switch contract values. Adapter registration
// keys are snapshotted, while adapter behavior relies on Registration's immutability and concurrency
// contract.
type Registry struct {
	definitions       map[DefinitionKey]Definition
	identityContracts map[IdentityContractKey]IdentityContract
	principalAdapters map[PrincipalKind]PrincipalAdapter
	resourceAdapters  map[ResourceKind]ResourceAdapter
	surfaces          map[Surface]struct{}
	transportAdapters map[TransportAdapterKey]TransportAdapter
	coverage          map[coverageKey]CoverageContract
}

type coverageKey struct {
	definition DefinitionKey
	surface    Surface
}

// BuildRegistry validates a complete registration and returns a finalized snapshot.
func BuildRegistry(input Registration) (*Registry, error) {
	registry := &Registry{
		definitions:       make(map[DefinitionKey]Definition, len(input.Definitions)),
		identityContracts: make(map[IdentityContractKey]IdentityContract, len(input.IdentityContracts)),
		principalAdapters: make(map[PrincipalKind]PrincipalAdapter, len(input.PrincipalAdapters)),
		resourceAdapters:  make(map[ResourceKind]ResourceAdapter, len(input.ResourceAdapters)),
		surfaces:          make(map[Surface]struct{}, len(input.Surfaces)),
		transportAdapters: make(map[TransportAdapterKey]TransportAdapter, len(input.TransportAdapters)),
		coverage:          make(map[coverageKey]CoverageContract, len(input.Coverage)),
	}

	for _, surface := range input.Surfaces {
		if err := validateIdentifier("surface", string(surface)); err != nil {
			return nil, err
		}
		if _, exists := registry.surfaces[surface]; exists {
			return nil, fmt.Errorf("duplicate surface %q", surface)
		}
		registry.surfaces[surface] = struct{}{}
	}
	for _, registration := range input.TransportAdapters {
		if err := validateIdentifier("transport adapter key", string(registration.Key)); err != nil {
			return nil, err
		}
		if isNilInterface(registration.Adapter) {
			return nil, fmt.Errorf("transport adapter %q must not be nil", registration.Key)
		}
		if _, exists := registry.transportAdapters[registration.Key]; exists {
			return nil, fmt.Errorf("duplicate transport adapter %q", registration.Key)
		}
		registry.transportAdapters[registration.Key] = wrapTransportAdapter(registration.Key, registration.Adapter)
	}
	for _, registration := range input.PrincipalAdapters {
		adapter := registration.Adapter
		if isNilInterface(adapter) {
			return nil, fmt.Errorf("principal adapter must not be nil")
		}
		kind := adapter.Kind()
		if err := validateIdentifier("principal adapter kind", string(kind)); err != nil {
			return nil, err
		}
		if _, exists := registry.principalAdapters[kind]; exists {
			return nil, fmt.Errorf("duplicate principal adapter %q", kind)
		}
		if err := validatePrincipalCanonicalizationFixtures(kind, adapter, registration.Fixtures); err != nil {
			return nil, err
		}
		registry.principalAdapters[kind] = adapter
	}
	for _, registration := range input.ResourceAdapters {
		adapter := registration.Adapter
		if isNilInterface(adapter) {
			return nil, fmt.Errorf("resource adapter must not be nil")
		}
		kind := adapter.Kind()
		if err := validateIdentifier("resource adapter kind", string(kind)); err != nil {
			return nil, err
		}
		if _, exists := registry.resourceAdapters[kind]; exists {
			return nil, fmt.Errorf("duplicate resource adapter %q", kind)
		}
		if err := validateResourceCanonicalizationFixtures(kind, adapter, registration.Fixtures); err != nil {
			return nil, err
		}
		registry.resourceAdapters[kind] = adapter
	}
	for _, contract := range input.IdentityContracts {
		if err := validateIdentityContract(contract, registry); err != nil {
			return nil, err
		}
		if _, exists := registry.identityContracts[contract.Key]; exists {
			return nil, fmt.Errorf("duplicate identity contract %q", contract.Key)
		}
		registry.identityContracts[contract.Key] = normalizeIdentityContract(contract)
	}
	for _, definition := range input.Definitions {
		normalized, err := validateDefinition(definition, registry)
		if err != nil {
			return nil, err
		}
		if _, exists := registry.definitions[definition.Key]; exists {
			return nil, fmt.Errorf("duplicate definition %q", definition.Key)
		}
		registry.definitions[definition.Key] = normalized
	}
	for _, contract := range input.Coverage {
		normalized, err := validateCoverage(contract, registry)
		if err != nil {
			return nil, err
		}
		key := coverageKey{definition: contract.Definition, surface: contract.Surface}
		if _, exists := registry.coverage[key]; exists {
			return nil, fmt.Errorf("duplicate coverage for definition %q and surface %q", contract.Definition, contract.Surface)
		}
		registry.coverage[key] = normalized
	}
	if err := validateCoverageCompleteness(registry); err != nil {
		return nil, err
	}
	for kind, adapter := range registry.principalAdapters {
		registry.principalAdapters[kind] = registeredPrincipalAdapter{kind: kind, adapter: adapter, registry: registry}
	}
	for kind, adapter := range registry.resourceAdapters {
		registry.resourceAdapters[kind] = registeredResourceAdapter{kind: kind, adapter: adapter}
	}
	return registry, nil
}

const canonicalizationFixtureAttempts = 3

func validatePrincipalCanonicalizationFixtures(kind PrincipalKind, adapter PrincipalAdapter, fixtures []PrincipalCanonicalizationFixture) error {
	if len(fixtures) == 0 {
		return fmt.Errorf("principal adapter %q requires canonicalization fixtures", kind)
	}
	for index, fixture := range fixtures {
		if err := validateCanonicalizationFixture(
			fmt.Sprintf("principal adapter %q fixture %d", kind, index),
			fixture.OrganizationID,
			fixture.Input,
			fixture.Expected,
			adapter.Canonicalize,
		); err != nil {
			return err
		}
	}
	return nil
}

func validateResourceCanonicalizationFixtures(kind ResourceKind, adapter ResourceAdapter, fixtures []ResourceCanonicalizationFixture) error {
	if len(fixtures) == 0 {
		return fmt.Errorf("resource adapter %q requires canonicalization fixtures", kind)
	}
	for index, fixture := range fixtures {
		if err := validateCanonicalizationFixture(
			fmt.Sprintf("resource adapter %q fixture %d", kind, index),
			fixture.OrganizationID,
			fixture.Input,
			fixture.Expected,
			adapter.Canonicalize,
		); err != nil {
			return err
		}
	}
	return nil
}

func validateCanonicalizationFixture[T ~string](
	label string,
	organizationID OrganizationID,
	input string,
	expected CanonicalizationResult[T],
	canonicalize func(OrganizationID, string) (CanonicalizationResult[T], error),
) error {
	if err := validateIdentifier("canonicalization fixture organization ID", string(organizationID)); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	expectedKey, expectedSupported, err := expected.Key()
	if err != nil {
		return fmt.Errorf("%s has an invalid expected result: %w", label, err)
	}

	var firstKey T
	var firstSupported bool
	for attempt := range canonicalizationFixtureAttempts {
		result, err := canonicalize(organizationID, input)
		if err != nil {
			return fmt.Errorf("%s attempt %d: %w", label, attempt+1, err)
		}
		key, supported, err := result.Key()
		if err != nil {
			return fmt.Errorf("%s attempt %d returned an invalid result: %w", label, attempt+1, err)
		}
		if attempt == 0 {
			firstKey, firstSupported = key, supported
		} else if key != firstKey || supported != firstSupported {
			return fmt.Errorf("%s canonicalization is unstable across repeated calls", label)
		}
	}
	if firstKey != expectedKey || firstSupported != expectedSupported {
		return fmt.Errorf("%s returned key %q supported=%t, expected key %q supported=%t", label, firstKey, firstSupported, expectedKey, expectedSupported)
	}
	return nil
}

func wrapTransportAdapter(key TransportAdapterKey, adapter TransportAdapter) TransportAdapter {
	return func(result EvaluationResult, failurePolicy FailurePolicy) (TransportDisposition, error) {
		expected, err := ResolveTransportDisposition(result, failurePolicy)
		if err != nil {
			return TransportDisposition{}, fmt.Errorf("transport adapter %q: %w", key, err)
		}
		disposition, err := adapter(result, failurePolicy)
		if err != nil {
			return TransportDisposition{}, fmt.Errorf("transport adapter %q: %w", key, err)
		}
		if disposition.kind != expected.kind {
			return TransportDisposition{}, fmt.Errorf("transport adapter %q returned disposition %q, expected %q for the result/policy matrix", key, disposition.kind, expected.kind)
		}
		if disposition.externalNote != expected.externalNote {
			return TransportDisposition{}, fmt.Errorf("transport adapter %q did not preserve the result/policy matrix external note", key)
		}
		return disposition, nil
	}
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

type registeredPrincipalAdapter struct {
	kind     PrincipalKind
	adapter  PrincipalAdapter
	registry *Registry
}

func (a registeredPrincipalAdapter) Kind() PrincipalKind { return a.kind }

func (a registeredPrincipalAdapter) Canonicalize(organizationID OrganizationID, input string) (CanonicalizationResult[PrincipalKey], error) {
	result, err := a.adapter.Canonicalize(organizationID, input)
	if err != nil {
		return CanonicalizationResult[PrincipalKey]{}, fmt.Errorf("principal adapter %q canonicalize: %w", a.kind, err)
	}
	if _, _, err := result.Key(); err != nil {
		return CanonicalizationResult[PrincipalKey]{}, fmt.Errorf("principal adapter %q: %w", a.kind, err)
	}
	return result, nil
}

func (a registeredPrincipalAdapter) ValidateCurrentOrganization(ctx context.Context, organizationID OrganizationID, key PrincipalKey) (bool, error) {
	valid, err := a.adapter.ValidateCurrentOrganization(ctx, organizationID, key)
	if err != nil {
		return false, fmt.Errorf("principal adapter %q validate current organization: %w", a.kind, err)
	}
	return valid, nil
}

func (a registeredPrincipalAdapter) DeriveCandidates(ctx context.Context, organizationID OrganizationID, source any) (PrincipalCandidateResult, error) {
	result, err := a.adapter.DeriveCandidates(ctx, organizationID, source)
	if err != nil {
		return PrincipalCandidateResult{}, fmt.Errorf("principal adapter %q derive candidates: %w", a.kind, err)
	}
	switch result.Kind() {
	case PrincipalCandidateResultUnsupported:
		return result, nil
	case PrincipalCandidateResultCandidates:
		candidates := result.Candidates()
		if len(candidates) == 0 {
			return PrincipalCandidateResult{}, fmt.Errorf("principal adapter %q returned no candidates", a.kind)
		}
		for _, candidate := range candidates {
			if _, ok := a.registry.principalAdapters[candidate.Kind]; !ok {
				return PrincipalCandidateResult{}, fmt.Errorf("principal adapter %q returned candidate with unregistered kind %q", a.kind, candidate.Kind)
			}
		}
		return result, nil
	default:
		return PrincipalCandidateResult{}, fmt.Errorf("principal adapter %q returned an invalid candidate result", a.kind)
	}
}

type registeredResourceAdapter struct {
	kind    ResourceKind
	adapter ResourceAdapter
}

func (a registeredResourceAdapter) Kind() ResourceKind { return a.kind }

func (a registeredResourceAdapter) Canonicalize(organizationID OrganizationID, input string) (CanonicalizationResult[ResourceKey], error) {
	result, err := a.adapter.Canonicalize(organizationID, input)
	return a.validateResult("canonicalize", result, err)
}

func (a registeredResourceAdapter) ValidateCurrentOrganization(ctx context.Context, organizationID OrganizationID, key ResourceKey) (bool, error) {
	valid, err := a.adapter.ValidateCurrentOrganization(ctx, organizationID, key)
	if err != nil {
		return false, fmt.Errorf("resource adapter %q validate current organization: %w", a.kind, err)
	}
	return valid, nil
}

func (a registeredResourceAdapter) Derive(ctx context.Context, organizationID OrganizationID, source any) (CanonicalizationResult[ResourceKey], error) {
	result, err := a.adapter.Derive(ctx, organizationID, source)
	return a.validateResult("derive", result, err)
}

func (a registeredResourceAdapter) validateResult(operation string, result CanonicalizationResult[ResourceKey], err error) (CanonicalizationResult[ResourceKey], error) {
	if err != nil {
		return CanonicalizationResult[ResourceKey]{}, err
	}
	if _, _, err := result.Key(); err != nil {
		return CanonicalizationResult[ResourceKey]{}, fmt.Errorf("resource adapter %q %s: %w", a.kind, operation, err)
	}
	return result, nil
}

func validateIdentityContract(contract IdentityContract, registry *Registry) error {
	if err := validateIdentifier("identity contract key", string(contract.Key)); err != nil {
		return err
	}
	if len(contract.PrincipalKinds) == 0 {
		return fmt.Errorf("identity contract %q requires principal kinds", contract.Key)
	}
	if len(contract.ResourceKinds) == 0 {
		return fmt.Errorf("identity contract %q requires resource kinds", contract.Key)
	}
	if err := validateUniqueIdentifiers("principal kind", contract.PrincipalKinds, func(v PrincipalKind) string { return string(v) }); err != nil {
		return fmt.Errorf("identity contract %q: %w", contract.Key, err)
	}
	if err := validateUniqueIdentifiers("resource kind", contract.ResourceKinds, func(v ResourceKind) string { return string(v) }); err != nil {
		return fmt.Errorf("identity contract %q: %w", contract.Key, err)
	}
	for _, kind := range contract.PrincipalKinds {
		if _, ok := registry.principalAdapters[kind]; !ok {
			return fmt.Errorf("identity contract %q references unknown principal adapter %q", contract.Key, kind)
		}
	}
	for _, kind := range contract.ResourceKinds {
		if _, ok := registry.resourceAdapters[kind]; !ok {
			return fmt.Errorf("identity contract %q references unknown resource adapter %q", contract.Key, kind)
		}
	}
	return nil
}

func validateDefinition(definition Definition, registry *Registry) (Definition, error) {
	if err := validateIdentifier("definition key", string(definition.Key)); err != nil {
		return Definition{}, err
	}
	if len(definition.PrincipalKinds) == 0 || len(definition.ResourceKinds) == 0 {
		return Definition{}, fmt.Errorf("definition %q requires principal and resource kinds", definition.Key)
	}
	if definition.FailurePolicy != FailurePolicyFailOpen && definition.FailurePolicy != FailurePolicyFailClosed {
		return Definition{}, fmt.Errorf("definition %q has invalid failure policy %q", definition.Key, definition.FailurePolicy)
	}
	note, err := NormalizeExternalNote(definition.DefaultExternalNote)
	if err != nil {
		return Definition{}, fmt.Errorf("definition %q default external note: %w", definition.Key, err)
	}
	owner := strings.TrimSpace(definition.EnforcementOwner)
	if err := validateRequiredText("enforcement owner", owner); err != nil {
		return Definition{}, fmt.Errorf("definition %q: %w", definition.Key, err)
	}
	identity, ok := registry.identityContracts[definition.IdentityContract]
	if !ok {
		return Definition{}, fmt.Errorf("definition %q references unknown identity contract %q", definition.Key, definition.IdentityContract)
	}
	if len(definition.Surfaces) == 0 || len(definition.TransportAdapters) == 0 {
		return Definition{}, fmt.Errorf("definition %q requires surfaces and transport adapters", definition.Key)
	}
	if err := validateUniqueIdentifiers("principal kind", definition.PrincipalKinds, func(v PrincipalKind) string { return string(v) }); err != nil {
		return Definition{}, fmt.Errorf("definition %q: %w", definition.Key, err)
	}
	if err := validateUniqueIdentifiers("resource kind", definition.ResourceKinds, func(v ResourceKind) string { return string(v) }); err != nil {
		return Definition{}, fmt.Errorf("definition %q: %w", definition.Key, err)
	}
	if err := validateUniqueIdentifiers("surface", definition.Surfaces, func(v Surface) string { return string(v) }); err != nil {
		return Definition{}, fmt.Errorf("definition %q: %w", definition.Key, err)
	}
	if err := validateUniqueIdentifiers("transport adapter", definition.TransportAdapters, func(v TransportAdapterKey) string { return string(v) }); err != nil {
		return Definition{}, fmt.Errorf("definition %q: %w", definition.Key, err)
	}
	for _, kind := range definition.PrincipalKinds {
		if !slices.Contains(identity.PrincipalKinds, kind) {
			return Definition{}, fmt.Errorf("definition %q principal kind %q is outside identity contract %q", definition.Key, kind, identity.Key)
		}
	}
	for _, kind := range definition.ResourceKinds {
		if !slices.Contains(identity.ResourceKinds, kind) {
			return Definition{}, fmt.Errorf("definition %q resource kind %q is outside identity contract %q", definition.Key, kind, identity.Key)
		}
	}
	for _, surface := range definition.Surfaces {
		if _, ok := registry.surfaces[surface]; !ok {
			return Definition{}, fmt.Errorf("definition %q references unknown surface %q", definition.Key, surface)
		}
	}
	for _, adapter := range definition.TransportAdapters {
		if _, ok := registry.transportAdapters[adapter]; !ok {
			return Definition{}, fmt.Errorf("definition %q references unknown transport adapter %q", definition.Key, adapter)
		}
	}
	definition.DefaultExternalNote = note
	definition.EnforcementOwner = owner
	definition.PrincipalKinds = sortedClone(definition.PrincipalKinds)
	definition.ResourceKinds = sortedClone(definition.ResourceKinds)
	definition.Surfaces = sortedClone(definition.Surfaces)
	definition.TransportAdapters = sortedClone(definition.TransportAdapters)
	return definition, nil
}

func validateCoverage(contract CoverageContract, registry *Registry) (CoverageContract, error) {
	definition, ok := registry.definitions[contract.Definition]
	if !ok {
		return CoverageContract{}, fmt.Errorf("coverage references unknown definition %q", contract.Definition)
	}
	if _, ok := registry.surfaces[contract.Surface]; !ok {
		return CoverageContract{}, fmt.Errorf("coverage for definition %q references unknown surface %q", contract.Definition, contract.Surface)
	}
	if !slices.Contains(definition.Surfaces, contract.Surface) {
		return CoverageContract{}, fmt.Errorf("coverage surface %q is not declared by definition %q", contract.Surface, contract.Definition)
	}
	if _, ok := registry.transportAdapters[contract.TransportAdapter]; !ok {
		return CoverageContract{}, fmt.Errorf("coverage references unknown transport adapter %q", contract.TransportAdapter)
	}
	if !slices.Contains(definition.TransportAdapters, contract.TransportAdapter) {
		return CoverageContract{}, fmt.Errorf("coverage transport adapter %q is not declared by definition %q", contract.TransportAdapter, contract.Definition)
	}
	if contract.FailurePolicy != definition.FailurePolicy {
		return CoverageContract{}, fmt.Errorf("coverage failure policy does not match definition %q", contract.Definition)
	}
	identity, ok := registry.identityContracts[contract.IdentityContract]
	if !ok {
		return CoverageContract{}, fmt.Errorf("coverage references unknown identity contract %q", contract.IdentityContract)
	}
	for _, kind := range definition.PrincipalKinds {
		if !slices.Contains(identity.PrincipalKinds, kind) {
			return CoverageContract{}, fmt.Errorf("coverage identity contract %q does not support principal kind %q for definition %q", identity.Key, kind, definition.Key)
		}
	}
	for _, kind := range definition.ResourceKinds {
		if !slices.Contains(identity.ResourceKinds, kind) {
			return CoverageContract{}, fmt.Errorf("coverage identity contract %q does not support resource kind %q for definition %q", identity.Key, kind, definition.Key)
		}
	}

	contract.EnforcementOwner = strings.TrimSpace(contract.EnforcementOwner)
	if err := validateRequiredText("enforcement owner", contract.EnforcementOwner); err != nil {
		return CoverageContract{}, fmt.Errorf("coverage for definition %q and surface %q: %w", contract.Definition, contract.Surface, err)
	}
	contract.PrincipalSource = strings.TrimSpace(contract.PrincipalSource)
	if err := validateRequiredText("principal source", contract.PrincipalSource); err != nil {
		return CoverageContract{}, fmt.Errorf("coverage for definition %q and surface %q: %w", contract.Definition, contract.Surface, err)
	}
	contract.ResourceSource = strings.TrimSpace(contract.ResourceSource)
	if err := validateRequiredText("resource source", contract.ResourceSource); err != nil {
		return CoverageContract{}, fmt.Errorf("coverage for definition %q and surface %q: %w", contract.Definition, contract.Surface, err)
	}
	contract.Checkpoint = strings.TrimSpace(contract.Checkpoint)
	if err := validateRequiredText("checkpoint", contract.Checkpoint); err != nil {
		return CoverageContract{}, fmt.Errorf("coverage for definition %q and surface %q: %w", contract.Definition, contract.Surface, err)
	}
	contract.ProtectedWork = strings.TrimSpace(contract.ProtectedWork)
	if err := validateRequiredText("protected work", contract.ProtectedWork); err != nil {
		return CoverageContract{}, fmt.Errorf("coverage for definition %q and surface %q: %w", contract.Definition, contract.Surface, err)
	}
	return contract, nil
}

func validateCoverageCompleteness(registry *Registry) error {
	for _, definition := range registry.Definitions() {
		usedAdapters := make(map[TransportAdapterKey]struct{}, len(definition.TransportAdapters))
		for _, surface := range definition.Surfaces {
			contract, ok := registry.coverage[coverageKey{definition: definition.Key, surface: surface}]
			if !ok {
				return fmt.Errorf("definition %q has no coverage inventory for surface %q", definition.Key, surface)
			}
			usedAdapters[contract.TransportAdapter] = struct{}{}
		}
		for _, adapter := range definition.TransportAdapters {
			if _, ok := usedAdapters[adapter]; !ok {
				return fmt.Errorf("definition %q transport adapter %q has no coverage inventory", definition.Key, adapter)
			}
		}
	}
	return nil
}

// Definition returns a defensive copy of a definition.
func (r *Registry) Definition(key DefinitionKey) (Definition, bool) {
	definition, ok := r.definitions[key]
	return cloneDefinition(definition), ok
}

// Definitions returns sorted defensive copies of all definitions.
func (r *Registry) Definitions() []Definition {
	result := make([]Definition, 0, len(r.definitions))
	for _, definition := range r.definitions {
		result = append(result, cloneDefinition(definition))
	}
	slices.SortFunc(result, func(a, b Definition) int { return cmp.Compare(a.Key, b.Key) })
	return result
}

// IdentityContract returns a defensive copy of an identity contract.
func (r *Registry) IdentityContract(key IdentityContractKey) (IdentityContract, bool) {
	contract, ok := r.identityContracts[key]
	return cloneIdentityContract(contract), ok
}

// IdentityContracts returns sorted defensive copies of all identity contracts.
func (r *Registry) IdentityContracts() []IdentityContract {
	result := make([]IdentityContract, 0, len(r.identityContracts))
	for _, contract := range r.identityContracts {
		result = append(result, cloneIdentityContract(contract))
	}
	slices.SortFunc(result, func(a, b IdentityContract) int { return cmp.Compare(a.Key, b.Key) })
	return result
}

// PrincipalAdapter returns the registered adapter for a principal kind.
func (r *Registry) PrincipalAdapter(kind PrincipalKind) (PrincipalAdapter, bool) {
	adapter, ok := r.principalAdapters[kind]
	return adapter, ok
}

// PrincipalKinds returns all registered principal adapter kinds in sorted order.
func (r *Registry) PrincipalKinds() []PrincipalKind {
	return slices.Sorted(maps.Keys(r.principalAdapters))
}

// ResourceAdapter returns the registered adapter for a resource kind.
func (r *Registry) ResourceAdapter(kind ResourceKind) (ResourceAdapter, bool) {
	adapter, ok := r.resourceAdapters[kind]
	return adapter, ok
}

// ResourceKinds returns all registered resource adapter kinds in sorted order.
func (r *Registry) ResourceKinds() []ResourceKind {
	return slices.Sorted(maps.Keys(r.resourceAdapters))
}

// Surfaces returns all registered surfaces in sorted order.
func (r *Registry) Surfaces() []Surface {
	return slices.Sorted(maps.Keys(r.surfaces))
}

// HasSurface reports whether a surface is registered.
func (r *Registry) HasSurface(surface Surface) bool {
	_, ok := r.surfaces[surface]
	return ok
}

// TransportAdapters returns all registered transport adapter keys in sorted order.
func (r *Registry) TransportAdapters() []TransportAdapterKey {
	return slices.Sorted(maps.Keys(r.transportAdapters))
}

// TransportAdapter returns registered neutral transport behavior.
func (r *Registry) TransportAdapter(key TransportAdapterKey) (TransportAdapter, bool) {
	adapter, ok := r.transportAdapters[key]
	return adapter, ok
}

// HasTransportAdapter reports whether a transport adapter is registered.
func (r *Registry) HasTransportAdapter(adapter TransportAdapterKey) bool {
	_, ok := r.transportAdapters[adapter]
	return ok
}

// Coverage returns the inventory entry for a definition and surface.
func (r *Registry) Coverage(definition DefinitionKey, surface Surface) (CoverageContract, bool) {
	contract, ok := r.coverage[coverageKey{definition: definition, surface: surface}]
	return contract, ok
}

// CoverageInventory returns all inventory entries sorted by definition and surface.
func (r *Registry) CoverageInventory() []CoverageContract {
	result := make([]CoverageContract, 0, len(r.coverage))
	for _, contract := range r.coverage {
		result = append(result, contract)
	}
	slices.SortFunc(result, func(a, b CoverageContract) int {
		if order := cmp.Compare(a.Definition, b.Definition); order != 0 {
			return order
		}
		return cmp.Compare(a.Surface, b.Surface)
	})
	return result
}

func cloneDefinition(definition Definition) Definition {
	definition.PrincipalKinds = slices.Clone(definition.PrincipalKinds)
	definition.ResourceKinds = slices.Clone(definition.ResourceKinds)
	definition.Surfaces = slices.Clone(definition.Surfaces)
	definition.TransportAdapters = slices.Clone(definition.TransportAdapters)
	return definition
}

func normalizeIdentityContract(contract IdentityContract) IdentityContract {
	contract.PrincipalKinds = sortedClone(contract.PrincipalKinds)
	contract.ResourceKinds = sortedClone(contract.ResourceKinds)
	return contract
}

func cloneIdentityContract(contract IdentityContract) IdentityContract {
	contract.PrincipalKinds = slices.Clone(contract.PrincipalKinds)
	contract.ResourceKinds = slices.Clone(contract.ResourceKinds)
	return contract
}

func sortedClone[S ~[]E, E ~string](values S) S {
	result := slices.Clone(values)
	slices.Sort(result)
	return result
}

func validateUniqueIdentifiers[T comparable](label string, values []T, stringify func(T) string) error {
	seen := make(map[T]struct{}, len(values))
	for _, value := range values {
		if err := validateIdentifier(label, stringify(value)); err != nil {
			return err
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("duplicate %s %q", label, stringify(value))
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateIdentifier(label, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", label)
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", label)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not have surrounding whitespace", label)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s contains a control character", label)
		}
	}
	return nil
}

func validateRequiredText(label, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", label)
	}
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			return fmt.Errorf("%s contains a disallowed control character", label)
		}
	}
	return nil
}
