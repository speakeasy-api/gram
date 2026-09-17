package remotesessions

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/managedrows"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// requireIssuerWithoutManagedClients refuses an issuer write while a managed
// client in the organization is registered against it.
func requireIssuerWithoutManagedClients(ctx context.Context, logger *slog.Logger, txRepo *repo.Queries, issuerID uuid.UUID, organizationID string) error {
	managed, err := txRepo.ManagedRemoteSessionClientExistsForIssuer(ctx, repo.ManagedRemoteSessionClientExistsForIssuerParams{
		RemoteSessionIssuerID: issuerID,
		OrganizationID:        conv.ToPGText(organizationID),
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check for managed remote session clients").LogError(ctx, logger)
	}
	if managed {
		return managedrows.Error("this identity provider")
	}

	return nil
}
