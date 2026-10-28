package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"
)

func TestVerify(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), "test"))
	require.NoError(t, err)
	public := keys{set: jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Use: "sig", Algorithm: "RS256"}}}}
	issuer, subject, err := (identityConfig{"123", "456", "us-central1", "test", "kit-agent-poc-test", "native-agent-probe"}).expected()
	require.NoError(t, err)
	now := time.Now()
	claims := jwt.Claims{Issuer: issuer, Subject: subject, Audience: jwt.Audience{"https://example.invalid/poc", "https://example.invalid/additional"}, IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(time.Hour))}
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	require.NoError(t, err)
	out, err := verify(context.Background(), raw, issuer, claims.Subject, claims.Audience[0], public)
	require.NoError(t, err)
	require.Equal(t, []string(claims.Audience), out.Audience)
	require.True(t, out.WrongSubjectRejected)
	require.True(t, out.WrongAudienceRejected)
	_, err = verify(context.Background(), raw, issuer, "another-agent", claims.Audience[0], public)
	require.Error(t, err)
	_, err = verify(context.Background(), raw, issuer, claims.Subject, "https://wrong.invalid", public)
	require.Error(t, err)
	_, err = verify(context.Background(), raw, issuer+"/wrong", claims.Subject, claims.Audience[0], public)
	require.Error(t, err)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	wrongKeys := keys{set: jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &otherKey.PublicKey, KeyID: "test", Use: "sig", Algorithm: "RS256"}}}}
	_, err = verify(context.Background(), raw, issuer, claims.Subject, claims.Audience[0], wrongKeys)
	require.Error(t, err)
	bound, err := jwt.Signed(signer).Claims(claims).Claims(map[string]any{"cnf": map[string]string{"x5t#S256": "test"}}).Serialize()
	require.NoError(t, err)
	_, err = verify(context.Background(), bound, issuer, claims.Subject, claims.Audience[0], public)
	require.ErrorContains(t, err, "certificate-bound")
	claims.Expiry = jwt.NewNumericDate(now.Add(-2 * time.Minute))
	raw, err = jwt.Signed(signer).Claims(claims).Serialize()
	require.NoError(t, err)
	_, err = verify(context.Background(), raw, issuer, claims.Subject, claims.Audience[0], public)
	require.Error(t, err)
}

func TestIndependentIdentity(t *testing.T) {
	t.Parallel()
	c := identityConfig{"123", "456", "us-central1", "test", "kit-agent-poc-test", "native-agent-probe"}
	issuer, subject, err := c.expected()
	require.NoError(t, err)
	require.Equal(t, "https://sts.googleapis.com/v1/organizations/456/locations/global/workloadIdentityPools/agents.global.org-456.system.id.goog", issuer)
	require.Equal(t, "spiffe://agents.global.org-456.system.id.goog/resources/container/projects/123/locations/us-central1/clusters/test/ns/kit-agent-poc-test/sa/native-agent-probe", subject)
	c.Organization = ""
	issuer, subject, err = c.expected()
	require.NoError(t, err)
	require.Contains(t, issuer, "/projects/123/")
	require.Contains(t, subject, "spiffe://agents.global.proj-123.system.id.goog/")
	c.Cluster = "../bad"
	_, _, err = c.expected()
	require.Error(t, err)
}
