package upstreamclients

import (
	"errors"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
)

var (
	// ErrChoiceNeedsCredential is a no-auth choice for a server that needs
	// upstream authentication.
	ErrChoiceNeedsCredential = errors.New("this server needs upstream credentials; choose one")

	// ErrChoiceNotOffered names a credential the server does not offer
	// enabled.
	ErrChoiceNotOffered = errors.New("the server does not offer that credential")

	// ErrChoiceUpstreamAuthUndecided is a no-auth choice for a server whose
	// need for upstream authentication nobody has decided.
	ErrChoiceUpstreamAuthUndecided = errors.New("decide on the server whether it needs upstream authentication before choosing none")

	// ErrChoiceNeedsNoCredential names a credential for a server that needs
	// no upstream authentication.
	ErrChoiceNeedsNoCredential = errors.New("this server needs no upstream authentication; choose none")

	// ErrChoicePersonal names a person's own connection or identity-provider
	// sign-in for a caller that represents nobody.
	ErrChoicePersonal = errors.New("this choice is for a caller that acts for nobody, so it can only use a shared credential")
)

// CheckConfiguredChoice checks a choice configured ahead of use, by an agent
// key or an issuance profile, against what the server offers: offered is the
// server's ListServerConsentAuthClients rows, one with no client when it
// offers none. clientID is invalid for no upstream authentication, which only
// a server decided to need none takes; such a server takes nothing else. With
// sharedOnly, personal and identity-provider clients are refused. The choice
// grants nothing: whoever uses it is checked again on use.
func CheckConfiguredChoice(offered []repo.ListServerConsentAuthClientsRow, clientID uuid.NullUUID, sharedOnly bool) error {
	if len(offered) == 0 {
		return ErrChoiceNotOffered
	}
	requires := offered[0].RequiresUpstreamAuth
	needsNone := requires.Valid && !requires.Bool
	if !clientID.Valid {
		switch {
		case needsNone:
			return nil
		case requires.Valid || offered[0].AuthClientID.Valid:
			return ErrChoiceNeedsCredential
		default:
			return ErrChoiceUpstreamAuthUndecided
		}
	}
	if needsNone {
		return ErrChoiceNeedsNoCredential
	}
	for _, row := range offered {
		if row.AuthClientID != clientID {
			continue
		}
		if authz.IsSharedAuthClientType(row.ClientType.String) {
			return nil
		}
		if sharedOnly {
			return ErrChoicePersonal
		}
		return nil
	}
	return ErrChoiceNotOffered
}
