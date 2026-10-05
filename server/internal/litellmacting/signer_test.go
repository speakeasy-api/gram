package litellmacting

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

const testUserID = "user_01test"

var testBinding = AssertionBinding{
	OrganizationID: "org_test",
	ProjectID:      "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
	InstanceID:     "22222222-2222-4222-8222-222222222222",
	APIKeyID:       "33333333-3333-4333-8333-333333333333",
	InvocationID:   "018f0c9a-7b2d-7cc1-8a23-123456789abc",
}

func TestSignerMintAndVerifyAssertion(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	signer, err := NewSigner("test-secret")
	require.NoError(t, err)
	signer.now = func() time.Time { return now }

	raw, err := signer.MintAssertion(testUserID, testBinding)
	require.NoError(t, err)
	identity, err := signer.VerifyAssertion(raw, testBinding)
	require.NoError(t, err)
	require.Equal(t, testUserID, identity.UserID)
	decodedJTI, err := base64.RawURLEncoding.DecodeString(identity.JTI)
	require.NoError(t, err)
	require.Len(t, decodedJTI, 32)

	claims := assertionClaims{}
	token, err := jwt.NewParser(jwt.WithoutClaimsValidation()).ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) {
		return signer.key, nil
	})
	require.NoError(t, err)
	require.True(t, token.Valid)
	require.Equal(t, jwt.SigningMethodHS256.Alg(), token.Header["alg"])
	require.Equal(t, TokenType, token.Header["typ"])
	require.Equal(t, signer.keyID, token.Header["kid"])
	require.Equal(t, signer.keyID, claims.KeyID)
	require.Equal(t, ContractVersion, claims.ContractVersion)
	require.Equal(t, Issuer, claims.Issuer)
	require.Equal(t, "user:"+testUserID, claims.Subject)
	require.Equal(t, jwt.ClaimStrings{Audience}, claims.Audience)
	require.True(t, now.Equal(claims.IssuedAt.Time))
	require.True(t, claims.IssuedAt.Equal(claims.NotBefore.Time))
	require.Equal(t, AssertionLifetime, claims.ExpiresAt.Sub(claims.IssuedAt.Time))
	require.Equal(t, testBinding.OrganizationID, claims.OrganizationID)
	require.Equal(t, testBinding.ProjectID, claims.ProjectID)
	require.Equal(t, testBinding.InstanceID, claims.InstanceID)
	require.Equal(t, testBinding.APIKeyID, claims.APIKeyID)
	require.Equal(t, testBinding.InvocationID, claims.InvocationID)

	sameSecret, err := NewSigner("test-secret")
	require.NoError(t, err)
	otherSecret, err := NewSigner("other-secret")
	require.NoError(t, err)
	require.Equal(t, signer.keyID, sameSecret.keyID)
	require.NotEqual(t, signer.keyID, otherSecret.keyID)
}

func TestSignerRejectsBindingMismatch(t *testing.T) {
	t.Parallel()
	signer, err := NewSigner("test-secret")
	require.NoError(t, err)
	raw, err := signer.MintAssertion(testUserID, testBinding)
	require.NoError(t, err)

	cases := map[string]func(*AssertionBinding){
		"organization": func(b *AssertionBinding) { b.OrganizationID = "org_other" },
		"project": func(b *AssertionBinding) {
			b.ProjectID = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
		},
		"instance": func(b *AssertionBinding) {
			b.InstanceID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		},
		"API key": func(b *AssertionBinding) {
			b.APIKeyID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
		},
		"invocation": func(b *AssertionBinding) {
			b.InvocationID = "018f0c9a-7b2d-7cc1-8a23-123456789abd"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expected := testBinding
			mutate(&expected)
			_, verifyErr := signer.VerifyAssertion(raw, expected)
			require.Error(t, verifyErr)
		})
	}
}

