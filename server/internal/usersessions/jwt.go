// Canonical Gram session-token primitives live in sessiontokens. This package
// retains aliases while the hosted user-session management API migrates.
package usersessions

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
)

// JWTSigningKeyFlag is the CLI flag (and env var via GRAM_JWT_SIGNING_KEY)
// consumed by server startup wiring.
const JWTSigningKeyFlag = "jwt-signing-key"

// TokenRevoker pushes a JTI into the shared revocation cache.
type TokenRevoker interface {
	RevokeToken(ctx context.Context, jti string) error
}

// Deprecated: use sessiontokens.RevocationChecker.
type RevocationChecker = sessiontokens.RevocationChecker

// Deprecated: use sessiontokens.SessionClaims.
type SessionClaims = sessiontokens.SessionClaims

// Deprecated: use sessiontokens.FirstPartyClientID.
const FirstPartyClientID = sessiontokens.FirstPartyClientID

// Deprecated: use sessiontokens.Signer.
type Signer = sessiontokens.Signer

// Deprecated: use sessiontokens.NewSigner.
func NewSigner(secret string) *sessiontokens.Signer {
	return sessiontokens.NewSigner(secret)
}

// Deprecated: use sessiontokens.MintParams.
type MintParams = sessiontokens.MintParams

// Deprecated: use sessiontokens.ValidatedSession.
type ValidatedSession = sessiontokens.ValidatedSession
