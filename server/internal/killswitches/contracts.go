// Package killswitches defines dependency-light contracts for kill-switch registration,
// identity adaptation, evaluation results, and mutation hashing.
package killswitches

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// DefinitionKey identifies a code-owned kill-switch definition.
type DefinitionKey string

// IdentityContractKey identifies a principal and resource identity contract.
type IdentityContractKey string

// PrincipalKind identifies a canonical principal namespace.
type PrincipalKind string

// PrincipalKey identifies a principal within a principal namespace.
type PrincipalKey string

// ResourceKind identifies a canonical resource namespace.
type ResourceKind string

// ResourceKey identifies a resource within a resource namespace.
type ResourceKey string

type canonicalizationResultState uint8

const (
	canonicalizationResultInvalid canonicalizationResultState = iota
	canonicalizationResultSupported
	canonicalizationResultUnsupported
)

// CanonicalizationResult is an immutable supported or deliberately unsupported
// canonicalization result. Infrastructure failures are returned separately as errors.
// Its zero value is invalid and is rejected by Key and registered adapter wrappers.
type CanonicalizationResult[T ~string] struct {
	key   T
	state canonicalizationResultState
}

// NewCanonicalizationResult constructs a supported result with a nonempty canonical key.
func NewCanonicalizationResult[T ~string](key T) (CanonicalizationResult[T], error) {
	if err := validateIdentifier("canonical key", string(key)); err != nil {
		return CanonicalizationResult[T]{}, err
	}
	return CanonicalizationResult[T]{key: key, state: canonicalizationResultSupported}, nil
}

// UnsupportedCanonicalizationResult reports deliberate absence of supported input.
func UnsupportedCanonicalizationResult[T ~string]() CanonicalizationResult[T] {
	return CanonicalizationResult[T]{key: "", state: canonicalizationResultUnsupported}
}

// Key returns a canonical key for supported input, no key for deliberately unsupported input,
// or an error for an invalid result such as the zero value.
func (r CanonicalizationResult[T]) Key() (T, bool, error) {
	switch r.state {
	case canonicalizationResultSupported:
		return r.key, true, nil
	case canonicalizationResultUnsupported:
		return r.key, false, nil
	default:
		return r.key, false, errors.New("invalid canonicalization result")
	}
}

// OrganizationID identifies the organization against which current references are validated.
type OrganizationID string

// PrescriptionID identifies an existing kill-switch prescription.
type PrescriptionID string

// Surface identifies an enforcement surface.
type Surface string

// TransportAdapterKey identifies a transport-specific result adapter.
type TransportAdapterKey string

// FailurePolicy controls the posture used when evaluation infrastructure fails.
type FailurePolicy string

const (
	// FailurePolicyFailOpen permits protected work when evaluation infrastructure fails.
	FailurePolicyFailOpen FailurePolicy = "fail_open"
	// FailurePolicyFailClosed denies protected work when evaluation infrastructure fails.
	FailurePolicyFailClosed FailurePolicy = "fail_closed"
)

// ResourceScope controls whether a prescription covers all resources or selected resources.
type ResourceScope string

const (
	// ResourceScopeAll covers all resources of the declared resource kind.
	ResourceScopeAll ResourceScope = "all"
	// ResourceScopeSelected covers an explicit nonempty set of resource keys.
	ResourceScopeSelected ResourceScope = "selected"
)

// MutationOperation identifies a lifecycle request in the canonical mutation contract.
type MutationOperation string

const (
	// MutationOperationActivate creates a new active prescription.
	MutationOperationActivate MutationOperation = "activate"
	// MutationOperationChange changes an existing prescription.
	MutationOperationChange MutationOperation = "change"
	// MutationOperationDeactivate deactivates an existing prescription.
	MutationOperationDeactivate MutationOperation = "deactivate"
	// MutationOperationReactivate reactivates an existing prescription.
	MutationOperationReactivate MutationOperation = "reactivate"
)

// StartMode distinguishes a stable "now" request from an explicit start instant.
type StartMode string

