package killswitches

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

type fakePrincipalAdapter struct {
	kind             PrincipalKind
	canonicalize     func(OrganizationID, string) (CanonicalizationResult[PrincipalKey], error)
	validate         func(context.Context, OrganizationID, PrincipalKey) (bool, error)
	deriveCandidates func(context.Context, OrganizationID, any) (PrincipalCandidateResult, error)
}

func (f *fakePrincipalAdapter) Kind() PrincipalKind { return f.kind }
func (f *fakePrincipalAdapter) Canonicalize(org OrganizationID, input string) (CanonicalizationResult[PrincipalKey], error) {
	if f.canonicalize != nil {
		return f.canonicalize(org, input)
	}
	return NewCanonicalizationResult(PrincipalKey(strings.ToLower(strings.TrimSpace(input))))
}
func (f *fakePrincipalAdapter) ValidateCurrentOrganization(ctx context.Context, org OrganizationID, key PrincipalKey) (bool, error) {
	if f.validate != nil {
		return f.validate(ctx, org, key)
	}
	return true, nil
}
func (f *fakePrincipalAdapter) DeriveCandidates(ctx context.Context, org OrganizationID, source any) (PrincipalCandidateResult, error) {
	if f.deriveCandidates != nil {
		return f.deriveCandidates(ctx, org, source)
	}
	return NewPrincipalCandidateResult([]PrincipalCandidate{{Kind: f.kind, Key: "user:alpha"}})
}

type fakeResourceAdapter struct {
	kind         ResourceKind
	canonicalize func(OrganizationID, string) (CanonicalizationResult[ResourceKey], error)
	validate     func(context.Context, OrganizationID, ResourceKey) (bool, error)
	derive       func(context.Context, OrganizationID, any) (CanonicalizationResult[ResourceKey], error)
}

func (f *fakeResourceAdapter) Kind() ResourceKind { return f.kind }
func (f *fakeResourceAdapter) Canonicalize(org OrganizationID, input string) (CanonicalizationResult[ResourceKey], error) {
	if f.canonicalize != nil {
		return f.canonicalize(org, input)
	}
	return NewCanonicalizationResult(ResourceKey(string(org) + ":" + strings.ToLower(strings.TrimSpace(input))))
}
func (f *fakeResourceAdapter) ValidateCurrentOrganization(ctx context.Context, org OrganizationID, key ResourceKey) (bool, error) {
	if f.validate != nil {
		return f.validate(ctx, org, key)
	}
	return true, nil
}
func (f *fakeResourceAdapter) Derive(ctx context.Context, org OrganizationID, source any) (CanonicalizationResult[ResourceKey], error) {
	if f.derive != nil {
		return f.derive(ctx, org, source)
	}
	return NewCanonicalizationResult(ResourceKey(string(org) + ":derived"))
}

