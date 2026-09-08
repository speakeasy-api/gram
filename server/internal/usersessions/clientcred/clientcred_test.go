package clientcred_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/clientcred"
)

func declared(v string) pgtype.Text { return pgtype.Text{String: v, Valid: true} }

var undeclared = pgtype.Text{String: "", Valid: false}

// The derivation is the control the token endpoint branches on, so every arm is
// pinned: the legacy NULL population reads as it always did, each declared
// method maps to exactly one credential kind, and anything the code does not
// recognize, or a row whose columns contradict its method, is an error rather
// than a fallback to public.
func TestKindOf(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		method    pgtype.Text
		hasSecret bool
		want      clientcred.Kind
		wantErr   bool
	}{
		{name: "legacy null with secret is symmetric", method: undeclared, hasSecret: true, want: clientcred.KindSecret},
		{name: "legacy null without secret is public", method: undeclared, hasSecret: false, want: clientcred.KindPublic},
		{name: "client_secret_basic", method: declared("client_secret_basic"), hasSecret: true, want: clientcred.KindSecret},
		{name: "client_secret_post", method: declared("client_secret_post"), hasSecret: true, want: clientcred.KindSecret},
		{name: "none", method: declared("none"), hasSecret: false, want: clientcred.KindPublic},
		{name: "private_key_jwt", method: declared("private_key_jwt"), hasSecret: false, want: clientcred.KindKey},
		{name: "symmetric method with no stored secret is an error", method: declared("client_secret_basic"), hasSecret: false, wantErr: true},
		{name: "none with a stored secret is an error", method: declared("none"), hasSecret: true, wantErr: true},
		{name: "private_key_jwt with a stored secret is an error", method: declared("private_key_jwt"), hasSecret: true, wantErr: true},
		{name: "unrecognized method is an error, never public", method: declared("private-key-jwt"), hasSecret: false, wantErr: true},
		{name: "empty string is an error, never public", method: declared(""), hasSecret: false, wantErr: true},
	} {
		got, err := clientcred.KindOf(tc.method, tc.hasSecret)
		if tc.wantErr {
			require.Error(t, err, tc.name)
			require.NotEqual(t, clientcred.KindMisconfigured, got, "KindOf must report an unreadable row as an error, not as a fourth kind")
			continue
		}
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.want, got, tc.name)
	}
}

// Resolve differs from KindOf on exactly one axis: the rows KindOf rejects
// become a nameable state instead of an error.
func TestResolveFoldsUnreadableRowsIntoMisconfigured(t *testing.T) {
	t.Parallel()

	require.Equal(t, clientcred.KindMisconfigured, clientcred.Resolve(declared("private_key_jwt"), true))
	require.Equal(t, clientcred.KindMisconfigured, clientcred.Resolve(declared("none"), true))
	require.Equal(t, clientcred.KindMisconfigured, clientcred.Resolve(declared("client_secret_basic"), false))
	require.Equal(t, clientcred.KindMisconfigured, clientcred.Resolve(declared("tls_client_auth"), false))

	require.Equal(t, clientcred.KindPublic, clientcred.Resolve(undeclared, false))
	require.Equal(t, clientcred.KindSecret, clientcred.Resolve(undeclared, true))
	require.Equal(t, clientcred.KindKey, clientcred.Resolve(declared("private_key_jwt"), false))
}

// A session with no bound client reads exactly like one whose client predates
// the column, so the two are separated here rather than at the wire.
func TestForBoundClient(t *testing.T) {
	t.Parallel()

	kind, method := clientcred.ForBoundClient(false, declared("private_key_jwt"), false)
	require.Nil(t, kind, "a session with no client has no credential kind")
	require.Nil(t, method)

	kind, method = clientcred.ForBoundClient(true, declared("private_key_jwt"), false)
	require.NotNil(t, kind)
	require.Equal(t, "key", *kind)
	require.NotNil(t, method)
	require.Equal(t, "private_key_jwt", *method)

	kind, method = clientcred.ForBoundClient(true, undeclared, true)
	require.NotNil(t, kind)
	require.Equal(t, "secret", *kind, "a client predating the column still resolves")
	require.Nil(t, method, "a client that declared nothing must not report a method")
}
