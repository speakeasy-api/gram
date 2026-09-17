package identityproviderconnections_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
)

func TestCreate_RejectsDisallowedOrgURLs(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	for _, raw := range []string{
		"http://example.okta.com",
		"https://example.okta.com/oauth2/default",
		"https://example.okta.com?x=1",
		"https://user:pw@example.okta.com",
		"https://example.okta.com:8443",
		"https://okta.com",
		"https://example.evil.com",
		"https://example.okta.com.evil.com",
		"https://notokta.com",
		"https://example.okta.com.",
		"https://xn--exmple-cua.okta.com",
		"",
	} {
		_, err := si.svc.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: raw, ListingMode: nil})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}

func TestCreate_RefusesIssuerMismatch(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	si.discovery.issuerFor = func(string) string { return "https://other.okta.com" }
	_, err := si.svc.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)

	// Nothing was left behind for the organization.
	empty, err := si.svc.Get(ctx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: nil})
	require.NoError(t, err)
	require.Nil(t, empty.Connection)
}

func TestCreate_RefusesTokenEndpointOnOtherHost(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	si.discovery.tokenEndpoint = func(string) string { return "https://attacker.example/oauth2/v1/token" }
	_, err := si.svc.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
}

func TestCreate_RefusesWithoutPrivateKeyJWT(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	si.discovery.authMethods = []string{"client_secret_basic"}
	_, err := si.svc.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
}

func TestCreate_DiscoveryFailure(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	si.discovery.err = errors.New("dial tcp: timeout")
	_, err := si.svc.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
}

func TestCreate_ProvisionsPendingConnection(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	before, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionCreate)
	require.NoError(t, err)

	created := createConnection(t, ctx, si, "https://Example.okta.com/")

	require.Equal(t, si.orgID, created.OrganizationID)
	require.Equal(t, "okta", created.Provider)
	require.Equal(t, identityproviderconnections.StatusPending, created.Status)
	require.Equal(t, fullOrgURL, created.OrgURL)
	require.Equal(t, fullOrgURL, created.IssuerURL)
	require.Equal(t, identityproviderconnections.ListingModeCustomApp, created.ListingMode)
	require.Contains(t, created.JwksURL, testServerURL)
	require.Nil(t, created.ClientID)
	require.False(t, created.ClientIDSubmitted)
	require.Equal(t, allScopes(), created.RequiredScopes)
	require.Empty(t, created.GrantedScopes)
	require.Empty(t, created.MissingScopes, "missing scopes are unknown before the first verification")
	require.Empty(t, created.VerificationReasons)
	require.Nil(t, created.LastVerifiedAt)
	require.NotNil(t, created.ActiveKey)
	require.NotEmpty(t, created.ActiveKey.Kid)

	keys := checklistKeys(created.Checklist)
	require.Equal(t, []string{
		"create_api_services_app", "public_key_auth", "dpop", "grant_scopes", "assign_admin_roles", "submit_client_id",
		"enable_xaa_on_resource_apps", "create_ai_agent", "agent_delegated_caller", "agent_public_key", "activate_agent",
		"resource_connections", "record_agent",
	}, keys)
	for _, item := range created.Checklist {
		if item.Key == "public_key_auth" || item.Key == "agent_public_key" {
			require.Contains(t, item.Description, created.JwksURL)
		}
		if item.Key == "resource_connections" {
			require.Contains(t, item.Description, "Allow all")
		}
		if item.Key == "assign_admin_roles" {
			require.Contains(t, item.Description, "MFA")
		}
	}

	after, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	record, err := audittest.LatestAuditLogByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionCreate)
	require.NoError(t, err)
	require.Equal(t, si.authCtx.UserID, record.ActorID)

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: nil})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.Connection.ID)
}

func TestCreate_OINListingSkipsCustomAppAndXAASteps(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created, err := si.svc.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: conv.PtrEmpty("oin")})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.ListingModeOIN, created.ListingMode)

	keys := checklistKeys(created.Checklist)
	require.Contains(t, keys, "add_oin_app")
	require.NotContains(t, keys, "create_api_services_app")
	require.NotContains(t, keys, "enable_xaa_on_resource_apps")
	require.Contains(t, keys, "agent_delegated_caller")
}

