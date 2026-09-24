package remotesessions

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// The token_endpoint_auth_method rules for outbound clients, kept together the
// way usersessions/clientauthmethod.go keeps the inbound ones. The method
// constants themselves live in types.go, mirroring how the inbound rules sit
// apart from the oauthwire values they test.

// requireDetachableKeySet enforces the outbound half of the rule the inbound
// client registration surface already enforces as
// "private_key_jwt requires jwks or jwks_uri" (usersessions/jwks.ValidateKeySource).
// A client that declares private_key_jwt authenticates by signing an assertion
// with a key from its set; detaching the set leaves it declaring a method it
// cannot execute, which fails at the counterparty's token endpoint as an
// opaque 401 rather than here.
//
// Unreachable until AIM-156 adds private_key_jwt to tokenEndpointAuthMethodEnum.
// That ordering is deliberate: the guard lands before the value it guards is
// selectable, so there is never a window where an administrator can choose a
// method Gram cannot execute.
func requireDetachableKeySet(client repo.RemoteSessionClient) error {
	if TokenEndpointAuthMethod(client.TokenEndpointAuthMethod.String) != TokenEndpointAuthMethodPrivateKeyJWT {
		return nil
	}

	return oops.E(oops.CodeConflict, nil, "client authenticates with private_key_jwt and has nothing to sign with once the key set is detached; change token_endpoint_auth_method first")
}

// lockProjectClientForAuthMethodWrite and
// lockOrganizationClientForAuthMethodWrite pin the client row for the rest of
// the transaction, so the private_key_jwt coupling cannot be decided from a
// read another handler invalidates before either commits. Must run before the
// client is read, or the read it protects is the stale one.
//
// Each is scoped to its own surface's reachability rather than locking by id
// alone. An unscoped lock would let any authenticated caller take a row lock on
// another tenant's client for the length of a transaction before the ownership
// check rejects them. A missing row is left to the caller's own read, which
// reports it as not found.
func lockProjectClientForAuthMethodWrite(ctx context.Context, logger *slog.Logger, txRepo *repo.Queries, clientID uuid.UUID, projectID uuid.UUID) error {
	_, err := txRepo.LockRemoteSessionClientForAuthMethodWrite(ctx, repo.LockRemoteSessionClientForAuthMethodWriteParams{
		ID:        clientID,
		ProjectID: conv.ToNullUUID(projectID),
	})

	return interpretClientLock(ctx, logger, err)
}

func lockOrganizationClientForAuthMethodWrite(ctx context.Context, logger *slog.Logger, txRepo *repo.Queries, clientID uuid.UUID, organizationID string) error {
	_, err := txRepo.LockOrganizationRemoteSessionClientForAuthMethodWrite(ctx, repo.LockOrganizationRemoteSessionClientForAuthMethodWriteParams{
		ID:             clientID,
		OrganizationID: conv.ToPGText(organizationID),
	})

	return interpretClientLock(ctx, logger, err)
}

func interpretClientLock(ctx context.Context, logger *slog.Logger, err error) error {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "lock remote session client").LogError(ctx, logger)
	}

	return nil
}

// requirePrivateKeyJWTKeySet is the converse of requireDetachableKeySet: a
// client cannot declare private_key_jwt without a set to sign with, in the same
// way that requireDetachableKeySet stops the set being taken away afterwards.
//
// Neither rule is sufficient on its own, and neither is sufficient together
// without a lock: each reads a column the other writes, so two concurrent
// transactions can both pass against the same starting row and commit a client
// that declares private_key_jwt with no set. Every caller of either rule takes
// LockRemoteSessionClientForAuthMethodWrite on the client row first, which is
// what actually makes the pair hold.
//
// existing is the set the client already holds. On a create that is always the
// zero value, because a client is born without one: the link is attached
// through attachKeySet after the fact, so declaring private_key_jwt up front is
// always a refusal. On a global (platform-admin) client it is also always the
// zero value, permanently — those rows carry a NULL organization_id by
// construction and remote_session_clients_json_web_key_set_id_check forbids a
// set without one.
//
// Unreachable until AIM-156 adds private_key_jwt to tokenEndpointAuthMethodEnum;
// Goa rejects the value at the boundary until then. See
// TokenEndpointAuthMethodPrivateKeyJWT.
func requirePrivateKeyJWTKeySet(method *string, existing uuid.NullUUID) error {
	if method == nil || TokenEndpointAuthMethod(*method) != TokenEndpointAuthMethodPrivateKeyJWT {
		return nil
	}

	if existing.Valid {
		return nil
	}

	return oops.E(oops.CodeConflict, nil, "private_key_jwt requires an attached JSON Web Key Set; attach one before selecting this authentication method")
}
