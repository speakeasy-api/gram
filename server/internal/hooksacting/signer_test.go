package hooksacting

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/delegation"
)

func TestSignerProofBoundDelegationAndBindings(t *testing.T) {
	t.Parallel()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	now := time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)
	signer, err := NewSigner("test-secret")
	require.NoError(t, err)
	signer.now = func() time.Time { return now }

	refresh, err := signer.MintRefresh("user-1", "org-1", publicKey)
	require.NoError(t, err)
	identity, err := signer.VerifyRefresh(refresh)
	require.NoError(t, err)
	require.NotEmpty(t, identity.RefreshJTI)

	req := delegation.MintRequest{RefreshToken: refresh, ContractVersion: delegation.ContractVersion, Provider: delegation.ProviderClaude, Event: delegation.EventPreToolUse, SessionID: "session-1", IdempotencyKey: "invocation-1", SignedAt: now.Unix(), Nonce: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
	req.Signature, err = delegation.Sign(privateKey, req)
	require.NoError(t, err)
	assertion, err := signer.MintAssertion(identity, req)
	require.NoError(t, err)
	expiresIn, err := signer.AssertionExpiresIn(assertion)
	require.NoError(t, err)
	require.Equal(t, int(AssertionLifetime.Seconds()), expiresIn)
	signer.now = func() time.Time { return now.Add(10 * time.Second) }
	expiresIn, err = signer.AssertionExpiresIn(assertion)
	require.NoError(t, err)
	require.Equal(t, 20, expiresIn)
	signer.now = func() time.Time { return now }

	verified, err := signer.VerifyAssertion(assertion, AssertionBinding{OrganizationID: "org-1", Provider: req.Provider, Event: req.Event, SessionID: req.SessionID, IdempotencyKey: req.IdempotencyKey})
	require.NoError(t, err)
	require.Equal(t, "user-1", verified.UserID)

	for _, binding := range []AssertionBinding{
		{OrganizationID: "org-2", Provider: req.Provider, Event: req.Event, SessionID: req.SessionID, IdempotencyKey: req.IdempotencyKey},
		{OrganizationID: "org-1", Provider: delegation.ProviderCodex, Event: req.Event, SessionID: req.SessionID, IdempotencyKey: req.IdempotencyKey},
		{OrganizationID: "org-1", Provider: req.Provider, Event: req.Event, SessionID: "other", IdempotencyKey: req.IdempotencyKey},
		{OrganizationID: "org-1", Provider: req.Provider, Event: req.Event, SessionID: req.SessionID, IdempotencyKey: "other"},
		{OrganizationID: "org-1", Provider: req.Provider, Event: req.Event, SessionID: req.SessionID, IdempotencyKey: req.IdempotencyKey, Observational: true},
	} {
		_, err := signer.VerifyAssertion(assertion, binding)
		require.Error(t, err)
	}
}