func TestCreate_SecondLiveConnectionConflicts(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	createConnection(t, ctx, si, fullOrgURL)

	_, err := si.svc.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: degradedOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestCreate_SameOktaOrgInAnotherOrganizationConflicts(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	createConnection(t, ctx, si, fullOrgURL)

	otherCtx, _ := asOtherOrganization(t, ctx, si)
	_, err := si.svc.Create(otherCtx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestSubmitClientID_RejectsMalformedIDs(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	for _, raw := range []string{"", "abc", "0oa", "0oashort", "0oa-with-dashes-0000000", "okta-pending-" + created.ID} {
		_, err := si.svc.SubmitClientID(ctx, &gen.SubmitClientIDPayload{SessionToken: nil, ID: created.ID, ClientID: raw})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}

func TestSubmitClientID_VerifiesAndPersists(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)

	submitBefore, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionSubmitClientID)
	require.NoError(t, err)
	verifyBefore, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionVerify)
	require.NoError(t, err)

	submitted := submitClientID(t, ctx, si, created.ID)

	require.Equal(t, identityproviderconnections.StatusVerified, submitted.Status)
	require.True(t, submitted.ClientIDSubmitted)
	require.Equal(t, testClientID, conv.PtrValOr(submitted.ClientID, ""))
	require.True(t, submitted.DpopRequired)
	require.Equal(t, allScopes(), submitted.GrantedScopes)
	require.Empty(t, submitted.MissingScopes)
	require.Empty(t, submitted.VerificationReasons)
	require.NotNil(t, submitted.LastVerifiedAt)
	require.Nil(t, submitted.LastError)

	fake := si.oktaFakes.Fake(fullOrgURL)
	require.Equal(t, []string{"VerifyScopes", "ListApps", "ListAppUsers", "ListGroups"}, fake.Calls())

	managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, mustParseUUID(t, created.ID))
	require.NoError(t, err)
	require.Equal(t, testClientID, managed.ClientID)

	submitAfter, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionSubmitClientID)
	require.NoError(t, err)
	require.Equal(t, submitBefore+1, submitAfter)
	verifyAfter, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionVerify)
	require.NoError(t, err)
	require.Equal(t, verifyBefore, verifyAfter, "the verification outcome is folded into the submission entry")

	record, err := audittest.LatestAuditLogByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionSubmitClientID)
	require.NoError(t, err)
	require.Equal(t, si.authCtx.UserID, record.ActorID)
	afterSnap, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, testClientID, afterSnap["client_id"])
	require.Equal(t, identityproviderconnections.StatusVerified, afterSnap["status"])

	clientRecord, err := audittest.LatestAuditLogByAction(ctx, si.conn.conn, audit.ActionRemoteSessionClientUpdate)
	require.NoError(t, err)
	require.Equal(t, si.authCtx.UserID, clientRecord.ActorID)
	clientBefore, err := audittest.DecodeAuditData(clientRecord.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.PlaceholderClientID("okta", mustParseUUID(t, created.ID)), clientBefore["ClientID"])
	clientAfter, err := audittest.DecodeAuditData(clientRecord.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, testClientID, clientAfter["ClientID"])
}

func TestSubmitClientID_IsImmutable(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	submitClientID(t, ctx, si, created.ID)

	_, err := si.svc.SubmitClientID(ctx, &gen.SubmitClientIDPayload{SessionToken: nil, ID: created.ID, ClientID: "0oaanotherclient00002"})
	requireOopsCode(t, err, oops.CodeConflict)

	// Re-submitting the same id is refused too: the connection is no longer pending.
	_, err = si.svc.SubmitClientID(ctx, &gen.SubmitClientIDPayload{SessionToken: nil, ID: created.ID, ClientID: testClientID})
	requireOopsCode(t, err, oops.CodeConflict)

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, testClientID, conv.PtrValOr(fetched.Connection.ClientID, ""))
}

func TestSubmitClientID_DegradedStillPersists(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, degradedOrgURL)
	submitted := submitClientID(t, ctx, si, created.ID)

	require.Equal(t, identityproviderconnections.StatusDegraded, submitted.Status)
	require.True(t, submitted.ClientIDSubmitted)
	require.Equal(t, []string{"okta.apps.read"}, submitted.GrantedScopes)
	require.Equal(t, []string{"okta.users.read", "okta.groups.read"}, submitted.MissingScopes)
	require.Equal(t, []string{identityproviderconnections.ReasonMissingScope}, submitted.VerificationReasons)
	require.Nil(t, submitted.LastError, "reasons are not failures")
	require.Nil(t, submitted.LastVerifiedAt, "only a verified outcome advances last_verified_at")
}

