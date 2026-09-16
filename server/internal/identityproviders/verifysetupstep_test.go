package identityproviders_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const (
	fakeOktaPassed            = "passed"
	fakeOktaRefused           = "refused"
	fakeOktaUnavailable       = "unavailable"
	fakeOktaAppsBlocked       = "apps_blocked"
	fakeOktaManageDenied      = "manage_scope_denied"
	fakeOktaManageOmitted     = "manage_scope_omitted"
	fakeOktaClaimsOmitted     = "claims_scopes_omitted"
	fakeOktaExistingApp       = "existing_sign_in_app"
	fakeOktaAppInactive       = "sign_in_app_inactive"
	fakeOktaAppForbidden      = "sign_in_app_forbidden"
	fakeOktaAppRefused        = "sign_in_app_refused"
	fakeOktaAssignFails       = "sign_in_assignment_fails"
	fakeOktaDefaultMissing    = "default_authorization_server_missing"
	testAccessToken           = "test-access-token"
	testClientID              = "test-client-id"
	testSignInAppID           = "app-example"
	testSignInClientID        = "public-client-example"
	testEveryoneGroupID       = "group-everyone"
	testAuthorizationServerID = "auth-server-default"
	testFakeClientSecret      = "FAKE_SECRET_SENTINEL_DO_NOT_USE"
)

type fakeOktaServer struct {
	server *httptest.Server
	mode   string

	mu                   sync.Mutex
	publicJWK            jose.JSONWebKey
	validationErr        error
	createdApp           map[string]any
	createdAppCount      int
	applicationReads     int
	resolvedClientID     string
	assignedAppID        string
	assignedGroupID      string
	createdClaim         map[string]any
	createdClaimCount    int
	inventoryApps        []map[string]any
	assignmentCounts     map[string][2]int
	indirectUserCounts   map[string]int
	duplicateUserCounts  map[string]int
	assignmentFailures   map[string]bool
	assignmentStalls     map[string]bool
	assignmentCanceled   chan string
	assignmentReads      int
	embedInventoryGroups bool
	inventoryGroupReads  int
	signInClientID       string
	tokenStarted         chan struct{}
	releaseToken         chan struct{}
	startOnce            sync.Once
	releaseOnce          sync.Once
}

func TestVerifySetupStepPassesAndPersistsEvidence(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	connection := prepareConnectionForVerification(t, ctx, ti, fake)
	beforeAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "passed", result.Outcome)
	require.Equal(t, []string{"directory_read", "application_assignment_read", "sign_in_provisioning", "claims_provisioning"}, result.Capabilities)
	require.Equal(t, []string{"okta.apps.read", "okta.groups.read", "okta.users.read", "okta.apps.manage", "okta.authorizationServers.read", "okta.authorizationServers.manage"}, result.GrantedScopes)
	require.Len(t, result.Evidence.Reads, 4)
	require.True(t, result.Evidence.Reads[0].OK)
	require.Contains(t, *result.Evidence.Reads[0].Detail, "more pages are available")
	require.Equal(t, "authorization_servers", result.Evidence.Reads[3].Resource)
	require.True(t, result.Evidence.Reads[3].OK)
	require.NoError(t, fake.ValidationError())

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, mustUUID(t, connection.ID), stored.ID)
	require.Equal(t, "active", stored.Status)
	require.Equal(t, result.Capabilities, stored.Capabilities)
	require.Equal(t, result.GrantedScopes, stored.GrantedScopes)
	require.True(t, stored.LastVerifiedAt.Valid)
	require.NotContains(t, string(stored.VerifyEvidence), testAccessToken)
	require.NotContains(t, string(stored.VerifyEvidence), "PRIVATE KEY")

	signingKey, err := repo.New(ti.conn).GetIdentityProviderSigningKey(ctx, repo.GetIdentityProviderSigningKeyParams{
		OrganizationID:               ti.orgID,
		IdentityProviderConnectionID: stored.ID,
	})
	require.NoError(t, err)
	require.True(t, signingKey.LastUsedAt.Valid)

	afterAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)
	require.Equal(t, beforeAudits+1, afterAudits)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "passed", afterSnapshot["outcome"])
	require.Equal(t, []any{"directory_read", "application_assignment_read", "sign_in_provisioning", "claims_provisioning"}, afterSnapshot["capabilities"])
	auditJSON := string(record.Metadata) + string(record.BeforeSnapshot) + string(record.AfterSnapshot)
	require.NotContains(t, auditJSON, testAccessToken)
	require.NotContains(t, auditJSON, "PRIVATE KEY")

	setup, err := ti.service.DescribeSetup(ctx, &gen.DescribeSetupPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "passed", setup.Steps[0].State)
	require.Equal(t, result, setup.Steps[0].LastOutcome)
	getResult, err := ti.service.Get(ctx, &gen.GetPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, result.Evidence, getResult.Connection.VerifyEvidence)
}

