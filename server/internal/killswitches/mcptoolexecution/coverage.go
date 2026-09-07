package mcptoolexecution

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
)

// IdentityCoverageRecorder is implemented by each production MCP metric scope.
type IdentityCoverageRecorder interface {
	RecordKillswitchIdentityCoverage(context.Context, mcpmetrics.KillswitchCoverageSurface, mcpmetrics.KillswitchIdentityClass, mcpmetrics.KillswitchResourceClass)
}

type coverageDerivation struct {
	principalSource any
	principalResult killswitches.PrincipalCandidateResult
	principalErr    error
	resourceSource  ServerSource
	resourceResult  killswitches.CanonicalizationResult[killswitches.ResourceKey]
	resourceErr     error
}

func deriveCoverage(
	ctx context.Context,
	organization killswitches.OrganizationID,
	principal killswitches.PrincipalAdapter,
	resource killswitches.ResourceAdapter,
	resourceSource ServerSource,
) coverageDerivation {
	result := coverageDerivation{
		principalSource: nil,
		principalResult: killswitches.UnsupportedPrincipalCandidateResult(),
		principalErr:    nil,
		resourceSource:  resourceSource,
		resourceResult:  killswitches.CanonicalizationResult[killswitches.ResourceKey]{},
		resourceErr:     nil,
	}

	var group sync.WaitGroup
	if identity, ok := mcpidentity.FromContext(ctx); ok {
		result.principalSource = identity
		group.Go(func() {
			result.principalResult, result.principalErr = principal.DeriveCandidates(ctx, organization, identity)
		})
	}
	group.Go(func() {
		result.resourceResult, result.resourceErr = resource.Derive(ctx, organization, resourceSource)
	})
	group.Wait()

	return result
}

func (d coverageDerivation) record(ctx context.Context, recorder IdentityCoverageRecorder, surface mcpmetrics.KillswitchCoverageSurface) {
	if recorder == nil {
		return
	}
	recorder.RecordKillswitchIdentityCoverage(
		ctx,
		surface,
		ClassifyPrincipalCoverage(d.principalSource, d.principalResult, d.principalErr),
		ClassifyResourceCoverage(d.resourceSource, d.resourceResult, d.resourceErr),
	)
}

// IdentityCoverageCheckpoint derives and records the bounded identity and
// resource coverage of each tools/call that reaches a covered MCP surface.
// Derivation deliberately runs on every call so membership and live server
// ownership cannot become stale. Metrics are observational and never reject a
// call; enforcement is owned by the separately registered checkpoints.
type IdentityCoverageCheckpoint struct {
	principal *AuthenticatedUserPrincipalAdapter
	resource  *MCPServerResourceAdapter
	recorder  IdentityCoverageRecorder
}

// NewIdentityCoverageCheckpoint wires the production adapters to an MCP
// metric scope. A nil database or recorder produces a disabled checkpoint.
func NewIdentityCoverageCheckpoint(db *pgxpool.Pool, recorder IdentityCoverageRecorder) *IdentityCoverageCheckpoint {
	if db == nil || recorder == nil {
		return nil
	}
	return &IdentityCoverageCheckpoint{
		principal: NewAuthenticatedUserPrincipalAdapter(db),
		resource:  NewMCPServerResourceAdapter(db),
		recorder:  recorder,
	}
}

// Record observes one covered tools/call using only credential provenance
// stamped by successful authentication and the fronting server resolved from
// the live route. Unstamped requests never enter principal derivation.
func (c *IdentityCoverageCheckpoint) Record(ctx context.Context, organizationID string, surface mcpmetrics.KillswitchCoverageSurface, resourceSource ServerSource) {
	if c == nil {
		return
	}

	derivation := deriveCoverage(ctx, killswitches.OrganizationID(organizationID), c.principal, c.resource, resourceSource)
	derivation.record(ctx, c.recorder, surface)
}
