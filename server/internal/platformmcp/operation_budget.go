package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

const (
	CatalogConnectionLimitName         = "platform-mcp-catalog-connection"
	CatalogOrganizationLimitName       = "platform-mcp-catalog-organization"
	RegistrationConnectionLimitName    = "platform-mcp-registration-connection"
	RegistrationOrganizationLimitName  = "platform-mcp-registration-organization"
	HandoffConnectionLimitName         = "platform-mcp-handoff-connection"
	HandoffOrganizationLimitName       = "platform-mcp-handoff-organization"
	SetupConnectionLimitName           = "platform-mcp-setup-connection"
	SetupOrganizationLimitName         = "platform-mcp-setup-organization"
	RepairConnectionLimitName          = "platform-mcp-repair-connection"
	RepairOrganizationLimitName        = "platform-mcp-repair-organization"
	DocsConnectionLimitName            = "platform-mcp-docs-connection"
	DocsOrganizationLimitName          = "platform-mcp-docs-organization"
	SkillsConnectionLimitName          = "platform-mcp-skills-connection"
	SkillsOrganizationLimitName        = "platform-mcp-skills-organization"
	LifecycleConnectionLimitName       = "platform-mcp-lifecycle-connection"
	LifecycleOrganizationLimitName     = "platform-mcp-lifecycle-organization"
	SessionRecallConnectionLimitName   = "platform-mcp-session-recall-connection"
	SessionRecallOrganizationLimitName = "platform-mcp-session-recall-organization"
	RiskMutationConnectionLimitName    = "platform-mcp-risk-mutation-connection"
	RiskMutationOrganizationLimitName  = "platform-mcp-risk-mutation-organization"
	ReviewRequestConnectionLimitName   = "platform-mcp-review-request-connection"
	ReviewRequestOrganizationLimitName = "platform-mcp-review-request-organization"
)

const (
	PluginAssignmentMutationConnectionLimitName   = "platform-mcp-plugin-assignment-mutation-connection"
	PluginAssignmentMutationOrganizationLimitName = "platform-mcp-plugin-assignment-mutation-organization"
	PluginMetadataMutationConnectionLimitName     = "platform-mcp-plugin-metadata-mutation-connection"
	PluginMetadataMutationOrganizationLimitName   = "platform-mcp-plugin-metadata-mutation-organization"
	AccessRoleMutationConnectionLimitName         = "platform-mcp-access-role-mutation-connection"
	AccessRoleMutationOrganizationLimitName       = "platform-mcp-access-role-mutation-organization"
	ShadowAccessDecisionConnectionLimitName       = "platform-mcp-shadow-access-decision-connection"
	ShadowAccessDecisionOrganizationLimitName     = "platform-mcp-shadow-access-decision-organization"
	ToolExposureReadConnectionLimitName           = "platform-mcp-tool-exposure-read-connection"
	ToolExposureReadOrganizationLimitName         = "platform-mcp-tool-exposure-read-organization"
	ToolExposureMutationConnectionLimitName       = "platform-mcp-tool-exposure-mutation-connection"
	ToolExposureMutationOrganizationLimitName     = "platform-mcp-tool-exposure-mutation-organization"
	DataExportToggleConnectionLimitName           = "platform-mcp-data-export-toggle-connection"
	DataExportToggleOrganizationLimitName         = "platform-mcp-data-export-toggle-organization"
	PluginRepublishConnectionLimitName            = "platform-mcp-plugin-republish-connection"
	PluginRepublishOrganizationLimitName          = "platform-mcp-plugin-republish-organization"
	ProjectMutationConnectionLimitName            = "platform-mcp-project-mutation-connection"
	ProjectMutationOrganizationLimitName          = "platform-mcp-project-mutation-organization"
)

