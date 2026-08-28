package mcptoolexecution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
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
func NewCheckpoint(db *pgxpool.Pool, timeout time.Duration, meterProvider metric.MeterProvider, logger *slog.Logger) (*Checkpoint, error) {
	registry, err := NewRegistry(db)
	if err != nil {
		return nil, err
	}
	evaluation, err := killswitches.NewEvaluator(db, registry, timeout, meterProvider, logger)
	if err != nil {
		return nil, fmt.Errorf("build mcp tool-execution evaluator: %w", err)
	}
	return newCheckpoint(registry, evaluation, timeout)
}

func newCheckpoint(registry *killswitches.Registry, evaluation evaluator, timeout time.Duration) (*Checkpoint, error) {
	if registry == nil {
		return nil, errors.New("mcp tool-execution registry is required")
	}
	if evaluation == nil {
		return nil, errors.New("mcp tool-execution evaluator is required")
	}
	if timeout <= 0 {
		return nil, errors.New("mcp tool-execution checkpoint timeout must be positive")
	}
	principal, ok := registry.PrincipalAdapter(PrincipalKindUser)
	if !ok {
		return nil, errors.New("authenticated-user principal adapter is not registered")
	}
	resource, ok := registry.ResourceAdapter(ResourceKindMCPServer)
	if !ok {
		return nil, errors.New("mcp-server resource adapter is not registered")
	}
	transport, ok := registry.TransportAdapter(TransportAdapterPrivateProxyJSONRPC)
	if !ok {
		return nil, errors.New("private proxy transport adapter is not registered")
	}
	coverage, ok := registry.Coverage(DefinitionKeyMCPToolExecution, SurfacePrivateProxyToolsCall)
	if !ok {
		return nil, errors.New("private proxy coverage contract is not registered")
	}

	return &Checkpoint{
		principal:     principal,
		resource:      resource,
		evaluator:     evaluation,
		transport:     transport,
		failurePolicy: coverage.FailurePolicy,
		timeout:       timeout,
		recorder:      nil,
	}, nil
}

// WithIdentityCoverageRecorder returns a checkpoint copy that records the
// principal and resource classifications produced by its authoritative
// derivation, avoiding a second pair of database lookups for metrics.
func (c *Checkpoint) WithIdentityCoverageRecorder(recorder IdentityCoverageRecorder) *Checkpoint {
	if c == nil {
		return nil
	}
	result := *c
	result.recorder = recorder
	return &result
}

// Evaluate resolves and evaluates one covered tools/call. Deliberately
// unsupported provenance continues without inventing an acting user. Every
// other resolution or evaluator failure follows the registered failure policy.
func (c *Checkpoint) Evaluate(ctx context.Context, organizationID, mcpServerID string) (killswitches.TransportDisposition, error) {
	if c == nil {
		return killswitches.NewInfrastructureRejectionDisposition(), errors.New("mcp tool-execution checkpoint is unavailable")
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	organization := killswitches.OrganizationID(organizationID)
	serverID, parseErr := uuid.Parse(mcpServerID)
	resourceSource := ServerSource{FrontingServerID: uuid.NullUUID{UUID: serverID, Valid: parseErr == nil}}
	resourceResult := killswitches.CanonicalizationResult[killswitches.ResourceKey]{}
	resourceErr := parseErr
	principals := killswitches.UnsupportedPrincipalCandidateResult()
	identity, hasIdentity := mcpidentity.FromContext(ctx)
	var principalSource any
	var principalErr error

	var derivations sync.WaitGroup
	if resourceErr == nil {
		derivations.Go(func() {
			resourceResult, resourceErr = c.resource.Derive(ctx, organization, resourceSource)
		})
	}
	if hasIdentity {
		principalSource = identity
		derivations.Go(func() {
			principals, principalErr = c.principal.DeriveCandidates(ctx, organization, identity)
		})
	}
	derivations.Wait()
	if c.recorder != nil {
		c.recorder.RecordKillswitchIdentityCoverage(
			ctx,
			mcpmetrics.KillswitchSurfacePrivateProxy,
			ClassifyPrincipalCoverage(principalSource, principals, principalErr),
			ClassifyResourceCoverage(resourceSource, resourceResult, resourceErr),
		)
	}

	if resourceErr != nil {
		return c.infrastructureFailure(fmt.Errorf("derive canonical mcp server: %w", resourceErr))
	}
	resourceKey, supported, err := resourceResult.Key()
	if err != nil {
		return c.infrastructureFailure(fmt.Errorf("read canonical mcp server: %w", err))
	}
	if !supported {
		return c.infrastructureFailure(errors.New("covered tools/call has no canonical mcp server"))
	}
	if principalErr != nil {
		return c.infrastructureFailure(fmt.Errorf("derive authenticated user: %w", principalErr))
	}
	if principals.Kind() == killswitches.PrincipalCandidateResultUnsupported {
		return killswitches.NewContinueDisposition(), nil
	}

	result := c.evaluator.Evaluate(ctx, killswitches.EvaluationRequest{
		OrganizationID:      organization,
		DefinitionKeys:      []killswitches.DefinitionKey{DefinitionKeyMCPToolExecution},
		PrincipalCandidates: principals.Candidates(),
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
