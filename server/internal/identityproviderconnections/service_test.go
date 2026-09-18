package identityproviderconnections_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

	// Byte-for-byte: a trailing slash is a different issuer.
	si.discovery.issuerFor = func(orgURL string) string { return orgURL + "/" }
	_, err = si.svc.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)

	// Nothing was left behind for the organization.
	empty, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: nil})
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

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: nil})
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

func TestCreate_PendingIssuersCoexistUntilCredentialProof(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	first := createConnection(t, ctx, si, fullOrgURL)
	otherCtx, _ := asOtherOrganization(t, ctx, si)
	second := createConnection(t, otherCtx, si, fullOrgURL)

	// The later creator can prove ownership; the earlier pending row cannot squat.
	submitClientID(t, otherCtx, si, second.ID)
	_, err := si.svc.SubmitClientID(ctx, &gen.SubmitClientIDPayload{ID: first.ID, ClientID: testClientID})
	requireOopsCode(t, err, oops.CodeConflict)
	fetched, err := si.svc.Get(ctx, &gen.GetPayload{ID: &first.ID})
	require.NoError(t, err)
	require.False(t, fetched.Connection.ClientIDSubmitted, "conflicting claim rolls back the client id")
	require.Equal(t, identityproviderconnections.StatusPending, fetched.Connection.Status)
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
	require.Equal(t, []string{"VerifyScopes", "ListApps", "ListUsers", "ListGroups"}, fake.Calls())

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

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
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

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusPending, fetched.Connection.Status)
	require.False(t, fetched.Connection.ClientIDSubmitted)
	require.Equal(t, identityproviderconnections.LastErrorCredentialRejected, conv.PtrValOr(fetched.Connection.LastError, ""))

	submits, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionSubmitClientID)
	require.NoError(t, err)
	require.EqualValues(t, 1, submits)
	verifies, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionVerify)
	require.NoError(t, err)
	require.Zero(t, verifies, "a failed submission must not masquerade as Verify")

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
	fake.SetMethodError("ListApps", &okta.APIError{Method: http.MethodGet, Path: "/api/v1/apps", StatusCode: http.StatusForbidden, ErrorCode: "E0000006", Summary: "You do not have permission to access the feature you are requesting"})
	verified, err := si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusDegraded, verified.Status)
	require.Equal(t, []string{identityproviderconnections.ReasonReadFailedApps, identityproviderconnections.ReasonMissingRole}, verified.VerificationReasons)
	require.Nil(t, verified.LastError)
	require.Equal(t, allScopes(), verified.GrantedScopes)
	require.Contains(t, fake.Calls(), "ListUsers", "every granted scope is read")

	// The token mint failing as a non-credential error keeps the status and
	// records the typed failure.
	fake.SetMethodError("ListApps", nil)
	fake.SetError(&okta.APIError{Method: http.MethodPost, Path: "/oauth2/v1/token", StatusCode: http.StatusBadGateway, ErrorCode: "", Summary: "upstream unavailable"})
	_, err = si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeUnavailable)

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusDegraded, fetched.Connection.Status)
	require.Equal(t, identityproviderconnections.LastErrorOktaUnreachable, conv.PtrValOr(fetched.Connection.LastError, ""))
}

func TestVerify_CredentialRejectedDegrades(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	submitClientID(t, ctx, si, created.ID)

	audited, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionVerify)
	require.NoError(t, err)

	si.oktaFakes.Fake(fullOrgURL).SetError(&okta.APIError{Method: http.MethodPost, Path: "/oauth2/v1/token", StatusCode: http.StatusBadRequest, ErrorCode: "invalid_client", Summary: "The client_assertion signature is invalid"})
	_, err = si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusDegraded, fetched.Connection.Status)
	require.Equal(t, []string{identityproviderconnections.ReasonKeyNotFetched}, fetched.Connection.VerificationReasons)
	require.Equal(t, identityproviderconnections.LastErrorCredentialRejected, conv.PtrValOr(fetched.Connection.LastError, ""))
	require.NotNil(t, fetched.Connection.LastVerifiedAt, "the earlier verified outcome is kept")

	// The status change is audited even though the attempt rolled back.
	after, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionVerify)
	require.NoError(t, err)
	require.Equal(t, audited+1, after)
	record, err := audittest.LatestAuditLogByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionVerify)
	require.NoError(t, err)
	require.Equal(t, si.authCtx.UserID, record.ActorID)
}

