package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type provenanceNeverRevoked struct{}

func (provenanceNeverRevoked) IsTokenRevoked(context.Context, string) (bool, error) {
	return false, nil
}

func validatedSessionProof(t *testing.T, subject urn.SessionSubject) sessiontokens.ValidatedSession {
	t.Helper()
	signer := sessiontokens.NewSigner("mcp-provenance-test-secret")
	token, _, err := signer.Mint(sessiontokens.MintParams{Subject: subject, Audience: "mcp-test", Issuer: "mcp-test", Lifetime: time.Hour})
	require.NoError(t, err)
	proof, err := signer.ValidateBearer(t.Context(), token, "mcp-test", provenanceNeverRevoked{})
	require.NoError(t, err)
	return proof
}

// TestIdentityForValidatedSession pins the provenance mapping after bearer
// validation: only a concrete validated user session yields an acting user.
func TestIdentityForValidatedSession(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		subject  urn.SessionSubject
		wantKind mcpidentity.Kind
		wantUser string
	}{
		{name: "concrete user is authoritative", subject: urn.NewUserSubject("user_01J8EXAMPLE"), wantKind: mcpidentity.KindUserSession, wantUser: "user_01J8EXAMPLE"},
		{name: "api key never carries an acting user", subject: urn.NewAPIKeySubject(uuid.MustParse("11111111-1111-1111-1111-111111111111")), wantKind: mcpidentity.KindAPIKey},
		{name: "anonymous never carries an acting user", subject: urn.NewAnonymousSubject("session_01J8EXAMPLE"), wantKind: mcpidentity.KindAnonymous},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := mcpidentity.NewValidatorBoundary().StampValidatedSession(t.Context(), validatedSessionProof(t, tt.subject))
			got, stamped := mcpidentity.FromContext(ctx)
			require.True(t, stamped)
			require.Equal(t, tt.wantKind, got.Kind())
			require.Equal(t, tt.wantUser, got.UserID())
		})
	}
}