func TestVerifySetupStepMintsFreshTokenAfterScopeGrantChanges(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaManageOmitted)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareConnectionForVerification(t, ctx, ti, fake)

	first, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "passed", first.Outcome)
	require.NotContains(t, first.Capabilities, "sign_in_provisioning")

	fake.SetMode(fakeOktaPassed)
	second, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "passed", second.Outcome)
	require.Contains(t, second.Capabilities, "sign_in_provisioning")
	require.Contains(t, second.Capabilities, "claims_provisioning")
}

func TestVerifySetupStepDoesNotRequireDefaultAuthorizationServer(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaDefaultMissing)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareConnectionForVerification(t, ctx, ti, fake)

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "passed", result.Outcome)
	require.NotContains(t, result.Capabilities, "claims_provisioning")
	require.Len(t, result.Evidence.Reads, 4)
	require.Equal(t, "authorization_servers", result.Evidence.Reads[3].Resource)
	require.False(t, result.Evidence.Reads[3].OK)
	require.Contains(t, *result.Evidence.Reads[3].Detail, "none named default")
}

func TestVerifySetupStepPersistsInvalidClientRefusal(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaRefused)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareConnectionForVerification(t, ctx, ti, fake)

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "refused", result.Outcome)
	require.Equal(t, "Client authentication must be Public key / Private key with Use a URL; entering the URL alone is not enough", result.Detail)
	require.Empty(t, result.Capabilities)
	require.Empty(t, result.GrantedScopes)
	require.Empty(t, result.Evidence.Reads)
	require.NoError(t, fake.ValidationError())

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "failed", stored.Status)
	require.Equal(t, result.Detail, stored.StatusDetail.String)
	require.NotContains(t, string(stored.VerifyEvidence), testAccessToken)
	signingKey, err := repo.New(ti.conn).GetIdentityProviderSigningKey(ctx, repo.GetIdentityProviderSigningKeyParams{OrganizationID: ti.orgID, IdentityProviderConnectionID: stored.ID})
	require.NoError(t, err)
	require.False(t, signingKey.LastUsedAt.Valid)
}

func TestVerifySetupStepReportsMissingApplicationRead(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaAppsBlocked)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareConnectionForVerification(t, ctx, ti, fake)

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "capability_missing", result.Outcome)
	require.Contains(t, result.Detail, "apps read failed")
	require.Equal(t, []string{"directory_read", "sign_in_provisioning", "claims_provisioning"}, result.Capabilities)
	require.Len(t, result.Evidence.Reads, 4)
	require.False(t, result.Evidence.Reads[2].OK)
	require.Equal(t, "Okta application read is not permitted.", *result.Evidence.Reads[2].Detail)
	require.NoError(t, fake.ValidationError())

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "failed", stored.Status)
	require.Equal(t, result.Capabilities, stored.Capabilities)
	require.Equal(t, result.GrantedScopes, stored.GrantedScopes)
	signingKey, err := repo.New(ti.conn).GetIdentityProviderSigningKey(ctx, repo.GetIdentityProviderSigningKeyParams{OrganizationID: ti.orgID, IdentityProviderConnectionID: stored.ID})
	require.NoError(t, err)
	require.True(t, signingKey.LastUsedAt.Valid)
}

func TestVerifySetupStepPersistsUnreachableOutcome(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	ctx, ti := newTestServiceWithOktaEndpoint(t, endpoint)
	connection := createConnection(t, ctx, ti, "https://example.okta.com")
	setStoredClientID(t, ctx, ti)

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "unreachable", result.Outcome)
	require.Contains(t, result.Detail, "Unable to reach")
	require.Empty(t, result.Evidence.Reads)

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, mustUUID(t, connection.ID), stored.ID)
	require.Equal(t, "failed", stored.Status)
}

func TestVerifySetupStepPersistsProviderUnreachableDetail(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaUnavailable)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareConnectionForVerification(t, ctx, ti, fake)

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "unreachable", result.Outcome)
	require.Contains(t, result.Detail, "Okta said: server_error: The token service is temporarily unavailable.")

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, result.Detail, stored.StatusDetail.String)
}

func TestVerifySetupStepRejectsPendingConnectionWithoutClientID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	createConnection(t, ctx, ti, "https://example.okta.com")
	beforeAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)

	_, err = ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorContains(t, err, "Client ID")
	afterAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)
	require.Equal(t, beforeAudits, afterAudits)
}

func TestVerifySetupStepRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestVerifySetupStepRejectsResultWhenClientIDChangesDuringProbe(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	fake.BlockTokenResponse()
	t.Cleanup(fake.ReleaseTokenResponse)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareConnectionForVerification(t, ctx, ti, fake)
	beforeAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)

	type verifyCall struct {
		result *gen.IdentityProviderVerifyResult
		err    error
	}
	finished := make(chan verifyCall, 1)
	go func() {
		result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
		finished <- verifyCall{result: result, err: err}
	}()
	<-fake.tokenStarted
	_, err = ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "connect",
		Values:       []*gen.IdentityProviderSetupValue{{Key: "client_id", Value: "replacement-client-id"}},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	fake.ReleaseTokenResponse()
	call := <-finished
	require.Nil(t, call.result)
	requireOopsCode(t, call.err, oops.CodeConflict)
	require.NoError(t, fake.ValidationError())

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "replacement-client-id", stored.ClientID.String)
	require.Equal(t, "awaiting_verification", stored.Status)
	afterAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)
	require.Equal(t, beforeAudits, afterAudits)
}