func TestVerify_StaleFailureDoesNotOverwriteALaterSuccess(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	submitClientID(t, ctx, si, created.ID)
	id := mustParseUUID(t, created.ID)
	q := repo.New(si.conn.conn)

	// The row a failed attempt observed before a concurrent verification committed.
	observed, err := q.GetIdentityProviderConnection(ctx, repo.GetIdentityProviderConnectionParams{ID: id, OrganizationID: si.orgID})
	require.NoError(t, err)
	verified, err := si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusVerified, verified.Status)

	_, err = q.RecordIdentityProviderConnectionVerificationFailure(ctx, repo.RecordIdentityProviderConnectionVerificationFailureParams{
		Status:            identityproviderconnections.StatusDegraded,
		LastError:         conv.ToPGText(identityproviderconnections.LastErrorCredentialRejected),
		ID:                id,
		OrganizationID:    si.orgID,
		ExpectedUpdatedAt: observed.UpdatedAt,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "the compare-and-swap refuses the stale write")

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusVerified, fetched.Connection.Status)
	require.Nil(t, fetched.Connection.LastError)
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
	// A subtype-only mutation moves the reported updated_at.
	rows, err := repo.New(si.conn.conn).GetOktaIdentityProviderConnection(ctx, repo.GetOktaIdentityProviderConnectionParams{OrganizationID: si.orgID, ID: conv.ToNullUUID(mustParseUUID(t, created.ID))})
	require.NoError(t, err)
	require.True(t, rows.OktaIdentityProviderConnection.UpdatedAt.Time.After(rows.IdentityProviderConnection.UpdatedAt.Time))
	require.Equal(t, rows.OktaIdentityProviderConnection.UpdatedAt.Time.UTC().Format(time.RFC3339), recorded.UpdatedAt)
	fetchedAgent, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, recorded.UpdatedAt, fetchedAgent.Connection.UpdatedAt)

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

func TestRevoke_BeforeSubmitReportsNoMissingScopes(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	revoked, err := si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusRevoked, revoked.Status)
	require.Nil(t, revoked.LastVerifiedAt)
	require.Empty(t, revoked.MissingScopes, "nothing was verified, so nothing is missing")
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

	_, err = si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
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
	fetched, err := si.svc.Get(supportCtx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusPending, fetched.Connection.Status)
}

func TestConnections_AreInvisibleToOtherOrganizations(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	otherCtx, _ := asOtherOrganization(t, ctx, si)

	_, err := si.svc.Get(otherCtx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	requireOopsCode(t, err, oops.CodeNotFound)
	empty, err := si.svc.Get(otherCtx, &gen.GetPayload{SessionToken: nil, ID: nil})
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

	_, err = si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
}

func TestRBAC_ReadersCannotMutate(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	readerCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, si.orgID))
	_, err := si.svc.Create(readerCtx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	created := createConnection(t, ctx, si, fullOrgURL)
	fetched, err := si.svc.Get(readerCtx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.Connection.ID)

	noneCtx := authztest.WithExactGrants(t, ctx)
	_, err = si.svc.Get(noneCtx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestFlag_OnlyGatesCreate(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	si.flags.SetFlag(feature.FlagOktaConnections, si.orgID, false)

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: nil})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.Connection.ID)
	revoked, err := si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusRevoked, revoked.Status)

	_, err = si.svc.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	errCtx, errSi := newTestServiceWithFlags(t, errFlags{})
	empty, err := errSi.svc.Get(errCtx, &gen.GetPayload{SessionToken: nil, ID: nil})
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
	_, err := unconfigured.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: nil})
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

	fetched, err := si.svc.Get(keyCtx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
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

	empty, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: nil})
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

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusPending, fetched.Connection.Status)
	require.False(t, fetched.Connection.ClientIDSubmitted)
	require.Equal(t, identityproviderconnections.LastErrorOktaUnreachable, conv.PtrValOr(fetched.Connection.LastError, ""))
	require.Empty(t, fetched.Connection.VerificationReasons)

	fake.SetError(nil)
	submitted := submitClientID(t, ctx, si, created.ID)
	require.Equal(t, identityproviderconnections.StatusVerified, submitted.Status)
}

