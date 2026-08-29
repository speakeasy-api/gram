package mcptoolexecution

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func testIdentity(t *testing.T, kind mcpidentity.Kind, userID string) mcpidentity.Identity {
	t.Helper()
	identity, ok := mcpidentity.FromContext(testIdentityContext(t, kind, userID))
	require.True(t, ok)
	return identity
}

type provenanceNeverRevoked struct{}

func (provenanceNeverRevoked) IsTokenRevoked(context.Context, string) (bool, error) {
	return false, nil
}

func stampValidatedSession(t *testing.T, boundary *mcpidentity.ValidatorBoundary, subject urn.SessionSubject) context.Context {
	t.Helper()
	signer := sessiontokens.NewSigner("killswitch-provenance-test-secret")
	token, _, err := signer.Mint(sessiontokens.MintParams{Subject: subject, Audience: "killswitch-test", Issuer: "killswitch-test", Lifetime: time.Hour})
	require.NoError(t, err)
	proof, err := signer.ValidateBearer(t.Context(), token, "killswitch-test", provenanceNeverRevoked{})
	require.NoError(t, err)
	return boundary.StampValidatedSession(t.Context(), proof)
}

func testIdentityContext(t *testing.T, kind mcpidentity.Kind, userID string) context.Context {
	t.Helper()
	boundary := mcpidentity.NewValidatorBoundary()
	switch kind {
	case mcpidentity.KindUserSession:
		return stampValidatedSession(t, boundary, urn.NewUserSubject(userID))
	case mcpidentity.KindAnonymous:
		return stampValidatedSession(t, boundary, urn.NewAnonymousSubject("session"))
	case mcpidentity.KindAPIKey:
		return boundary.StampAPIKey(t.Context())
	case mcpidentity.KindAssistant:
		return boundary.StampAssistant(t.Context())
	case mcpidentity.KindDelegatedUser:
		return boundary.StampDelegatedUser(t.Context(), userID)
	case mcpidentity.KindChatSession:
		return boundary.StampChatSession(t.Context())
	default:
		return t.Context()
	}
}