func TestVerifySignInStepPassesForActiveOktaAndGenericOIDCConnections(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := prepareProvisionedSignInApplication(t, fake)
	storedBefore, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.True(t, storedBefore.WorkosID.Valid)
	expectWorkOSConnectionRead(t, ti, storedBefore.WorkosID.String, "active")
	beforeAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "sign_in", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, &gen.IdentityProviderVerifyResult{
		Outcome:       "passed",
		Detail:        "Okta sign-in application and WorkOS OIDC connection verified.",
		Capabilities:  []string{"directory_read", "application_assignment_read", "sign_in_provisioning", "claims_provisioning"},
		GrantedScopes: []string{"okta.apps.read", "okta.groups.read", "okta.users.read", "okta.apps.manage", "okta.authorizationServers.read", "okta.authorizationServers.manage"},
		Evidence: &gen.IdentityProviderVerifyEvidence{
			CheckedAt: result.Evidence.CheckedAt,
			Reads: []*gen.IdentityProviderCapabilityRead{
				{Capability: "sign_in", Resource: "sign_in_application", OK: true, Count: nil, Detail: new("Okta sign-in application is active.")},
				{Capability: "sign_in", Resource: "sign_in_connection", OK: true, Count: nil, Detail: new("WorkOS OIDC connection is active.")},
			},
		},
	}, result)
	_, err = time.Parse(time.RFC3339Nano, result.Evidence.CheckedAt)
	require.NoError(t, err)
	require.NoError(t, fake.ValidationError())

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "passed", stored.SignInState.String)
	require.Equal(t, "conn-example", stored.WorkosConnectionID.String)
	var evidence map[string]any
	require.NoError(t, json.Unmarshal(stored.SignInEvidence, &evidence))
	require.Equal(t, testSignInClientID, evidence["client_id"])
	require.Equal(t, "passed", evidence["outcome"])
	require.Equal(t, result.Evidence.CheckedAt, evidence["checked_at"])
	require.Len(t, evidence["reads"], 2)
	require.NotContains(t, string(stored.SignInEvidence), testFakeClientSecret)

	afterAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)
	require.Equal(t, beforeAudits+1, afterAudits)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "application_created", beforeSnapshot["sign_in_state"])
	require.Equal(t, "passed", afterSnapshot["sign_in_state"])
	require.Equal(t, "passed", afterSnapshot["outcome"])
	auditJSON := string(record.Metadata) + string(record.BeforeSnapshot) + string(record.AfterSnapshot)
	for _, sensitive := range []string{testAccessToken, testClientID, testSignInClientID, testSignInAppID, testFakeClientSecret, "conn-example"} {
		require.NotContains(t, auditJSON, sensitive)
	}

	getResult, err := ti.service.Get(ctx, &gen.GetPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, new("passed"), getResult.Connection.SignInState)
	require.Equal(t, new("conn-example"), getResult.Connection.SignInConnectionID)
	setup, err := ti.service.DescribeSetup(ctx, &gen.DescribeSetupPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "passed", setup.Steps[1].State)
	require.Equal(t, "passed", setup.Steps[1].LastOutcome.Outcome)
}

func TestVerifySignInStepDiscoversPortalFallbackConnectionThenReadsItByID(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	ti.workos.On("CreateOIDCConnection", mock.Anything, mock.Anything).Return(workos.Connection{}, &workos.APIError{
		Method:     http.MethodPost,
		Path:       "/connections",
		StatusCode: http.StatusNotFound,
		Body:       `{"message":"This endpoint is part of the Connections API migration capabilities, which are not enabled for your environment. Contact support@workos.com to enable them."}`,
	}).Once()
	_, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "sign_in",
		Values:       []*gen.IdentityProviderSetupValue{},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	ti.workos.On("ListConnections", mock.Anything, stored.WorkosID.String).Return([]workos.Connection{{
		ID:             "conn-portal-example",
		OrganizationID: stored.WorkosID.String,
		ConnectionType: "GenericOIDC",
		Name:           "Okta",
		State:          "active",
		CreatedAt:      "2026-09-15T00:00:00Z",
		UpdatedAt:      "2026-09-15T00:00:00Z",
	}}, nil).Once()
	ti.workos.On("GetConnection", mock.Anything, "conn-portal-example").Return(workos.Connection{
		ID:             "conn-portal-example",
		OrganizationID: stored.WorkosID.String,
		ConnectionType: "GenericOIDC",
		Name:           "Okta",
		State:          "active",
		CreatedAt:      "2026-09-15T00:00:00Z",
		UpdatedAt:      "2026-09-15T00:00:00Z",
	}, nil).Once()

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "sign_in", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "passed", result.Outcome)

	stored, err = repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "conn-portal-example", stored.WorkosConnectionID.String)
}

