package workloadpolicy_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy"
)

func createJoinToken(t *testing.T, ctx context.Context, ti *testInstance, agentID uuid.UUID) string {
	t.Helper()
	res, err := ti.service.CreateDeviceJoinToken(ctx, &gen.CreateDeviceJoinTokenPayload{
		AgentID:          agentID.String(),
		ExpiresInSeconds: 3600,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(res.Token, "gram_dj_"))
	return res.Token
}

func publicJWK(t *testing.T, key any) map[string]any {
	t.Helper()
	raw, err := jose.JSONWebKey{Key: key, KeyID: "", Algorithm: "", Use: ""}.MarshalJSON()
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func newDeviceKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return key
}

func registerDevice(ctx context.Context, ti *testInstance, token string, jwk map[string]any) (*gen.WorkloadDeviceRegistration, error) {
	reg, err := ti.service.RegisterDevice(ctx, &gen.RegisterDevicePayload{
		JoinToken: token,
		PublicJwk: jwk,
		Hostname:  conv.PtrEmpty("runner-1"),
		Serial:    nil,
	})
	if err != nil {
		return nil, fmt.Errorf("register device: %w", err)
	}
	return reg, nil
}

func TestRegisterDevice_EnrollsTheDeviceUnderTheTokensAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	agentID := newAgent(t, ctx, ti, "vps-agent")
	token := createJoinToken(t, ctx, ti, agentID)

	beforeAdmit, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)
	beforeRedeem, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadDeviceJoinTokenRedeem)
	require.NoError(t, err)

	reg, err := registerDevice(ctx, ti, token, publicJWK(t, newDeviceKey(t).Public()))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(reg.DeviceSubject, "device:"))
	require.Equal(t, workloadpolicy.DeviceIssuer(testServerURL, ti.orgID), reg.Issuer)
	require.Equal(t, agentID.String(), reg.AgentID)
	require.Equal(t, ti.orgID, reg.OrganizationID)

	policy, err := ti.service.List(ctx, &gen.ListPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	var found bool
	for _, admission := range policy.Admissions {
		if admission.Subject == reg.DeviceSubject {
			found = true
			require.Equal(t, agentID.String(), admission.AgentID)
			require.Equal(t, "exact", admission.MatchKind)
		}
	}
	require.True(t, found, "the device should be admitted")

	keys, err := testrepo.New(ti.conn).CountLiveWorkloadDeviceKeysFixture(ctx, testrepo.CountLiveWorkloadDeviceKeysFixtureParams{OrganizationID: ti.orgID, Subject: reg.DeviceSubject})
	require.NoError(t, err)
	require.Equal(t, int64(1), keys)

	afterAdmit, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)
	require.Equal(t, beforeAdmit+1, afterAdmit)
	afterRedeem, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadDeviceJoinTokenRedeem)
	require.NoError(t, err)
	require.Equal(t, beforeRedeem+1, afterRedeem)
}

func TestRegisterDevice_AcceptsEd25519(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	token := createJoinToken(t, ctx, ti, newAgent(t, ctx, ti, "ed-agent"))

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	_, err = registerDevice(ctx, ti, token, publicJWK(t, pub))
	require.NoError(t, err)
}