func TestBuildRegistryFinalizesImmutableSortedInventory(t *testing.T) {
	t.Parallel()

	input := validRegistration()
	input.IdentityContracts = append(input.IdentityContracts, IdentityContract{
		Key: "surface-actor-tool", PrincipalKinds: []PrincipalKind{"service", "user"}, ResourceKinds: []ResourceKind{"tool"},
	})
	input.Coverage[0].IdentityContract = "surface-actor-tool"
	input.Coverage[0].EnforcementOwner = "mcp-enforcement"
	registry, err := BuildRegistry(input)
	if err != nil {
		t.Fatalf("build registry: %v", err)
	}

	input.Definitions[0].PrincipalKinds[0] = "mutated"
	input.Definitions[0].Surfaces[0] = "mutated"
	input.IdentityContracts[0].ResourceKinds[0] = "mutated"
	input.Coverage[0].Checkpoint = "mutated"

	definition, ok := registry.Definition("block-tools")
	if !ok {
		t.Fatal("definition not found")
	}
	if !slices.Equal(definition.PrincipalKinds, []PrincipalKind{"service", "user"}) {
		t.Fatalf("principal kinds not sorted and immutable: %v", definition.PrincipalKinds)
	}
	if !slices.Equal(definition.Surfaces, []Surface{"api", "mcp"}) {
		t.Fatalf("surfaces not sorted and immutable: %v", definition.Surfaces)
	}
	if definition.DefaultExternalNote != "Access paused." || definition.EnforcementOwner != "security" {
		t.Fatalf("normalized definition mismatch: %+v", definition)
	}
	definition.PrincipalKinds[0] = "again"
	again, _ := registry.Definition("block-tools")
	if again.PrincipalKinds[0] != "service" {
		t.Fatal("definition lookup leaked a mutable slice")
	}

	identity, ok := registry.IdentityContract("actor-tool")
	if !ok || !slices.Equal(identity.ResourceKinds, []ResourceKind{"tool"}) {
		t.Fatalf("identity contract mismatch: %+v", identity)
	}
	identity.ResourceKinds[0] = "again"
	againIdentity, _ := registry.IdentityContract("actor-tool")
	if againIdentity.ResourceKinds[0] != "tool" {
		t.Fatal("identity lookup leaked a mutable slice")
	}

	definitions := registry.Definitions()
	definitions[0].Surfaces[0] = "again"
	if after, _ := registry.Definition("block-tools"); after.Surfaces[0] != "api" {
		t.Fatal("definitions list leaked a mutable slice")
	}
	if got := registry.PrincipalKinds(); !slices.Equal(got, []PrincipalKind{"service", "user"}) {
		t.Fatalf("principal kinds not sorted: %v", got)
	}
	if got := registry.ResourceKinds(); !slices.Equal(got, []ResourceKind{"tool"}) {
		t.Fatalf("resource kinds mismatch: %v", got)
	}
	if got := registry.Surfaces(); !slices.Equal(got, []Surface{"api", "mcp"}) {
		t.Fatalf("surfaces not sorted: %v", got)
	}
	if got := registry.TransportAdapters(); !slices.Equal(got, []TransportAdapterKey{"http", "jsonrpc"}) {
		t.Fatalf("transport adapters not sorted: %v", got)
	}
	if !registry.HasSurface("mcp") || registry.HasSurface("unknown") {
		t.Fatal("surface lookup mismatch")
	}
	if !registry.HasTransportAdapter("jsonrpc") || registry.HasTransportAdapter("unknown") {
		t.Fatal("transport adapter lookup mismatch")
	}
	contracts := registry.IdentityContracts()
	contracts[0].PrincipalKinds[0] = "again"
	if after := registry.IdentityContracts(); after[0].PrincipalKinds[0] != "service" {
		t.Fatal("identity contracts list leaked a mutable slice")
	}

	coverage := registry.CoverageInventory()
	if len(coverage) != 2 || coverage[0].Surface != "api" || coverage[1].Surface != "mcp" {
		t.Fatalf("coverage inventory is not sorted: %+v", coverage)
	}
	if coverage[0].Checkpoint == "mutated" {
		t.Fatal("coverage retained caller mutation")
	}
	mcpCoverage, ok := registry.Coverage("block-tools", "mcp")
	if !ok || mcpCoverage.EnforcementOwner != "mcp-enforcement" || mcpCoverage.IdentityContract != "surface-actor-tool" {
		t.Fatalf("surface-specific owner or identity contract was not preserved: %+v", mcpCoverage)
	}
	if _, ok := registry.Definition("unknown"); ok {
		t.Fatal("unknown definition found")
	}
	if _, ok := registry.Coverage("block-tools", "unknown"); ok {
		t.Fatal("unknown coverage found")
	}
}

