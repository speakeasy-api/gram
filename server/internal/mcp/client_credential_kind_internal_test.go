package mcp

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// The derivation is the control the token endpoint branches on, so every
// arm is pinned: the legacy NULL population reads as it always did, each
// declared method maps to exactly one credential kind, and anything the code
// does not recognize, or a row whose columns contradict its method, is an
// error rather than a fallback to public.
func TestClientCredentialKindOf(t *testing.T) {
	t.Parallel()

	hash := pgtype.Text{String: "$2a$10$hash", Valid: true}
	noHash := pgtype.Text{String: "", Valid: false}
	method := func(v string) pgtype.Text { return pgtype.Text{String: v, Valid: true} }
	null := pgtype.Text{String: "", Valid: false}

	for _, tc := range []struct {
		name    string
		method  pgtype.Text
		hash    pgtype.Text
		want    clientCredentialKind
		wantErr bool
	}{
		{name: "legacy null with secret is symmetric", method: null, hash: hash, want: credentialKindSecret},
		{name: "legacy null without secret is public", method: null, hash: noHash, want: credentialKindNone},
		{name: "client_secret_basic", method: method("client_secret_basic"), hash: hash, want: credentialKindSecret},
		{name: "client_secret_post", method: method("client_secret_post"), hash: hash, want: credentialKindSecret},
		{name: "none", method: method("none"), hash: noHash, want: credentialKindNone},
		{name: "private_key_jwt", method: method("private_key_jwt"), hash: noHash, want: credentialKindAssertion},
		{name: "symmetric method with no stored secret is an error", method: method("client_secret_basic"), hash: noHash, wantErr: true},
		{name: "none with a stored secret is an error", method: method("none"), hash: hash, wantErr: true},
		{name: "private_key_jwt with a stored secret is an error", method: method("private_key_jwt"), hash: hash, wantErr: true},
		{name: "unrecognized method is an error, never public", method: method("private-key-jwt"), hash: noHash, wantErr: true},
		{name: "empty string is an error, never public", method: method(""), hash: noHash, wantErr: true},
	} {
		row := &usersessions_repo.UserSessionClient{TokenEndpointAuthMethod: tc.method, ClientSecretHash: tc.hash}
		got, err := clientCredentialKindOf(row)
		if tc.wantErr {
			require.Error(t, err, tc.name)
			continue
		}
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.want, got, tc.name)
	}
}
