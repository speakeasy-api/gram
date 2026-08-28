package mcptoolexecution

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
)

// IdentityCoverageRecorder is implemented by each production MCP metric scope.
type IdentityCoverageRecorder interface {
	RecordKillswitchIdentityCoverage(context.Context, mcpmetrics.KillswitchCoverageSurface, mcpmetrics.KillswitchIdentityClass, mcpmetrics.KillswitchResourceClass)
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

	organization := killswitches.OrganizationID(organizationID)
	principalResult := killswitches.UnsupportedPrincipalCandidateResult()
	var principalSource any
	var principalErr error
	if identity, ok := mcpidentity.FromContext(ctx); ok {
		principalSource = identity
		principalResult, principalErr = c.principal.DeriveCandidates(ctx, organization, identity)
	}

	resourceResult, resourceErr := c.resource.Derive(ctx, organization, resourceSource)
	c.recorder.RecordKillswitchIdentityCoverage(
		ctx,
		surface,
		ClassifyPrincipalCoverage(principalSource, principalResult, principalErr),
		ClassifyResourceCoverage(resourceSource, resourceResult, resourceErr),
	)
}