func TestBuildRegistryRejectsInvalidRegistration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Registration)
	}{
		{name: "no definitions", mutate: func(r *Registration) { r.Definitions, r.Coverage = nil, nil }},
		{name: "duplicate definition", mutate: func(r *Registration) { r.Definitions = append(r.Definitions, r.Definitions[0]) }},
		{name: "duplicate identity contract", mutate: func(r *Registration) { r.IdentityContracts = append(r.IdentityContracts, r.IdentityContracts[0]) }},
		{name: "duplicate principal adapter", mutate: func(r *Registration) { r.PrincipalAdapters = append(r.PrincipalAdapters, r.PrincipalAdapters[0]) }},
		{name: "duplicate resource adapter", mutate: func(r *Registration) { r.ResourceAdapters = append(r.ResourceAdapters, r.ResourceAdapters[0]) }},
		{name: "duplicate surface", mutate: func(r *Registration) { r.Surfaces = append(r.Surfaces, r.Surfaces[0]) }},
		{name: "duplicate transport", mutate: func(r *Registration) { r.TransportAdapters = append(r.TransportAdapters, r.TransportAdapters[0]) }},
		{name: "duplicate coverage", mutate: func(r *Registration) { r.Coverage = append(r.Coverage, r.Coverage[0]) }},
		{name: "nil principal adapter", mutate: func(r *Registration) { r.PrincipalAdapters[0].Adapter = nil }},
		{name: "typed nil principal adapter", mutate: func(r *Registration) { r.PrincipalAdapters[0].Adapter = (*fakePrincipalAdapter)(nil) }},
		{name: "nil resource adapter", mutate: func(r *Registration) { r.ResourceAdapters[0].Adapter = nil }},
		{name: "typed nil resource adapter", mutate: func(r *Registration) { r.ResourceAdapters[0].Adapter = (*fakeResourceAdapter)(nil) }},
		{name: "empty principal adapter key", mutate: func(r *Registration) { r.PrincipalAdapters[0].Adapter = &fakePrincipalAdapter{} }},
		{name: "empty resource adapter key", mutate: func(r *Registration) { r.ResourceAdapters[0].Adapter = &fakeResourceAdapter{} }},
		{name: "principal adapter no fixtures", mutate: func(r *Registration) { r.PrincipalAdapters[0].Fixtures = nil }},
		{name: "resource adapter no fixtures", mutate: func(r *Registration) { r.ResourceAdapters[0].Fixtures = nil }},
		{name: "principal fixture invalid organization", mutate: func(r *Registration) { r.PrincipalAdapters[0].Fixtures[0].OrganizationID = "" }},
		{name: "resource fixture invalid expected result", mutate: func(r *Registration) {
			r.ResourceAdapters[0].Fixtures[0].Expected = CanonicalizationResult[ResourceKey]{}
		}},
		{name: "empty surface", mutate: func(r *Registration) { r.Surfaces[0] = "" }},
		{name: "empty transport", mutate: func(r *Registration) { r.TransportAdapters[0].Key = "" }},
		{name: "nil transport behavior", mutate: func(r *Registration) { r.TransportAdapters[0].Adapter = nil }},
		{name: "identity empty key", mutate: func(r *Registration) { r.IdentityContracts[0].Key = "" }},
		{name: "identity no principal kinds", mutate: func(r *Registration) { r.IdentityContracts[0].PrincipalKinds = nil }},
		{name: "identity no resource kinds", mutate: func(r *Registration) { r.IdentityContracts[0].ResourceKinds = nil }},
		{name: "identity unknown principal", mutate: func(r *Registration) { r.IdentityContracts[0].PrincipalKinds[0] = "missing" }},
		{name: "identity unknown resource", mutate: func(r *Registration) { r.IdentityContracts[0].ResourceKinds[0] = "missing" }},
		{name: "definition empty key", mutate: func(r *Registration) { r.Definitions[0].Key = "" }},
		{name: "definition no principal kinds", mutate: func(r *Registration) { r.Definitions[0].PrincipalKinds = nil }},
		{name: "definition no resource kinds", mutate: func(r *Registration) { r.Definitions[0].ResourceKinds = nil }},
		{name: "definition invalid policy", mutate: func(r *Registration) { r.Definitions[0].FailurePolicy = "" }},
		{name: "definition blank note", mutate: func(r *Registration) { r.Definitions[0].DefaultExternalNote = " " }},
		{name: "definition blank owner", mutate: func(r *Registration) { r.Definitions[0].EnforcementOwner = " " }},
		{name: "definition unknown identity", mutate: func(r *Registration) { r.Definitions[0].IdentityContract = "missing" }},
		{name: "definition no surfaces", mutate: func(r *Registration) { r.Definitions[0].Surfaces = nil }},
		{name: "definition unknown surface", mutate: func(r *Registration) { r.Definitions[0].Surfaces[0] = "missing" }},
		{name: "definition no transports", mutate: func(r *Registration) { r.Definitions[0].TransportAdapters = nil }},
		{name: "definition unknown transport", mutate: func(r *Registration) { r.Definitions[0].TransportAdapters[0] = "missing" }},
		{name: "definition principal outside identity", mutate: func(r *Registration) { r.Definitions[0].PrincipalKinds[0] = "other" }},
		{name: "coverage unknown definition", mutate: func(r *Registration) { r.Coverage[0].Definition = "missing" }},
		{name: "coverage unknown surface", mutate: func(r *Registration) { r.Coverage[0].Surface = "missing" }},
		{name: "coverage undeclared surface", mutate: func(r *Registration) { r.Surfaces = append(r.Surfaces, "other"); r.Coverage[0].Surface = "other" }},
		{name: "coverage unknown transport", mutate: func(r *Registration) { r.Coverage[0].TransportAdapter = "missing" }},
		{name: "coverage wrong policy", mutate: func(r *Registration) { r.Coverage[0].FailurePolicy = FailurePolicyFailClosed }},
		{name: "coverage unknown identity", mutate: func(r *Registration) { r.Coverage[0].IdentityContract = "missing" }},
		{name: "coverage identity lacks definition kind", mutate: func(r *Registration) {
			r.IdentityContracts = append(r.IdentityContracts, IdentityContract{Key: "user-tool", PrincipalKinds: []PrincipalKind{"user"}, ResourceKinds: []ResourceKind{"tool"}})
			r.Coverage[0].IdentityContract = "user-tool"
		}},
		{name: "coverage no owner", mutate: func(r *Registration) { r.Coverage[0].EnforcementOwner = " " }},
		{name: "coverage no principal source", mutate: func(r *Registration) { r.Coverage[0].PrincipalSource = " " }},
		{name: "coverage no resource source", mutate: func(r *Registration) { r.Coverage[0].ResourceSource = " " }},
		{name: "coverage no checkpoint", mutate: func(r *Registration) { r.Coverage[0].Checkpoint = " " }},
		{name: "coverage no protected work", mutate: func(r *Registration) { r.Coverage[0].ProtectedWork = " " }},
		{name: "missing surface coverage", mutate: func(r *Registration) { r.Coverage = r.Coverage[:1] }},
		{name: "unused definition transport", mutate: func(r *Registration) {
			r.TransportAdapters = append(r.TransportAdapters, TransportAdapterRegistration{Key: "unused", Adapter: ResolveTransportDisposition})
			r.Definitions[0].TransportAdapters = append(r.Definitions[0].TransportAdapters, "unused")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := validRegistration()
			tt.mutate(&input)
			if _, err := BuildRegistry(input); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestBuildRegistryValidatesCanonicalizationFixturesRepeatedly(t *testing.T) {
	t.Parallel()

	principalCalls, resourceCalls := 0, 0
	input := validRegistration()
	principal := registeredFakePrincipal(input.PrincipalAdapters[0])
	principal.canonicalize = func(OrganizationID, string) (CanonicalizationResult[PrincipalKey], error) {
		principalCalls++
		return NewCanonicalizationResult(PrincipalKey("user:alpha"))
	}
	resource := registeredFakeResource(input.ResourceAdapters[0])
	resource.canonicalize = func(OrganizationID, string) (CanonicalizationResult[ResourceKey], error) {
		resourceCalls++
		return NewCanonicalizationResult(ResourceKey("org:test:tool:alpha"))
	}

	if _, err := BuildRegistry(input); err != nil {
		t.Fatalf("build registry: %v", err)
	}
	if principalCalls != canonicalizationFixtureAttempts || resourceCalls != canonicalizationFixtureAttempts {
		t.Fatalf("fixture calls principal=%d resource=%d, want %d each", principalCalls, resourceCalls, canonicalizationFixtureAttempts)
	}
}

func TestBuildRegistryAcceptsSupportedAndUnsupportedCanonicalizationFixtures(t *testing.T) {
	t.Parallel()

	input := validRegistration()
	principal := registeredFakePrincipal(input.PrincipalAdapters[0])
	principal.canonicalize = func(OrganizationID, string) (CanonicalizationResult[PrincipalKey], error) {
		return UnsupportedCanonicalizationResult[PrincipalKey](), nil
	}
	input.PrincipalAdapters[0].Fixtures[0].Expected = UnsupportedCanonicalizationResult[PrincipalKey]()
	resource := registeredFakeResource(input.ResourceAdapters[0])
	resource.canonicalize = func(OrganizationID, string) (CanonicalizationResult[ResourceKey], error) {
		return UnsupportedCanonicalizationResult[ResourceKey](), nil
	}
	input.ResourceAdapters[0].Fixtures[0].Expected = UnsupportedCanonicalizationResult[ResourceKey]()

	if _, err := BuildRegistry(input); err != nil {
		t.Fatalf("build registry: %v", err)
	}
}

func TestBuildRegistryRejectsBadCanonicalizationFixtureBehavior(t *testing.T) {
	t.Parallel()

	infrastructureErr := errors.New("canonicalization unavailable")
	tests := []struct {
		name   string
		mutate func(*Registration)
	}{
		{name: "principal expected mismatch", mutate: func(r *Registration) {
			r.PrincipalAdapters[0].Fixtures[0].Expected = UnsupportedCanonicalizationResult[PrincipalKey]()
		}},
		{name: "resource expected mismatch", mutate: func(r *Registration) {
			r.ResourceAdapters[0].Fixtures[0].Expected = UnsupportedCanonicalizationResult[ResourceKey]()
		}},
		{name: "principal error", mutate: func(r *Registration) {
			registeredFakePrincipal(r.PrincipalAdapters[0]).canonicalize = func(OrganizationID, string) (CanonicalizationResult[PrincipalKey], error) {
				return CanonicalizationResult[PrincipalKey]{}, infrastructureErr
			}
		}},
		{name: "resource invalid zero result", mutate: func(r *Registration) {
			registeredFakeResource(r.ResourceAdapters[0]).canonicalize = func(OrganizationID, string) (CanonicalizationResult[ResourceKey], error) {
				return CanonicalizationResult[ResourceKey]{}, nil
			}
		}},
		{name: "principal unstable supported state", mutate: func(r *Registration) {
			calls := 0
			registeredFakePrincipal(r.PrincipalAdapters[0]).canonicalize = func(OrganizationID, string) (CanonicalizationResult[PrincipalKey], error) {
				calls++
				if calls%2 == 0 {
					return UnsupportedCanonicalizationResult[PrincipalKey](), nil
				}
				return NewCanonicalizationResult(PrincipalKey("user:alpha"))
			}
		}},
		{name: "resource unstable key", mutate: func(r *Registration) {
			calls := 0
			registeredFakeResource(r.ResourceAdapters[0]).canonicalize = func(OrganizationID, string) (CanonicalizationResult[ResourceKey], error) {
				calls++
				return NewCanonicalizationResult(ResourceKey(fmt.Sprintf("org:test:tool:%d", calls%2)))
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := validRegistration()
			tt.mutate(&input)
			if _, err := BuildRegistry(input); err == nil {
				t.Fatal("expected canonicalization fixture error")
			}
		})
	}
}

func TestAdapterResultContracts(t *testing.T) {
	t.Parallel()

	canonical, err := NewCanonicalizationResult(PrincipalKey("user:alpha"))
	if err != nil {
		t.Fatalf("canonical result: %v", err)
	}
	key, supported, err := canonical.Key()
	if err != nil || !supported || key != "user:alpha" {
		t.Fatalf("canonical key=%q supported=%v err=%v", key, supported, err)
	}
	if _, err := NewCanonicalizationResult(PrincipalKey("")); err == nil {
		t.Fatal("empty canonical principal key accepted")
	}
	if _, supported, err := UnsupportedCanonicalizationResult[PrincipalKey]().Key(); err != nil || supported {
		t.Fatalf("unsupported canonicalization supported=%v err=%v", supported, err)
	}
	if _, _, err := (CanonicalizationResult[PrincipalKey]{}).Key(); err == nil {
		t.Fatal("zero canonicalization result accepted")
	}

	if _, err := NewPrincipalCandidateResult(nil); err == nil {
		t.Fatal("empty successful candidate result accepted")
	}
	candidates, err := NewPrincipalCandidateResult([]PrincipalCandidate{
		{Kind: "service", Key: "service:b"},
		{Kind: "user", Key: "user:a"},
		{Kind: "service", Key: "service:b"},
	})
	if err != nil {
		t.Fatalf("candidate result: %v", err)
	}
	want := []PrincipalCandidate{{Kind: "service", Key: "service:b"}, {Kind: "user", Key: "user:a"}}
	got := candidates.Candidates()
	if !slices.Equal(got, want) {
		t.Fatalf("candidate order got %+v, want %+v", got, want)
	}
	got[0].Key = "mutated"
	if candidates.Candidates()[0].Key != "service:b" {
		t.Fatal("candidate result leaked mutable slice")
	}
}

func TestRegisteredAdaptersValidateDerivedResults(t *testing.T) {
	t.Parallel()

	input := validRegistration()
	principal, ok := input.PrincipalAdapters[0].Adapter.(*fakePrincipalAdapter)
	if !ok {
		t.Fatal("principal adapter has unexpected type")
	}
	principal.deriveCandidates = func(context.Context, OrganizationID, any) (PrincipalCandidateResult, error) {
		return NewPrincipalCandidateResult([]PrincipalCandidate{
			{Kind: "service", Key: "service:first"},
			{Kind: "user", Key: "user:second"},
		})
	}
	registry, err := BuildRegistry(input)
	if err != nil {
		t.Fatalf("build registry: %v", err)
	}
	registeredPrincipal, _ := registry.PrincipalAdapter("user")
	result, err := registeredPrincipal.DeriveCandidates(context.Background(), "org:test", struct{}{})
	if err != nil {
		t.Fatalf("derive registered candidates: %v", err)
	}
	want := []PrincipalCandidate{{Kind: "service", Key: "service:first"}, {Kind: "user", Key: "user:second"}}
	if got := result.Candidates(); !slices.Equal(got, want) {
		t.Fatalf("candidate order got %+v, want %+v", got, want)
	}

	badInput := validRegistration()
	badPrincipal, ok := badInput.PrincipalAdapters[0].Adapter.(*fakePrincipalAdapter)
	if !ok {
		t.Fatal("principal adapter has unexpected type")
	}
	badPrincipal.deriveCandidates = func(context.Context, OrganizationID, any) (PrincipalCandidateResult, error) {
		return NewPrincipalCandidateResult([]PrincipalCandidate{{Kind: "unknown", Key: "unknown:key"}})
	}
	badRegistry, err := BuildRegistry(badInput)
	if err != nil {
		t.Fatalf("build bad-result registry: %v", err)
	}
	registeredPrincipal, _ = badRegistry.PrincipalAdapter("user")
	if _, err := registeredPrincipal.DeriveCandidates(context.Background(), "org:test", struct{}{}); err == nil {
		t.Fatal("unregistered candidate kind accepted")
	}
}

func TestRegisteredResourceAdapterDerivationContract(t *testing.T) {
	t.Parallel()

	infrastructureErr := errors.New("resource store unavailable")
	tests := []struct {
		name          string
		derive        func(context.Context, OrganizationID, any) (CanonicalizationResult[ResourceKey], error)
		wantKey       ResourceKey
		wantSupported bool
		wantError     error
	}{
		{name: "supported", derive: func(context.Context, OrganizationID, any) (CanonicalizationResult[ResourceKey], error) {
			return NewCanonicalizationResult(ResourceKey("tool:search"))
		}, wantKey: "tool:search", wantSupported: true},
		{name: "unsupported", derive: func(context.Context, OrganizationID, any) (CanonicalizationResult[ResourceKey], error) {
			return UnsupportedCanonicalizationResult[ResourceKey](), nil
		}},
		{name: "infrastructure error", derive: func(context.Context, OrganizationID, any) (CanonicalizationResult[ResourceKey], error) {
			return CanonicalizationResult[ResourceKey]{}, infrastructureErr
		}, wantError: infrastructureErr},
		{name: "invalid zero result", derive: func(context.Context, OrganizationID, any) (CanonicalizationResult[ResourceKey], error) {
			return CanonicalizationResult[ResourceKey]{}, nil
		}, wantError: errors.New("contract error")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := validRegistration()
			resource, ok := input.ResourceAdapters[0].Adapter.(*fakeResourceAdapter)
			if !ok {
				t.Fatal("resource adapter has unexpected type")
			}
			resource.derive = tt.derive
			registry, err := BuildRegistry(input)
			if err != nil {
				t.Fatalf("build registry: %v", err)
			}
			adapter, _ := registry.ResourceAdapter("tool")
			result, err := adapter.Derive(context.Background(), "org:test", struct{}{})
			if tt.wantError != nil {
				if err == nil {
					t.Fatal("expected derivation error")
				}
				if errors.Is(tt.wantError, infrastructureErr) && !errors.Is(err, infrastructureErr) {
					t.Fatalf("derivation error got %v", err)
				}
				return
			}
			key, supported, err := result.Key()
			if err != nil || key != tt.wantKey || supported != tt.wantSupported {
				t.Fatalf("derived key=%q supported=%v err=%v", key, supported, err)
			}
		})
	}
}

func TestRegisteredTransportAdapterResolvesNeutralBehaviorMatrix(t *testing.T) {
	t.Parallel()

	registry, err := BuildRegistry(validRegistration())
	if err != nil {
		t.Fatalf("build registry: %v", err)
	}
	adapter, ok := registry.TransportAdapter("jsonrpc")
	if !ok {
		t.Fatal("transport adapter not found")
	}
	match, _ := NewMatchResult(testPrescriptionID, "Access paused.")
	noMatch, _ := NewNoMatchResult(NoMatchReasonNoPrescription)
	failure, _ := NewInfrastructureFailureResult(errors.New("evaluation unavailable"))
	tests := []struct {
		name        string
		result      EvaluationResult
		policy      FailurePolicy
		wantKind    TransportDispositionKind
		wantNote    string
		wantHasNote bool
	}{
		{name: "match fail open", result: match, policy: FailurePolicyFailOpen, wantKind: TransportDispositionMatchedDenial, wantNote: "Access paused.", wantHasNote: true},
		{name: "match fail closed", result: match, policy: FailurePolicyFailClosed, wantKind: TransportDispositionMatchedDenial, wantNote: "Access paused.", wantHasNote: true},
		{name: "no match fail open", result: noMatch, policy: FailurePolicyFailOpen, wantKind: TransportDispositionContinue},
		{name: "no match fail closed", result: noMatch, policy: FailurePolicyFailClosed, wantKind: TransportDispositionContinue},
		{name: "failure fail open", result: failure, policy: FailurePolicyFailOpen, wantKind: TransportDispositionContinue},
		{name: "failure fail closed", result: failure, policy: FailurePolicyFailClosed, wantKind: TransportDispositionInfrastructureRejection},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			disposition, err := adapter(tt.result, tt.policy)
			if err != nil {
				t.Fatalf("adapt: %v", err)
			}
			if disposition.Kind() != tt.wantKind {
				t.Fatalf("kind got %q, want %q", disposition.Kind(), tt.wantKind)
			}
			note, hasNote := disposition.ExternalNote()
			if note != tt.wantNote || hasNote != tt.wantHasNote {
				t.Fatalf("note got %q,%t want %q,%t", note, hasNote, tt.wantNote, tt.wantHasNote)
			}
		})
	}
	if _, err := adapter(EvaluationResult{}, FailurePolicyFailOpen); err == nil {
		t.Fatal("zero evaluation result accepted")
	}
	if _, err := adapter(noMatch, FailurePolicy("other")); err == nil {
		t.Fatal("invalid failure policy accepted")
	}
}

func TestRegisteredTransportAdapterRejectsOffMatrixDispositions(t *testing.T) {
	t.Parallel()

	match, _ := NewMatchResult(testPrescriptionID, "Access paused.")
	noMatch, _ := NewNoMatchResult(NoMatchReasonNoPrescription)
	failure, _ := NewInfrastructureFailureResult(errors.New("evaluation unavailable"))
	otherMatchedDenial, _ := NewMatchedDenialDisposition("Different note.")
	tests := []struct {
		name        string
		result      EvaluationResult
		policy      FailurePolicy
		disposition TransportDisposition
	}{
		{name: "invalid shape", result: noMatch, policy: FailurePolicyFailOpen, disposition: TransportDisposition{}},
		{name: "match continues", result: match, policy: FailurePolicyFailOpen, disposition: NewContinueDisposition()},
		{name: "match note replaced", result: match, policy: FailurePolicyFailClosed, disposition: otherMatchedDenial},
		{name: "no match denied", result: noMatch, policy: FailurePolicyFailClosed, disposition: NewInfrastructureRejectionDisposition()},
		{name: "fail open rejected", result: failure, policy: FailurePolicyFailOpen, disposition: NewInfrastructureRejectionDisposition()},
		{name: "fail closed continues", result: failure, policy: FailurePolicyFailClosed, disposition: NewContinueDisposition()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := validRegistration()
			input.TransportAdapters[0].Adapter = func(EvaluationResult, FailurePolicy) (TransportDisposition, error) {
				return tt.disposition, nil
			}
			registry, err := BuildRegistry(input)
			if err != nil {
				t.Fatalf("build registry: %v", err)
			}
			adapter, _ := registry.TransportAdapter("jsonrpc")
			if _, err := adapter(tt.result, tt.policy); err == nil {
				t.Fatal("off-matrix transport disposition accepted")
			}
		})
	}
}