func TestVerifySignInStepFailsForDraftWorkOSConnection(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := prepareProvisionedSignInApplication(t, fake)
	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	expectWorkOSConnectionRead(t, ti, stored.WorkosID.String, "draft")

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "sign_in", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "refused", result.Outcome)
	require.Equal(t, "WorkOS reported the sign-in connection state as draft.", result.Detail)
	require.True(t, result.Evidence.Reads[0].OK)
	require.False(t, result.Evidence.Reads[1].OK)

	stored, err = repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "failed", stored.SignInState.String)
	require.Equal(t, "conn-example", stored.WorkosConnectionID.String)
}

func TestVerifySignInStepWaitsForFirstSignInWhileWorkOSIsValidating(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := prepareProvisionedSignInApplication(t, fake)
	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	expectWorkOSConnectionRead(t, ti, stored.WorkosID.String, "validating")

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "sign_in", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "pending_validation", result.Outcome)
	require.Equal(t, "Speakeasy has configured sign-in. It becomes active after the first successful sign-in through Okta; run a test sign-in from the sign-in provider or sign in to Speakeasy with Okta, then check again.", result.Detail)
	require.True(t, result.Evidence.Reads[0].OK)
	require.False(t, result.Evidence.Reads[1].OK)
	require.Equal(t, "WorkOS has the sign-in configuration and is waiting for the first successful sign-in.", *result.Evidence.Reads[1].Detail)

	stored, err = repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "application_created", stored.SignInState.String)
}

func TestVerifySignInStepFailsWhenWorkOSConnectionIsMissing(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := prepareProvisionedSignInApplication(t, fake)
	ti.workos.On("GetConnection", mock.Anything, "conn-example").Return(workos.Connection{}, &workos.APIError{
		Method:     http.MethodGet,
		Path:       "/connections/conn-example",
		StatusCode: http.StatusNotFound,
		Body:       `{"message":"connection not found"}`,
	}).Once()

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "sign_in", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "mismatched_value", result.Outcome)
	require.Equal(t, "The WorkOS sign-in connection no longer exists.", result.Detail)
	require.True(t, result.Evidence.Reads[0].OK)
	require.False(t, result.Evidence.Reads[1].OK)
}

func TestVerifySignInStepFailsForInactiveOktaApplication(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := prepareProvisionedSignInApplication(t, fake)
	fake.SetMode(fakeOktaAppInactive)
	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	expectWorkOSConnectionRead(t, ti, stored.WorkosID.String, "active")

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "sign_in", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "mismatched_value", result.Outcome)
	require.Equal(t, "The Okta sign-in application is not active.", result.Detail)
	require.False(t, result.Evidence.Reads[0].OK)
	require.True(t, result.Evidence.Reads[1].OK)
}

func TestVerifySignInStepReportsOktaForbidden(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := prepareProvisionedSignInApplication(t, fake)
	fake.SetMode(fakeOktaAppForbidden)
	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	expectWorkOSConnectionRead(t, ti, stored.WorkosID.String, "active")

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "sign_in", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "capability_missing", result.Outcome)
	require.Equal(t, "Okta did not permit reading the sign-in application.", result.Detail)
	require.False(t, result.Evidence.Reads[0].OK)
}

func TestVerifySignInStepReportsOktaRefusal(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := prepareProvisionedSignInApplication(t, fake)
	fake.SetMode(fakeOktaAppRefused)
	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	expectWorkOSConnectionRead(t, ti, stored.WorkosID.String, "active")

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "sign_in", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "refused", result.Outcome)
	require.Equal(t, "Okta refused the sign-in application read.", result.Detail)
}

func TestVerifySignInStepReportsOktaUnreachable(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := prepareProvisionedSignInApplication(t, fake)
	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	expectWorkOSConnectionRead(t, ti, stored.WorkosID.String, "active")
	fake.server.Close()

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "sign_in", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "unreachable", result.Outcome)
	require.Equal(t, "Unable to reach Okta while reading the sign-in application.", result.Detail)
}

func TestVerifySignInStepReportsWorkOSRefusal(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := prepareProvisionedSignInApplication(t, fake)
	ti.workos.On("GetConnection", mock.Anything, "conn-example").Return(workos.Connection{}, &workos.APIError{
		Method:     http.MethodGet,
		Path:       "/connections/conn-example",
		StatusCode: http.StatusForbidden,
		Body:       "example refusal",
	}).Once()

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "sign_in", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "refused", result.Outcome)
	require.Equal(t, "WorkOS refused the sign-in connection read.", result.Detail)
}

func TestVerifySignInStepReportsWorkOSUnreachable(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := prepareProvisionedSignInApplication(t, fake)
	ti.workos.On("GetConnection", mock.Anything, "conn-example").Return(workos.Connection{}, errors.New("example network failure")).Once()

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "sign_in", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "unreachable", result.Outcome)
	require.Equal(t, "Unable to reach WorkOS while reading the sign-in connection.", result.Detail)
}