func TestSubmitClientID_CredentialRejectedIsNotPersisted(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, rejectedOrgURL)
	si.oktaFakes.Fake(rejectedOrgURL).SetError(&okta.APIError{Method: http.MethodPost, Path: "/oauth2/v1/token", StatusCode: http.StatusUnauthorized, ErrorCode: "invalid_client", Summary: "The client_assertion signature is invalid"})

	_, err := si.svc.SubmitClientID(ctx, &gen.SubmitClientIDPayload{SessionToken: nil, ID: created.ID, ClientID: testClientID})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusPending, fetched.Connection.Status)
	require.False(t, fetched.Connection.ClientIDSubmitted)
	require.Equal(t, identityproviderconnections.LastErrorCredentialRejected, conv.PtrValOr(fetched.Connection.LastError, ""))

	// The corrected submission goes through once Okta accepts the credential.
	si.oktaFakes.Fake(rejectedOrgURL).SetError(nil)
	submitted := submitClientID(t, ctx, si, created.ID)
	require.Equal(t, identityproviderconnections.StatusVerified, submitted.Status)
	require.Nil(t, submitted.LastError)
}

func TestVerify_RequiresSubmittedClientID(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	_, err := si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
}

func TestVerify_RecordsOutcomeAndAudits(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	submitted := submitClientID(t, ctx, si, created.ID)

	before, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionVerify)
	require.NoError(t, err)

	verified, err := si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusVerified, verified.Status)
	require.True(t, verified.ClientIDSubmitted)
	require.Equal(t, submitted.ClientID, verified.ClientID)
	require.NotNil(t, verified.LastVerifiedAt)

	after, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionVerify)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	record, err := audittest.LatestAuditLogByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionVerify)
	require.NoError(t, err)
	require.Equal(t, si.authCtx.UserID, record.ActorID)
}

func TestVerify_ReadFailureDegrades(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	submitClientID(t, ctx, si, created.ID)

	fake := si.oktaFakes.Fake(fullOrgURL)
	fake.SetError(&okta.APIError{Method: http.MethodGet, Path: "/api/v1/apps", StatusCode: http.StatusForbidden, ErrorCode: "E0000006", Summary: "You do not have permission to access the feature you are requesting"})
	// VerifyScopes shares the fake error, so the token mint itself fails as a
	// non-credential error: the connection keeps its status and records the
	// typed failure.
	_, err := si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeUnavailable)

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusVerified, fetched.Connection.Status)
	require.Equal(t, identityproviderconnections.LastErrorOktaUnreachable, conv.PtrValOr(fetched.Connection.LastError, ""))
}

func TestVerify_CredentialRejectedDegrades(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	submitClientID(t, ctx, si, created.ID)

	si.oktaFakes.Fake(fullOrgURL).SetError(&okta.APIError{Method: http.MethodPost, Path: "/oauth2/v1/token", StatusCode: http.StatusBadRequest, ErrorCode: "invalid_client", Summary: "The client_assertion signature is invalid"})
	_, err := si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusDegraded, fetched.Connection.Status)
	require.Equal(t, []string{identityproviderconnections.ReasonKeyNotFetched}, fetched.Connection.VerificationReasons)
	require.Equal(t, identityproviderconnections.LastErrorCredentialRejected, conv.PtrValOr(fetched.Connection.LastError, ""))
	require.NotNil(t, fetched.Connection.LastVerifiedAt, "the earlier verified outcome is kept")
}

func TestVerify_RateLimitedPerOrganization(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	submitClientID(t, ctx, si, created.ID)

	var limited error
	for range 6 {
		_, err := si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: created.ID})
		if err != nil {
			limited = err
			break
		}
	}
	requireOopsCode(t, limited, oops.CodeRateLimitExceeded)
}