func TestEvaluationResultsHaveNeutralValidShapes(t *testing.T) {
	t.Parallel()

	match, err := NewMatchResult(PrescriptionID(strings.ToUpper(string(testPrescriptionID))), "  Access paused. ")
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if match.Kind() != EvaluationResultMatch {
		t.Fatalf("match kind: %q", match.Kind())
	}
	if id, ok := match.PrescriptionID(); !ok || id != testPrescriptionID {
		t.Fatalf("match prescription ID: %q, %v", id, ok)
	}
	if note, ok := match.ExternalNote(); !ok || note != "Access paused." {
		t.Fatalf("match external note: %q, %v", note, ok)
	}
	if _, ok := match.NoMatchReason(); ok || match.InfrastructureError() != nil {
		t.Fatal("match exposed non-match metadata")
	}
	if _, err := NewMatchResult("not-a-uuid", "note"); err == nil {
		t.Fatal("match with invalid prescription UUID accepted")
	}
	if _, err := NewMatchResult(testPrescriptionID, " "); err == nil {
		t.Fatal("match without note accepted")
	}

	for _, reason := range []NoMatchReason{NoMatchReasonNoPrescription, NoMatchReasonUnsupportedIdentity, NoMatchReasonUnsupportedResource} {
		noMatch, err := NewNoMatchResult(reason)
		if err != nil {
			t.Fatalf("no match: %v", err)
		}
		if noMatch.Kind() != EvaluationResultNoMatch {
			t.Fatalf("no-match kind: %q", noMatch.Kind())
		}
		if got, ok := noMatch.NoMatchReason(); !ok || got != reason {
			t.Fatalf("no-match reason: %q, %v", got, ok)
		}
		if _, ok := noMatch.ExternalNote(); ok || noMatch.InfrastructureError() != nil {
			t.Fatal("no-match exposed denial or infrastructure metadata")
		}
	}
	if _, err := NewNoMatchResult("other"); err == nil {
		t.Fatal("unknown no-match reason accepted")
	}

	cause := errors.New("database unavailable")
	failure, err := NewInfrastructureFailureResult(cause)
	if err != nil {
		t.Fatalf("infrastructure failure: %v", err)
	}
	if failure.Kind() != EvaluationResultInfrastructureFailure || !errors.Is(failure.InfrastructureError(), cause) {
		t.Fatalf("infrastructure result mismatch: %+v", failure)
	}
	if _, ok := failure.ExternalNote(); ok {
		t.Fatal("infrastructure failure exposed denial language")
	}
	if _, err := NewInfrastructureFailureResult(nil); err == nil {
		t.Fatal("nil infrastructure cause accepted")
	}
	var typedNil *testError
	if _, err := NewInfrastructureFailureResult(typedNil); err == nil {
		t.Fatal("typed-nil infrastructure cause accepted")
	}
}

