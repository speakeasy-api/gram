package risk

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// stubCache returns getErr from every read so lookup error handling can be
// exercised without a real backend. Writes are no-ops.
type stubCache struct {
	getErr error
}

func (s stubCache) Get(_ context.Context, _ string, _ any) error          { return s.getErr }
func (s stubCache) GetAndDelete(_ context.Context, _ string, _ any) error { return s.getErr }
func (s stubCache) Set(_ context.Context, _ string, _ any, _ time.Duration) error {
	return nil
}
func (s stubCache) Add(_ context.Context, _ string, _ time.Duration) (bool, error) { return true, nil }
func (s stubCache) Update(_ context.Context, _ string, _ any) error                { return nil }
func (s stubCache) Delete(_ context.Context, _ string) error                       { return nil }
func (s stubCache) Expire(_ context.Context, _ string, _ time.Duration) error      { return nil }
func (s stubCache) ListAppend(_ context.Context, _ string, _ any, _ time.Duration) error {
	return nil
}
func (s stubCache) ListRange(_ context.Context, _ string, _, _ int64, _ any) error { return nil }
func (s stubCache) ListDrain(_ context.Context, _ string, _ any) error             { return nil }
func (s stubCache) DeleteByPrefix(_ context.Context, _ string) error               { return nil }

// TestParsePolicyBypassRequestToken_CacheUnavailable verifies that an
// operational cache failure is reported as a distinct, retriable error rather
// than collapsed into an invalid-token client error.
func TestParsePolicyBypassRequestToken_CacheUnavailable(t *testing.T) {
	t.Parallel()

	c := stubCache{getErr: errors.New("dial tcp: connection refused")}
	_, err := parsePolicyBypassRequestToken(t.Context(), c, "test-jwt-secret", policyBypassRequestTokenPrefixV2+"someid")
	require.Error(t, err)
	require.ErrorIs(t, err, errPolicyBypassRequestStoreUnavailable)
}

// TestParsePolicyBypassRequestToken_CacheMiss verifies that a genuine miss
// (expired or never-existed link) is NOT treated as a store-unavailable error,
// so the caller still surfaces it as an invalid token.
func TestParsePolicyBypassRequestToken_CacheMiss(t *testing.T) {
	t.Parallel()

	c := stubCache{getErr: redisCache.ErrCacheMiss}
	_, err := parsePolicyBypassRequestToken(t.Context(), c, "test-jwt-secret", policyBypassRequestTokenPrefixV2+"someid")
	require.Error(t, err)
	require.NotErrorIs(t, err, errPolicyBypassRequestStoreUnavailable)
}

// TestParsePolicyBypassRequestToken_LegacyRPBR1 verifies that links minted in
// the legacy inline-encrypted (rpbr1) format still decode after the rpbr2
// cutover, so requests already in flight keep working until they expire.
func TestParsePolicyBypassRequestToken_LegacyRPBR1(t *testing.T) {
	t.Parallel()

	const jwtSecret = "test-jwt-secret"
	host := "mcp.example.com"
	now := time.Now()
	claims := policyBypassRequestClaims{
		OrganizationID:         "org_legacy",
		ProjectID:              "00000000-0000-0000-0000-000000000001",
		RequesterUserID:        "user_legacy",
		ObservedName:           nil,
		ObservedFullURL:        nil,
		ObservedURLHost:        &host,
		ObservedServerIdentity: nil,
		ToolName:               nil,
		ToolCall:               nil,
		BlockReason:            nil,
		RiskPolicyID:           "00000000-0000-0000-0000-000000000002",
		RiskResultID:           nil,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    policyBypassRequestTokenIssuer,
			Subject:   policyBypassRequestTokenSubject,
			Audience:  nil,
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        uuid.NewString(),
		},
	}
	plaintext, err := json.Marshal(claims)
	require.NoError(t, err)
	token, err := encryptPolicyBypassRequestToken(jwtSecret, plaintext)
	require.NoError(t, err)
	require.Contains(t, token, policyBypassRequestTokenPrefix)

	// The legacy path decodes the claims inline, so it never touches the cache
	// and a nil cache is acceptable here.
	parsed, err := parsePolicyBypassRequestToken(t.Context(), nil, jwtSecret, token)
	require.NoError(t, err)
	require.Equal(t, claims.OrganizationID, parsed.OrganizationID)
	require.Equal(t, claims.ID, parsed.ID)
	require.NotNil(t, parsed.ObservedURLHost)
	require.Equal(t, host, *parsed.ObservedURLHost)
}
