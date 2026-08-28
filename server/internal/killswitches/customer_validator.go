package killswitches

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

const (
	customerPrincipalKindUser     PrincipalKind = "user"
	customerResourceKindMCPServer ResourceKind  = "mcp_server"
)

// customerLifecycleValidator performs owner-domain, transaction-enlisted locks
// for the only principal and resource kinds exposed by the customer API.
type customerLifecycleValidator struct{}

var _ LifecycleValidator = customerLifecycleValidator{}

// NewCustomerLifecycleValidator returns the owner-domain validator used by the
// curated customer service.
func NewCustomerLifecycleValidator() LifecycleValidator {
	return customerLifecycleValidator{}
}

func (customerLifecycleValidator) ValidateCurrent(ctx context.Context, dbtx LifecycleTransactionQueries, batch CurrentReferenceBatch) error {
	if batch.Principal != nil {
		if batch.Principal.Kind != customerPrincipalKindUser {
			return fmt.Errorf("%w: unsupported user reference", ErrInvalidReference)
		}
		_, err := orgrepo.New(dbtx).LockActiveOrganizationUser(ctx, orgrepo.LockActiveOrganizationUserParams{
			UserID: pgtype.Text{String: string(batch.Principal.Key), Valid: true}, OrganizationID: string(batch.OrganizationID),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: user is not available", ErrInvalidReference)
		}
		if err != nil {
			return fmt.Errorf("lock current organization user: %w", err)
		}
	}

	if batch.Resources == nil {
		return nil
	}
	if batch.Resources.Kind != customerResourceKindMCPServer {
		return fmt.Errorf("%w: unsupported server reference", ErrInvalidReference)
	}
	ids := make([]uuid.UUID, len(batch.Resources.Keys))
	for i, key := range batch.Resources.Keys {
		id, err := uuid.Parse(string(key))
		if err != nil || id.String() != string(key) {
			return fmt.Errorf("%w: server is not available", ErrInvalidReference)
		}
		ids[i] = id
	}
	locked, err := mcpserversrepo.New(dbtx).LockLiveMCPServersInOrganization(ctx, mcpserversrepo.LockLiveMCPServersInOrganizationParams{
		Ids: ids, OrganizationID: string(batch.OrganizationID),
	})
	if err != nil {
		return fmt.Errorf("lock current organization servers: %w", err)
	}
	if len(locked) != len(ids) {
		return fmt.Errorf("%w: one or more servers are not available", ErrInvalidReference)
	}
	return nil
}