const (
	// DocsQueriesPerConnectionPerMinute allows iterative documentation research
	// over the bounded in-process corpus without throttling ordinary reading.
	DocsQueriesPerConnectionPerMinute = 120

	// DocsQueriesPerOrganizationPerMinute leaves room for concurrent researchers
	// while retaining a ceiling on repeated in-process retrieval.
	DocsQueriesPerOrganizationPerMinute = 1200

	// SkillsOperationsPerConnectionPerMinute leaves room for reviewing dozens of
	// suggestions, including the reads and approvals sharing this allowance.
	SkillsOperationsPerConnectionPerMinute = 120

	// SkillsOperationsPerOrganizationPerMinute allows multiple connections to
	// review skills concurrently while retaining an organization-wide ceiling.
	SkillsOperationsPerOrganizationPerMinute = 1200

	// LifecycleOperationsPerConnectionPerMinute supports read/change/read across
	// several servers. The moderate ceiling also bounds client-admission writes.
	LifecycleOperationsPerConnectionPerMinute = 30

	// LifecycleOperationsPerOrganizationPerMinute leaves room for concurrent
	// administrators without removing the shared metadata/admission ceiling.
	LifecycleOperationsPerOrganizationPerMinute = 300

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

	// RiskMutationsPer* bound all risk policy and exclusion writes together.
	// Keeping one shared budget prevents a caller from multiplying the permitted
	// write rate by alternating between mutation tools.
	RiskMutationsPerConnectionPerMinute   = 5
	RiskMutationsPerOrganizationPerMinute = 50

	// PluginAssignmentMutationsPerConnectionPerMinute supports rolling out
	// assignments across dozens of plugins while bounding access-affecting writes.
	PluginAssignmentMutationsPerConnectionPerMinute = 30

	// PluginAssignmentMutationsPerOrganizationPerMinute allows concurrent
	// administrators to manage assignments under an organization-wide ceiling.
	PluginAssignmentMutationsPerOrganizationPerMinute = 300

	// AccessRoleMutationsPerConnectionPerMinute supports creating, updating and
	// assigning roles in one bulk administration workflow.
	AccessRoleMutationsPerConnectionPerMinute = 30

	// AccessRoleMutationsPerOrganizationPerMinute bounds access-role changes
	// across concurrent administrators independently of other mutation budgets.
	AccessRoleMutationsPerOrganizationPerMinute = 300

	ShadowAccessDecisionsPerConnectionPerMinute   = 5
	ShadowAccessDecisionsPerOrganizationPerMinute = 50
	ReviewRequestsPerConnectionPerMinute          = 5
	ReviewRequestsPerOrganizationPerMinute        = 50

	// PluginMetadataMutationsPer* bound creating and renaming plugins together
	// on their own allowance. This is what stops a conversational create loop;
	// there is no separate cap on how many plugins a project holds, because the
	// dashboard has none and the two paths must refuse the same requests.
	PluginMetadataMutationsPerConnectionPerMinute   = 5
	PluginMetadataMutationsPerOrganizationPerMinute = 50

	// ToolExposureReadsPerConnectionPerMinute allows paging through bounded
	// project tool definitions without spending the separate mutation allowance.
	ToolExposureReadsPerConnectionPerMinute = 120

	// ToolExposureReadsPerOrganizationPerMinute supports concurrent tool
	// inventory inspection while retaining an organization-wide read ceiling.
	ToolExposureReadsPerOrganizationPerMinute = 1200

	// ToolExposureMutationsPer* bound adding and removing a server's tools on
	// their own allowance. Both tools share it, so alternating between them
	// cannot multiply the permitted write rate: each allowed call takes the
	// toolset row lock, appends a version, and republishes every plugin
	// carrying the server.
	ToolExposureMutationsPerConnectionPerMinute   = 5
	ToolExposureMutationsPerOrganizationPerMinute = 50

	// DataExportTogglesPer* bound pausing and resuming data export routes on
	// their own allowance. Both tools share it, so alternating between them
	// cannot multiply the permitted write rate. A person handling an incident
	// pauses a handful of routes, never dozens a minute; each allowed call
	// takes the route row lock and writes an audit entry.
	DataExportTogglesPerConnectionPerMinute   = 5
	DataExportTogglesPerOrganizationPerMinute = 50

	// PluginRepublishesPer* bound republish_plugin on its own allowance. Each
	// allowed call can regenerate every package in a project, so it is metered
	// like the other package-affecting writes rather than like a read.
	PluginRepublishesPerConnectionPerMinute   = 5
	PluginRepublishesPerOrganizationPerMinute = 50

	// ProjectMutationsPer* bound creating and renaming projects on their own
	// allowance, shared by both tools so alternating between them cannot
	// multiply the write rate. A person sets up a handful of projects at a
	// time; a loop creating them by the dozen is a runaway agent, and every
	// created project is a row an administrator has to delete by hand.
	ProjectMutationsPerConnectionPerMinute   = 5
	ProjectMutationsPerOrganizationPerMinute = 50

	// DrilldownRowsPerConnectionPerWindow and
	// DrilldownMetricQueriesPerConnectionPerWindow are the second cap the
	// drill-downs carry, over DrilldownVolumeWindow. A per-minute call budget
	// alone does not bound how much a caller can accumulate: paging steadily
	// under the call rate still walks an entire window's occurrences. These
	// meter the volume rather than the calls.
	DrilldownRowsPerConnectionPerWindow          = 1000
	DrilldownMetricQueriesPerConnectionPerWindow = 20

	// SessionRecallsPer* bound continue_session. Metered separately and lower
	// than every other read: each allowed call serves an entire session
	// transcript as a digest, so this allowance must not be fundable by
	// spending any other budget.
	SessionRecallsPerConnectionPerMinute   = 10
	SessionRecallsPerOrganizationPerMinute = 100
)