func TestRevoke_RetryAfterManagedClientDeleted(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	submitClientID(t, ctx, si, created.ID)

	revoked, err := si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusRevoked, revoked.Status)

	managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, mustParseUUID(t, created.ID))
	require.NoError(t, err)
	_, err = remotesessionsrepo.New(si.conn.conn).DeleteOrganizationRemoteSessionClient(ctx, remotesessionsrepo.DeleteOrganizationRemoteSessionClientParams{
		ID:             managed.ClientRowID,
		OrganizationID: conv.ToPGText(si.orgID),
	})
	require.NoError(t, err, "a revoked connection's client is the organization's to remove")

	again, err := si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusRevoked, again.Status)
	require.Nil(t, again.ActiveKey)
	require.Nil(t, again.ClientID)
	require.False(t, again.ClientIDSubmitted)
	require.Empty(t, again.JwksURL)
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
	submitClientID(t, ctx, si, first.ID)
	otherCtx, _ := asOtherOrganization(t, ctx, si)
	second := createConnection(t, otherCtx, si, fullOrgURL)
	_, err := si.svc.SubmitClientID(otherCtx, &gen.SubmitClientIDPayload{ID: second.ID, ClientID: testClientID})
	requireOopsCode(t, err, oops.CodeConflict)
	_, err = si.svc.Revoke(ctx, &gen.RevokePayload{ID: first.ID})
	require.NoError(t, err)
	submitClientID(t, otherCtx, si, second.ID)
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
	for _, raw := range []string{"https://okta.com", "https://okta.com.evil.com", "https://example.okta.com/path", "http://localhost", "https://[::1]", "https://example.okta.com.", "https://xn--exmple-cua.okta.com", "https://exämple.okta.com", "https://%zz.okta.com", "https://\u212Aexample.okta.com", "https://example.okta.com?", "https://example.okta.com#", "https://example.okta.com/#"} {
		_, err := identityproviderconnections.NormalizeOktaOrgURL(raw)
		require.Error(t, err, raw)
	}
}