const (
	// StartModeNow asks the lifecycle service to resolve the start time after replay matching.
	StartModeNow StartMode = "now"
	// StartModeAt supplies an explicit start instant.
	StartModeAt StartMode = "at"
)

// Definition declares one code-owned kill-switch capability.
type Definition struct {
	Key                 DefinitionKey
	PrincipalKinds      []PrincipalKind
	ResourceKinds       []ResourceKind
	FailurePolicy       FailurePolicy
	DefaultExternalNote string
	EnforcementOwner    string
	IdentityContract    IdentityContractKey
	Surfaces            []Surface
	TransportAdapters   []TransportAdapterKey
}

// IdentityContract declares the principal and resource namespaces a definition may use.
type IdentityContract struct {
	Key            IdentityContractKey
	PrincipalKinds []PrincipalKind
	ResourceKinds  []ResourceKind
}

// CoverageContract inventories one definition's enforcement coverage on one surface.
type CoverageContract struct {
	Definition       DefinitionKey
	Surface          Surface
	PrincipalSource  string
	ResourceSource   string
	Checkpoint       string
	ProtectedWork    string
	FailurePolicy    FailurePolicy
	TransportAdapter TransportAdapterKey
	EnforcementOwner string
	IdentityContract IdentityContractKey
}

// PrincipalCandidate is a canonical principal derived from an enforcement request.
type PrincipalCandidate struct {
	Kind PrincipalKind
	Key  PrincipalKey
}

const (
	// MaxEvaluationDefinitionCandidates bounds server-controlled definition precedence input.
	MaxEvaluationDefinitionCandidates = 16
	// MaxEvaluationPrincipalCandidates bounds server-controlled principal specificity input.
	MaxEvaluationPrincipalCandidates = 16
)

// EvaluationRequest contains ordered, canonical, server-derived candidates for one protected resource.
// Definition and principal order are the authoritative precedence used by the evaluator.
type EvaluationRequest struct {
	OrganizationID      OrganizationID
	DefinitionKeys      []DefinitionKey
	PrincipalCandidates []PrincipalCandidate
	ResourceKind        ResourceKind
	ResourceKey         ResourceKey
}

// PrincipalCandidateResultKind identifies a successful or deliberately unsupported derivation.
type PrincipalCandidateResultKind string

const (
	// PrincipalCandidateResultCandidates indicates that one or more candidates were derived.
	PrincipalCandidateResultCandidates PrincipalCandidateResultKind = "candidates"
	// PrincipalCandidateResultUnsupported indicates that the source deliberately has no supported identity.
	PrincipalCandidateResultUnsupported PrincipalCandidateResultKind = "unsupported_identity"
)

// PrincipalCandidateResult is an immutable candidate-derivation result. Adapter errors are
// reserved for infrastructure failures and are returned separately by PrincipalAdapter.
// Its zero value is invalid; use one of its constructors.
type PrincipalCandidateResult struct {
	kind       PrincipalCandidateResultKind
	candidates []PrincipalCandidate
}

