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

	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
)

// DefaultEvaluationTimeout bounds the authoritative database lookup on the
// per-call serving path.
const DefaultEvaluationTimeout = time.Second

type evaluator interface {
	Evaluate(context.Context, killswitches.EvaluationRequest) killswitches.EvaluationResult
}

// Checkpoint resolves the trusted principal and canonical MCP server for one
// covered tools/call, then performs an authoritative evaluation. It holds no
// per-call decision cache.
type Checkpoint struct {
	principal     killswitches.PrincipalAdapter
	resource      killswitches.ResourceAdapter
	evaluator     evaluator
	transport     killswitches.TransportAdapter
	failurePolicy killswitches.FailurePolicy
	timeout       time.Duration
	recorder      IdentityCoverageRecorder
}

// NewCheckpoint builds the private MCP tool-execution checkpoint
// from the registered adapters and authoritative PostgreSQL evaluator.
func NewCheckpoint(db *pgxpool.Pool, timeout time.Duration, meterProvider metric.MeterProvider, logger *slog.Logger) *Checkpoint {
	registry := NewRegistry(db)
	return newCheckpoint(registry, killswitches.NewEvaluator(db, registry, timeout, meterProvider, logger), timeout)
}

func newCheckpoint(registry *killswitches.Registry, evaluation evaluator, timeout time.Duration) *Checkpoint {
	resource, resourceOK := registry.ResourceAdapter(ResourceKindMCPServer)
	transport, transportOK := registry.TransportAdapter(TransportAdapterPrivateProxyJSONRPC)
	coverage, coverageOK := registry.Coverage(DefinitionKeyMCPToolExecution, SurfacePrivateProxyToolsCall)
	inv.Require("mcp tool-execution checkpoint",
		"timeout is positive", timeout > 0,
		"mcp-server resource adapter is registered", resourceOK,
		"private proxy transport adapter is registered", transportOK,
		"private proxy coverage contract is registered", coverageOK,
	)
	principal := registeredPrincipalAdapter(registry)

	return &Checkpoint{
		principal:     principal,
		resource:      resource,
		evaluator:     evaluation,
		transport:     transport,
		failurePolicy: coverage.FailurePolicy,
		timeout:       timeout,
		recorder:      nil,
	}
}

// WithIdentityCoverageRecorder returns a checkpoint copy that records the
// principal and resource classifications produced by its authoritative
// derivation, avoiding a second pair of database lookups for metrics.
func (c *Checkpoint) WithIdentityCoverageRecorder(recorder IdentityCoverageRecorder) *Checkpoint {
	result := *c
	result.recorder = recorder
	return &result
}

// Evaluate resolves and evaluates one covered tools/call. Deliberately
// unsupported provenance continues without inventing an acting user. Every
// other resolution or evaluator failure follows the registered failure policy.
func (c *Checkpoint) Evaluate(ctx context.Context, organizationID, mcpServerID string) (killswitches.TransportDisposition, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	organization := killswitches.OrganizationID(organizationID)
	serverID, parseErr := uuid.Parse(mcpServerID)
	resourceSource := ServerSource{FrontingServerID: uuid.NullUUID{UUID: serverID, Valid: parseErr == nil}}
	derivation := deriveCoverage(ctx, organization, c.principal, c.resource, resourceSource)
	if parseErr != nil {
		derivation.resourceErr = parseErr
	}
	derivation.record(ctx, c.recorder, mcpmetrics.KillswitchSurfacePrivateProxy)

	if derivation.resourceErr != nil {
		return c.infrastructureFailure(fmt.Errorf("derive canonical mcp server: %w", derivation.resourceErr))
	}
	resourceKey, supported, err := derivation.resourceResult.Key()
	if err != nil {
		return c.infrastructureFailure(fmt.Errorf("read canonical mcp server: %w", err))
	}
	if !supported {
		return c.infrastructureFailure(errors.New("covered tools/call has no canonical mcp server"))
	}
	if derivation.principalErr != nil {
		return c.infrastructureFailure(fmt.Errorf("derive authenticated user: %w", derivation.principalErr))
	}
	if derivation.principalResult.Kind() == killswitches.PrincipalCandidateResultUnsupported {
		if derivation.hasAgentBackedPrincipal() {
			return c.infrastructureFailure(errors.New("agent or workload principal has no kill-switch principal candidate"))
		}
		return killswitches.NewContinueDisposition(), nil
	}

	result := c.evaluator.Evaluate(ctx, killswitches.EvaluationRequest{
		OrganizationID:      organization,
		DefinitionKeys:      []killswitches.DefinitionKey{DefinitionKeyMCPToolExecution},
		PrincipalCandidates: derivation.principalResult.Candidates(),
		ResourceKind:        ResourceKindMCPServer,
		ResourceKey:         resourceKey,
	})
	disposition, err := c.transport(result, c.failurePolicy)
	if err != nil {
		return c.infrastructureFailure(fmt.Errorf("resolve private proxy transport disposition: %w", err))
	}
	if disposition.Kind() == killswitches.TransportDispositionInfrastructureRejection {
		cause := result.InfrastructureError()
		if cause == nil {
			cause = errors.New("evaluator returned an infrastructure rejection without a cause")
		}
		return disposition, cause
	}
	return disposition, nil
}

func (c *Checkpoint) infrastructureFailure(cause error) (killswitches.TransportDisposition, error) {
	result, err := killswitches.NewInfrastructureFailureResult(cause)
	if err != nil {
		return killswitches.NewInfrastructureRejectionDisposition(), errors.Join(cause, err)
	}
	disposition, err := c.transport(result, c.failurePolicy)
	if err != nil {
		return killswitches.NewInfrastructureRejectionDisposition(), errors.Join(cause, err)
	}
	return disposition, cause
}
