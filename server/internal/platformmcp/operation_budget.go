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
	if principal.ConnectionID != "" {
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
}

func (b OperationBudgets) Valid() bool {
	return b.Catalog.valid() && b.Registration.valid() && b.Handoff.valid() && b.SetupStart.valid() && b.Repair.valid()
}
