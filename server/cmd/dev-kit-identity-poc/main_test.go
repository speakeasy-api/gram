package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestVerify(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), "test"))
	require.NoError(t, err)
	public := keys{set: jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Use: "sig", Algorithm: "RS256"}}}}
	now := time.Now()
	claims := jwt.Claims{Issuer: googleIssuer, Subject: "100000000000000000001", Audience: jwt.Audience{"https://example.invalid/poc"}, IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(time.Hour))}
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	require.NoError(t, err)
	out, err := verify(context.Background(), raw, claims.Subject, string(claims.Audience[0]), public)
	require.NoError(t, err)
	require.True(t, out.CachedTokenAccepted)
	require.True(t, out.WrongSubjectRejected)
	require.True(t, out.WrongAudienceRejected)
	_, err = verify(context.Background(), raw, "another-agent", string(claims.Audience[0]), public)
	require.Error(t, err)
	_, err = verify(context.Background(), raw, claims.Subject, "https://wrong.invalid", public)
	require.Error(t, err)
	claims.Expiry = jwt.NewNumericDate(now.Add(-2 * time.Minute))
	raw, err = jwt.Signed(signer).Claims(claims).Serialize()
	require.NoError(t, err)
	_, err = verify(context.Background(), raw, claims.Subject, string(claims.Audience[0]), public)
	require.Error(t, err)
}
