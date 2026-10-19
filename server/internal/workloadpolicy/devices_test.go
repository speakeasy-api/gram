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
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
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
	require.Equal(t, workloadidentity.DeviceIssuerURL(testServerURL, ti.orgID), reg.Issuer)
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

// A device that crashed after registering but before saving the result retries
// with the same token and key, and gets its existing enrollment back rather than
// a refusal or a second device.
func TestRegisterDevice_SameKeyRetryReturnsTheExistingEnrollment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	agentID := newAgent(t, ctx, ti, "retrying")
	token := createJoinToken(t, ctx, ti, agentID)
	jwk := publicJWK(t, newDeviceKey(t).Public())

	first, err := registerDevice(ctx, ti, token, jwk)
	require.NoError(t, err)
	admits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)

	again, err := registerDevice(ctx, ti, token, jwk)
	require.NoError(t, err)
	require.Equal(t, first, again)

	keys, err := testrepo.New(ti.conn).CountLiveWorkloadDeviceKeysFixture(ctx, testrepo.CountLiveWorkloadDeviceKeysFixtureParams{OrganizationID: ti.orgID, Subject: first.DeviceSubject})
	require.NoError(t, err)
	require.Equal(t, int64(1), keys)
	afterAdmits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)
	require.Equal(t, admits, afterAdmits, "a retry must not admit the device again")
}

// The retry is honoured only within the token's lifetime and while the device
// is still enrolled.
func TestRegisterDevice_SameKeyRetryRefusedOnceExpiredOrWithdrawn(t *testing.T) {
	t.Parallel()

	t.Run("expired", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		token := createJoinToken(t, ctx, ti, newAgent(t, ctx, ti, "retry-expired"))
		jwk := publicJWK(t, newDeviceKey(t).Public())
		_, err := registerDevice(ctx, ti, token, jwk)
		require.NoError(t, err)

		_, err = testrepo.New(ti.conn).ExpireWorkloadDeviceJoinTokensFixture(ctx, ti.orgID)
		require.NoError(t, err)
		_, err = registerDevice(ctx, ti, token, jwk)
		requireOopsCode(t, err, oops.CodeUnauthorized)
	})

	t.Run("withdrawn", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		token := createJoinToken(t, ctx, ti, newAgent(t, ctx, ti, "retry-withdrawn"))
		jwk := publicJWK(t, newDeviceKey(t).Public())
		reg, err := registerDevice(ctx, ti, token, jwk)
		require.NoError(t, err)

		withdrawDevice(t, ctx, ti, reg.DeviceSubject)
		_, err = registerDevice(ctx, ti, token, jwk)
		requireOopsCode(t, err, oops.CodeUnauthorized)
	})
}

func withdrawDevice(t *testing.T, ctx context.Context, ti *testInstance, subject string) {
	t.Helper()
	policy, err := ti.service.List(ctx, &gen.ListPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	for _, admission := range policy.Admissions {
		if admission.Subject == subject {
			_, err = ti.service.WithdrawSubject(ctx, &gen.WithdrawSubjectPayload{ID: admission.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
			require.NoError(t, err)
			return
		}
	}
	t.Fatalf("no admission for %s", subject)
}

// Grants removed after the token was issued are caught at redemption, and the
// refusal leaves the token unspent.
func TestRegisterDevice_RechecksTheDeviceAgentGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	agentID := newAgent(t, ctx, ti, "grants-removed")
	token := createJoinToken(t, ctx, ti, agentID)

	selectors, err := authz.NewSelector(authz.ScopeOrgHooksIngest, ti.orgID).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).DeletePrincipalGrantByIdentity(ctx, accessrepo.DeletePrincipalGrantByIdentityParams{
		OrganizationID: ti.orgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()),
		Scope:          string(authz.ScopeOrgHooksIngest),
		Selectors:      selectors,
	})
	require.NoError(t, err)

	_, err = registerDevice(ctx, ti, token, publicJWK(t, newDeviceKey(t).Public()))
	requireOopsCode(t, err, oops.CodeFailedPrecondition)

	seedAgentGrant(t, ctx, ti, agentID, authz.ScopeOrgHooksIngest)
	_, err = registerDevice(ctx, ti, token, publicJWK(t, newDeviceKey(t).Public()))
	require.NoError(t, err, "the refused attempt must not have spent the token")
}

