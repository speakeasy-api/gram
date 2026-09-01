package mcptoolexecution

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	litellmrepo "github.com/speakeasy-api/gram/server/internal/litellm/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// customerLifecycleValidator performs owner-domain, transaction-enlisted locks
// for the MCP tool-execution principal and resource kinds.
type customerLifecycleValidator struct{}

var _ killswitches.LifecycleValidator = customerLifecycleValidator{}

func NewCustomerLifecycleValidator() killswitches.LifecycleValidator {
	return customerLifecycleValidator{}
}

func (customerLifecycleValidator) ValidateCurrent(ctx context.Context, dbtx killswitches.LifecycleTransactionQueries, batch killswitches.CurrentReferenceBatch) error {
	// Lock the principal first, then resources in canonical UUID order. Every
	// lifecycle mutation uses this order to avoid cross-operation deadlocks.
	if batch.Principal != nil {
		if batch.Principal.Kind != PrincipalKindUser {
			return fmt.Errorf("%w: unsupported user reference", killswitches.ErrInvalidReference)
		}
		_, err := orgrepo.New(dbtx).LockActiveOrganizationUser(ctx, orgrepo.LockActiveOrganizationUserParams{
			UserID: pgtype.Text{String: string(batch.Principal.Key), Valid: true}, OrganizationID: string(batch.OrganizationID),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: user is not available", killswitches.ErrInvalidReference)
		}
		if err != nil {
			return fmt.Errorf("lock current organization user: %w", err)
		}
	}

	if batch.Resources == nil {
		return nil
	}
	if len(batch.Resources.Keys) == 0 {
		return killswitches.ErrInvalidReference
	}
	if batch.Resources.Kind == ResourceKindHookActivity {
		if batch.OrganizationID == "" {
			return fmt.Errorf("%w: hook activity is not available", killswitches.ErrInvalidReference)
		}
		for _, key := range batch.Resources.Keys {
			if !supportedHookActivity(string(key)) {
				return fmt.Errorf("%w: hook activity is not available", killswitches.ErrInvalidReference)
			}
		}
		return nil
	}
	ids, ok := canonicalResourceIDs(batch.Resources.Keys)
	if !ok {
		return fmt.Errorf("%w: resource is not available", killswitches.ErrInvalidReference)
	}
	var locked []uuid.UUID
	var err error
	resourceName := ""
	switch batch.Resources.Kind {
	case ResourceKindLiteLLMInstance:
		resourceName = "LiteLLM instances"
		locked, err = litellmrepo.New(dbtx).LockActiveLiteLLMInstancesInOrganization(ctx, litellmrepo.LockActiveLiteLLMInstancesInOrganizationParams{
			Ids: ids, OrganizationID: string(batch.OrganizationID),
		})
	case ResourceKindMCPServer:
		resourceName = "servers"
		locked, err = mcpserversrepo.New(dbtx).LockLiveMCPServersInOrganization(ctx, mcpserversrepo.LockLiveMCPServersInOrganizationParams{
			Ids: ids, OrganizationID: string(batch.OrganizationID),
		})
	default:
		return fmt.Errorf("%w: unsupported resource reference", killswitches.ErrInvalidReference)
	}
	if err != nil {
		return fmt.Errorf("lock current organization %s: %w", resourceName, err)
	}
	if len(locked) != len(ids) {
		return fmt.Errorf("%w: one or more %s are not available", killswitches.ErrInvalidReference, resourceName)
	}
	return nil
}

// ValidateLiveMCPServersInOrganization validates a canonical resource batch in
// one owner-domain query. It deliberately returns only an opaque boolean.
func ValidateLiveMCPServersInOrganization(ctx context.Context, dbtx killswitches.LifecycleTransactionQueries, organizationID killswitches.OrganizationID, keys []killswitches.ResourceKey) (bool, error) {
	if organizationID == "" {
		return false, nil
	}
	ids, ok := canonicalResourceIDs(keys)
	if !ok {
		return false, nil
	}
	found, err := mcpserversrepo.New(dbtx).ListLiveMCPServerIDsInOrganization(ctx, mcpserversrepo.ListLiveMCPServerIDsInOrganizationParams{
		Ids: ids, OrganizationID: string(organizationID),
	})
	if err != nil {
		return false, fmt.Errorf("check live mcp server ownership: %w", err)
	}
	return len(found) == len(ids), nil
}

func canonicalResourceIDs(keys []killswitches.ResourceKey) ([]uuid.UUID, bool) {
	ids := make([]uuid.UUID, len(keys))
	for i, key := range keys {
		id, err := uuid.Parse(string(key))
		if err != nil || id == uuid.Nil || id.String() != string(key) {
			return nil, false
		}
		ids[i] = id
	}
	return ids, true
}
