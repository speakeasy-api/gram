package killswitches

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/killswitches/repo"
)

// ErrEvaluatorTimeout marks the evaluator-owned deadline rather than parent cancellation.
var ErrEvaluatorTimeout = errors.New("kill-switch evaluator timeout")

type evaluationQueries interface {
	EvaluateCurrentPrescriptions(context.Context, repo.EvaluateCurrentPrescriptionsParams) (repo.EvaluateCurrentPrescriptionsRow, error)
}

// Evaluator performs one authoritative PostgreSQL query for each supported evaluation request.
type Evaluator struct {
	queries  evaluationQueries
	registry *Registry
	timeout  time.Duration
	metrics  *evaluationMetrics
}

type preparedEvaluation struct {
	params          repo.EvaluateCurrentPrescriptionsParams
	policies        map[DefinitionKey]FailurePolicy
	effectivePolicy FailurePolicy
	noMatchReason   NoMatchReason
}

// NewEvaluator constructs an evaluator over the authoritative PostgreSQL connection. A positive
// evaluator-specific timeout is mandatory even when callers normally carry a deadline.
func NewEvaluator(db repo.DBTX, registry *Registry, timeout time.Duration, meterProvider metric.MeterProvider, logger *slog.Logger) (*Evaluator, error) {
	if isNilInterface(db) {
		return nil, errors.New("kill-switch evaluator database is required")
	}
	return newEvaluator(repo.New(db), registry, timeout, newEvaluationMetrics(meterProvider, logger))
}

func newEvaluator(queries evaluationQueries, registry *Registry, timeout time.Duration, metrics *evaluationMetrics) (*Evaluator, error) {
	if isNilInterface(queries) {
		return nil, errors.New("kill-switch evaluator queries are required")
	}
	if registry == nil {
		return nil, errors.New("kill-switch evaluator registry is required")
	}
	if timeout <= 0 {
		return nil, errors.New("kill-switch evaluator timeout must be positive")
	}
	return &Evaluator{queries: queries, registry: registry, timeout: timeout, metrics: metrics}, nil
}

// Evaluate returns a matched denial, an ordinary no-match, or a classified infrastructure failure.
func (e *Evaluator) Evaluate(ctx context.Context, request EvaluationRequest) EvaluationResult {
	outcome := killswitchEvaluationOutcomeEvaluatorFailure
	if e.metrics.enabled(ctx) {
		started := time.Now()
		defer func() { e.metrics.record(ctx, outcome, time.Since(started)) }()
	}

	prepared, err := e.prepare(request)
	if err != nil {
		return infrastructureFailureResult(err, prepared.effectivePolicy, InfrastructureFailureInvalidRequest)
	}
	if err := ctx.Err(); err != nil {
		return infrastructureFailureResult(err, prepared.effectivePolicy, InfrastructureFailureParentCancellation)
	}
	if prepared.noMatchReason != "" {
		outcome = killswitchEvaluationOutcomeUnmatched
		result, _ := NewNoMatchResult(prepared.noMatchReason)
		return result
	}

	queryContext, cancel := context.WithTimeoutCause(ctx, e.timeout, ErrEvaluatorTimeout)
	defer cancel()
	row, err := e.queries.EvaluateCurrentPrescriptions(queryContext, prepared.params)
	if errors.Is(err, pgx.ErrNoRows) {
		outcome = killswitchEvaluationOutcomeUnmatched
		result, _ := NewNoMatchResult(NoMatchReasonNoPrescription)
		return result
	}
	if cause := context.Cause(queryContext); err != nil && cause != nil {
		if errors.Is(cause, ErrEvaluatorTimeout) {
			return infrastructureFailureResult(errors.Join(cause, queryContext.Err()), prepared.effectivePolicy, InfrastructureFailureTimeout)
		}
		return infrastructureFailureResult(cause, prepared.effectivePolicy, InfrastructureFailureParentCancellation)
	}
	if err != nil {
		return infrastructureFailureResult(fmt.Errorf("evaluate current kill-switch prescriptions: %w", err), prepared.effectivePolicy, InfrastructureFailureDatabase)
	}

	policy, ok := prepared.policies[DefinitionKey(row.DefinitionKey)]
	if !ok {
		return infrastructureFailureResult(errors.New("evaluation returned an unexpected definition"), prepared.effectivePolicy, InfrastructureFailureDataIntegrity)
	}
	result, err := NewMatchResult(PrescriptionID(row.PrescriptionID.String()), row.ExternalNote)
	if err != nil {
		return infrastructureFailureResult(errors.New("evaluation returned invalid match data"), prepared.effectivePolicy, InfrastructureFailureDataIntegrity)
	}
	result.failurePolicy = policy
	outcome = killswitchEvaluationOutcomeMatched
	return result
}