func newFakeOktaServer(t *testing.T, mode string) *fakeOktaServer {
	t.Helper()
	fake := &fakeOktaServer{
		server:               nil,
		mode:                 mode,
		mu:                   sync.Mutex{},
		publicJWK:            jose.JSONWebKey{},
		validationErr:        nil,
		createdApp:           nil,
		createdAppCount:      0,
		applicationReads:     0,
		resolvedClientID:     "",
		assignedAppID:        "",
		assignedGroupID:      "",
		createdClaim:         nil,
		createdClaimCount:    0,
		inventoryApps:        nil,
		assignmentCounts:     make(map[string][2]int),
		indirectUserCounts:   make(map[string]int),
		duplicateUserCounts:  make(map[string]int),
		assignmentFailures:   make(map[string]bool),
		assignmentStalls:     make(map[string]bool),
		assignmentCanceled:   make(chan string, 1),
		assignmentReads:      0,
		embedInventoryGroups: true,
		inventoryGroupReads:  0,
		signInClientID:       testSignInClientID,
		tokenStarted:         nil,
		releaseToken:         nil,
		startOnce:            sync.Once{},
		releaseOnce:          sync.Once{},
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeOktaServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/apps/") && (strings.HasSuffix(r.URL.Path, "/groups") || strings.HasSuffix(r.URL.Path, "/users")) {
		f.handleInventoryAssignments(w, r)
		return
	}
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/groups/") {
		f.handleInventoryGroup(w, r)
		return
	}
	switch r.URL.Path {
	case "/oauth2/v1/token":
		f.handleToken(w, r)
	case "/api/v1/groups":
		if r.URL.Query().Get("limit") == "200" {
			f.handleEveryoneGroup(w, r)
		} else {
			f.handleCollection(w, r, "groups")
		}
	case "/api/v1/users":
		f.handleCollection(w, r, "users")
	case "/api/v1/apps":
		f.handleApplications(w, r)
	case "/api/v1/apps/" + testSignInAppID:
		f.handleSignInApplication(w, r)
	case "/api/v1/apps/" + testSignInAppID + "/groups/" + testEveryoneGroupID:
		f.handleApplicationAssignment(w, r)
	case "/api/v1/authorizationServers":
		f.handleAuthorizationServers(w, r)
	case "/api/v1/authorizationServers/" + testAuthorizationServerID + "/claims":
		f.handleGroupsClaim(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeOktaServer) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		f.fail(w, err)
		return
	}
	requestedScopes := r.Form.Get("scope")
	allScopes := "okta.apps.read okta.groups.read okta.users.read okta.apps.manage okta.authorizationServers.read okta.authorizationServers.manage"
	claimVerificationScopes := "okta.apps.read okta.groups.read okta.users.read okta.authorizationServers.read okta.authorizationServers.manage"
	applicationScopes := "okta.apps.read okta.groups.read okta.users.read okta.apps.manage"
	readScopes := "okta.apps.read okta.groups.read okta.users.read"
	claimScopes := "okta.authorizationServers.read okta.authorizationServers.manage"
	mode := f.Mode()
	validScopes := requestedScopes == allScopes || requestedScopes == claimVerificationScopes || requestedScopes == applicationScopes || requestedScopes == readScopes || requestedScopes == claimScopes || requestedScopes == "okta.apps.read" || requestedScopes == "okta.apps.read okta.groups.read"
	if r.Method != http.MethodPost || r.Form.Get("grant_type") != "client_credentials" || !validScopes || r.Form.Get("client_assertion_type") != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" {
		f.fail(w, errors.New("unexpected token request"))
		return
	}

	f.mu.Lock()
	publicJWK := f.publicJWK
	f.mu.Unlock()
	token, err := jwt.ParseSigned(r.Form.Get("client_assertion"), []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		f.fail(w, err)
		return
	}
	var claims jwt.Claims
	if err := token.Claims(publicJWK.Key, &claims); err != nil {
		f.fail(w, err)
		return
	}
	if len(token.Headers) != 1 || token.Headers[0].KeyID != publicJWK.KeyID || claims.Issuer != testClientID || claims.Subject != testClientID || len(claims.Audience) != 1 || claims.Audience[0] != f.server.URL+"/oauth2/v1/token" || claims.ID == "" || claims.Expiry == nil {
		f.fail(w, errors.New("unexpected client assertion"))
		return
	}
	if f.tokenStarted != nil {
		f.startOnce.Do(func() { close(f.tokenStarted) })
		<-f.releaseToken
	}

	if mode == fakeOktaRefused {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"The client assertion could not be verified."}`))
		return
	}
	if mode == fakeOktaUnavailable {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"server_error","error_description":"The token service is temporarily unavailable."}`))
		return
	}
	if mode == fakeOktaManageDenied && requestedScopes != readScopes {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_scope","error_description":"The requested scope is not granted"}`))
		return
	}
	grantedScopes := requestedScopes
	if mode == fakeOktaManageOmitted {
		grantedScopes = readScopes
	} else if mode == fakeOktaClaimsOmitted && requestedScopes == allScopes {
		grantedScopes = applicationScopes
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"access_token":"` + testAccessToken + `","expires_in":3600,"scope":"` + grantedScopes + `"}`))
}

func (f *fakeOktaServer) handleAuthorizationServers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+testAccessToken {
		f.fail(w, errors.New("unexpected authorization servers request"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if f.Mode() == fakeOktaDefaultMissing {
		_, _ = w.Write([]byte(`[{"id":"other-server","name":"other"}]`))
		return
	}
	_, _ = w.Write([]byte(`[{"id":"` + testAuthorizationServerID + `","name":"default"}]`))
}

func (f *fakeOktaServer) handleGroupsClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+testAccessToken || r.Header.Get("Content-Type") != "application/json" {
		f.fail(w, errors.New("unexpected groups claim request"))
		return
	}
	var claim map[string]any
	if err := json.NewDecoder(r.Body).Decode(&claim); err != nil {
		f.fail(w, err)
		return
	}
	f.mu.Lock()
	f.createdClaim = claim
	f.createdClaimCount++
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"id":"claim-groups"}`))
}

