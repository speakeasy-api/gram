package hostedinference

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
)

// AttemptCheckpoint is the dependency injected into the production ChatClient.
// Check is called once before request setup and again immediately before every
// actual provider attempt.
const DefaultEvaluationTimeout = time.Second

// ErrCheckpointUnavailable is the shared fail-closed cause for missing
// hosted-inference enforcement dependencies.
var ErrCheckpointUnavailable = errors.New("hosted-inference checkpoint is unavailable")

type AttemptCheckpoint interface {
	Check(ctx context.Context, organizationID string) error
}

type evaluator interface {
	Evaluate(context.Context, killswitches.EvaluationRequest) killswitches.EvaluationResult
}

// Checkpoint evaluates ai_access for governed user calls and explicitly
// bypasses the registered internal, background, and unsupported classes.
type Checkpoint struct {
	principal     killswitches.PrincipalAdapter
	resource      killswitches.ResourceAdapter
	evaluator     evaluator
	transport     killswitches.TransportAdapter
	failurePolicy killswitches.FailurePolicy
	timeout       time.Duration
}

var _ AttemptCheckpoint = (*Checkpoint)(nil)

func NewCheckpoint(registry *killswitches.Registry, evaluation evaluator, timeout time.Duration) (*Checkpoint, error) {
	if registry == nil || evaluation == nil {
		return nil, errors.New("hosted-inference registry and evaluator are required")
	}
	if timeout <= 0 {
		return nil, errors.New("hosted-inference checkpoint timeout must be positive")
	}
	definition, ok := registry.Definition(DefinitionKeyAIAccess)
	if !ok {
		return nil, errors.New("ai_access definition is not registered")
	}
	coverage, ok := registry.Coverage(DefinitionKeyAIAccess, SurfaceGramHostedInference)
	if !ok {
		return nil, errors.New("gram-hosted-inference coverage is not registered")
	}
	principal, ok := registry.PrincipalAdapter(PrincipalKindUser)
	if !ok {
		return nil, errors.New("authenticated user principal adapter is not registered")
	}
	resource, ok := registry.ResourceAdapter(ResourceKindGramHostedInference)
	if !ok {
		return nil, errors.New("gram-hosted-inference resource adapter is not registered")
	}
	transport, ok := registry.TransportAdapter(coverage.TransportAdapter)
	if !ok {
		return nil, errors.New("gram-hosted-inference transport adapter is not registered")
	}
	if definition.FailurePolicy != killswitches.FailurePolicyFailClosed || coverage.FailurePolicy != definition.FailurePolicy {
		return nil, errors.New("gram-hosted-inference ai_access must be fail-closed")
	}
	return &Checkpoint{principal: principal, resource: resource, evaluator: evaluation, transport: transport, failurePolicy: coverage.FailurePolicy, timeout: timeout}, nil
}

func (c *Checkpoint) Check(ctx context.Context, organizationID string) error {
	if c == nil {
		return newInfrastructureUnavailable(ErrCheckpointUnavailable)
	}
	classification, ok := classificationFromContext(ctx)
	if !ok {
		return newInfrastructureUnavailable(errors.New("hosted-inference classification is required"))
	}
	if err := validateCategoryClass(classification.category, classification.class); err != nil {
		return newInfrastructureUnavailable(err)
	}
	switch classification.class {
	case CallClassInternal, CallClassBackground, CallClassUnsupported:
		return nil
	case CallClassGovernedUser:
	default:
		return newInfrastructureUnavailable(fmt.Errorf("unknown hosted-inference call class %q", classification.class))
	}

	organization := killswitches.OrganizationID(organizationID)
	if organization == "" || classification.actingUser.OrganizationID() != organizationID {
		return newInfrastructureUnavailable(errors.New("acting user organization does not match the inference organization"))
	}

	checkCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	principals, err := c.principal.DeriveCandidates(checkCtx, organization, classification.actingUser)
	if err != nil {
		return newInfrastructureUnavailable(fmt.Errorf("derive acting user: %w", err))
	}
	if principals.Kind() != killswitches.PrincipalCandidateResultCandidates {
		return newInfrastructureUnavailable(errors.New("acting user is not an active organization member"))
	}
	resourceResult, err := c.resource.Derive(checkCtx, organization, classification.category)
	if err != nil {
		return newInfrastructureUnavailable(fmt.Errorf("derive hosted-inference resource: %w", err))
	}
	resourceKey, supported, err := resourceResult.Key()
	if err != nil || !supported {
		return newInfrastructureUnavailable(errors.New("hosted-inference resource is unavailable"))
	}

	result := c.evaluator.Evaluate(checkCtx, killswitches.EvaluationRequest{
		OrganizationID:      organization,
		DefinitionKeys:      []killswitches.DefinitionKey{DefinitionKeyAIAccess},
		PrincipalCandidates: principals.Candidates(),
		ResourceKind:        ResourceKindGramHostedInference,
		ResourceKey:         resourceKey,
	})
	disposition, err := c.transport(result, c.failurePolicy)
	if err != nil {
		return newInfrastructureUnavailable(fmt.Errorf("resolve hosted-inference disposition: %w", err))
	}
	switch disposition.Kind() {
	case killswitches.TransportDispositionContinue:
		return nil
	case killswitches.TransportDispositionMatchedDenial:
		note, ok := disposition.ExternalNote()
		if !ok {
			return newInfrastructureUnavailable(errors.New("matched denial has no external note"))
		}
		return &MatchedDenialError{externalNote: note}
	case killswitches.TransportDispositionInfrastructureRejection:
		cause := result.InfrastructureError()
		if cause == nil {
			cause = errors.New("ai_access evaluation is unavailable")
		}
		return newInfrastructureUnavailable(cause)
	default:
		return newInfrastructureUnavailable(errors.New("invalid hosted-inference disposition"))
	}
}

// MatchedDenialError is a typed matched prescription denial. Error stays
// generic so tenant-authored notes cannot enter logs, traces, or wrapped causes;
// only HTTP/Goa boundary mapping may read ExternalNote.
type MatchedDenialError struct{ externalNote string }

func (*MatchedDenialError) Error() string          { return "hosted inference access denied" }
func (e *MatchedDenialError) ExternalNote() string { return e.externalNote }

// InfrastructureUnavailableError is deliberately free of match language and
// external notes. Its cause is available to trusted internal handling only.
type InfrastructureUnavailableError struct{ cause error }

func newInfrastructureUnavailable(cause error) *InfrastructureUnavailableError {
	return &InfrastructureUnavailableError{cause: cause}
}
func (e *InfrastructureUnavailableError) Error() string {
	return "hosted inference access evaluation is unavailable"
}
func (e *InfrastructureUnavailableError) Unwrap() error { return e.cause }
