package identitychaining

import (
	"context"

	"github.com/go-jose/go-jose/v4"
)

// assertionVerifier checks an identity provider's signature on a JWT and
// decodes its claims, leaving claim validation to the caller.
type assertionVerifier interface {
	VerifyAssertion(ctx context.Context, raw string, dest ...any) (jose.Header, error)
}