// DrilldownVolumeWindow is the interval the drill-down volume caps refill over.
const DrilldownVolumeWindow = 10 * time.Minute

var (
	ErrOperationRateLimited       = errors.New("platform mcp operation rate limited")
	ErrOperationBudgetUnavailable = errors.New("platform mcp operation budget unavailable")
)

// Limiter is the narrow Platform MCP boundary around Speakeasy's shared rate limiter.
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
	return b.allow(ctx, principal, false)
}

// AllowConnectionOrOrganization charges OAuth calls to both their connection
// and organization buckets. A connection-less assistant has no connection to
// charge, so it consumes only the organization allowance.
func (b OperationBudget) AllowConnectionOrOrganization(ctx context.Context, principal Principal) error {
	return b.allow(ctx, principal, true)
}

func (b OperationBudget) allow(ctx context.Context, principal Principal, organizationOnlyWithoutConnection bool) error {
	if !b.valid() || principal.OrganizationID == "" {
		return ErrOperationBudgetUnavailable
	}
	if principal.HasConnection() || !organizationOnlyWithoutConnection {
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
// into one empty-key bucket. A dashboard setup handoff has no OAuth connection
// either, so it receives its own user-scoped bucket.
func operationBudgetActorKey(principal Principal) (string, error) {
	if principal.HasConnection() {
		if principal.ConnectionID == "" {
			return "", ErrOperationBudgetUnavailable
		}
		return principal.ConnectionID, nil
	}
	if principal.UserID == "" {
		return "", ErrOperationBudgetUnavailable
	}
	switch principal.surface() {
	case SurfaceProjectAssistant:
		if principal.ClientID == "" {
			return "", ErrOperationBudgetUnavailable
		}
		return "assistant:" + principal.ClientID + ":" + principal.UserID, nil
	case SurfaceDashboard:
		return "dashboard:" + principal.UserID, nil
	default:
		return "", ErrOperationBudgetUnavailable
	}
}

// OperationBudgets groups the independently metered public Platform MCP
// operations. Every value is injected at composition; no production defaults
// are assigned here.
type OperationBudgets struct {
	// RiskFindings meters row-level Watchdog reads independently.
	RiskFindings   OperationBudget
	Catalog        OperationBudget
	Registration   OperationBudget
	ReviewRequests OperationBudget
	Handoff        OperationBudget
	SetupStart     OperationBudget
	Repair         OperationBudget
	Docs           OperationBudget
	// Skills meters authoring and distribution together. Reads and writes share
	// one allowance because they are one workflow: a caller reads a skill to
	// obtain the version token its next write needs, and metering the read
	// separately would only let a loop spend twice as much reaching the same
	// write.
	Skills            OperationBudget
	LifecycleMetadata OperationBudget
	// Plugins meters the plugin inventory reads. They are bounded PostgreSQL
	// reads of a project's own plugins, metered separately from diagnostics so
	// an administrator walking the inventory does not spend the allowance the
	// failure diagnosis it leads to will need.
	Plugins OperationBudget
	// AccessReads meters role/member access inspection separately. Member search
	// returns masked personal data and must not be fundable by another read lane.
	AccessReads OperationBudget
	// AccessRoleMutations independently meters custom MCP access-role writes.
	AccessRoleMutations OperationBudget
	// Diagnostics meters the observability reads. They are bounded aggregate
	// queries over Speakeasy-owned telemetry, so the cost being metered is the
	// ClickHouse scan, not an external egress.
	Diagnostics OperationBudget
	// SensitiveDiagnostics meters the bounded drill-downs. It is separate from
	// Diagnostics so exhausting it is not possible by spending the summary
	// allowance, and so it can be tightened on its own.
	SensitiveDiagnostics OperationBudget
	// SensitiveSessionRecall meters continue_session — the only operation that
	// serves whole-transcript content — on its own low allowance.
	SensitiveSessionRecall OperationBudget
	// RiskMutations is shared by policy and exclusion writes. Connection-less
	// assistant calls consume only its organization bucket.
	RiskMutations OperationBudget

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
	return b.RiskFindings.valid() && b.Catalog.valid() && b.Registration.valid() && b.ReviewRequests.valid() && b.Handoff.valid() && b.SetupStart.valid() && b.Repair.valid() && b.Docs.valid() && b.Skills.valid() && b.LifecycleMetadata.valid() && b.Plugins.valid() && b.AccessReads.valid() && b.AccessRoleMutations.valid() && b.Diagnostics.valid() && b.SensitiveDiagnostics.valid() && b.SensitiveSessionRecall.valid() && b.RiskMutations.valid() && b.DrilldownVolume.valid()
}
