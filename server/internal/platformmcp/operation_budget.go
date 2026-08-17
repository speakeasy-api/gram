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

// OperationBudget applies independently configured caller and organization
// buckets. The caller bucket is always charged first; a denial prevents the
// second bucket, mutations, and provider egress.
type OperationBudget struct {
	Connection   Limiter
	Organization Limiter
}

func (b OperationBudget) valid() bool {
	return b.Connection != nil && b.Organization != nil
}

// callerBudgetKey names the bucket a single caller is metered in. An OAuth
// client is metered per connection, so reauthorization does not reset its
// allowance. A connection-less surface — the project assistant acts under
// assistant identity — has no connection to meter, so it is metered per acting
// user instead. Keying on the subject URN keeps the two namespaces disjoint.
func callerBudgetKey(principal Principal) string {
	if principal.ConnectionID != "" {
		return principal.ConnectionID
	}
	if principal.UserID == "" {
		return ""
	}
	return userSubjectURN(principal.UserID)
}

func (b OperationBudget) Allow(ctx context.Context, principal Principal) error {
	callerKey := callerBudgetKey(principal)
	if !b.valid() || callerKey == "" || principal.OrganizationID == "" {
		return ErrOperationBudgetUnavailable
	}
	connection, err := b.Connection.Allow(ctx, callerKey)
	if err != nil {
		return fmt.Errorf("limit platform mcp connection operation: %w: %w", ErrOperationBudgetUnavailable, err)
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
