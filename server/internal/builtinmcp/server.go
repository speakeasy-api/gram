package builtinmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/builtinmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// EnsureServer materializes the stable built-in identity for typed auth associations.
// It never restores deleted servers or overwrites administrator-managed metadata.
// Callers must authorize discovery or resolve the runtime issuer before calling.
func EnsureServer(ctx context.Context, db repo.DBTX, organizationID string, projectID uuid.UUID, key string, issuerID uuid.NullUUID) error {
	definition, ok := Lookup(key)
	if !ok || key != Slack {
		return oops.C(oops.CodeNotFound)
	}
	queries := repo.New(db)
	if issuerID.Valid {
		if _, err := queries.ValidateUserSessionIssuer(ctx, repo.ValidateUserSessionIssuerParams{ID: issuerID.UUID, ProjectID: projectID, OrganizationID: organizationID}); err != nil {
			return fmt.Errorf("validate built-in server issuer: %w", err)
		}
	}
	err := queries.EnsureBuiltinServer(ctx, repo.EnsureBuiltinServerParams{
		ID: ID(projectID, key), ProjectID: projectID, OrganizationID: organizationID,
		Name: definition.Name, BuiltinKey: key, UserSessionIssuerID: issuerID,
	})
	if err != nil {
		return fmt.Errorf("insert built-in server: %w", err)
	}
	_, err = queries.SyncBuiltinServerIssuer(ctx, repo.SyncBuiltinServerIssuerParams{
		ID: ID(projectID, key), ProjectID: projectID, OrganizationID: organizationID,
		BuiltinKey: key, UserSessionIssuerID: issuerID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return fmt.Errorf("ensure built-in server: %w", err)
	}
	return nil
}