func TestRegisterDevice_TokenIsSingleUse(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	token := createJoinToken(t, ctx, ti, newAgent(t, ctx, ti, "single-use"))

	_, err := registerDevice(ctx, ti, token, publicJWK(t, newDeviceKey(t).Public()))
	require.NoError(t, err)

	_, err = registerDevice(ctx, ti, token, publicJWK(t, newDeviceKey(t).Public()))
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestRegisterDevice_RefusesExpiredAndUnknownTokens(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	token := createJoinToken(t, ctx, ti, newAgent(t, ctx, ti, "expiring"))

	_, err := testrepo.New(ti.conn).ExpireWorkloadDeviceJoinTokensFixture(ctx, ti.orgID)
	require.NoError(t, err)

	_, err = registerDevice(ctx, ti, token, publicJWK(t, newDeviceKey(t).Public()))
	requireOopsCode(t, err, oops.CodeUnauthorized)

	_, err = registerDevice(ctx, ti, "gram_dj_not-a-real-token", publicJWK(t, newDeviceKey(t).Public()))
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestRegisterDevice_RefusesUnacceptableKeysWithoutSpendingTheToken(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	token := createJoinToken(t, ctx, ti, newAgent(t, ctx, ti, "picky"))

	private := newDeviceKey(t)
	withPrivate := publicJWK(t, private)
	require.Contains(t, withPrivate, "d")

	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	for name, jwk := range map[string]map[string]any{
		"private key": withPrivate,
		"P-384 curve": publicJWK(t, p384.Public()),
		"RSA key":     publicJWK(t, rsaKey.Public()),
		"not a JWK":   {"kty": "EC"},
		"symmetric":   {"kty": "oct", "k": "c2VjcmV0"},
	} {
		_, err := registerDevice(ctx, ti, token, jwk)
		require.Error(t, err, name)
		requireOopsCode(t, err, oops.CodeInvalid)
	}

	// None of the refusals consumed the token.
	_, err = registerDevice(ctx, ti, token, publicJWK(t, newDeviceKey(t).Public()))
	require.NoError(t, err)
}

func TestCreateDeviceJoinToken_RequiresAuthorizingTheAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	agentID := newAgent(t, ctx, ti, "someone-elses-agent")
	payload := &gen.CreateDeviceJoinTokenPayload{
		AgentID:          agentID.String(),
		ExpiresInSeconds: 3600,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}

	// The owner may create one with workload:write alone.
	_, err := ti.service.CreateDeviceJoinToken(withScopes(t, ctx, ti, authz.ScopeWorkloadWrite), payload)
	require.NoError(t, err)

	// Anyone else needs agent:authorize on the agent as well, so ownership is
	// the only thing standing in for it.
	other := asUser(t, ctx, ti.userID+"-other")

	_, err = ti.service.CreateDeviceJoinToken(withScopes(t, other, ti, authz.ScopeWorkloadWrite), payload)
	requireOopsCode(t, err, oops.CodeForbidden)

	authorized := authztest.WithExactGrants(t, other,
		authz.NewGrant(authz.ScopeWorkloadWrite, ti.orgID),
		authz.NewGrant(authz.ScopeAgentAuthorize, agentID.String()),
	)
	_, err = ti.service.CreateDeviceJoinToken(authorized, payload)
	require.NoError(t, err)

	authorizeOnly := authztest.WithExactGrants(t, other, authz.NewGrant(authz.ScopeAgentAuthorize, agentID.String()))
	_, err = ti.service.CreateDeviceJoinToken(authorizeOnly, payload)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateDeviceJoinToken_RequiresTheDeviceAgentGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	payload := func(agentID uuid.UUID) *gen.CreateDeviceJoinTokenPayload {
		return &gen.CreateDeviceJoinTokenPayload{
			AgentID:          agentID.String(),
			ExpiresInSeconds: 3600,
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
		}
	}

	bare := newAgentWithoutDeviceGrants(t, ctx, ti, "no-device-grants")
	_, err := ti.service.CreateDeviceJoinToken(ctx, payload(bare))
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
	require.ErrorContains(t, err, "org:device_agent_sync and org:hooks_ingest")

	// Policy polling alone is not enough: hooks would be refused on every send.
	seedAgentGrant(t, ctx, ti, bare, authz.ScopeOrgDeviceAgentSync)
	_, err = ti.service.CreateDeviceJoinToken(ctx, payload(bare))
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
	require.ErrorContains(t, err, "org:hooks_ingest")
	require.NotContains(t, err.Error(), "org:device_agent_sync")

	seedAgentGrant(t, ctx, ti, bare, authz.ScopeOrgHooksIngest)
	_, err = ti.service.CreateDeviceJoinToken(ctx, payload(bare))
	require.NoError(t, err)
}

// asUser returns ctx as another user of the same organization arrives.
func asUser(t *testing.T, ctx context.Context, userID string) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	other := *authCtx
	other.UserID = userID
	return contextvalues.SetAuthContext(ctx, &other)
}

func TestWithdrawSubject_RetiresTheDevicesKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	token := createJoinToken(t, ctx, ti, newAgent(t, ctx, ti, "withdrawn"))
	reg, err := registerDevice(ctx, ti, token, publicJWK(t, newDeviceKey(t).Public()))
	require.NoError(t, err)

	policy, err := ti.service.List(ctx, &gen.ListPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	var admissionID string
	for _, admission := range policy.Admissions {
		if admission.Subject == reg.DeviceSubject {
			admissionID = admission.ID
		}
	}
	require.NotEmpty(t, admissionID)

	_, err = ti.service.WithdrawSubject(ctx, &gen.WithdrawSubjectPayload{ID: admissionID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	live, err := testrepo.New(ti.conn).CountLiveWorkloadDeviceKeysFixture(ctx, testrepo.CountLiveWorkloadDeviceKeysFixtureParams{OrganizationID: ti.orgID, Subject: reg.DeviceSubject})
	require.NoError(t, err)
	require.Zero(t, live)
}

func TestRegisterIssuer_RefusesTheDeviceIssuerPrefix(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		Name:                   "impostor",
		Issuer:                 workloadpolicy.DeviceIssuer(testServerURL, ti.orgID),
		JwksURI:                "https://attacker.example.com/jwks.json",
		Description:            nil,
		Tags:                   nil,
		AllowWildcardAdmission: nil,
		ProjectScoped:          false,
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}