// A key already live for one device cannot be registered for a second, which
// would let one private key sign as both.
func TestRegisterDevice_RefusesAKeyAlreadyRegistered(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	agentID := newAgent(t, ctx, ti, "shared-key")
	jwk := publicJWK(t, newDeviceKey(t).Public())

	_, err := registerDevice(ctx, ti, createJoinToken(t, ctx, ti, agentID), jwk)
	require.NoError(t, err)

	second := createJoinToken(t, ctx, ti, agentID)
	_, err = registerDevice(ctx, ti, second, jwk)
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = registerDevice(ctx, ti, second, publicJWK(t, newDeviceKey(t).Public()))
	require.NoError(t, err, "the conflicting attempt must not have spent the token")
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
	_, err := ti.service.CreateDeviceJoinToken(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeWorkloadWrite, ti.orgID)), payload)
	require.NoError(t, err)

	// Anyone else needs agent:authorize on the agent as well, so ownership is
	// the only thing standing in for it.
	other := asUser(t, ctx, ti.userID+"-other")

	_, err = ti.service.CreateDeviceJoinToken(authztest.WithExactGrants(t, other, authz.NewGrant(authz.ScopeWorkloadWrite, ti.orgID)), payload)
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

	// Every spelling issuer lookup treats as the same URL is reserved.
	for _, issuer := range []string{
		workloadidentity.DeviceIssuerURL(testServerURL, ti.orgID),
		"https://gram.example.com:443/workload/devices/" + ti.orgID,
		"https://GRAM.example.com/workload/devices/" + ti.orgID,
		"https://gram.example.com/workload/devices/" + ti.orgID + "/",
		"https://gram.example.com/workload/devices",
	} {
		_, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
			Name:                   "impostor",
			Issuer:                 issuer,
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
}

func TestRegisterDevice_ThrottlesOneTokenWithoutThrottlingOthers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	key := publicJWK(t, newDeviceKey(t).Public())

	// One token's replays exhaust only that token's bucket.
	busy := "gram_dj_throttled-" + uuid.NewString()
	var limited bool
	for range 20 {
		_, err := registerDevice(ctx, ti, busy, key)
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		if oopsErr.Code == oops.CodeRateLimitExceeded {
			limited = true
			break
		}
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	}
	require.True(t, limited, "repeated attempts with one token are throttled")

	// Another token is still answered on its merits.
	_, err := registerDevice(ctx, ti, "gram_dj_fresh-"+uuid.NewString(), key)
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestRegisterDevice_FleetCeilingIsCheckedBeforeThePerTokenBucket(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	key := publicJWK(t, newDeviceKey(t).Public())

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	store := ratelimit.NewRedisStore(redisClient)
	fleet := ratelimit.New(store, "test-fleet-"+uuid.NewString(), ratelimit.PerMinute(1))
	perToken := ratelimit.New(store, "test-token-"+uuid.NewString(), ratelimit.PerMinute(5))
	workloadpolicy.SetRegisterLimitersForTest(ti.service, fleet, perToken)

	// The first request spends the whole fleet ceiling.
	_, err = registerDevice(ctx, ti, "gram_dj_first-"+uuid.NewString(), key)
	requireOopsCode(t, err, oops.CodeUnauthorized)

	// A request past the ceiling is refused without touching its own bucket.
	second := "gram_dj_second-" + uuid.NewString()
	_, err = registerDevice(ctx, ti, second, key)
	requireOopsCode(t, err, oops.CodeRateLimitExceeded)

	res, err := perToken.Allow(ctx, workloadpolicy.HashDeviceJoinTokenForTest(second))
	require.NoError(t, err)
	require.Equal(t, 4, res.Remaining, "the refused request must not have consumed its per-token bucket")
}

func TestCreateDeviceJoinToken_RequiresAPerson(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	agentID := newAgent(t, ctx, ti, "person-bound-agent")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	noPerson := *authCtx
	noPerson.UserID = ""
	_, err := ti.service.CreateDeviceJoinToken(contextvalues.SetAuthContext(ctx, &noPerson), &gen.CreateDeviceJoinTokenPayload{
		AgentID:          agentID.String(),
		ExpiresInSeconds: 3600,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

// The project a hook may reach is decided per request, so an agent whose hooks
// grant is bounded to one project can still enroll a device.
func TestCreateDeviceJoinToken_AcceptsAnAgentWhoseHooksGrantNamesOneProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	agentID := newAgentWithoutDeviceGrants(t, ctx, ti, "one-project-hooks")
	seedAgentGrant(t, ctx, ti, agentID, authz.ScopeOrgDeviceAgentSync)
	selector := authz.NewSelector(authz.ScopeOrgHooksIngest, ti.orgID)
	selector[authz.SelectorKeyProjectID] = ti.projectID.String()
	selectors, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: ti.orgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()),
		Scope:          string(authz.ScopeOrgHooksIngest),
		Selectors:      selectors,
	})
	require.NoError(t, err)

	_, err = ti.service.CreateDeviceJoinToken(ctx, &gen.CreateDeviceJoinTokenPayload{
		AgentID:          agentID.String(),
		ExpiresInSeconds: 3600,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}