func TestSignerRequiresExactRegisteredClaimsProfiles(t *testing.T) {
	t.Parallel()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	now := time.Date(2027, 2, 3, 4, 5, 6, 0, time.UTC)
	signer, err := NewSigner("test-secret")
	require.NoError(t, err)
	signer.now = func() time.Time { return now }

	validRefresh := func() refreshClaims {
		return refreshClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer: delegation.RefreshIssuer, Subject: "user:user-1", Audience: jwt.ClaimStrings{delegation.RefreshAudience}, ID: "refresh-jti",
				IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(RefreshLifetime)),
			},
			ContractVersion: delegation.ContractVersion, OrganizationID: "org-1", PublicKey: delegation.EncodePublicKey(publicKey), KeyID: delegation.KeyID(publicKey),
		}
	}
	refreshCases := map[string]func(*refreshClaims){
		"wrong issuer":       func(c *refreshClaims) { c.Issuer = delegation.AssertionIssuer },
		"wrong audience":     func(c *refreshClaims) { c.Audience = jwt.ClaimStrings{delegation.AssertionAudience} },
		"multiple audience":  func(c *refreshClaims) { c.Audience = append(c.Audience, "other") },
		"missing expiry":     func(c *refreshClaims) { c.ExpiresAt = nil },
		"missing issued at":  func(c *refreshClaims) { c.IssuedAt = nil },
		"missing not before": func(c *refreshClaims) { c.NotBefore = nil },
		"not before mismatch": func(c *refreshClaims) {
			c.NotBefore = jwt.NewNumericDate(now.Add(time.Second))
		},
		"missing jti":      func(c *refreshClaims) { c.ID = "" },
		"short lifetime":   func(c *refreshClaims) { c.ExpiresAt = jwt.NewNumericDate(now.Add(RefreshLifetime - time.Second)) },
		"long lifetime":    func(c *refreshClaims) { c.ExpiresAt = jwt.NewNumericDate(now.Add(RefreshLifetime + time.Second)) },
		"missing contract": func(c *refreshClaims) { c.ContractVersion = "" },
	}
	for name, mutate := range refreshCases {
		t.Run("refresh/"+name, func(t *testing.T) {
			claims := validRefresh()
			mutate(&claims)
			raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(signer.refreshKey)
			require.NoError(t, err)
			_, err = signer.VerifyRefresh(raw)
			require.Error(t, err)
		})
	}

	validAssertion := func() delegationClaims {
		return delegationClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer: delegation.AssertionIssuer, Subject: "user:user-1", Audience: jwt.ClaimStrings{delegation.AssertionAudience}, ID: "assertion-jti",
				IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(AssertionLifetime)),
			},
			ContractVersion: delegation.ContractVersion, OrganizationID: "org-1", Provider: delegation.ProviderClaude, Event: delegation.EventPreToolUse, SessionID: "session", IdempotencyKey: "idempotency", KeyID: delegation.KeyID(publicKey),
		}
	}
	binding := AssertionBinding{OrganizationID: "org-1", Provider: delegation.ProviderClaude, Event: delegation.EventPreToolUse, SessionID: "session", IdempotencyKey: "idempotency"}
	assertionCases := map[string]func(*delegationClaims){
		"wrong issuer":       func(c *delegationClaims) { c.Issuer = delegation.RefreshIssuer },
		"wrong audience":     func(c *delegationClaims) { c.Audience = jwt.ClaimStrings{delegation.RefreshAudience} },
		"multiple audience":  func(c *delegationClaims) { c.Audience = append(c.Audience, "other") },
		"missing expiry":     func(c *delegationClaims) { c.ExpiresAt = nil },
		"missing issued at":  func(c *delegationClaims) { c.IssuedAt = nil },
		"missing not before": func(c *delegationClaims) { c.NotBefore = nil },
		"not before mismatch": func(c *delegationClaims) {
			c.NotBefore = jwt.NewNumericDate(now.Add(time.Second))
		},
		"missing jti":      func(c *delegationClaims) { c.ID = "" },
		"short lifetime":   func(c *delegationClaims) { c.ExpiresAt = jwt.NewNumericDate(now.Add(AssertionLifetime - time.Second)) },
		"long lifetime":    func(c *delegationClaims) { c.ExpiresAt = jwt.NewNumericDate(now.Add(AssertionLifetime + time.Second)) },
		"missing contract": func(c *delegationClaims) { c.ContractVersion = "" },
	}
	for name, mutate := range assertionCases {
		t.Run("assertion/"+name, func(t *testing.T) {
			t.Parallel()
			claims := validAssertion()
			mutate(&claims)
			raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(signer.assertionKey)
			require.NoError(t, err)
			_, err = signer.VerifyAssertion(raw, binding)
			require.Error(t, err)
		})
	}

	claims := validRefresh()
	wrongAlgorithm, err := jwt.NewWithClaims(jwt.SigningMethodHS384, claims).SignedString(signer.refreshKey)
	require.NoError(t, err)
	_, err = signer.VerifyRefresh(wrongAlgorithm)
	require.Error(t, err)
}

func TestSignerRejectsStaleAndSpoofedProof(t *testing.T) {
	t.Parallel()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := NewSigner("test-secret")
	require.NoError(t, err)
	now := time.Now().UTC()
	signer.now = func() time.Time { return now }
	refresh, err := signer.MintRefresh("user-1", "org-1", publicKey)
	require.NoError(t, err)
	identity, err := signer.VerifyRefresh(refresh)
	require.NoError(t, err)

	req := delegation.MintRequest{RefreshToken: refresh, ContractVersion: delegation.ContractVersion, Provider: delegation.ProviderClaude, Event: delegation.EventPreToolUse, SessionID: "session", IdempotencyKey: "idempotency", SignedAt: now.Add(-2 * ProofClockSkew).Unix(), Nonce: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
	req.Signature, err = delegation.Sign(privateKey, req)
	require.NoError(t, err)
	_, err = signer.MintAssertion(identity, req)
	require.Error(t, err)

	_, wrongPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	req.SignedAt = now.Unix()
	req.Signature, err = delegation.Sign(wrongPrivateKey, req)
	require.NoError(t, err)
	_, err = signer.MintAssertion(identity, req)
	require.Error(t, err)

	for name, mutate := range map[string]func(*delegation.MintRequest){
		"session whitespace":     func(r *delegation.MintRequest) { r.SessionID = " session " },
		"idempotency whitespace": func(r *delegation.MintRequest) { r.IdempotencyKey = " idempotency " },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			canonical := req
			canonical.SignedAt = now.Unix()
			mutate(&canonical)
			signature, signErr := delegation.Sign(privateKey, canonical)
			require.NoError(t, signErr)
			canonical.Signature = signature
			_, mintErr := signer.MintAssertion(identity, canonical)
			require.Error(t, mintErr)
		})
	}
}
