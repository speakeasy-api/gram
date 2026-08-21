package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

const (
	CatalogConnectionLimitName        = "platform-mcp-catalog-connection"
	CatalogOrganizationLimitName      = "platform-mcp-catalog-organization"
	RegistrationConnectionLimitName   = "platform-mcp-registration-connection"
	RegistrationOrganizationLimitName = "platform-mcp-registration-organization"
	HandoffConnectionLimitName        = "platform-mcp-handoff-connection"
	HandoffOrganizationLimitName      = "platform-mcp-handoff-organization"
	SetupConnectionLimitName          = "platform-mcp-setup-connection"
	SetupOrganizationLimitName        = "platform-mcp-setup-organization"
	RepairConnectionLimitName         = "platform-mcp-repair-connection"
	RepairOrganizationLimitName       = "platform-mcp-repair-organization"
	DocsConnectionLimitName           = "platform-mcp-docs-connection"
	DocsOrganizationLimitName         = "platform-mcp-docs-organization"
	SkillsConnectionLimitName         = "platform-mcp-skills-connection"
	SkillsOrganizationLimitName       = "platform-mcp-skills-organization"
	LifecycleConnectionLimitName      = "platform-mcp-lifecycle-connection"
	LifecycleOrganizationLimitName    = "platform-mcp-lifecycle-organization"
)

const (
	// DocsQueriesPerConnectionPerMinute and DocsQueriesPerOrganizationPerMinute
	// bound documentation search. Retrieval is in-process and cheap, so these
	// exist to stop a loop from spending the caller's context on repeated
	// queries rather than to protect a backend.
	DocsQueriesPerConnectionPerMinute   = 10
	DocsQueriesPerOrganizationPerMinute = 100

	// DiagnosticQueriesPer* bound the summary reads: the project overview and
	// the per-MCP diagnosis. They are generous because an administrator
	// investigating an incident legitimately makes many of them in a short
	// burst, and each one is a bounded aggregate that names no subject.
	DiagnosticQueriesPerConnectionPerMinute   = 60
	DiagnosticQueriesPerOrganizationPerMinute = 600

	// SensitiveDiagnosticQueriesPer* bound the drill-downs. They are metered
	// separately and lower than the summaries because they reach row-level
	// occurrences and, in one case, an individual: a caller must not be able to
	// fund them by spending the summary allowance.
	SensitiveDiagnosticQueriesPerConnectionPerMinute   = 30
	SensitiveDiagnosticQueriesPerOrganizationPerMinute = 300

	// DrilldownRowsPerConnectionPerWindow and
	// DrilldownMetricQueriesPerConnectionPerWindow are the second cap the
	// drill-downs carry, over DrilldownVolumeWindow. A per-minute call budget
	// alone does not bound how much a caller can accumulate: paging steadily
	// under the call rate still walks an entire window's occurrences. These
	// meter the volume rather than the calls.
	DrilldownRowsPerConnectionPerWindow          = 1000
	DrilldownMetricQueriesPerConnectionPerWindow = 20
)

// DrilldownVolumeWindow is the interval the drill-down volume caps refill over.
const DrilldownVolumeWindow = 10 * time.Minute

var (
	ErrOperationRateLimited       = errors.New("platform mcp operation rate limited")
	ErrOperationBudgetUnavailable = errors.New("platform mcp operation budget unavailable")
)

// Limiter is the narrow Platform MCP boundary around Gram's shared rate limiter.
// It lets unit tests deterministically model an allowance, a throttle, or a
// backing-store failure without depending on Redis.
type Limiter interface {
	Allow(ctx context.Context, key string) (ratelimit.Result, error)
	// AllowN charges n units at once. A volume cap meters rows or spans rather
	// than calls, and charging them one at a time would let a page that cannot
	// be afforded in full be paid for halfway.
	AllowN(ctx context.Context, key string, n int) (ratelimit.Result, error)
}

// OperationBudget applies independently configured connection and organization
// buckets. Connection is always charged first; a denial prevents the second
// bucket, mutations, and provider egress.
type OperationBudget struct {
	Connection   Limiter
	Organization Limiter
}

func (b OperationBudget) valid() bool {
	return b.Connection != nil && b.Organization != nil
}

func (b OperationBudget) Allow(ctx context.Context, principal Principal) error {
	if !b.valid() || principal.OrganizationID == "" {
		return ErrOperationBudgetUnavailable
	}
	actorKey, err := operationBudgetActorKey(principal)
	if err != nil {
		return err
	}
	connection, err := b.Connection.Allow(ctx, actorKey)
	if err != nil {
		return fmt.Errorf("limit platform mcp actor operation: %w: %w", ErrOperationBudgetUnavailable, err)
	}
	if !connection.Allowed {
		return ErrOperationRateLimited
	}
	organization, err := b.Organization.Allow(ctx, principal.OrganizationID)
	if err != nil {
		return fmt.Errorf("limit platform mcp organization operation: %w: %w", ErrOperationBudgetUnavailable, err)
	}
	if !organization.Allowed {
		return ErrOperationRateLimited
	}
	return nil
}

