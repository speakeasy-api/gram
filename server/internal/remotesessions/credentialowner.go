package remotesessions

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// clientCredentialState is the credential configuration a client will hold
// once a create or update commits, which is what the self rules are decided
// on: an update that leaves a self client unable to authenticate must be
// refused even when the fields it sends are each valid on their own.
type clientCredentialState struct {
	// owner is the client's credential_owner.
	owner CredentialOwner

	// method is the token_endpoint_auth_method the client will hold.
	method string

	// hasSecret reports whether the client will hold a client secret.
	hasSecret bool

	// hasKeySet reports whether the client will hold a JSON Web Key Set.
	hasKeySet bool

	// legacyCallbackURL is the legacy callback mode the client will hold.
	legacyCallbackURL bool
}

// parseCreateCredentialOwner validates the credential_owner a create form
// carries. The HTTP decoder applies the subject default and the enum, but a
// direct service call does neither, and the column has no CHECK on its values,
// so an empty value is read as the default and anything unknown is refused.
func parseCreateCredentialOwner(ctx context.Context, logger *slog.Logger, raw string) (CredentialOwner, error) {
	switch owner := CredentialOwner(raw); owner {
	case "", CredentialOwnerSubject:
		return CredentialOwnerSubject, nil
	case CredentialOwnerSelf:
		return CredentialOwnerSelf, nil
	default:
		return "", oops.E(oops.CodeBadRequest, nil, "unknown credential_owner %q; use subject or self", raw).LogWarn(ctx, logger)
	}
}

// updatedClientCredentialState is the state an update patch leaves the client
// in. The update queries keep the stored value for every omitted field and
// never clear a secret, so a new secret only ever adds one.
func updatedClientCredentialState(existing repo.RemoteSessionClient, method *string, newSecret bool, legacyCallbackURL *bool) clientCredentialState {
	return clientCredentialState{
		owner:             CredentialOwner(existing.CredentialOwner),
		method:            conv.PtrValOr(method, existing.TokenEndpointAuthMethod.String),
		hasSecret:         newSecret || existing.ClientSecretEncrypted.Valid,
		hasKeySet:         existing.JsonWebKeySetID.Valid,
		legacyCallbackURL: conv.PtrValOr(legacyCallbackURL, existing.LegacyCallbackUrl),
	}
}

// requireSelfClientCredential refuses a self client configuration that cannot
// complete the client_credentials grant. The
// remote_session_clients_credential_owner_check constraint backs the structural
// half (an explicit method other than none, no legacy callback), and checking
// it here first turns a constraint violation into an error the caller can act
// on. The key material half (a secret for client_secret_*, a key set for
// private_key_jwt) has no constraint behind it.
func requireSelfClientCredential(state clientCredentialState) error {
	if state.owner != CredentialOwnerSelf {
		return nil
	}

	if state.legacyCallbackURL {
		return oops.E(oops.CodeBadRequest, nil, "a client with credential_owner self has no callback, so it cannot use the legacy callback URL")
	}

	switch TokenEndpointAuthMethod(state.method) {
	case TokenEndpointAuthMethodBasic, TokenEndpointAuthMethodPost:
		if !state.hasSecret {
			return oops.E(oops.CodeBadRequest, nil, "a client with credential_owner self and token_endpoint_auth_method %s requires a client_secret", state.method)
		}
	case TokenEndpointAuthMethodPrivateKeyJWT:
		if !state.hasKeySet {
			return oops.E(oops.CodeBadRequest, nil, "a client with credential_owner self and token_endpoint_auth_method private_key_jwt requires a JSON Web Key Set")
		}
	case TokenEndpointAuthMethodNone:
		return oops.E(oops.CodeBadRequest, nil, "a client with credential_owner self must authenticate at the token endpoint; use client_secret_basic, client_secret_post or private_key_jwt")
	default:
		return oops.E(oops.CodeBadRequest, nil, "a client with credential_owner self requires token_endpoint_auth_method client_secret_basic, client_secret_post or private_key_jwt")
	}

	return nil
}

// requireSelfClientIssuer refuses an issuer a self client cannot obtain a
// credential from. The client_credentials grant needs only the token
// endpoint, so an issuer entered by hand without discovery qualifies.
func requireSelfClientIssuer(owner CredentialOwner, issuer repo.RemoteSessionIssuer) error {
	if owner != CredentialOwnerSelf {
		return nil
	}

	if !issuer.TokenEndpoint.Valid || issuer.TokenEndpoint.String == "" {
		return oops.E(oops.CodeBadRequest, nil, "a client with credential_owner self requires an issuer with a token_endpoint")
	}

	return nil
}

// requireIssuerTokenEndpointForSelfClients refuses an issuer write that leaves
// the issuer without a token endpoint while a self client uses it, which
// requireSelfClientIssuer checked when each of those clients was created.
// Callers must hold LockRemoteSessionIssuerForClientBinding, which every client
// create path takes too, so no self client can be created against the issuer
// between the count and the commit. Issuer migration needs no separate check:
// it requires the target's token endpoint to match the source's.
func requireIssuerTokenEndpointForSelfClients(ctx context.Context, logger *slog.Logger, txRepo *repo.Queries, issuer repo.RemoteSessionIssuer) error {
	if issuer.TokenEndpoint.Valid && issuer.TokenEndpoint.String != "" {
		return nil
	}

	count, err := txRepo.CountSelfRemoteSessionClientsByIssuerID(ctx, issuer.ID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "count self remote session clients for issuer").LogError(ctx, logger)
	}

	if count > 0 {
		// The count spans tenants, so it stays out of the message.
		return oops.E(oops.CodeConflict, nil, "clients with credential_owner self use this issuer's token_endpoint, so it cannot be removed").LogWarn(ctx, logger)
	}

	return nil
}

// selfClientCreateAuthMethod returns the token_endpoint_auth_method a create
// stores. A subject client keeps an omitted method as NULL, which resolves at
// runtime from whether a secret is present. A self client stores the
// documented client_secret_basic default explicitly, because the
// credential_owner constraint requires a method on every self row.
func selfClientCreateAuthMethod(owner CredentialOwner, requested *string) *string {
	if owner != CredentialOwnerSelf || requested != nil {
		return requested
	}

	return new(string(TokenEndpointAuthMethodBasic))
}

// clientCreateGrantTypes returns the grant_types a create records. Subject
// clients leave it NULL (unknown). A self client never runs an interactive
// grant, so client_credentials is the only grant it uses.
func clientCreateGrantTypes(owner CredentialOwner) []string {
	if owner != CredentialOwnerSelf {
		return nil
	}

	return []string{oauthwire.GrantTypeClientCredentials}
}
