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
	default:
		return oops.E(oops.CodeUnexpected, err, "store client secret").LogError(ctx, logger)
	}
}

// clientOwner returns who a client row belongs to, which decides the key its
// secret is sealed with.
func clientOwner(client repo.RemoteSessionClient) clientsecrets.Owner {
	return clientsecrets.OwnerOf(client.OrganizationID, client.ProjectID)
}