// operationBudgetActorKey is the actor bucket for remote egress and mutations.
// External Platform MCP calls are isolated by their OAuth connection. A managed
// assistant has no connection, so it is isolated by its fixed client identity
// and the real user that authorized the action; it never pools all assistants
// into one empty-key bucket.
func operationBudgetActorKey(principal Principal) (string, error) {
	if principal.HasConnection() {
		if principal.ConnectionID == "" {
			return "", ErrOperationBudgetUnavailable
		}
		return principal.ConnectionID, nil
	}
	if principal.surface() != SurfaceProjectAssistant || principal.ClientID == "" || principal.UserID == "" {
		return "", ErrOperationBudgetUnavailable
	}
	return "assistant:" + principal.ClientID + ":" + principal.UserID, nil
}

// OperationBudgets groups the independently metered public Platform MCP
// operations. Every value is injected at composition; no production defaults
// are assigned here.
type OperationBudgets struct {
	Catalog      OperationBudget
	Registration OperationBudget
	Handoff      OperationBudget
	SetupStart   OperationBudget
	Repair       OperationBudget
	Docs         OperationBudget
	// Skills meters authoring and distribution together. Reads and writes share
	// one allowance because they are one workflow: a caller reads a skill to
	// obtain the version token its next write needs, and metering the read
	// separately would only let a loop spend twice as much reaching the same
	// write.
	Skills            OperationBudget
	LifecycleMetadata OperationBudget
	// Diagnostics meters the observability reads. They are bounded aggregate
	// queries over Gram-owned telemetry, so the cost being metered is the
	// ClickHouse scan, not an external egress.
	Diagnostics OperationBudget
	// SensitiveDiagnostics meters the bounded drill-downs. It is separate from
	// Diagnostics so exhausting it is not possible by spending the summary
	// allowance, and so it can be tightened on its own.
	SensitiveDiagnostics OperationBudget
	// DrilldownVolume meters what the drill-downs return rather than how often
	// they are called: rows and spans against one bucket, metric queries
	// against another, both per connection over DrilldownVolumeWindow.
	DrilldownVolume DrilldownVolumeBudget
}

// DrilldownVolumeBudget is the second cap on the drill-down tools. It is keyed
// on the connection alone: it exists to stop one caller from walking a window
// row by row, which an organization-wide bucket would not catch until every
// other connection had already been starved.
type DrilldownVolumeBudget struct {
	Rows          Limiter
	MetricQueries Limiter
}

func (b DrilldownVolumeBudget) valid() bool {
	return b.Rows != nil && b.MetricQueries != nil
}

// AllowRows charges n rows or spans. A connection-less principal is not
// metered here: it holds no connection key to charge, and the per-call
// organization budget already bounds it.
func (b DrilldownVolumeBudget) AllowRows(ctx context.Context, principal Principal, n int) error {
	return b.allow(ctx, principal, b.Rows, n, "rows")
}

// AllowMetricQuery charges one metric query.
func (b DrilldownVolumeBudget) AllowMetricQuery(ctx context.Context, principal Principal) error {
	return b.allow(ctx, principal, b.MetricQueries, 1, "metric queries")
}

func (b DrilldownVolumeBudget) allow(ctx context.Context, principal Principal, limiter Limiter, n int, what string) error {
	if !b.valid() || principal.OrganizationID == "" {
		return ErrOperationBudgetUnavailable
	}
	if n <= 0 {
		return nil
	}
	if !principal.HasConnection() {
		return nil
	}
	if principal.ConnectionID == "" {
		return ErrOperationBudgetUnavailable
	}
	result, err := limiter.AllowN(ctx, principal.ConnectionID, n)
	if err != nil {
		return fmt.Errorf("limit platform mcp drilldown %s: %w: %w", what, ErrOperationBudgetUnavailable, err)
	}
	if !result.Allowed {
		return ErrOperationRateLimited
	}
	return nil
}

func (b OperationBudgets) Valid() bool {
	return b.Catalog.valid() && b.Registration.valid() && b.Handoff.valid() && b.SetupStart.valid() && b.Repair.valid() && b.Docs.valid() && b.Skills.valid() && b.LifecycleMetadata.valid() && b.Diagnostics.valid() && b.SensitiveDiagnostics.valid() && b.DrilldownVolume.valid()
}