func TestSignerRejectsInvalidAssertionProfile(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	signer, err := NewSigner("test-secret")
	require.NoError(t, err)
	signer.now = func() time.Time { return now }

	type testCase struct {
		mutate func(*assertionClaims)
		method jwt.SigningMethod
		typ    string
		kid    string
	}
	cases := map[string]testCase{
		"algorithm": {method: jwt.SigningMethodHS384},
		"type":      {typ: "JWT"},
		"header key ID": {
			kid: "wrong",
		},
		"claim key ID": {
			mutate: func(c *assertionClaims) { c.KeyID = "wrong" },
		},
		"issuer": {
			mutate: func(c *assertionClaims) { c.Issuer = "wrong" },
		},
		"audience": {
			mutate: func(c *assertionClaims) { c.Audience = jwt.ClaimStrings{"wrong"} },
		},
		"multiple audiences": {
			mutate: func(c *assertionClaims) { c.Audience = append(c.Audience, "other") },
		},
		"missing issued at": {
			mutate: func(c *assertionClaims) { c.IssuedAt = nil },
		},
		"missing not before": {
			mutate: func(c *assertionClaims) { c.NotBefore = nil },
		},
		"missing expiry": {
			mutate: func(c *assertionClaims) { c.ExpiresAt = nil },
		},
		"not before mismatch": {
			mutate: func(c *assertionClaims) { c.NotBefore = jwt.NewNumericDate(now.Add(time.Second)) },
		},
		"short lifetime": {
			mutate: func(c *assertionClaims) { c.ExpiresAt = jwt.NewNumericDate(now.Add(AssertionLifetime - time.Second)) },
		},
		"long lifetime": {
			mutate: func(c *assertionClaims) { c.ExpiresAt = jwt.NewNumericDate(now.Add(AssertionLifetime + time.Second)) },
		},
		"contract": {
			mutate: func(c *assertionClaims) { c.ContractVersion = "wrong" },
		},
		"subject": {
			mutate: func(c *assertionClaims) { c.Subject = "apikey:" + testBinding.ProjectID },
		},
		"JTI": {
			mutate: func(c *assertionClaims) { c.ID = "not-256-bits" },
		},
		"noncanonical project UUID": {
			mutate: func(c *assertionClaims) { c.ProjectID = strings.ToUpper(c.ProjectID) },
		},
		"non-v7 invocation UUID": {
			mutate: func(c *assertionClaims) { c.InvocationID = "44444444-4444-4444-8444-444444444444" },
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			claims := validClaims(now, signer)
			if tc.mutate != nil {
				tc.mutate(&claims)
			}
			method := tc.method
			if method == nil {
				method = jwt.SigningMethodHS256
			}
			typ := tc.typ
			if typ == "" {
				typ = TokenType
			}
			kid := tc.kid
			if kid == "" {
				kid = signer.keyID
			}
			raw := signForTest(t, signer, claims, method, typ, kid)
			_, verifyErr := signer.VerifyAssertion(raw, testBinding)
			require.Error(t, verifyErr)
		})
	}
}

func TestSignerUsesFiveSecondLeeway(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	signer, err := NewSigner("test-secret")
	require.NoError(t, err)
	signer.now = func() time.Time { return now }

	withinLeeway := validClaims(now.Add(-AssertionLifetime-4*time.Second), signer)
	raw := signForTest(t, signer, withinLeeway, jwt.SigningMethodHS256, TokenType, signer.keyID)
	_, err = signer.VerifyAssertion(raw, testBinding)
	require.NoError(t, err)

	outsideLeeway := validClaims(now.Add(-AssertionLifetime-6*time.Second), signer)
	raw = signForTest(t, signer, outsideLeeway, jwt.SigningMethodHS256, TokenType, signer.keyID)
	_, err = signer.VerifyAssertion(raw, testBinding)
	require.Error(t, err)
}

func TestSignerRejectsInvalidMintValues(t *testing.T) {
	t.Parallel()
	signer, err := NewSigner("test-secret")
	require.NoError(t, err)

	_, err = signer.MintAssertion("", testBinding)
	require.Error(t, err)
	cases := map[string]func(*AssertionBinding){
		"organization": func(v *AssertionBinding) { v.OrganizationID = " org_test" },
		"project":      func(v *AssertionBinding) { v.ProjectID = strings.ToUpper(v.ProjectID) },
		"instance":     func(v *AssertionBinding) { v.InstanceID = "not-a-uuid" },
		"API key":      func(v *AssertionBinding) { v.APIKeyID = "" },
		"invocation": func(v *AssertionBinding) {
			v.InvocationID = "44444444-4444-4444-8444-444444444444"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			binding := testBinding
			mutate(&binding)
			_, mintErr := signer.MintAssertion(testUserID, binding)
			require.Error(t, mintErr)
		})
	}

	_, err = NewSigner("  ")
	require.Error(t, err)
}

func validClaims(now time.Time, signer *Signer) assertionClaims {
	return assertionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   "user:" + testUserID,
			Audience:  jwt.ClaimStrings{Audience},
			ExpiresAt: jwt.NewNumericDate(now.Add(AssertionLifetime)),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        strings.Repeat("A", 43),
		},
		ContractVersion: ContractVersion,
		OrganizationID:  testBinding.OrganizationID,
		ProjectID:       testBinding.ProjectID,
		InstanceID:      testBinding.InstanceID,
		APIKeyID:        testBinding.APIKeyID,
		InvocationID:    testBinding.InvocationID,
		KeyID:           signer.keyID,
	}
}

func signForTest(t *testing.T, signer *Signer, claims assertionClaims, method jwt.SigningMethod, typ, kid string) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	token.Header["typ"] = typ
	token.Header["kid"] = kid
	raw, err := token.SignedString(signer.key)
	require.NoError(t, err)
	return raw
}