type testError struct{}

func (*testError) Error() string { return "test error" }

func registeredFakePrincipal(registration PrincipalAdapterRegistration) *fakePrincipalAdapter {
	switch adapter := registration.Adapter.(type) {
	case *fakePrincipalAdapter:
		return adapter
	default:
		panic("principal adapter has unexpected type")
	}
}

func registeredFakeResource(registration ResourceAdapterRegistration) *fakeResourceAdapter {
	switch adapter := registration.Adapter.(type) {
	case *fakeResourceAdapter:
		return adapter
	default:
		panic("resource adapter has unexpected type")
	}
}

func validRegistration() Registration {
	principalUser := &fakePrincipalAdapter{kind: "user"}
	principalService := &fakePrincipalAdapter{kind: "service"}
	resource := &fakeResourceAdapter{kind: "tool"}
	userKey, _ := NewCanonicalizationResult(PrincipalKey("user:alpha"))
	serviceKey, _ := NewCanonicalizationResult(PrincipalKey("service:alpha"))
	resourceKey, _ := NewCanonicalizationResult(ResourceKey("org:test:tool:alpha"))
	return Registration{
		PrincipalAdapters: []PrincipalAdapterRegistration{
			{Adapter: principalUser, Fixtures: []PrincipalCanonicalizationFixture{
				{OrganizationID: "org:test", Input: " User:Alpha ", Expected: userKey},
			}},
			{Adapter: principalService, Fixtures: []PrincipalCanonicalizationFixture{
				{OrganizationID: "org:test", Input: " Service:Alpha ", Expected: serviceKey},
			}},
		},
		ResourceAdapters: []ResourceAdapterRegistration{
			{Adapter: resource, Fixtures: []ResourceCanonicalizationFixture{
				{OrganizationID: "org:test", Input: " Tool:Alpha ", Expected: resourceKey},
			}},
		},
		Surfaces: []Surface{"mcp", "api"},
		TransportAdapters: []TransportAdapterRegistration{
			{Key: "jsonrpc", Adapter: ResolveTransportDisposition},
			{Key: "http", Adapter: ResolveTransportDisposition},
		},
		IdentityContracts: []IdentityContract{{
			Key: "actor-tool", PrincipalKinds: []PrincipalKind{"user", "service"}, ResourceKinds: []ResourceKind{"tool"},
		}},
		Definitions: []Definition{{
			Key: "block-tools", PrincipalKinds: []PrincipalKind{"user", "service"}, ResourceKinds: []ResourceKind{"tool"},
			FailurePolicy: FailurePolicyFailOpen, DefaultExternalNote: "  Access paused. ", EnforcementOwner: " security ",
			IdentityContract: "actor-tool", Surfaces: []Surface{"mcp", "api"}, TransportAdapters: []TransportAdapterKey{"jsonrpc", "http"},
		}},
		Coverage: []CoverageContract{
			{
				Definition: "block-tools", Surface: "mcp", PrincipalSource: " authenticated actor ", ResourceSource: " tool request ",
				Checkpoint: " before dispatch ", ProtectedWork: " tool execution ", FailurePolicy: FailurePolicyFailOpen,
				TransportAdapter: "jsonrpc", EnforcementOwner: "security", IdentityContract: "actor-tool",
			},
			{
				Definition: "block-tools", Surface: "api", PrincipalSource: " authenticated actor ", ResourceSource: " route resource ",
				Checkpoint: " before handler ", ProtectedWork: " handler execution ", FailurePolicy: FailurePolicyFailOpen,
				TransportAdapter: "http", EnforcementOwner: "security", IdentityContract: "actor-tool",
			},
		},
	}
}