func TestRecordAgent_StoresDisplayIDs(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)

	before, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionRecordAgent)
	require.NoError(t, err)

	recorded, err := si.svc.RecordAgent(ctx, &gen.RecordAgentPayload{SessionToken: nil, ID: created.ID, AgentID: conv.PtrEmpty("0oaagent000000000001"), AgentAppID: conv.PtrEmpty("0oassoapp00000000001")})
	require.NoError(t, err)
	require.Equal(t, "0oaagent000000000001", conv.PtrValOr(recorded.AgentID, ""))
	require.Equal(t, "0oassoapp00000000001", conv.PtrValOr(recorded.AgentAppID, ""))

	after, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionRecordAgent)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	record, err := audittest.LatestAuditLogByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionRecordAgent)
	require.NoError(t, err)
	require.Equal(t, si.authCtx.UserID, record.ActorID)

	same, err := si.svc.RecordAgent(ctx, &gen.RecordAgentPayload{SessionToken: nil, ID: created.ID, AgentID: conv.PtrEmpty("0oaagent000000000001"), AgentAppID: conv.PtrEmpty("0oassoapp00000000001")})
	require.NoError(t, err)
	require.Equal(t, "0oaagent000000000001", conv.PtrValOr(same.AgentID, ""))
	unchanged, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionRecordAgent)
	require.NoError(t, err)
	require.Equal(t, after, unchanged, "an unchanged record is neither written nor audited")

	cleared, err := si.svc.RecordAgent(ctx, &gen.RecordAgentPayload{SessionToken: nil, ID: created.ID, AgentID: nil, AgentAppID: nil})
	require.NoError(t, err)
	require.Nil(t, cleared.AgentID)
	require.Nil(t, cleared.AgentAppID)

	_, err = si.svc.RecordAgent(ctx, &gen.RecordAgentPayload{SessionToken: nil, ID: created.ID, AgentID: conv.PtrEmpty("not an id!"), AgentAppID: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestRevoke_IsIdempotentAndFreesTheOrganization(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	submitClientID(t, ctx, si, created.ID)

	before, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionRevoke)
	require.NoError(t, err)

	revoked, err := si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusRevoked, revoked.Status)
	require.Nil(t, revoked.ActiveKey, "every key was withdrawn")

	// The managed client stays live serving an empty JWKS at the same URL.
	managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, mustParseUUID(t, created.ID))
	require.NoError(t, err)
	require.Equal(t, created.JwksURL, managed.JSONWebKeySetURL)
	require.False(t, managed.ActiveKeyID.Valid)
	require.JSONEq(t, `{"keys":[]}`, managedJWKS(t, ctx, si, managed))

	again, err := si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusRevoked, again.Status)

	after, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionRevoke)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "a repeat revoke changes nothing and is not audited twice")
	record, err := audittest.LatestAuditLogByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionRevoke)
	require.NoError(t, err)
	require.Equal(t, si.authCtx.UserID, record.ActorID)

	_, err = si.svc.Get(ctx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: &created.ID})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = si.svc.SubmitClientID(ctx, &gen.SubmitClientIDPayload{SessionToken: nil, ID: created.ID, ClientID: testClientID})
	requireOopsCode(t, err, oops.CodeNotFound)

	replacement := createConnection(t, ctx, si, fullOrgURL)
	require.NotEqual(t, created.ID, replacement.ID)
	require.Equal(t, identityproviderconnections.StatusPending, replacement.Status)
}

