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
	fakeOktaPassed        = "passed"
	fakeOktaRefused       = "refused"
	fakeOktaUnavailable   = "unavailable"
	fakeOktaAppsBlocked   = "apps_blocked"
	fakeOktaManageDenied  = "manage_scope_denied"
	fakeOktaManageOmitted = "manage_scope_omitted"
	fakeOktaExistingApp   = "existing_sign_in_app"
	fakeOktaAppInactive   = "sign_in_app_inactive"
	fakeOktaAppForbidden  = "sign_in_app_forbidden"
	fakeOktaAppRefused    = "sign_in_app_refused"
	fakeOktaAssignFails   = "sign_in_assignment_fails"
	testAccessToken       = "test-access-token"
	testClientID          = "test-client-id"
	testSignInAppID       = "app-example"
	testSignInClientID    = "public-client-example"
	testEveryoneGroupID   = "group-everyone"
	testFakeClientSecret  = "FAKE_SECRET_SENTINEL_DO_NOT_USE"
)

type fakeOktaServer struct {
	server *httptest.Server
	mode   string

	mu               sync.Mutex
	publicJWK        jose.JSONWebKey
	validationErr    error
	createdApp       map[string]any
	createdAppCount  int
	applicationReads int
	resolvedClientID string
	assignedAppID    string
	assignedGroupID  string
	signInClientID   string
	tokenStarted     chan struct{}
	releaseToken     chan struct{}
	startOnce        sync.Once
	releaseOnce      sync.Once
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
	require.Equal(t, []string{"directory_read", "application_assignment_read", "sign_in_provisioning"}, result.Capabilities)
	require.Equal(t, []string{"okta.apps.read", "okta.groups.read", "okta.users.read", "okta.apps.manage"}, result.GrantedScopes)
	require.Len(t, result.Evidence.Reads, 3)
	require.True(t, result.Evidence.Reads[0].OK)
	require.Contains(t, *result.Evidence.Reads[0].Detail, "more pages are available")
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
	require.Equal(t, []any{"directory_read", "application_assignment_read", "sign_in_provisioning"}, afterSnapshot["capabilities"])
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
	require.Equal(t, []string{"directory_read", "sign_in_provisioning"}, result.Capabilities)
	require.Len(t, result.Evidence.Reads, 3)
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
		Capabilities:  []string{"directory_read", "application_assignment_read", "sign_in_provisioning"},
		GrantedScopes: []string{"okta.apps.read", "okta.groups.read", "okta.users.read", "okta.apps.manage"},
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
		server:           nil,
		mode:             mode,
		mu:               sync.Mutex{},
		publicJWK:        jose.JSONWebKey{},
		validationErr:    nil,
		createdApp:       nil,
		createdAppCount:  0,
		applicationReads: 0,
		resolvedClientID: "",
		assignedAppID:    "",
		assignedGroupID:  "",
		signInClientID:   testSignInClientID,
		tokenStarted:     nil,
		releaseToken:     nil,
		startOnce:        sync.Once{},
		releaseOnce:      sync.Once{},
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeOktaServer) handle(w http.ResponseWriter, r *http.Request) {
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
	allScopes := "okta.apps.read okta.groups.read okta.users.read okta.apps.manage"
	readScopes := "okta.apps.read okta.groups.read okta.users.read"
	mode := f.Mode()
	validScopes := requestedScopes == allScopes || requestedScopes == readScopes
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
	if mode == fakeOktaManageDenied && requestedScopes == allScopes {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_scope","error_description":"The requested scope is not granted"}`))
		return
	}
	grantedScopes := requestedScopes
	if mode == fakeOktaManageOmitted {
		grantedScopes = readScopes
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"access_token":"` + testAccessToken + `","expires_in":3600,"scope":"` + grantedScopes + `"}`))
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
	expectDirectWorkOSConnection(t, ti, testSignInClientID)
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