func (f *fakeOktaServer) handleApplications(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if r.Header.Get("Authorization") != "Bearer "+testAccessToken || r.Header.Get("Content-Type") != "application/json" {
			f.fail(w, errors.New("unexpected create application request"))
			return
		}
		var created map[string]any
		if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
			f.fail(w, err)
			return
		}
		f.mu.Lock()
		f.createdApp = created
		f.createdAppCount++
		clientID := f.signInClientID
		f.mu.Unlock()
		f.writeApplication(w, "ACTIVE", clientID)
		return
	}
	if r.Method == http.MethodGet && r.URL.Query().Get("limit") == "200" {
		if r.Header.Get("Authorization") != "Bearer "+testAccessToken {
			f.fail(w, errors.New("unexpected resolve application authorization"))
			return
		}
		f.mu.Lock()
		inventoryApps := append([]map[string]any(nil), f.inventoryApps...)
		configured := f.inventoryApps != nil
		f.mu.Unlock()
		if configured {
			split := (len(inventoryApps) + 1) / 2
			page := inventoryApps[:split]
			if r.URL.Query().Get("after") == "inventory-page-2" {
				page = inventoryApps[split:]
			} else if split < len(inventoryApps) {
				w.Header().Set("Link", `<`+f.server.URL+`/api/v1/apps?after=inventory-page-2&limit=200>; rel="next"`)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(page)
			return
		}
		filter := r.URL.Query().Get("filter")
		if filter == "" {
			w.Header().Set("Content-Type", "application/json")
			if f.Mode() == fakeOktaExistingApp {
				_, _ = w.Write([]byte(`[{"id":"` + testSignInAppID + `","status":"ACTIVE","label":"Speakeasy sign-in","credentials":{"oauthClient":{"client_id":"` + f.SignInClientID() + `","client_secret":"` + testFakeClientSecret + `"}}}]`))
				return
			}
			_, _ = w.Write([]byte(`[]`))
			return
		}
		const prefix = `credentials.oauthClient.client_id eq "`
		clientID, hasPrefix := strings.CutPrefix(filter, prefix)
		clientID, hasSuffix := strings.CutSuffix(clientID, `"`)
		if !hasPrefix || !hasSuffix || clientID == "" {
			f.fail(w, errors.New("unexpected resolve application filter"))
			return
		}
		f.mu.Lock()
		f.resolvedClientID = clientID
		expectedClientID := f.signInClientID
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if clientID != expectedClientID {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte(`[{"id":"` + testSignInAppID + `","status":"ACTIVE","label":"Speakeasy sign-in","credentials":{"oauthClient":{"client_id":"` + expectedClientID + `","client_secret":"` + testFakeClientSecret + `"}}}]`))
		return
	}
	f.handleCollection(w, r, "apps")
}