func TestCreate_ConcurrentInstanceCannotAbandonProvisioning(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	kms := provisiontest.NewKMSClients(t)
	blocked := provisiontest.NewProvisioner(t, si.conn.conn, func(ctx context.Context, ts oauth2.TokenSource) (gcpkms.ProvisioningClient, error) {
		enterOnce.Do(func() { close(entered) })
		select {
		case <-release:
			return kms.Factory(ctx, ts)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}, testServerURL, si.credentialID)
	firstService := si.build(blocked)
	secondService := si.build(si.provisioner)
	first := make(chan error, 1)
	go func() {
		_, err := firstService.Create(ctx, &gen.CreatePayload{OrgURL: fullOrgURL})
		first <- err
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("create did not reach KMS provisioning")
	}
	parent, err := repo.New(si.conn.conn).GetLiveOktaIdentityProviderConnectionForOrganization(ctx, si.orgID)
	require.NoError(t, err)
	second := make(chan error, 1)
	go func() {
		_, err := secondService.Create(ctx, &gen.CreatePayload{OrgURL: fullOrgURL})
		second <- err
	}()
	// The second instance must refuse recovery while the first is paused after
	// the parent commit, not abandon it as an interrupted create.
	select {
	case err := <-second:
		requireOopsCode(t, err, oops.CodeConflict)
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent create blocked rather than returning conflict")
	}
	unblock()
	require.NoError(t, <-first)
	preserved, err := repo.New(si.conn.conn).GetIdentityProviderConnectionIncludingDeleted(ctx, repo.GetIdentityProviderConnectionIncludingDeletedParams{
		ID: parent.IdentityProviderConnection.ID, OrganizationID: si.orgID,
	})
	require.NoError(t, err)
	require.False(t, preserved.Deleted, "the active parent must not be abandoned and recreated")
}

func TestSubmitClientID_NoMintedTokenDoesNotClaimIssuer(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	// The default empty fixture returns missing scopes and no minted token.
	const orgURL = "https://unproven.okta.com"
	first := createConnection(t, ctx, si, orgURL)
	otherCtx, _ := asOtherOrganization(t, ctx, si)
	second := createConnection(t, otherCtx, si, orgURL)
	contexts := []context.Context{ctx, otherCtx}
	for i, connection := range []*gen.OktaIdentityProviderConnection{first, second} {
		result := submitClientID(t, contexts[i], si, connection.ID)
		require.Equal(t, identityproviderconnections.StatusDegraded, result.Status)
		stored, err := repo.New(si.conn.conn).GetOktaIdentityProviderConnection(ctx, repo.GetOktaIdentityProviderConnectionParams{
			OrganizationID: connection.OrganizationID, ID: conv.ToNullUUID(mustParseUUID(t, connection.ID)),
		})
		require.NoError(t, err)
		require.False(t, stored.OktaIdentityProviderConnection.OwnershipClaimed, "nil verification error is not credential proof")
	}
}

func TestSubmitClientID_ConcurrentIssuerClaimsHaveOneWinner(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	first := createConnection(t, ctx, si, fullOrgURL)
	otherCtx, _ := asOtherOrganization(t, ctx, si)
	second := createConnection(t, otherCtx, si, fullOrgURL)
	start := make(chan struct{})
	results := make(chan error, 2)
	contexts := []context.Context{ctx, otherCtx}
	connections := []*gen.OktaIdentityProviderConnection{first, second}
	for i, connection := range connections {
		go func() {
			<-start
			_, err := si.svc.SubmitClientID(contexts[i], &gen.SubmitClientIDPayload{ID: connection.ID, ClientID: testClientID})
			results <- err
		}()
	}
	close(start)
	a, b := <-results, <-results
	if a == nil {
		requireOopsCode(t, b, oops.CodeConflict)
	} else {
		requireOopsCode(t, a, oops.CodeConflict)
		require.NoError(t, b)
	}
	claimed := 0
	for _, connection := range connections {
		stored, err := repo.New(si.conn.conn).GetOktaIdentityProviderConnection(ctx, repo.GetOktaIdentityProviderConnectionParams{
			OrganizationID: connection.OrganizationID, ID: conv.ToNullUUID(mustParseUUID(t, connection.ID)),
		})
		require.NoError(t, err)
		if stored.OktaIdentityProviderConnection.OwnershipClaimed {
			claimed++
		}
	}
	require.Equal(t, 1, claimed)
}

func TestCreate_SmallPoolAdmissionPreventsCrossOrganizationStarvation(t *testing.T) {
	t.Parallel()
	for _, maxConns := range []int32{2, 4} {
		t.Run(fmt.Sprintf("pool_%d", maxConns), func(t *testing.T) {
			t.Parallel()
			ctx, si := newTestServiceWithPoolLimit(t, nil, maxConns)
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			slots := int(maxConns / 2)
			contexts := []context.Context{ctx}
			for range slots {
				otherCtx, _ := asOtherOrganization(t, ctx, si)
				contexts = append(contexts, otherCtx)
			}
			entered := make(chan struct{}, slots)
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			kms := provisiontest.NewKMSClients(t)
			blocked := provisiontest.NewProvisioner(t, si.conn.conn, func(ctx context.Context, ts oauth2.TokenSource) (gcpkms.ProvisioningClient, error) {
				entered <- struct{}{}
				select {
				case <-release:
					return kms.Factory(ctx, ts)
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}, testServerURL, si.credentialID)
			results := make(chan error, slots)
			for i := range slots {
				// Separate Service values deliberately share the same pool: admission
				// must not accidentally give each instance its own full allowance.
				service := si.build(blocked)
				go func() {
					_, err := service.Create(contexts[i], &gen.CreatePayload{OrgURL: fullOrgURL})
					results <- err
				}()
			}
			for range slots {
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal("admitted creates starved before reaching KMS")
				}
			}
			// Every admitted create is holding its advisory connection at KMS.
			// An additional organization must be refused, not take the last
			// connection and prevent their subsequent database writes.
			overflowCtx, overflowCancel := context.WithTimeout(contexts[slots], time.Second)
			defer overflowCancel()
			_, err := si.svc.Create(overflowCtx, &gen.CreatePayload{OrgURL: fullOrgURL})
			requireOopsCode(t, err, oops.CodeUnavailable)
			require.NoError(t, overflowCtx.Err(), "admission must fail without waiting")
			unblock()
			for range slots {
				select {
				case err := <-results:
					require.NoError(t, err)
				case <-ctx.Done():
					t.Fatal("admitted creates starved after KMS completed")
				}
			}
			// Completion returns admission capacity, including across instances.
			createConnection(t, contexts[slots], si, fullOrgURL)
		})
	}
}

func TestCreate_SingleConnectionPoolIsUnavailable(t *testing.T) {
	t.Parallel()
	ctx, si := newTestServiceWithPoolLimit(t, nil, 1)
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_, err := si.svc.Create(ctx, &gen.CreatePayload{OrgURL: fullOrgURL})
	requireOopsCode(t, err, oops.CodeUnavailable)
	require.NoError(t, ctx.Err())
	count, err := repo.New(si.conn.conn).CountIdentityProviderConnectionsCreatedSince(ctx, repo.CountIdentityProviderConnectionsCreatedSinceParams{
		OrganizationID: si.orgID, Provider: identityproviderconnections.ProviderOkta, Since: conv.ToPGTimestamptz(time.Time{}),
	})
	require.NoError(t, err)
	require.Zero(t, count)
}

// Every mutation that holds the row lock must finish with only that one pool
// connection available to it.
func TestMutations_CompleteWithOneSparePoolConnection(t *testing.T) {
	t.Parallel()
	ctx, si := newTestServiceWithPoolLimit(t, nil, 2)
	created := createConnection(t, ctx, si, fullOrgURL)

	held, err := si.conn.conn.Acquire(ctx)
	require.NoError(t, err)
	t.Cleanup(held.Release)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	agentID := "0oaagent000000000001"
	recorded, err := si.svc.RecordAgent(ctx, &gen.RecordAgentPayload{SessionToken: nil, ID: created.ID, AgentID: &agentID, AgentAppID: nil})
	require.NoError(t, err)
	require.Equal(t, agentID, conv.PtrValOr(recorded.AgentID, ""))

	submitClientID(t, ctx, si, created.ID)

	verified, err := si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusVerified, verified.Status)

	revoked, err := si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusRevoked, revoked.Status)

	again, err := si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusRevoked, again.Status)
	require.NoError(t, ctx.Err())
}
