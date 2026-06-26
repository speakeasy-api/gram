package risk

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

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