// NewPrincipalCandidateResult validates and stably deduplicates a nonempty ordered candidate set.
// Candidate order expresses surface precedence and is preserved across principal kinds.
func NewPrincipalCandidateResult(candidates []PrincipalCandidate) (PrincipalCandidateResult, error) {
	if len(candidates) == 0 {
		return PrincipalCandidateResult{}, errors.New("principal candidates must not be empty")
	}

	result := make([]PrincipalCandidate, 0, len(candidates))
	seen := make(map[PrincipalCandidate]struct{}, len(candidates))
	for _, candidate := range candidates {
		if err := validateIdentifier("principal candidate kind", string(candidate.Kind)); err != nil {
			return PrincipalCandidateResult{}, err
		}
		if err := validateIdentifier("principal candidate key", string(candidate.Key)); err != nil {
			return PrincipalCandidateResult{}, err
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		result = append(result, candidate)
	}

	return PrincipalCandidateResult{kind: PrincipalCandidateResultCandidates, candidates: result}, nil
}

// UnsupportedPrincipalCandidateResult reports deliberate absence of a supported identity.
func UnsupportedPrincipalCandidateResult() PrincipalCandidateResult {
	return PrincipalCandidateResult{kind: PrincipalCandidateResultUnsupported, candidates: []PrincipalCandidate{}}
}

// Kind returns the candidate derivation outcome.
func (r PrincipalCandidateResult) Kind() PrincipalCandidateResultKind { return r.kind }

// Candidates returns a defensive copy of the canonical candidates.
func (r PrincipalCandidateResult) Candidates() []PrincipalCandidate {
	return slices.Clone(r.candidates)
}

// PrincipalAdapter defines principal canonicalization, current-organization validation,
// and request candidate derivation. Canonicalize must be pure: it must not perform I/O.
// DeriveCandidates accepts only authoritative context established by the enforcement surface;
// implementations must type-check unsupported source values and must not trust caller-provided
// identity. An unsupported result means deliberate absence of supported input; a non-nil error
// means infrastructure failure. Current validation is intentionally separate so stored canonical
// keys can be used without requiring a deleted principal to still exist.
type PrincipalAdapter interface {
	Kind() PrincipalKind
	Canonicalize(organizationID OrganizationID, input string) (CanonicalizationResult[PrincipalKey], error)
	ValidateCurrentOrganization(ctx context.Context, organizationID OrganizationID, key PrincipalKey) (valid bool, err error)
	DeriveCandidates(ctx context.Context, organizationID OrganizationID, source any) (PrincipalCandidateResult, error)
}

// ResourceAdapter defines pure, organization-bound resource canonicalization separately
// from current existence and organization validation. Derive accepts only authoritative context
// established by the enforcement surface and must reject caller-controlled identifiers. A
// deliberately unsupported source returns an unsupported result and nil error; failures to derive
// an expected resource return an infrastructure error.
type ResourceAdapter interface {
	Kind() ResourceKind
	Canonicalize(organizationID OrganizationID, input string) (CanonicalizationResult[ResourceKey], error)
	ValidateCurrentOrganization(ctx context.Context, organizationID OrganizationID, key ResourceKey) (valid bool, err error)
	Derive(ctx context.Context, organizationID OrganizationID, source any) (CanonicalizationResult[ResourceKey], error)
}

// PrincipalCanonicalizationFixture freezes one representative principal adapter call vector.
type PrincipalCanonicalizationFixture struct {
	OrganizationID OrganizationID
	Input          string
	Expected       CanonicalizationResult[PrincipalKey]
}

// PrincipalAdapterRegistration pairs an adapter with startup-validated canonicalization fixtures.
type PrincipalAdapterRegistration struct {
	Adapter  PrincipalAdapter
	Fixtures []PrincipalCanonicalizationFixture
}

// ResourceCanonicalizationFixture freezes one representative resource adapter call vector.
type ResourceCanonicalizationFixture struct {
	OrganizationID OrganizationID
	Input          string
	Expected       CanonicalizationResult[ResourceKey]
}

// ResourceAdapterRegistration pairs an adapter with startup-validated canonicalization fixtures.
type ResourceAdapterRegistration struct {
	Adapter  ResourceAdapter
	Fixtures []ResourceCanonicalizationFixture
}

// EvaluationResultKind identifies a transport-neutral evaluator outcome.
type EvaluationResultKind string

const (
	// EvaluationResultMatch means a prescription matched and protected work is denied.
	EvaluationResultMatch EvaluationResultKind = "match"
	// EvaluationResultNoMatch means no prescription matched.
	EvaluationResultNoMatch EvaluationResultKind = "no_match"
	// EvaluationResultInfrastructureFailure means evaluation could not complete.
	EvaluationResultInfrastructureFailure EvaluationResultKind = "infrastructure_failure"
)

// NoMatchReason preserves bounded internal observability without exposing denial language.
type NoMatchReason string

const (
	// NoMatchReasonNoPrescription means evaluation completed without a matching prescription.
	NoMatchReasonNoPrescription NoMatchReason = "no_prescription"
	// NoMatchReasonUnsupportedIdentity means the surface deliberately lacked a supported identity.
	NoMatchReasonUnsupportedIdentity NoMatchReason = "unsupported_identity"
	// NoMatchReasonUnsupportedResource means the surface deliberately lacked a supported resource.
	NoMatchReasonUnsupportedResource NoMatchReason = "unsupported_resource"
)

// EvaluationResult is an immutable transport-neutral evaluation result. Its zero value is
// invalid; use one of its constructors.
type EvaluationResult struct {
	kind                      EvaluationResultKind
	prescriptionID            PrescriptionID
	externalNote              string
	noMatchReason             NoMatchReason
	infrastructureErr         error
	failurePolicy             FailurePolicy
	infrastructureFailureKind InfrastructureFailureKind
}

// InfrastructureFailureKind distinguishes bounded evaluator failure classes without exposing data.
type InfrastructureFailureKind string

const (
	InfrastructureFailureInvalidRequest     InfrastructureFailureKind = "invalid_request"
	InfrastructureFailureParentCancellation InfrastructureFailureKind = "parent_cancellation"
	InfrastructureFailureTimeout            InfrastructureFailureKind = "timeout"
	InfrastructureFailureDatabase           InfrastructureFailureKind = "database"
	InfrastructureFailureDataIntegrity      InfrastructureFailureKind = "data_integrity"
)

// NewMatchResult constructs a matched result with customer-safe denial language.
func NewMatchResult(prescriptionID PrescriptionID, externalNote string) (EvaluationResult, error) {
	canonicalID, err := canonicalPrescriptionID(&prescriptionID)
	if err != nil {
		return EvaluationResult{}, fmt.Errorf("prescription ID: %w", err)
	}
	note, err := NormalizeExternalNote(externalNote)
	if err != nil {
		return EvaluationResult{}, fmt.Errorf("external note: %w", err)
	}
	return EvaluationResult{
		kind:                      EvaluationResultMatch,
		prescriptionID:            *canonicalID,
		externalNote:              note,
		noMatchReason:             "",
		infrastructureErr:         nil,
		failurePolicy:             "",
		infrastructureFailureKind: "",
	}, nil
}

// NewNoMatchResult constructs a completed evaluation with no match.
func NewNoMatchResult(reason NoMatchReason) (EvaluationResult, error) {
	if reason != NoMatchReasonNoPrescription && reason != NoMatchReasonUnsupportedIdentity && reason != NoMatchReasonUnsupportedResource {
		return EvaluationResult{}, fmt.Errorf("invalid no-match reason %q", reason)
	}
	return EvaluationResult{
		kind:                      EvaluationResultNoMatch,
		prescriptionID:            "",
		externalNote:              "",
		noMatchReason:             reason,
		infrastructureErr:         nil,
		failurePolicy:             "",
		infrastructureFailureKind: "",
	}, nil
}

// NewInfrastructureFailureResult constructs an infrastructure failure without match metadata.
func NewInfrastructureFailureResult(cause error) (EvaluationResult, error) {
	if isNilInterface(cause) {
		return EvaluationResult{}, errors.New("infrastructure failure cause is required")
	}
	return EvaluationResult{
		kind:                      EvaluationResultInfrastructureFailure,
		prescriptionID:            "",
		externalNote:              "",
		noMatchReason:             "",
		infrastructureErr:         cause,
		failurePolicy:             "",
		infrastructureFailureKind: "",
	}, nil
}

// NewInfrastructureFailureResultWithPolicy constructs a classified infrastructure failure and
// retains the effective policy for the complete ordered definition candidate set.
func NewInfrastructureFailureResultWithPolicy(cause error, policy FailurePolicy, failureKind InfrastructureFailureKind) (EvaluationResult, error) {
	result, err := NewInfrastructureFailureResult(cause)
	if err != nil {
		return EvaluationResult{}, err
	}
	if policy != FailurePolicyFailOpen && policy != FailurePolicyFailClosed {
		return EvaluationResult{}, fmt.Errorf("invalid failure policy %q", policy)
	}
	if !validInfrastructureFailureKind(failureKind) {
		return EvaluationResult{}, fmt.Errorf("invalid infrastructure failure kind %q", failureKind)
	}
	result.failurePolicy = policy
	result.infrastructureFailureKind = failureKind
	return result, nil
}

// Kind returns the result discriminator.
func (r EvaluationResult) Kind() EvaluationResultKind { return r.kind }

// PrescriptionID returns matched prescription metadata only for a match.
func (r EvaluationResult) PrescriptionID() (PrescriptionID, bool) {
	return r.prescriptionID, r.kind == EvaluationResultMatch
}

// ExternalNote returns public denial language only for a match.
func (r EvaluationResult) ExternalNote() (string, bool) {
	return r.externalNote, r.kind == EvaluationResultMatch
}

// NoMatchReason returns the bounded reason only for a no-match result.
func (r EvaluationResult) NoMatchReason() (NoMatchReason, bool) {
	return r.noMatchReason, r.kind == EvaluationResultNoMatch
}

// InfrastructureError returns the cause only for an infrastructure failure.
func (r EvaluationResult) InfrastructureError() error {
	if r.kind != EvaluationResultInfrastructureFailure {
		return nil
	}
	return r.infrastructureErr
}

// FailurePolicy returns evaluator-selected policy information for a match or infrastructure failure.
func (r EvaluationResult) FailurePolicy() (FailurePolicy, bool) {
	return r.failurePolicy, r.failurePolicy == FailurePolicyFailOpen || r.failurePolicy == FailurePolicyFailClosed
}

// InfrastructureFailureKind returns the bounded class only for a classified infrastructure failure.
func (r EvaluationResult) InfrastructureFailureKind() (InfrastructureFailureKind, bool) {
	return r.infrastructureFailureKind, r.kind == EvaluationResultInfrastructureFailure && validInfrastructureFailureKind(r.infrastructureFailureKind)
}

func validInfrastructureFailureKind(kind InfrastructureFailureKind) bool {
	switch kind {
	case InfrastructureFailureInvalidRequest, InfrastructureFailureParentCancellation, InfrastructureFailureTimeout, InfrastructureFailureDatabase, InfrastructureFailureDataIntegrity:
		return true
	default:
		return false
	}
}

// TransportDispositionKind identifies the neutral action selected before concrete transport mapping.
type TransportDispositionKind string

const (
	// TransportDispositionContinue permits protected work to continue.
	TransportDispositionContinue TransportDispositionKind = "continue"
	// TransportDispositionMatchedDenial denies work with the matched prescription's external note.
	TransportDispositionMatchedDenial TransportDispositionKind = "matched_denial"
	// TransportDispositionInfrastructureRejection denies work without match language.
	TransportDispositionInfrastructureRejection TransportDispositionKind = "infrastructure_rejection"
)

// TransportDisposition is an immutable transport-neutral action. Its zero value is invalid.
type TransportDisposition struct {
	kind         TransportDispositionKind
	externalNote string
}

// NewContinueDisposition constructs a neutral continue action.
func NewContinueDisposition() TransportDisposition {
	return TransportDisposition{kind: TransportDispositionContinue, externalNote: ""}
}

// NewMatchedDenialDisposition constructs a denial that preserves an already-normalized external note.
func NewMatchedDenialDisposition(externalNote string) (TransportDisposition, error) {
	normalized, err := NormalizeExternalNote(externalNote)
	if err != nil {
		return TransportDisposition{}, fmt.Errorf("external note: %w", err)
	}
	if normalized != externalNote {
		return TransportDisposition{}, errors.New("external note must be normalized")
	}
	return TransportDisposition{kind: TransportDispositionMatchedDenial, externalNote: externalNote}, nil
}

// NewInfrastructureRejectionDisposition constructs a denial with no match language.
func NewInfrastructureRejectionDisposition() TransportDisposition {
	return TransportDisposition{kind: TransportDispositionInfrastructureRejection, externalNote: ""}
}

// Kind returns the neutral disposition discriminator.
func (d TransportDisposition) Kind() TransportDispositionKind { return d.kind }

// ExternalNote returns public denial language only for a matched denial.
func (d TransportDisposition) ExternalNote() (string, bool) {
	return d.externalNote, d.kind == TransportDispositionMatchedDenial
}

// TransportAdapter consumes neutral evaluation data and failure policy. Concrete HTTP and
// JSON-RPC mapping remains the responsibility of transport-specific packages.
type TransportAdapter func(EvaluationResult, FailurePolicy) (TransportDisposition, error)

// TransportAdapterRegistration pairs a code-owned transport key with neutral behavior.
type TransportAdapterRegistration struct {
	Key     TransportAdapterKey
	Adapter TransportAdapter
}

// ResolveTransportDisposition applies the shared transport-neutral evaluation matrix.
func ResolveTransportDisposition(result EvaluationResult, failurePolicy FailurePolicy) (TransportDisposition, error) {
	if err := validateEvaluationResult(result); err != nil {
		return TransportDisposition{}, err
	}
	if failurePolicy != FailurePolicyFailOpen && failurePolicy != FailurePolicyFailClosed {
		return TransportDisposition{}, fmt.Errorf("invalid failure policy %q", failurePolicy)
	}
	if authoritativePolicy, ok := result.FailurePolicy(); ok {
		failurePolicy = authoritativePolicy
	}

	switch result.kind {
	case EvaluationResultMatch:
		return NewMatchedDenialDisposition(result.externalNote)
	case EvaluationResultNoMatch:
		return NewContinueDisposition(), nil
	case EvaluationResultInfrastructureFailure:
		if failurePolicy == FailurePolicyFailOpen {
			return NewContinueDisposition(), nil
		}
		return NewInfrastructureRejectionDisposition(), nil
	default:
		return TransportDisposition{}, errors.New("invalid evaluation result")
	}
}

func validateEvaluationResult(result EvaluationResult) error {
	switch result.kind {
	case EvaluationResultMatch:
		hasPolicy := result.failurePolicy == FailurePolicyFailOpen || result.failurePolicy == FailurePolicyFailClosed
		if result.prescriptionID == "" || result.externalNote == "" || result.noMatchReason != "" || result.infrastructureErr != nil || (result.failurePolicy != "" && !hasPolicy) || result.infrastructureFailureKind != "" {
			return errors.New("invalid match evaluation result")
		}
	case EvaluationResultNoMatch:
		if result.prescriptionID != "" || result.externalNote != "" || result.infrastructureErr != nil || result.failurePolicy != "" || result.infrastructureFailureKind != "" {
			return errors.New("invalid no-match evaluation result")
		}
		if result.noMatchReason != NoMatchReasonNoPrescription && result.noMatchReason != NoMatchReasonUnsupportedIdentity && result.noMatchReason != NoMatchReasonUnsupportedResource {
			return errors.New("invalid no-match evaluation result")
		}
	case EvaluationResultInfrastructureFailure:
		if result.prescriptionID != "" || result.externalNote != "" || result.noMatchReason != "" || isNilInterface(result.infrastructureErr) {
			return errors.New("invalid infrastructure-failure evaluation result")
		}
		hasPolicy := result.failurePolicy == FailurePolicyFailOpen || result.failurePolicy == FailurePolicyFailClosed
		if (result.failurePolicy != "" && !hasPolicy) || (result.infrastructureFailureKind != "" && !validInfrastructureFailureKind(result.infrastructureFailureKind)) || (hasPolicy != (result.infrastructureFailureKind != "")) {
			return errors.New("invalid infrastructure-failure evaluation result")
		}
	default:
		return errors.New("invalid evaluation result")
	}
	return nil
}
