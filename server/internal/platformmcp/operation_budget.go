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
)

const (
	// DocsQueriesPerConnectionPerMinute and DocsQueriesPerOrganizationPerMinute
	// bound documentation search. Retrieval is in-process and cheap, so these
	// exist to stop a loop from spending the caller's context on repeated
	// queries rather than to protect a backend.
	DocsQueriesPerConnectionPerMinute   = 10
	DocsQueriesPerOrganizationPerMinute = 100
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
	Skills OperationBudget
}

func (b OperationBudgets) Valid() bool {
	return b.Catalog.valid() && b.Registration.valid() && b.Handoff.valid() && b.SetupStart.valid() && b.Repair.valid() && b.Docs.valid() && b.Skills.valid()
}