func (f *fakeOktaServer) handleInventoryAssignments(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/apps/")
	applicationID, resource, ok := strings.Cut(path, "/")
	if !ok || applicationID == "" || (resource != "groups" && resource != "users") || r.Header.Get("Authorization") != "Bearer "+testAccessToken {
		f.fail(w, errors.New("unexpected application assignments request"))
		return
	}
	limit := 200
	if resource == "groups" && r.URL.Query().Get("after") == "" {
		limit = 20
	}
	if r.URL.Query().Get("limit") != fmt.Sprintf("%d", limit) || (resource == "groups" && r.URL.Query().Get("expand") != "group") {
		f.fail(w, errors.New("unexpected application assignments query"))
		return
	}
	f.mu.Lock()
	f.assignmentReads++
	counts := f.assignmentCounts[applicationID]
	indirectUserCount := f.indirectUserCounts[applicationID]
	duplicateUserCount := f.duplicateUserCounts[applicationID]
	failure := f.assignmentFailures[applicationID+"/"+resource]
	stall := f.assignmentStalls[applicationID+"/"+resource]
	embedInventoryGroups := f.embedInventoryGroups
	f.mu.Unlock()
	if failure {
		http.Error(w, "assignment read failed", http.StatusServiceUnavailable)
		return
	}
	if stall {
		<-r.Context().Done()
		f.assignmentCanceled <- applicationID + "/" + resource
		return
	}
	count := counts[0]
	if resource == "users" {
		count = counts[1]
	}
	total := count
	if resource == "users" {
		total += indirectUserCount + duplicateUserCount
	}
	start := 0
	if after := r.URL.Query().Get("after"); after != "" {
		if after != "assignment-page-2" {
			f.fail(w, errors.New("unexpected application assignments cursor"))
			return
		}
		start = 200
		if resource == "groups" {
			start = 20
		}
	}
	end := min(start+limit, total)
	items := make([]map[string]any, end-start)
	for i := start; i < end; i++ {
		itemIndex := i
		if resource == "users" && i >= count+indirectUserCount && count+indirectUserCount > 0 {
			itemIndex = (i - count - indirectUserCount) % (count + indirectUserCount)
		}
		id := fmt.Sprintf("%s-%s-%d", applicationID, resource, itemIndex)
		item := map[string]any{"id": id}
		if resource == "users" {
			item["scope"] = "USER"
			if i >= count {
				item["scope"] = "GROUP"
			}
		} else if embedInventoryGroups {
			item["_embedded"] = map[string]any{
				"group": map[string]any{
					"id":      id,
					"profile": map[string]any{"name": inventoryGroupName(id)},
				},
			}
		}
		items[i-start] = item
	}
	if end < total {
		w.Header().Set("Link", `<`+f.server.URL+r.URL.Path+`?after=assignment-page-2&limit=200>; rel="next"`)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func (f *fakeOktaServer) handleInventoryGroup(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimPrefix(r.URL.Path, "/api/v1/groups/")
	if groupID == "" || r.Header.Get("Authorization") != "Bearer "+testAccessToken {
		f.fail(w, errors.New("unexpected group request"))
		return
	}
	f.mu.Lock()
	f.inventoryGroupReads++
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":      groupID,
		"profile": map[string]any{"name": inventoryGroupName(groupID)},
	})
}

func inventoryGroupName(groupID string) string {
	return "Group " + groupID
}

func (f *fakeOktaServer) handleEveryoneGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+testAccessToken || r.URL.Query().Get("filter") != `type eq "BUILT_IN"` {
		f.fail(w, errors.New("unexpected Everyone group request"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`[{"id":"` + testEveryoneGroupID + `","type":"BUILT_IN","profile":{"name":"Everyone"}}]`))
}

func (f *fakeOktaServer) handleSignInApplication(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+testAccessToken {
		f.fail(w, errors.New("unexpected sign-in application request"))
		return
	}
	f.mu.Lock()
	f.applicationReads++
	f.mu.Unlock()
	switch f.Mode() {
	case fakeOktaAppForbidden:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorCode":"E0000006","errorSummary":"Application read is not permitted."}`))
		return
	case fakeOktaAppRefused:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorCode":"E0000001","errorSummary":"Application read was refused."}`))
		return
	case fakeOktaAppInactive:
		f.writeApplication(w, "INACTIVE", f.SignInClientID())
		return
	default:
		f.writeApplication(w, "ACTIVE", f.SignInClientID())
	}
}

func (f *fakeOktaServer) handleApplicationAssignment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut || r.Header.Get("Authorization") != "Bearer "+testAccessToken || r.Header.Get("Content-Type") != "application/json" {
		f.fail(w, errors.New("unexpected application assignment request"))
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		f.fail(w, err)
		return
	}
	if len(body) != 0 {
		f.fail(w, errors.New("unexpected application assignment body"))
		return
	}
	if f.Mode() == fakeOktaAssignFails {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorCode":"E0000006","errorSummary":"Application assignment is not permitted."}`))
		return
	}
	f.mu.Lock()
	f.assignedAppID = testSignInAppID
	f.assignedGroupID = testEveryoneGroupID
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{}`))
}

func (f *fakeOktaServer) writeApplication(w http.ResponseWriter, status, clientID string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"id":"` + testSignInAppID + `","status":"` + status + `","label":"Speakeasy sign-in","credentials":{"oauthClient":{"client_id":"` + clientID + `","client_secret":"` + testFakeClientSecret + `"}}}`))
}

func (f *fakeOktaServer) handleCollection(w http.ResponseWriter, r *http.Request, resource string) {
	if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+testAccessToken || r.URL.Query().Get("limit") != "1" {
		f.fail(w, errors.New("unexpected collection request"))
		return
	}
	if resource == "apps" && f.Mode() == fakeOktaAppsBlocked {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorCode":"E0000006","errorSummary":"Okta application read is not permitted."}`))
		return
	}
	if resource == "groups" {
		w.Header().Set("Link", `<`+f.server.URL+`/api/v1/groups?after=next-cursor&limit=1>; rel="next"`)
	}
	w.Header().Set("X-Rate-Limit-Remaining", "99")
	w.Header().Set("X-Rate-Limit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Minute).Unix()))
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`[{"id":"` + resource + `-1"}]`))
}

