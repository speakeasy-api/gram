package platformmcp

import (
	"context"
	"errors"
	"fmt"

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
	ProbeConnectionLimitName          = "platform-mcp-probe-connection"
	ProbeOrganizationLimitName        = "platform-mcp-probe-organization"
)

const (
	// DocsQueriesPerConnectionPerMinute and DocsQueriesPerOrganizationPerMinute
	// bound documentation search. Retrieval is in-process and cheap, so these
	// exist to stop a loop from spending the caller's context on repeated
	// queries rather than to protect a backend.
	DocsQueriesPerConnectionPerMinute   = 10
	DocsQueriesPerOrganizationPerMinute = 100
)

const (
	// ProbesPerConnectionPerMinute and ProbesPerOrganizationPerMinute bound
	// remote MCP verification probes. They sit deliberately below the shared
	// read allowances: each probe makes Gram perform egress to an arbitrary
	// user-supplied host, so an unbounded probe tool would be an SSRF and
	// port-scan primitive. The guardian policy bounds where a probe may go;
	// this budget bounds how often one may go anywhere.
	ProbesPerConnectionPerMinute   = 3
	ProbesPerOrganizationPerMinute = 15
)

var (
	ErrOperationRateLimited       = errors.New("platform mcp operation rate limited")
	ErrOperationBudgetUnavailable = errors.New("platform mcp operation budget unavailable")
)

// Limiter is the narrow Platform MCP boundary around Gram's shared rate limiter.
// It lets unit tests deterministically model an allowance, a throttle, or a
// backing-store failure without depending on Redis.
type Limiter interface {
	Allow(ctx context.Context, key string) (ratelimit.Result, error)
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
	// A surface acting under assistant identity holds no OAuth connection, so
	// there is no connection bucket to charge and the organization bucket meters
	// it alone. Refusing the operation instead would deny every connection-less
	// caller, and keying the connection bucket on the empty string would pool
	// every such caller across every organization into one bucket.
	if principal.HasConnection() {
		// HasConnection is an OR, so a principal claiming a connection through its
		// generation alone lands here rather than being metered as connection-less.
		// It has no key to charge, and charging the empty string would pool every
		// such caller into one bucket, so the budget is unavailable to it.
		if principal.ConnectionID == "" {
			return ErrOperationBudgetUnavailable
		}
		connection, err := b.Connection.Allow(ctx, principal.ConnectionID)
		if err != nil {
			return fmt.Errorf("limit platform mcp connection operation: %w: %w", ErrOperationBudgetUnavailable, err)
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
	Skills OperationBudget

	// Probe meters remote MCP verification probes on its own, tighter,
	// allowance rather than sharing a read budget: every probe is egress to an
	// arbitrary user-supplied host with Gram as the egress point.
	Probe OperationBudget
}

func (b OperationBudgets) Valid() bool {
	return b.Catalog.valid() && b.Registration.valid() && b.Handoff.valid() && b.SetupStart.valid() && b.Repair.valid() && b.Docs.valid() && b.Skills.valid() && b.Probe.valid()
}
