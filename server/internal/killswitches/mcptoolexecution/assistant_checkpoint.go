package mcptoolexecution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
)

// AssistantCheckpoint evaluates ai_access against the canonical assistant at
// every governed model or MCP side-effect boundary. It deliberately has no
// decision cache, so next-call activation and membership revocation take effect.
type AssistantCheckpoint struct {
	principal     killswitches.PrincipalAdapter
	resource      killswitches.ResourceAdapter
	evaluator     evaluator
	transport     killswitches.TransportAdapter
	failurePolicy killswitches.FailurePolicy
	timeout       time.Duration
}

func NewAssistantCheckpoint(db *pgxpool.Pool, timeout time.Duration, meterProvider metric.MeterProvider, logger *slog.Logger) (*AssistantCheckpoint, error) {
	registry, err := NewRegistry(db)
	if err != nil {
		return nil, err
	}
	evaluation, err := killswitches.NewEvaluator(db, registry, timeout, meterProvider, logger)
	if err != nil {
		return nil, fmt.Errorf("build assistant AI-access evaluator: %w", err)
	}
	return newAssistantCheckpoint(registry, evaluation, timeout)
}

func newAssistantCheckpoint(registry *killswitches.Registry, evaluation evaluator, timeout time.Duration) (*AssistantCheckpoint, error) {
	if registry == nil || evaluation == nil || timeout <= 0 {
		return nil, errors.New("assistant AI-access checkpoint dependencies are required")
	}
	principal, ok := registry.PrincipalAdapter(PrincipalKindUser)
	if !ok {
		return nil, errors.New("assistant acting-user adapter is not registered")
	}
	resource, ok := registry.ResourceAdapter(ResourceKindAssistant)
	if !ok {
		return nil, errors.New("assistant resource adapter is not registered")
	}
	transport, ok := registry.TransportAdapter(TransportAdapterAssistantRuntime)
	if !ok {
		return nil, errors.New("assistant runtime transport is not registered")
	}
	coverage, ok := registry.Coverage(DefinitionKeyAIAccess, SurfaceAssistantModelCall)
	if !ok {
		return nil, errors.New("assistant model coverage is not registered")
	}
	return &AssistantCheckpoint{principal: principal, resource: resource, evaluator: evaluation, transport: transport, failurePolicy: coverage.FailurePolicy, timeout: timeout}, nil
}

func (c *AssistantCheckpoint) Evaluate(ctx context.Context, organizationID string, assistantID uuid.UUID) (killswitches.TransportDisposition, error) {
	if c == nil {
		return killswitches.NewInfrastructureRejectionDisposition(), errors.New("assistant AI access is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	identity, ok := mcpidentity.FromContext(ctx)
	if !ok || identity.Kind() != mcpidentity.KindDelegatedUser {
		return c.infrastructureFailure(errors.New("assistant work has no validated current-user delegation"))
	}
	organization := killswitches.OrganizationID(organizationID)
	principals, err := c.principal.DeriveCandidates(ctx, organization, identity)
	if err != nil {
		return c.infrastructureFailure(fmt.Errorf("derive delegated user: %w", err))
	}
	if principals.Kind() == killswitches.PrincipalCandidateResultUnsupported {
		return c.infrastructureFailure(errors.New("delegated user is not an active organization member"))
	}
	resource, err := c.resource.Derive(ctx, organization, AssistantSource{AssistantID: assistantID})
	if err != nil {
		return c.infrastructureFailure(fmt.Errorf("derive assistant resource: %w", err))
	}
	resourceKey, supported, err := resource.Key()
	if err != nil || !supported {
		return c.infrastructureFailure(errors.Join(errors.New("assistant resource is unavailable"), err))
	}
	result := c.evaluator.Evaluate(ctx, killswitches.EvaluationRequest{
		OrganizationID: organization, DefinitionKeys: []killswitches.DefinitionKey{DefinitionKeyAIAccess},
		PrincipalCandidates: principals.Candidates(), ResourceKind: ResourceKindAssistant, ResourceKey: resourceKey,
	})
	disposition, err := c.transport(result, c.failurePolicy)
	if err != nil {
		return c.infrastructureFailure(fmt.Errorf("resolve assistant AI-access disposition: %w", err))
	}
	if disposition.Kind() == killswitches.TransportDispositionInfrastructureRejection {
		cause := result.InfrastructureError()
		if cause == nil {
			cause = errors.New("assistant AI access is unavailable")
		}
		return disposition, cause
	}
	return disposition, nil
}

func (c *AssistantCheckpoint) infrastructureFailure(cause error) (killswitches.TransportDisposition, error) {
	return infrastructureFailure(c.transport, c.failurePolicy, cause)
}
