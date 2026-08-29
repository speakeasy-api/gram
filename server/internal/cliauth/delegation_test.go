package cliauth_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/delegation"
	gen "github.com/speakeasy-api/gram/server/gen/cli_auth"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDelegateHooksActingUserRequiresCurrentMembershipAndProof(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	verifier, challenge := pkcePair(t)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	encodedPublicKey := delegation.EncodePublicKey(publicKey)
	contractVersion := delegation.ContractVersion

	authorized, err := ti.service.Authorize(ctx, &gen.AuthorizePayload{
		CodeChallenge: challenge, CodeChallengeMethod: "S256",
		ProofPublicKey: &encodedPublicKey, DelegationContractVersion: &contractVersion,
	})
	require.NoError(t, err)
	redeemed, err := ti.service.Redeem(ctx, &gen.RedeemPayload{Code: authorized.Code, CodeVerifier: verifier})
	require.NoError(t, err)
	require.NotNil(t, redeemed.DelegationRefreshToken)
	require.NotNil(t, redeemed.OrganizationID)
	keyHash, err := auth.GetAPIKeyHash(redeemed.AccessToken)
	require.NoError(t, err)
	var scopes []string
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT scopes FROM api_keys WHERE key_hash = $1`, keyHash).Scan(&scopes)) //nolint:glint // notestingrawsql: bounded integration assertion verifies proof-bound key scopes
	require.ElementsMatch(t, []string{auth.APIKeyScopeAgentUser.String(), auth.APIKeyScopeHooks.String()}, scopes)

	request := delegation.MintRequest{
		RefreshToken: *redeemed.DelegationRefreshToken, ContractVersion: delegation.ContractVersion,
		Provider: delegation.ProviderClaude, Event: delegation.EventPreToolUse,
		SessionID: "session-" + uuid.NewString(), IdempotencyKey: uuid.NewString(), SignedAt: time.Now().Unix(), Nonce: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	}
	request.Signature, err = delegation.Sign(privateKey, request)
	require.NoError(t, err)
	payload := &gen.DelegateHooksActingUserPayload{
		RefreshToken: request.RefreshToken, ContractVersion: request.ContractVersion,
		Provider: request.Provider, Event: request.Event, SessionID: request.SessionID,
		IdempotencyKey: request.IdempotencyKey, SignedAt: request.SignedAt, Nonce: request.Nonce, Signature: request.Signature,
	}
	result, err := ti.service.DelegateHooksActingUser(ctx, payload)
	require.NoError(t, err)
	require.NotEmpty(t, result.Assertion)
	require.Positive(t, result.ExpiresIn)

	refreshClaims := jwt.MapClaims{}
	_, _, err = jwt.NewParser().ParseUnverified(request.RefreshToken, refreshClaims)
	require.NoError(t, err)
	refreshJTI, ok := refreshClaims["jti"].(string)
	require.True(t, ok)
	require.NotEmpty(t, refreshJTI)
	replayKeyHash := sha256.Sum256([]byte(refreshJTI + "\x00" + request.Nonce))
	nonceKey := "cliauth:hooks-mint-nonce:v1:" + hex.EncodeToString(replayKeyHash[:])
	ttlBeforeRetry, err := ti.redis.PTTL(ctx, nonceKey).Result()
	require.NoError(t, err)
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		retry, retryErr := ti.service.DelegateHooksActingUser(ctx, payload)
		require.NoError(collect, retryErr)
		if retryErr == nil {
			require.Equal(collect, result.Assertion, retry.Assertion, "exact nonce retry must return the original assertion bytes")
		}
		ttlAfterRetry, ttlErr := ti.redis.PTTL(ctx, nonceKey).Result()
		require.NoError(collect, ttlErr)
		require.Less(collect, ttlAfterRetry, ttlBeforeRetry, "retry must not extend nonce expiry")
	}, time.Second, 10*time.Millisecond)

	// A replay record outlives its short assertion. An exact request may
	// replace an unusable stored assertion without resetting that record's TTL.
	storedRaw, err := ti.redis.Get(ctx, nonceKey).Bytes()
	require.NoError(t, err)
	var staleRecord map[string]any
	require.NoError(t, json.Unmarshal(storedRaw, &staleRecord))
	staleRecord["assertion"] = "expired-assertion"
	storedRaw, err = json.Marshal(staleRecord)
	require.NoError(t, err)
	ttlBeforeRefresh, err := ti.redis.PTTL(ctx, nonceKey).Result()
	require.NoError(t, err)
	require.NoError(t, ti.redis.Set(ctx, nonceKey, storedRaw, ttlBeforeRefresh).Err())
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		refreshed, refreshErr := ti.service.DelegateHooksActingUser(ctx, payload)
		require.NoError(collect, refreshErr)
		if refreshErr == nil {
			require.NotEqual(collect, "expired-assertion", refreshed.Assertion)
			require.Positive(collect, refreshed.ExpiresIn)
		}
	}, time.Second, 10*time.Millisecond)
	ttlAfterRefresh, err := ti.redis.PTTL(ctx, nonceKey).Result()
	require.NoError(t, err)
	require.Less(t, ttlAfterRefresh, ttlBeforeRefresh, "assertion refresh must not extend nonce expiry")

	conflictRequest := request
	conflictRequest.IdempotencyKey = uuid.NewString()
	conflictRequest.Signature, err = delegation.Sign(privateKey, conflictRequest)
	require.NoError(t, err)
	conflict := *payload
	conflict.IdempotencyKey = conflictRequest.IdempotencyKey
	conflict.Signature = conflictRequest.Signature
	_, err = ti.service.DelegateHooksActingUser(ctx, &conflict)
	requireOopsCode(t, err, oops.CodeUnauthorized)

	// Nonce uniqueness is scoped to one refresh JTI. A separate proof-bound
	// enrollment can safely generate the same random nonce without colliding.
	secondVerifier, secondChallenge := pkcePair(t)
	secondAuthorized, err := ti.service.Authorize(ctx, &gen.AuthorizePayload{
		CodeChallenge: secondChallenge, CodeChallengeMethod: "S256",
		ProofPublicKey: &encodedPublicKey, DelegationContractVersion: &contractVersion,
	})
	require.NoError(t, err)
	secondRedeemed, err := ti.service.Redeem(ctx, &gen.RedeemPayload{Code: secondAuthorized.Code, CodeVerifier: secondVerifier})
	require.NoError(t, err)
	secondRequest := request
	secondRequest.RefreshToken = *secondRedeemed.DelegationRefreshToken
	secondRequest.Signature, err = delegation.Sign(privateKey, secondRequest)
	require.NoError(t, err)
	secondPayload := *payload
	secondPayload.RefreshToken = secondRequest.RefreshToken
	secondPayload.Signature = secondRequest.Signature
	secondResult, err := ti.service.DelegateHooksActingUser(ctx, &secondPayload)
	require.NoError(t, err)
	require.NotEmpty(t, secondResult.Assertion)

	spoofed := *payload
	spoofed.SessionID = "spoofed-session"
	_, err = ti.service.DelegateHooksActingUser(ctx, &spoofed)
	requireOopsCode(t, err, oops.CodeUnauthorized)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err = ti.conn.Exec(ctx, `UPDATE organization_user_relationships SET deleted_at = clock_timestamp() WHERE organization_id = $1 AND user_id = $2`, authCtx.ActiveOrganizationID, authCtx.UserID) //nolint:glint // notestingrawsql: isolated integration fixture must simulate membership removal
	require.NoError(t, err)
	t.Cleanup(func() {
		_, restoreErr := ti.conn.Exec(context.Background(), `UPDATE organization_user_relationships SET deleted_at = NULL WHERE organization_id = $1 AND user_id = $2`, authCtx.ActiveOrganizationID, authCtx.UserID) //nolint:glint // notestingrawsql: restore isolated membership fixture during cleanup
		require.NoError(t, restoreErr)
	})
	_, err = ti.service.DelegateHooksActingUser(ctx, payload)
	requireOopsCode(t, err, oops.CodeUnauthorized)
}
