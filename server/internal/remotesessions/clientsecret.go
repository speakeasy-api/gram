package remotesessions

import (
	"context"
	"errors"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/clientsecrets"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// clientSecretWriteError reports a failure to store or remove a client
// secret from a handler. An unusable organization key is retryable and says
// so; it is never reported as a problem with the client's configuration.
func clientSecretWriteError(ctx context.Context, logger *slog.Logger, err error) error {
	switch {
	case clientsecrets.IsKeyProblem(err):
		return oops.E(oops.CodeUnavailable, err, "the organization's encryption key is unavailable, so the client secret could not be stored; try again shortly").LogError(ctx, logger)
	case errors.Is(err, clientsecrets.ErrClientNotFound):
		return oops.E(oops.CodeNotFound, err, "remote session client not found").LogWarn(ctx, logger)
	case clientsecrets.IsUnreadable(err):
		// Only a copy opens a stored secret: the source client's no longer
		// decrypts.
		return oops.E(oops.CodeConflict, err, "the source client's stored secret could not be read; enter a client_secret instead").LogWarn(ctx, logger)
	default:
		return oops.E(oops.CodeUnexpected, err, "store client secret").LogError(ctx, logger)
	}
}

// hasClientSecret reports whether a client row stores a secret in either
// copy.
func hasClientSecret(client repo.RemoteSessionClient) bool {
	return clientsecrets.Has(clientsecrets.Stored(client.ClientSecretEncrypted, client.ClientSecretOrgEncrypted))
}

// hasNonEmptyClientSecret reports whether a client row stores a non-empty
// ciphertext in either copy, without opening it.
func hasNonEmptyClientSecret(client repo.RemoteSessionClient) bool {
	return (client.ClientSecretEncrypted.Valid && client.ClientSecretEncrypted.String != "") ||
		(client.ClientSecretOrgEncrypted.Valid && client.ClientSecretOrgEncrypted.String != "")
}

// clientOwner returns who a client row belongs to, which decides the key its
// secret is sealed with.
func clientOwner(client repo.RemoteSessionClient) clientsecrets.Owner {
	return clientsecrets.OwnerOf(client.OrganizationID, client.ProjectID)
}