func TestMutations_RefuseSupportSessions(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	supportCtx := asSupportSession(ctx, si)

	_, err := si.svc.Create(supportCtx, &gen.CreatePayload{SessionToken: nil, OrgURL: degradedOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = si.svc.SubmitClientID(supportCtx, &gen.SubmitClientIDPayload{SessionToken: nil, ID: created.ID, ClientID: testClientID})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = si.svc.Verify(supportCtx, &gen.VerifyPayload{SessionToken: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = si.svc.RecordAgent(supportCtx, &gen.RecordAgentPayload{SessionToken: nil, ID: created.ID, AgentID: nil, AgentAppID: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = si.svc.Revoke(supportCtx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeForbidden)

	// Reads stay open to support.
	fetched, err := si.svc.Get(supportCtx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusPending, fetched.Connection.Status)
}

func TestConnections_AreInvisibleToOtherOrganizations(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	otherCtx, _ := asOtherOrganization(t, ctx, si)

	_, err := si.svc.Get(otherCtx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: &created.ID})
	requireOopsCode(t, err, oops.CodeNotFound)
	empty, err := si.svc.Get(otherCtx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: nil})
	require.NoError(t, err)
	require.Nil(t, empty.Connection)
	_, err = si.svc.SubmitClientID(otherCtx, &gen.SubmitClientIDPayload{SessionToken: nil, ID: created.ID, ClientID: testClientID})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = si.svc.Verify(otherCtx, &gen.VerifyPayload{SessionToken: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = si.svc.RecordAgent(otherCtx, &gen.RecordAgentPayload{SessionToken: nil, ID: created.ID, AgentID: nil, AgentAppID: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = si.svc.Revoke(otherCtx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = si.svc.Get(ctx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
}

func TestRBAC_ReadersCannotMutate(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	readerCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, si.orgID))
	_, err := si.svc.Create(readerCtx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	created := createConnection(t, ctx, si, fullOrgURL)
	fetched, err := si.svc.Get(readerCtx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.Connection.ID)

	noneCtx := authztest.WithExactGrants(t, ctx)
	_, err = si.svc.Get(noneCtx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: &created.ID})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestFlag_OnlyGatesCreate(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	si.flags.SetFlag(feature.FlagOktaConnections, si.orgID, false)

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: nil})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.Connection.ID)
	revoked, err := si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusRevoked, revoked.Status)

	_, err = si.svc.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	errCtx, errSi := newTestServiceWithFlags(t, errFlags{})
	empty, err := errSi.svc.Get(errCtx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: nil})
	require.NoError(t, err)
	require.Nil(t, empty.Connection)
	_, err = errSi.svc.Create(errCtx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeUnavailable)
}

func TestFlag_OrganizationLookupFailureIsUnavailable(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	missingOrgID := "org_missing_" + uuid.NewString()
	other := *si.authCtx
	other.ActiveOrganizationID = missingOrgID
	missingCtx := authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &other), authz.NewGrant(authz.ScopeOrgAdmin, missingOrgID))
	si.flags.SetFlag(feature.FlagOktaConnections, missingOrgID, true)

	_, err := si.svc.Create(missingCtx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeUnavailable)
}

func TestFlag_RBACRunsBeforeTheFlag(t *testing.T) {
	t.Parallel()
	ctx, si := newTestServiceWithFlags(t, errFlags{})

	noneCtx := authztest.WithExactGrants(t, ctx)
	_, err := si.svc.Create(noneCtx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestUnconfiguredDeployment_IsUnavailable(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	unconfigured := newUnconfiguredService(t, si)
	_, err := unconfigured.Get(ctx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: nil})
	requireOopsCode(t, err, oops.CodeUnavailable)
	_, err = unconfigured.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeUnavailable)
}

func TestMutations_RefuseLegacyAPIKeys(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	keyed := *si.authCtx
	keyed.APIKeyID = uuid.NewString()
	keyed.APIKeyScopes = []string{"producer", "consumer"}
	keyCtx := contextvalues.WithLegacyAPIKeyAuthorization(ctx, &keyed)

	_, err := si.svc.Create(keyCtx, &gen.CreatePayload{SessionToken: nil, OrgURL: degradedOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = si.svc.Revoke(keyCtx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = si.svc.SubmitClientID(keyCtx, &gen.SubmitClientIDPayload{SessionToken: nil, ID: created.ID, ClientID: testClientID})
	requireOopsCode(t, err, oops.CodeForbidden)

	fetched, err := si.svc.Get(keyCtx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.Connection.ID)
	require.Equal(t, identityproviderconnections.StatusPending, fetched.Connection.Status, "the key could not revoke it")
}

func TestCreate_OrphanedParentRowIsRecoverable(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	orphanID := provisiontest.CreateConnection(t, ctx, si.conn.conn, si.orgID, identityproviderconnections.ProviderOkta)

	created := createConnection(t, ctx, si, fullOrgURL)
	require.NotEqual(t, orphanID.String(), created.ID)

	orphan, err := repo.New(si.conn.conn).GetIdentityProviderConnectionIncludingDeleted(ctx, repo.GetIdentityProviderConnectionIncludingDeletedParams{ID: orphanID, OrganizationID: si.orgID})
	require.NoError(t, err)
	require.True(t, orphan.Deleted, "the orphan was tombstoned")
}

func TestCreate_ProvisionFailureLeavesOrgRetryable(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	broken := si.build(provisiontest.NewProvisioner(t, si.conn.conn, func(context.Context, oauth2.TokenSource) (gcpkms.ProvisioningClient, error) {
		return nil, errors.New("kms unreachable")
	}, testServerURL, si.credentialID))
	_, err := broken.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeUnexpected)

	empty, err := si.svc.Get(ctx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: nil})
	require.NoError(t, err)
	require.Nil(t, empty.Connection)
	issuers, err := remotesessionsrepo.New(si.conn.conn).ListOrganizationRemoteSessionIssuers(ctx, remotesessionsrepo.ListOrganizationRemoteSessionIssuersParams{OrganizationID: conv.ToPGText(si.orgID), IncludeGlobal: false, Cursor: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, LimitValue: 10})
	require.NoError(t, err)
	require.Empty(t, issuers, "the abandoned issuer was tombstoned")

	created := createConnection(t, ctx, si, fullOrgURL)
	require.Equal(t, identityproviderconnections.StatusPending, created.Status)
}

func TestCreate_RateLimitedPerOrganization(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	var limited error
	for range 4 {
		created, err := si.svc.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
		if err != nil {
			limited = err
			break
		}
		_, err = si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
		require.NoError(t, err)
	}
	requireOopsCode(t, limited, oops.CodeRateLimitExceeded)
}

func TestCreate_DailyCapCountsTombstones(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	for range 5 {
		id := provisiontest.CreateConnection(t, ctx, si.conn.conn, si.orgID, identityproviderconnections.ProviderOkta)
		provisiontest.SoftDeleteConnection(t, ctx, si.conn.conn, si.orgID, id)
	}
	_, err := si.svc.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeRateLimitExceeded)
}

func TestSubmitClientID_TransportFailureLeavesPending(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	fake := si.oktaFakes.Fake(fullOrgURL)
	fake.SetError(&okta.APIError{Method: http.MethodPost, Path: "/oauth2/v1/token", StatusCode: http.StatusBadGateway, ErrorCode: "", Summary: "upstream unavailable"})

	_, err := si.svc.SubmitClientID(ctx, &gen.SubmitClientIDPayload{SessionToken: nil, ID: created.ID, ClientID: testClientID})
	requireOopsCode(t, err, oops.CodeUnavailable)

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{ApikeyToken: nil, SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusPending, fetched.Connection.Status)
	require.False(t, fetched.Connection.ClientIDSubmitted)
	require.Equal(t, identityproviderconnections.LastErrorOktaUnreachable, conv.PtrValOr(fetched.Connection.LastError, ""))
	require.Empty(t, fetched.Connection.VerificationReasons)

	fake.SetError(nil)
	submitted := submitClientID(t, ctx, si, created.ID)
	require.Equal(t, identityproviderconnections.StatusVerified, submitted.Status)
}

func TestRevoke_ConcurrentCallsBothSucceedAndAuditOnce(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	before, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionRevoke)
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			revoked, err := si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
			if err == nil && revoked.Status != identityproviderconnections.StatusRevoked {
				err = errors.New("unexpected status " + revoked.Status)
			}
			results <- err
		}()
	}
	close(start)
	for range 2 {
		require.NoError(t, <-results)
	}

	after, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionRevoke)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestCreate_SameOktaOrgFreedByRevoke(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	first := createConnection(t, ctx, si, fullOrgURL)
	otherCtx, _ := asOtherOrganization(t, ctx, si)
	_, err := si.svc.Create(otherCtx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: first.ID})
	require.NoError(t, err)

	second, err := si.svc.Create(otherCtx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
}

func TestNormalizeOktaOrgURL(t *testing.T) {
	t.Parallel()

	normalized, err := identityproviderconnections.NormalizeOktaOrgURL(" https://Example.OKTA.com/ ")
	require.NoError(t, err)
	require.Equal(t, fullOrgURL, normalized)

	for _, raw := range []string{"https://sub.example.okta.mil", "https://dev-1.oktapreview.com", "https://x.okta-emea.com"} {
		_, err := identityproviderconnections.NormalizeOktaOrgURL(raw)
		require.NoError(t, err, raw)
	}
	for _, raw := range []string{"https://okta.com", "https://okta.com.evil.com", "https://example.okta.com/path", "http://localhost", "https://[::1]", "https://example.okta.com.", "https://xn--exmple-cua.okta.com", "https://exämple.okta.com", "https://%zz.okta.com"} {
		_, err := identityproviderconnections.NormalizeOktaOrgURL(raw)
		require.Error(t, err, raw)
	}
}
