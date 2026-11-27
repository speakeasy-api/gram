package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// OrganizationDisabledMessage is what a caller acting in a disabled
// organization is told. Sign-in, sessions and API keys use the same words.
const OrganizationDisabledMessage = "this organization is disabled, please reach out to support@speakeasy.com for more information"

// requireOrganizationEnabled refuses every caller acting in an organization
// that staff or identity-provider sync have disabled. PrepareContext runs it
// first, so it covers each way a request reaches authorization: dashboard
// sessions, API keys, principal credentials, workload and agent sessions,
// assistant runtime tokens, and MCP serving.
//
// An engine built without a database evaluates only the grants it is given,
// and an organization with no row is not disabled: the authentication path
// decides whether an unknown organization may be used at all.
func (e *Engine) requireOrganizationEnabled(ctx context.Context, organizationID string) error {
	if organizationID == "" || e.db == nil {
		return nil
	}

	org, err := orgrepo.New(e.db).GetOrganizationMetadata(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		e.logger.ErrorContext(ctx, "read organization for authorization", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
		return fmt.Errorf("read organization for authorization: %w", err)
	case org.DisabledAt.Valid:
		return oops.E(oops.CodeUnauthorized, nil, OrganizationDisabledMessage)
	}

	return nil
}
