package mcptoolexecution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
)

const hostedEvaluatorTimeout = 2 * time.Second

// HostedCheckpoint authoritatively evaluates the ordered MCP and AI-access
// kill switches before hosted tools/call configuration or execution work begins.
type HostedCheckpoint struct {
	principal     killswitches.PrincipalAdapter
	resource      killswitches.ResourceAdapter
	evaluator     evaluator
	transport     killswitches.TransportAdapter
	failurePolicy killswitches.FailurePolicy
	recorder      IdentityCoverageRecorder
	logger        *slog.Logger
	flags         feature.Provider
}

// NewHostedCheckpoint builds the production checkpoint from the registered M2
// contracts and the authoritative PostgreSQL evaluator.
func NewHostedCheckpoint(db *pgxpool.Pool, meterProvider metric.MeterProvider, logger *slog.Logger, recorder IdentityCoverageRecorder, flags feature.Provider) (*HostedCheckpoint, error) {
	registry, err := NewRegistry(db)
	if err != nil {
		return nil, err
	}

	definition, ok := registry.Definition(DefinitionKeyMCPToolExecution)
	if !ok {
		return nil, errors.New("mcp_tool_execution definition is not registered")
	}
	principal, ok := registry.PrincipalAdapter(PrincipalKindUser)
	if !ok {
		return nil, errors.New("authenticated user principal adapter is not registered")
	}
	resource, ok := registry.ResourceAdapter(ResourceKindMCPServer)
	if !ok {
		return nil, errors.New("mcp server resource adapter is not registered")
	}
	transport, ok := registry.TransportAdapter(TransportAdapterHostedJSONRPC)
	if !ok {
		return nil, errors.New("hosted MCP JSON-RPC transport adapter is not registered")
	}

	eval, err := killswitches.NewEvaluator(db, registry, hostedEvaluatorTimeout, meterProvider, logger)
	if err != nil {
		return nil, fmt.Errorf("construct evaluator: %w", err)
	}

	return &HostedCheckpoint{
		principal:     principal,
		resource:      resource,
		evaluator:     eval,
		transport:     transport,
		failurePolicy: definition.FailurePolicy,
		recorder:      recorder,
		logger:        logger,
		flags:         flags,
	}, nil
}

// Evaluate revalidates principal membership and server ownership on every call.
// Unsupported identities and resources remain outside M2 and continue unchanged.
func (c *HostedCheckpoint) Evaluate(ctx context.Context, organizationID string, resourceSource ServerSource) (killswitches.TransportDisposition, error) {
	if c == nil {
		return killswitches.NewInfrastructureRejectionDisposition(), errors.New("hosted MCP killswitch checkpoint is unavailable")
	}

	return evaluateForRollout(ctx, c.flags, organizationID, func() (killswitches.TransportDisposition, error) {
		return c.evaluate(ctx, organizationID, resourceSource)
	})
}

func (c *HostedCheckpoint) evaluate(ctx context.Context, organizationID string, resourceSource ServerSource) (killswitches.TransportDisposition, error) {
	organization := killswitches.OrganizationID(organizationID)
	evaluationCtx, cancel := context.WithTimeout(ctx, hostedEvaluatorTimeout)
	defer cancel()

	derivation := deriveCoverage(evaluationCtx, organization, c.principal, c.resource, resourceSource)
	derivation.record(ctx, c.recorder, mcpmetrics.KillswitchSurfaceHosted)

	if derivation.principalErr == nil && derivation.principalResult.Kind() == killswitches.PrincipalCandidateResultUnsupported {
		return c.noMatch(killswitches.NoMatchReasonUnsupportedIdentity)
	}

	var resourceKey killswitches.ResourceKey
	if derivation.resourceErr == nil {
		var supported bool
		var err error
		resourceKey, supported, err = derivation.resourceResult.Key()
		if err != nil {
			return c.infrastructureRejection(ctx, err)
		}
		if !supported {
			return c.noMatch(killswitches.NoMatchReasonUnsupportedResource)
		}
	}

	if derivation.principalErr != nil {
		return c.infrastructureRejection(ctx, derivation.principalErr)
	}
	if derivation.resourceErr != nil {
		return c.infrastructureRejection(ctx, derivation.resourceErr)
	}

	result := c.evaluator.Evaluate(evaluationCtx, killswitches.EvaluationRequest{
		OrganizationID:      organization,
		DefinitionKeys:      mcpEvaluationDefinitionKeys(),
		PrincipalCandidates: derivation.principalResult.Candidates(),
		ResourceKind:        ResourceKindMCPServer,
		ResourceKey:         resourceKey,
	})
	if cause := result.InfrastructureError(); cause != nil {
		c.logger.ErrorContext(ctx, "hosted MCP kill-switch evaluation unavailable", attr.SlogError(cause))
	}
	return c.transport(result, c.failurePolicy)
}

func (c *HostedCheckpoint) noMatch(reason killswitches.NoMatchReason) (killswitches.TransportDisposition, error) {
	result, err := killswitches.NewNoMatchResult(reason)
	if err != nil {
		return killswitches.TransportDisposition{}, fmt.Errorf("construct no-match result: %w", err)
	}
	return c.transport(result, c.failurePolicy)
}

func (c *HostedCheckpoint) infrastructureRejection(ctx context.Context, cause error) (killswitches.TransportDisposition, error) {
	c.logger.ErrorContext(ctx, "hosted MCP kill-switch checkpoint unavailable", attr.SlogError(cause))
	result, err := killswitches.NewInfrastructureFailureResult(cause)
	if err != nil {
		return killswitches.TransportDisposition{}, fmt.Errorf("construct infrastructure-failure result: %w", err)
	}
	return c.transport(result, c.failurePolicy)
}
