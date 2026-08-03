package gcpkms

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
)

// The signer is only useful if go-jose can actually drive it. Signing a JWS and
// verifying it against the key's own public half exercises the whole chain:
// digest, provider signature, and the JOSE encoding conversion.
func TestSigner_RS256RoundTripsThroughGoJose(t *testing.T) {
	t.Parallel()

	requireJWSRoundTrip(t, jose.RS256)
}

// ES256 is the case that can silently produce malformed tokens, because the
// provider returns ASN.1 DER where JOSE wants R || S. Repeating the round trip
// exercises the short-component path that appears only in some signatures.
func TestSigner_ES256RoundTripsThroughGoJose(t *testing.T) {
	t.Parallel()

	for range 64 {
		requireJWSRoundTrip(t, jose.ES256)
	}
}

func requireJWSRoundTrip(t *testing.T, alg jose.SignatureAlgorithm) {
	t.Helper()

	ctx := t.Context()

	client, err := NewLocalSigningClient(alg)
	require.NoError(t, err)

	public, err := client.GetPublicKey(ctx, testResourceName)
	require.NoError(t, err)

	opaque, err := NewSigner(ctx, client, testResourceName, "test-kid", *public)
	require.NoError(t, err)
	require.Equal(t, []jose.SignatureAlgorithm{alg}, opaque.Algs())

	joseSigner, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: opaque}, nil)
	require.NoError(t, err)

	payload := []byte(`{"iss":"gram","sub":"round-trip"}`)
	jws, err := joseSigner.Sign(payload)
	require.NoError(t, err)

	compact, err := jws.CompactSerialize()
	require.NoError(t, err)

	parsed, err := jose.ParseSigned(compact, []jose.SignatureAlgorithm{alg})
	require.NoError(t, err)

	// Assert on the serialized header rather than reading the value back off the
	// signer: this proves kid actually reaches the JWS a verifier will see.
	require.Equal(t, "test-kid", parsed.Signatures[0].Header.KeyID)

	verified, err := parsed.Verify(public.Key)
	require.NoError(t, err, "signature produced via the opaque signer must verify against the key's public half")
	require.Equal(t, payload, verified)
}

// The public key is supplied separately from the algorithm and callers are told
// to cache it, so the two can drift apart. A mismatched pair must fail at
// construction rather than silently emitting one encoding under another's label.
func TestNewSigner_RejectsKeyThatDoesNotMatchAlgorithm(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	rsaClient, err := NewLocalSigningClient(jose.RS256)
	require.NoError(t, err)
	rsaPublic, err := rsaClient.GetPublicKey(ctx, testResourceName)
	require.NoError(t, err)

	ecClient, err := NewLocalSigningClient(jose.ES256)
	require.NoError(t, err)
	ecPublic, err := ecClient.GetPublicKey(ctx, testResourceName)
	require.NoError(t, err)

	_, err = NewSigner(ctx, rsaClient, testResourceName, "kid", PublicKey{Algorithm: jose.ES256, Key: rsaPublic.Key})
	require.ErrorContains(t, err, "must be an *ecdsa.PublicKey")

	_, err = NewSigner(ctx, ecClient, testResourceName, "kid", PublicKey{Algorithm: jose.RS256, Key: ecPublic.Key})
	require.ErrorContains(t, err, "must be an *rsa.PublicKey")

	_, err = NewSigner(ctx, rsaClient, testResourceName, "kid", PublicKey{Algorithm: jose.ES256, Key: nil})
	require.ErrorContains(t, err, "must be an *ecdsa.PublicKey")
}

// ES256 is P-256 by definition; a wider curve would produce R and S halves that
// no ES256 verifier expects.
func TestNewSigner_RejectsES256OnTheWrongCurve(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)

	client, err := NewLocalSigningClient(jose.ES256)
	require.NoError(t, err)

	_, err = NewSigner(t.Context(), client, testResourceName, "kid", PublicKey{
		Algorithm: jose.ES256,
		Key:       &key.PublicKey,
	})
	require.ErrorContains(t, err, "must be on P-256")
}

func TestNewSigner_RejectsInvalidResourceName(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	client, err := NewLocalSigningClient(jose.RS256)
	require.NoError(t, err)

	public, err := client.GetPublicKey(ctx, testResourceName)
	require.NoError(t, err)

	_, err = NewSigner(ctx, client, "projects/p/locations/l/keyRings/r/cryptoKeys/k", "kid", *public)
	require.ErrorIs(t, err, ErrInvalidResourceName)
}

func TestNewSigner_RejectsUnsupportedAlgorithm(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	client, err := NewLocalSigningClient(jose.RS256)
	require.NoError(t, err)

	public, err := client.GetPublicKey(ctx, testResourceName)
	require.NoError(t, err)
	public.Algorithm = jose.PS256

	_, err = NewSigner(ctx, client, testResourceName, "kid", *public)
	require.ErrorIs(t, err, ErrUnsupportedAlgorithm)
}

func TestSigner_RejectsMismatchedAlgorithmAtSignTime(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	client, err := NewLocalSigningClient(jose.ES256)
	require.NoError(t, err)

	public, err := client.GetPublicKey(ctx, testResourceName)
	require.NoError(t, err)

	opaque, err := NewSigner(ctx, client, testResourceName, "kid", *public)
	require.NoError(t, err)

	_, err = opaque.SignPayload([]byte("payload"), jose.RS256)
	require.ErrorIs(t, err, ErrUnsupportedAlgorithm)
}
