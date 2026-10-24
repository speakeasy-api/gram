package workload

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"
)

func TestVerifyAssertionReusePolicy(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"jti", "uti", "digest"} {
		t.Run(kind, func(t *testing.T) {
			signer := newTestSigner(t, "JWT")
			verifier, keys, _, expect := newTestVerifier(t, signer)
			claims := testClaims()
			var extra any
			if kind == "jti" {
				claims.ID = "token-id"
			}
			if kind == "uti" {
				extra = map[string]any{"uti": "token-id"}
			}
			raw := signer.sign(t, claims, extra)
			_, err := verifier.Verify(t.Context(), raw, expect)
			require.NoError(t, err)
			_, err = verifier.Verify(t.Context(), raw, expect)
			if kind == "digest" {
				require.NoError(t, err)
			} else {
				require.Equal(t, ReasonReplayed, ReasonOf(err))
			}
			expect.AllowAssertionReuse = true
			reused, err := verifier.Verify(t.Context(), raw, expect)
			require.NoError(t, err)
			require.True(t, reused.ReusedAssertion)
			expect.AllowAssertionReuse = false
			_, err = verifier.Verify(t.Context(), raw, expect)
			if kind == "digest" {
				require.NoError(t, err)
			} else {
				require.Equal(t, ReasonReplayed, ReasonOf(err))
			}
			require.Equal(t, 4, keys.lookups, "every exchange verifies the signature")
		})
	}
}

func TestVerifyOptInReservesIdentifiersAndScopesPolicy(t *testing.T) {
	t.Parallel()
	signer := newTestSigner(t, "JWT")
	verifier, _, _, expect := newTestVerifier(t, signer)
	claims := testClaims()
	claims.ID = "first-used-under-opt-in"
	raw := signer.sign(t, claims, nil)
	expect.AllowAssertionReuse = true
	_, err := verifier.Verify(t.Context(), raw, expect)
	require.NoError(t, err)
	expect.AllowAssertionReuse = false
	_, err = verifier.Verify(t.Context(), raw, expect)
	require.Equal(t, ReasonReplayed, ReasonOf(err))
	for _, axis := range []string{"issuer", "party", "subject"} {
		scoped := expect
		scopedClaims := claims
		switch axis {
		case "issuer":
			scoped.ReplayIssuer += "-other"
		case "party":
			scoped.ReplayParty += "-other"
		case "subject":
			scoped.Subject += "-other"
			scopedClaims.Subject = scoped.Subject
		}
		token := signer.sign(t, scopedClaims, nil)
		_, err := verifier.Verify(t.Context(), token, scoped)
		require.NoError(t, err)
		_, err = verifier.Verify(t.Context(), token, scoped)
		require.Equal(t, ReasonReplayed, ReasonOf(err))
	}
}

func TestVerifyOptInStillChecksEveryAssertion(t *testing.T) {
	t.Parallel()
	for _, invalid := range []string{"expiry", "issuer", "audience", "signature", "store"} {
		t.Run(invalid, func(t *testing.T) {
			signer := newTestSigner(t, "JWT")
			verifier, _, guard, expect := newTestVerifier(t, signer)
			expect.AllowAssertionReuse = true
			claims := testClaims()
			claims.ID = "same-id"
			_, err := verifier.Verify(t.Context(), signer.sign(t, claims, nil), expect)
			require.NoError(t, err)
			switch invalid {
			case "expiry":
				claims.Expiry = jwt.NewNumericDate(time.Now().Add(-time.Hour))
			case "issuer":
				claims.Issuer += "/other"
			case "audience":
				claims.Audience = jwt.Audience{"https://other.example.com"}
			case "signature":
				signer = newTestSigner(t, "JWT")
			case "store":
				guard.err = errors.New("unavailable")
			}
			_, err = verifier.Verify(t.Context(), signer.sign(t, claims, nil), expect)
			require.Error(t, err)
		})
	}
}

func TestVerifyConcurrentAssertionReuse(t *testing.T) {
	t.Parallel()
	for _, allow := range []bool{false, true} {
		signer := newTestSigner(t, "JWT")
		verifier, _, _, expect := newTestVerifier(t, signer)
		expect.AllowAssertionReuse = allow
		claims := testClaims()
		claims.ID = "concurrent-id"
		raw := signer.sign(t, claims, nil)
		const count = 12
		results := make(chan error, count)
		var wg sync.WaitGroup
		for range count {
			wg.Go(func() { _, err := verifier.Verify(t.Context(), raw, expect); results <- err })
		}
		wg.Wait()
		close(results)
		accepted := 0
		for err := range results {
			if err == nil {
				accepted++
			} else {
				require.Equal(t, ReasonReplayed, ReasonOf(err))
			}
		}
		if allow {
			require.Equal(t, count, accepted)
		} else {
			require.Equal(t, 1, accepted)
		}
	}
}