func (f *fakeOktaServer) fail(w http.ResponseWriter, err error) {
	f.mu.Lock()
	if f.validationErr == nil {
		f.validationErr = err
	}
	f.mu.Unlock()
	http.Error(w, "invalid request", http.StatusBadRequest)
}

func (f *fakeOktaServer) SetPublicJWK(raw []byte) {
	var publicJWK jose.JSONWebKey
	if err := json.Unmarshal(raw, &publicJWK); err != nil {
		f.mu.Lock()
		f.validationErr = err
		f.mu.Unlock()
		return
	}
	f.mu.Lock()
	f.publicJWK = publicJWK
	f.mu.Unlock()
}

func (f *fakeOktaServer) ValidationError() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.validationErr
}

func (f *fakeOktaServer) Mode() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mode
}

func (f *fakeOktaServer) SetMode(mode string) {
	f.mu.Lock()
	f.mode = mode
	f.mu.Unlock()
}

func (f *fakeOktaServer) SetSignInClientID(clientID string) {
	f.mu.Lock()
	f.signInClientID = clientID
	f.mu.Unlock()
}

func (f *fakeOktaServer) SignInClientID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.signInClientID
}

func (f *fakeOktaServer) CreatedApplication() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.createdApp)
}

func (f *fakeOktaServer) ApplicationCounts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createdAppCount, f.applicationReads
}

func (f *fakeOktaServer) ResolvedClientID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resolvedClientID
}

func (f *fakeOktaServer) Assignment() (string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.assignedAppID, f.assignedGroupID
}

func (f *fakeOktaServer) CreatedClaim() (map[string]any, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.createdClaim), f.createdClaimCount
}

func (f *fakeOktaServer) SetApplicationInventory(applications []map[string]any, counts map[string][2]int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inventoryApps = append([]map[string]any(nil), applications...)
	f.assignmentCounts = maps.Clone(counts)
}

func (f *fakeOktaServer) SetIndirectUserAssignments(applicationID string, count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.indirectUserCounts[applicationID] = count
}

func (f *fakeOktaServer) SetDuplicateUserAssignments(applicationID string, count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.duplicateUserCounts[applicationID] = count
}

func (f *fakeOktaServer) SetEmbeddedInventoryGroups(enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.embedInventoryGroups = enabled
}

func (f *fakeOktaServer) SetAssignmentFailure(applicationID, resource string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assignmentFailures[applicationID+"/"+resource] = true
}

func (f *fakeOktaServer) SetAssignmentStall(applicationID, resource string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assignmentStalls[applicationID+"/"+resource] = true
}

func (f *fakeOktaServer) AssignmentReads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.assignmentReads
}

func (f *fakeOktaServer) InventoryGroupReads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inventoryGroupReads
}

func (f *fakeOktaServer) BlockTokenResponse() {
	f.tokenStarted = make(chan struct{})
	f.releaseToken = make(chan struct{})
}

func (f *fakeOktaServer) ReleaseTokenResponse() {
	if f.releaseToken != nil {
		f.releaseOnce.Do(func() { close(f.releaseToken) })
	}
}

func prepareConnectionForVerification(t *testing.T, ctx context.Context, ti *testInstance, fake *fakeOktaServer) *gen.IdentityProviderConnection {
	t.Helper()
	connection := createConnection(t, ctx, ti, "https://example.okta.com")
	storedKey, err := repo.New(ti.conn).GetIdentityProviderSigningKey(ctx, repo.GetIdentityProviderSigningKeyParams{
		OrganizationID:               ti.orgID,
		IdentityProviderConnectionID: mustUUID(t, connection.ID),
	})
	require.NoError(t, err)
	fake.SetPublicJWK(storedKey.PublicJwk)
	setStoredClientID(t, ctx, ti)
	return connection
}

func prepareActiveConnection(t *testing.T, ctx context.Context, ti *testInstance, fake *fakeOktaServer) *gen.IdentityProviderConnection {
	t.Helper()
	connection := prepareConnectionForVerification(t, ctx, ti, fake)
	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "passed", result.Outcome)
	return connection
}

func prepareProvisionedSignInApplication(t *testing.T, fake *fakeOktaServer) (context.Context, *testInstance) {
	t.Helper()
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	expectDirectWorkOSConnection(t, ti, testSignInClientID, fake.Mode() != fakeOktaClaimsOmitted)
	_, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "sign_in",
		Values:       []*gen.IdentityProviderSetupValue{},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	return ctx, ti
}

func setStoredClientID(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()
	_, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "connect",
		Values:       []*gen.IdentityProviderSetupValue{{Key: "client_id", Value: testClientID}},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
}
