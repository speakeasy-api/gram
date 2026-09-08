// Package clientcred derives what credential a user_session_client row
// requires from the columns persisted on it.
//
// The authorization server branches on the result to decide what a client may
// present, and the management API reports it to operators. Both read it from
// here so what is enforced and what is displayed cannot drift apart.
package clientcred

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

// Kind is the class of credential a client row requires.
type Kind string

const (
	// KindPublic is a public client: nothing may be presented.
	KindPublic Kind = "public"

	// KindSecret is a symmetric client: the stored secret hash must match the
	// presented secret.
	KindSecret Kind = "secret"

	// KindKey is an asymmetric client: an assertion signed by the client's
	// published key must verify.
	KindKey Kind = "key"

	// KindMisconfigured is a row whose columns contradict each other, or whose
	// method this code does not recognize. Such a client cannot authenticate at
	// all. Only Resolve returns it, for display; KindOf reports the same rows as
	// an error.
	KindMisconfigured Kind = "misconfigured"
)

// KindOf derives what a client row requires from its declared
// token_endpoint_auth_method and whether the row stores a client secret.
//
// A NULL method is the population that predates the column, and is read the way
// the token endpoint always read it: a stored secret hash means a symmetric
// client, otherwise public. Any non-NULL value must be recognized and must
// agree with the row's other columns; a value this code does not know, or a row
// whose columns contradict its method, is an error rather than a fallback to
// public. Degrading an unreadable method to KindPublic would be precisely the
// downgrade the column exists to prevent.
//
// Never returns KindMisconfigured. Callers that authenticate a client have to
// refuse such a row outright rather than treat it as a fourth kind.
func KindOf(method pgtype.Text, hasSecret bool) (Kind, error) {
	if !method.Valid {
		if hasSecret {
			return KindSecret, nil
		}
		return KindPublic, nil
	}
	switch declared := method.String; declared {
	case oauthwire.AuthMethodClientSecretBasic, oauthwire.AuthMethodClientSecretPost:
		if !hasSecret {
			return "", fmt.Errorf("client declares %s but stores no secret", declared)
		}
		return KindSecret, nil
	case oauthwire.AuthMethodNone:
		if hasSecret {
			return "", errors.New("client declares none but stores a secret")
		}
		return KindPublic, nil
	case oauthwire.AuthMethodPrivateKeyJWT:
		if hasSecret {
			return "", errors.New("client declares private_key_jwt but stores a secret")
		}
		return KindKey, nil
	default:
		return "", fmt.Errorf("client declares unrecognized method %q", declared)
	}
}

// Resolve reports the kind for display, folding a row KindOf rejects into
// KindMisconfigured. That state is worth naming on an operator surface: the
// client cannot authenticate at all, and today the only trace of it is a log
// line emitted when the client next tries.
//
// Nothing that authenticates a client may use this. It erases the distinction
// between a recognized kind and an unreadable row, which is the distinction
// that makes the token endpoint fail closed.
func Resolve(method pgtype.Text, hasSecret bool) Kind {
	kind, err := KindOf(method, hasSecret)
	if err != nil {
		return KindMisconfigured
	}
	return kind
}

// ForBoundClient reports a session's registration for display: the credential
// kind and the raw method it declared, both nil when the session has no bound
// client.
//
// The queries that lift these columns onto a session row do so through a LEFT
// JOIN, so an absent method means either a client registered before the column
// existed or no client at all. Resolving only for a bound client keeps that
// ambiguity off the wire, where a reader cannot tell the two apart.
func ForBoundClient(bound bool, method pgtype.Text, hasSecret bool) (kind, declared *string) {
	if !bound {
		return nil, nil
	}
	resolved := string(Resolve(method, hasSecret))
	if !method.Valid {
		return &resolved, nil
	}
	return &resolved, &method.String
}