func (e *Evaluator) prepare(request EvaluationRequest) (preparedEvaluation, error) {
	prepared := preparedEvaluation{
		params: repo.EvaluateCurrentPrescriptionsParams{
			OrganizationID:           "",
			ResourceKind:             "",
			ResourceKey:              "",
			DefinitionKeys:           nil,
			PrincipalKinds:           nil,
			PrincipalKeys:            nil,
			CompatibleDefinitionKeys: nil,
			CompatiblePrincipalKinds: nil,
		},
		policies:        nil,
		effectivePolicy: FailurePolicyFailOpen,
		noMatchReason:   "",
	}
	invalid := func(format string, args ...any) (preparedEvaluation, error) {
		prepared.effectivePolicy = FailurePolicyFailClosed
		return prepared, fmt.Errorf(format, args...)
	}

	if len(request.DefinitionKeys) == 0 || len(request.DefinitionKeys) > MaxEvaluationDefinitionCandidates {
		return invalid("evaluation definition candidate count must be between 1 and %d", MaxEvaluationDefinitionCandidates)
	}
	if len(request.PrincipalCandidates) > MaxEvaluationPrincipalCandidates {
		return invalid("evaluation principal candidate count must not exceed %d", MaxEvaluationPrincipalCandidates)
	}
	if err := validateIdentifier("evaluation organization ID", string(request.OrganizationID)); err != nil {
		return invalid("%v", err)
	}

	prepared.policies = make(map[DefinitionKey]FailurePolicy, len(request.DefinitionKeys))
	definitions := make([]Definition, 0, len(request.DefinitionKeys))
	supportedPrincipalKinds := make(map[PrincipalKind]struct{})
	prepared.params.DefinitionKeys = make([]string, 0, len(request.DefinitionKeys))
	for _, key := range request.DefinitionKeys {
		if _, duplicate := prepared.policies[key]; duplicate {
			return invalid("duplicate evaluation definition candidate")
		}
		definition, ok := e.registry.Definition(key)
		if !ok {
			return invalid("unknown evaluation definition candidate")
		}
		definitions = append(definitions, definition)
		for _, kind := range definition.PrincipalKinds {
			supportedPrincipalKinds[kind] = struct{}{}
		}
		prepared.policies[key] = definition.FailurePolicy
		if definition.FailurePolicy == FailurePolicyFailClosed {
			prepared.effectivePolicy = FailurePolicyFailClosed
		}
		prepared.params.DefinitionKeys = append(prepared.params.DefinitionKeys, string(key))
	}

	if request.ResourceKind == "" && request.ResourceKey == "" {
		prepared.noMatchReason = NoMatchReasonUnsupportedResource
		return prepared, nil
	}
	if err := validateIdentifier("evaluation resource kind", string(request.ResourceKind)); err != nil {
		return invalid("%v", err)
	}
	if err := validateIdentifier("evaluation resource key", string(request.ResourceKey)); err != nil {
		return invalid("%v", err)
	}
	for _, definition := range definitions {
		if !slices.Contains(definition.ResourceKinds, request.ResourceKind) {
			return invalid("evaluation resource kind is not supported by every definition candidate")
		}
	}
	prepared.params.OrganizationID = string(request.OrganizationID)
	prepared.params.ResourceKind = string(request.ResourceKind)
	prepared.params.ResourceKey = string(request.ResourceKey)

	if len(request.PrincipalCandidates) == 0 {
		prepared.noMatchReason = NoMatchReasonUnsupportedIdentity
		return prepared, nil
	}
	seenPrincipals := make(map[PrincipalCandidate]struct{}, len(request.PrincipalCandidates))
	prepared.params.PrincipalKinds = make([]string, 0, len(request.PrincipalCandidates))
	prepared.params.PrincipalKeys = make([]string, 0, len(request.PrincipalCandidates))
	compatibleCapacity := len(definitions) * len(request.PrincipalCandidates)
	prepared.params.CompatibleDefinitionKeys = make([]string, 0, compatibleCapacity)
	prepared.params.CompatiblePrincipalKinds = make([]string, 0, compatibleCapacity)
	for _, candidate := range request.PrincipalCandidates {
		if err := validateIdentifier("evaluation principal kind", string(candidate.Kind)); err != nil {
			return invalid("%v", err)
		}
		if err := validateIdentifier("evaluation principal key", string(candidate.Key)); err != nil {
			return invalid("%v", err)
		}
		if _, duplicate := seenPrincipals[candidate]; duplicate {
			return invalid("duplicate evaluation principal candidate")
		}
		if _, supported := supportedPrincipalKinds[candidate.Kind]; !supported {
			return invalid("evaluation principal kind is not supported by any definition candidate")
		}
		seenPrincipals[candidate] = struct{}{}
		prepared.params.PrincipalKinds = append(prepared.params.PrincipalKinds, string(candidate.Kind))
		prepared.params.PrincipalKeys = append(prepared.params.PrincipalKeys, string(candidate.Key))
		for _, definition := range definitions {
			if slices.Contains(definition.PrincipalKinds, candidate.Kind) {
				prepared.params.CompatibleDefinitionKeys = append(prepared.params.CompatibleDefinitionKeys, string(definition.Key))
				prepared.params.CompatiblePrincipalKinds = append(prepared.params.CompatiblePrincipalKinds, string(candidate.Kind))
			}
		}
	}
	return prepared, nil
}

func infrastructureFailureResult(cause error, policy FailurePolicy, kind InfrastructureFailureKind) EvaluationResult {
	result, _ := NewInfrastructureFailureResultWithPolicy(cause, policy, kind)
	return result
}
