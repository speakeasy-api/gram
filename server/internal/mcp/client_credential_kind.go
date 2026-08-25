package mcp

import (
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// clientCredentialKind is the class of credential a client row requires.
type clientCredentialKind string

const (
	// credentialKindNone is a public client: nothing may be presented.
	credentialKindNone clientCredentialKind = "none"

	// credentialKindSecret is a symmetric client: the stored secret hash
	// must match the presented secret.
	credentialKindSecret clientCredentialKind = "secret"

	// credentialKindAssertion is an asymmetric client: an assertion signed
	// by the client's published key must verify.
	credentialKindAssertion clientCredentialKind = "assertion"
)

// clientCredentialKindOf derives what a client row requires from its
// persisted authentication method.
//
// NULL is the population that predates the column and is read the way the
// token endpoint always read it: a stored secret hash means a symmetric
// client, otherwise public. Any non-NULL value must be recognized and must
// agree with the row's other columns; a value this code does not know, or a
// row whose columns contradict its method, is an error rather than a fallback
// to public. Degrading an unreadable method to "none" would be precisely the
// downgrade the column exists to prevent.
func clientCredentialKindOf(row *usersessions_repo.UserSessionClient) (clientCredentialKind, error) {
	if !row.TokenEndpointAuthMethod.Valid {
		if row.ClientSecretHash.Valid {
			return credentialKindSecret, nil
		}
		return credentialKindNone, nil
	}
	switch method := row.TokenEndpointAuthMethod.String; method {
	case oauthwire.AuthMethodClientSecretBasic, oauthwire.AuthMethodClientSecretPost:
		if !row.ClientSecretHash.Valid {
			return "", fmt.Errorf("client declares %s but stores no secret", method)
		}
		return credentialKindSecret, nil
	case oauthwire.AuthMethodNone:
		if row.ClientSecretHash.Valid {
			return "", errors.New("client declares none but stores a secret")
		}
		return credentialKindNone, nil
	case oauthwire.AuthMethodPrivateKeyJWT:
		if row.ClientSecretHash.Valid {
			return "", errors.New("client declares private_key_jwt but stores a secret")
		}
		return credentialKindAssertion, nil
	default:
		return "", fmt.Errorf("client declares unrecognized method %q", method)
	}
}
