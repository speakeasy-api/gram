package remotesessions

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFederatedCredentialsConcurrentHandoff(t *testing.T) {
	t.Parallel()
	for _, discard := range []bool{false, true} {
		identity := &FederatedIdentity{credentials: &federatedCredentialState{value: EphemeralFederatedCredentials{idToken: "one-use-token"}}}
		copied := *identity // Value copies must share one-shot state too.
		var calls atomic.Int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for n := range 32 {
			wg.Go(func() {
				<-start
				if discard && n%2 == 0 {
					identity.DiscardCredentials()
					return
				}
				_ = copied.WithCredentials(func(c EphemeralFederatedCredentials) error {
					calls.Add(1)
					if c.IDToken() != "one-use-token" {
						t.Error("unexpected credential")
					}
					// Reentrant discard and handoff must not deadlock or replay.
					identity.DiscardCredentials()
					if err := identity.WithCredentials(nil); err == nil {
						t.Error("replayed credentials")
					}
					return errors.New("consumer failed")
				})
			})
		}
		close(start)
		wg.Wait()
		if discard {
			require.LessOrEqual(t, calls.Load(), int32(1))
		} else {
			require.Equal(t, int32(1), calls.Load())
		}
		require.Error(t, identity.WithCredentials(nil))
	}
}

func TestFederatedUntrustedCoAudience(t *testing.T) {
	t.Parallel()
	p := federatedFixture(t)
	now := time.Now()
	claims := federatedClaims(t, p, now)
	claims["aud"] = json.RawMessage(`["upstream-client","untrusted-client"]`)
	claims["azp"] = json.RawMessage(`"upstream-client"`)
	_, err := validateFederatedClaims(claims, p, tokenResponse{}, "code", "nonce", "RS256", now)
	require.ErrorIs(t, err, ErrFederatedIdentity)
	claims["aud"] = json.RawMessage(`["upstream-client"]`)
	_, err = validateFederatedClaims(claims, p, tokenResponse{}, "code", "nonce", "RS256", now)
	require.NoError(t, err)
}

func TestFederatedEd25519TokenHashes(t *testing.T) {
	t.Parallel()
	p := federatedFixture(t)
	now := time.Now()
	for _, name := range []string{"at_hash", "c_hash"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value := "access"
			if name == "c_hash" {
				value = "code"
			}
			sum := sha512.Sum512([]byte(value))
			hash := base64.RawURLEncoding.EncodeToString(sum[:32])
			claims := federatedClaims(t, p, now)
			encoded, err := json.Marshal(hash)
			require.NoError(t, err)
			claims[name] = encoded
			_, err = validateFederatedClaims(claims, p, tokenResponse{AccessToken: "access"}, "code", "nonce", "EdDSA", now)
			require.NoError(t, err)
			require.False(t, validFederatedTokenHash(hash, "different", "EdDSA"))
			claims[name] = json.RawMessage(`"invalid"`)
			_, err = validateFederatedClaims(claims, p, tokenResponse{AccessToken: "access"}, "code", "nonce", "EdDSA", now)
			require.ErrorIs(t, err, ErrFederatedIdentity)
		})
	}
}
