package identityproviders_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

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

func TestSubmitSetupStepSavesTrimmedClientIDAndAwaitsVerification(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connection := createConnection(t, ctx, ti, "https://acme.okta.com")
	beforeAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionUpdated)
	require.NoError(t, err)

	result, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey: "connect",
		Values: []*gen.IdentityProviderSetupValue{{
			Key:   "client_id",
			Value: "  client-123  ",
		}},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Step)
	require.Equal(t, "connect", result.Step.Key)
	require.Equal(t, "awaiting_verification", result.Step.State)
	require.Equal(t, "client-123", *result.Step.ExpectedValues[0].CurrentValue)
	require.Equal(t, []*gen.IdentityProviderFieldOutcome{{
		Key:     "client_id",
		Outcome: "accepted",
		Detail:  "Client ID saved.",
	}}, result.FieldOutcomes)
	require.Nil(t, result.NextStepKey)

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, mustUUID(t, connection.ID), stored.ID)
	require.True(t, stored.ClientID.Valid)
	require.Equal(t, "client-123", stored.ClientID.String)
	require.Equal(t, "awaiting_verification", stored.Status)
	reloaded, err := ti.service.Get(ctx, &gen.GetPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "client-123", *reloaded.Connection.ClientID)
	setup, err := ti.service.DescribeSetup(ctx, &gen.DescribeSetupPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "client-123", *setup.Steps[0].ExpectedValues[0].CurrentValue)

	afterAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionUpdated)
	require.NoError(t, err)
	require.Equal(t, beforeAudits+1, afterAudits)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionUpdated)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "pending", beforeSnapshot["status"])
	require.Equal(t, "awaiting_verification", afterSnapshot["status"])
	require.NotContains(t, beforeSnapshot, "client_id")
	require.NotContains(t, afterSnapshot, "client_id")
}

func TestSubmitSetupStepRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "connect",
		Values:       []*gen.IdentityProviderSetupValue{{Key: "client_id", Value: "client-123"}},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSubmitSetupStepRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	createConnection(t, ctx, ti, "https://acme.okta.com")
	payloads := []*gen.SubmitSetupStepPayload{
		{StepKey: "unknown", Values: []*gen.IdentityProviderSetupValue{{Key: "client_id", Value: "client-123"}}, SessionToken: nil, ApikeyToken: nil},
		{StepKey: "connect", Values: []*gen.IdentityProviderSetupValue{}, SessionToken: nil, ApikeyToken: nil},
		{StepKey: "connect", Values: []*gen.IdentityProviderSetupValue{nil}, SessionToken: nil, ApikeyToken: nil},
		{StepKey: "connect", Values: []*gen.IdentityProviderSetupValue{{Key: "other", Value: "client-123"}}, SessionToken: nil, ApikeyToken: nil},
		{StepKey: "connect", Values: []*gen.IdentityProviderSetupValue{{Key: "client_id", Value: "client-123"}, {Key: "client_id", Value: "client-456"}}, SessionToken: nil, ApikeyToken: nil},
		{StepKey: "connect", Values: []*gen.IdentityProviderSetupValue{{Key: "client_id", Value: " \t "}}, SessionToken: nil, ApikeyToken: nil},
	}

	for _, payload := range payloads {
		_, err := ti.service.SubmitSetupStep(ctx, payload)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}

func TestSubmitSetupStepReturnsNotFoundWhenAbsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "connect",
		Values:       []*gen.IdentityProviderSetupValue{{Key: "client_id", Value: "client-123"}},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestSubmitSignInStepProvisionsExactOktaApplicationAndAssignsEveryone(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	workOSSecret := expectDirectWorkOSConnection(t, ti, testSignInClientID, true)
	beforeAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionUpdated)
	require.NoError(t, err)

	result, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "sign_in",
		Values:       []*gen.IdentityProviderSetupValue{},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, configuredSignInStep(testSignInClientID, false, true), result.Step)
	require.Empty(t, result.FieldOutcomes)
	require.Nil(t, result.NextStepKey)
	createdApplication := fake.CreatedApplication()
	credentials, ok := createdApplication["credentials"].(map[string]any)
	require.True(t, ok)
	oauthClient, ok := credentials["oauthClient"].(map[string]any)
	require.True(t, ok)
	clientSecret, ok := oauthClient["client_secret"].(string)
	require.True(t, ok)
	decodedSecret, err := base64.RawURLEncoding.DecodeString(clientSecret)
	require.NoError(t, err)
	require.Len(t, decodedSecret, 32)
	require.Equal(t, clientSecret, *workOSSecret)
	require.Equal(t, map[string]any{
		"name":       "oidc_client",
		"label":      "Speakeasy sign-in",
		"signOnMode": "OPENID_CONNECT",
		"credentials": map[string]any{
			"oauthClient": map[string]any{
				"client_secret":              clientSecret,
				"token_endpoint_auth_method": "client_secret_post",
				"autoKeyRotation":            true,
				"pkce_required":              true,
			},
		},
		"settings": map[string]any{
			"oauthClient": map[string]any{
				"application_type": "web",
				"grant_types":      []any{"authorization_code", "refresh_token"},
				"response_types":   []any{"code"},
				"redirect_uris":    []any{},
				"consent_method":   "TRUSTED",
			},
		},
	}, createdApplication)
	updatedApplication, updateCount := fake.UpdatedApplication()
	require.Equal(t, 1, updateCount)
	settings, ok := updatedApplication["settings"].(map[string]any)
	require.True(t, ok)
	updatedOAuthClient, ok := settings["oauthClient"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, []any{testWorkOSRedirectURI}, updatedOAuthClient["redirect_uris"])
	assignedAppID, assignedGroupID := fake.Assignment()
	require.Equal(t, testSignInAppID, assignedAppID)
	require.Equal(t, testEveryoneGroupID, assignedGroupID)
	claims, claimCount := fake.CreatedClaims()
	require.Equal(t, 3, claimCount)
	require.Equal(t, []map[string]any{{
		"alwaysIncludeInToken": true,
		"claimType":            "IDENTITY",
		"conditions":           map[string]any{"scopes": []any{}},
		"group_filter_type":    "REGEX",
		"name":                 "groups",
		"status":               "ACTIVE",
		"value":                ".*",
		"valueType":            "GROUPS",
	}, {
		"alwaysIncludeInToken": true,
		"claimType":            "IDENTITY",
		"conditions":           map[string]any{"scopes": []any{}},
		"name":                 "given_name",
		"status":               "ACTIVE",
		"value":                "user.firstName",
		"valueType":            "EXPRESSION",
	}, {
		"alwaysIncludeInToken": true,
		"claimType":            "IDENTITY",
		"conditions":           map[string]any{"scopes": []any{}},
		"name":                 "family_name",
		"status":               "ACTIVE",
		"value":                "user.lastName",
		"valueType":            "EXPRESSION",
	}}, claims)
	require.NoError(t, fake.ValidationError())

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, testClientID, stored.ClientID.String)
	require.Equal(t, testSignInAppID, stored.SignInApplicationID.String)
	require.Equal(t, "conn-example", stored.WorkosConnectionID.String)
	require.Equal(t, "application_created", stored.SignInState.String)
	require.True(t, stored.GroupsClaimConfirmed)
	require.Equal(t, "token", stored.GroupsSource.String)
	var evidence map[string]any
	require.NoError(t, json.Unmarshal(stored.SignInEvidence, &evidence))
	require.Equal(t, map[string]any{"client_id": testSignInClientID, "groups_claim_provisioned": true, "redirect_uri": testWorkOSRedirectURI}, evidence)
	require.NotContains(t, string(stored.SignInEvidence), testFakeClientSecret)

	_, err = ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{StepKey: "sign_in", Values: []*gen.IdentityProviderSetupValue{}, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	_, claimCount = fake.CreatedClaims()
	require.Equal(t, 3, claimCount)

	afterAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionUpdated)
	require.NoError(t, err)
	require.Equal(t, beforeAudits+3, afterAudits)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionUpdated)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "application_created", afterSnapshot["sign_in_state"])
	auditJSON := string(record.Metadata) + string(record.BeforeSnapshot) + string(record.AfterSnapshot)
	for _, sensitive := range []string{testAccessToken, testClientID, testSignInClientID, testSignInAppID, testFakeClientSecret, clientSecret} {
		require.NotContains(t, auditJSON, sensitive)
	}
	for _, expected := range result.Step.ExpectedValues {
		require.False(t, expected.Secret)
		require.NotEqual(t, "client_secret", expected.Key)
	}
}

func TestSubmitSignInStepFallsBackToPublicClientIDResolution(t *testing.T) {
	t.Parallel()

	const manualClientID = "manual-public-client-example"
	fake := newFakeOktaServer(t, fakeOktaManageDenied)
	fake.SetSignInClientID(manualClientID)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	workOSSecret := expectDirectWorkOSConnection(t, ti, manualClientID, false)

	setup, err := ti.service.DescribeSetup(ctx, &gen.DescribeSetupPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "their_console", setup.Steps[1].Where)
	require.Equal(t, []*gen.IdentityProviderExpectedValue{
		{Key: "client_id", Label: "Client ID", Secret: false, CurrentValue: nil},
		{Key: "client_secret", Label: "Client secret", Secret: true, CurrentValue: nil},
	}, setup.Steps[1].ExpectedValues)

	invalidValues := [][]*gen.IdentityProviderSetupValue{
		{},
		{{Key: "other", Value: "not-accepted"}},
		{{Key: "client_id", Value: manualClientID}, {Key: "client_id", Value: "second-public-client-example"}},
		{{Key: "client_id", Value: "  "}, {Key: "client_secret", Value: "manual-secret"}},
		{{Key: "client_id", Value: manualClientID}, {Key: "client_secret", Value: "  "}},
	}
	for _, values := range invalidValues {
		_, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{StepKey: "sign_in", Values: values, SessionToken: nil, ApikeyToken: nil})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	result, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey: "sign_in",
		Values: []*gen.IdentityProviderSetupValue{
			{Key: "client_id", Value: "  " + manualClientID + "  "},
			{Key: "client_secret", Value: "manual-secret"},
		},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, manualClientID, fake.ResolvedClientID())
	require.Nil(t, fake.CreatedApplication())
	assignedAppID, assignedGroupID := fake.Assignment()
	require.Empty(t, assignedAppID)
	require.Empty(t, assignedGroupID)
	require.Equal(t, "manual-secret", *workOSSecret)
	require.Equal(t, configuredSignInStep(manualClientID, false, false), result.Step)
	require.Equal(t, []*gen.IdentityProviderFieldOutcome{
		{Key: "client_id", Outcome: "accepted", Detail: "Okta sign-in Client ID saved."},
		{Key: "client_secret", Outcome: "accepted", Detail: "Client secret sent to WorkOS and not retained."},
	}, result.FieldOutcomes)
	require.NoError(t, fake.ValidationError())

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, testSignInAppID, stored.SignInApplicationID.String)
	require.Equal(t, "conn-example", stored.WorkosConnectionID.String)
	require.Equal(t, "application_created", stored.SignInState.String)
	var evidence map[string]any
	require.NoError(t, json.Unmarshal(stored.SignInEvidence, &evidence))
	require.Equal(t, map[string]any{"client_id": manualClientID, "redirect_uri": testWorkOSRedirectURI}, evidence)
	require.NotContains(t, string(stored.SignInEvidence), testFakeClientSecret)

	retry, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey: "sign_in",
		Values: []*gen.IdentityProviderSetupValue{
			{Key: "client_id", Value: manualClientID},
			{Key: "client_secret", Value: "manual-secret"},
		},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, manualClientID, retry.Step.PrintedValues[0].Value)
	_, err = ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{StepKey: "sign_in", Values: []*gen.IdentityProviderSetupValue{}, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	assignedAppID, assignedGroupID = fake.Assignment()
	require.Empty(t, assignedAppID)
	require.Empty(t, assignedGroupID)
}

func TestSubmitSignInStepFallsBackToWorkOSAdminPortalForCapability404(t *testing.T) {
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

	result, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "sign_in",
		Values:       []*gen.IdentityProviderSetupValue{},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, configuredSignInStep(testSignInClientID, true, true), result.Step)
	require.Equal(t, new("sso"), result.Step.PortalIntent)

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, testSignInAppID, stored.SignInApplicationID.String)
	require.False(t, stored.WorkosConnectionID.Valid)
}

func TestSubmitSignInStepMapsWorkOSBadRequestAndKeepsPersistedApplication(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	ti.workos.On("CreateOIDCConnection", mock.Anything, mock.Anything).Return(workos.Connection{}, &workos.APIError{
		Method:     http.MethodPost,
		Path:       "/connections",
		StatusCode: http.StatusBadRequest,
		Body:       `{"code":"custom_attribute_not_found","message":"No matching custom attribute exists."}`,
	}).Once()

	result, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{StepKey: "sign_in", Values: []*gen.IdentityProviderSetupValue{}, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, []*gen.IdentityProviderFieldOutcome{{
		Key:     "workos_connection",
		Outcome: "rejected",
		Detail:  "custom_attribute_not_found: No matching custom attribute exists.",
	}}, result.FieldOutcomes)
	require.Equal(t, new("sso"), result.Step.PortalIntent)
	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, testSignInAppID, stored.SignInApplicationID.String)
	require.Equal(t, "application_created", stored.SignInState.String)
	require.False(t, stored.WorkosConnectionID.Valid)
}

func TestSubmitSignInStepRetriesWorkOSForPersistedManualApplication(t *testing.T) {
	t.Parallel()

	const manualClientID = "manual-public-client-example"
	fake := newFakeOktaServer(t, fakeOktaManageDenied)
	fake.SetSignInClientID(manualClientID)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	ti.workos.On("CreateOIDCConnection", mock.Anything, mock.Anything).Return(workos.Connection{}, &workos.APIError{
		Method:     http.MethodPost,
		Path:       "/connections",
		StatusCode: http.StatusBadRequest,
		Body:       `{"code":"invalid_client_secret","message":"The client secret was rejected."}`,
	}).Once()

	values := []*gen.IdentityProviderSetupValue{
		{Key: "client_id", Value: manualClientID},
		{Key: "client_secret", Value: "manual-secret"},
	}
	result, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{StepKey: "sign_in", Values: values, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, []*gen.IdentityProviderFieldOutcome{{
		Key:     "client_secret",
		Outcome: "rejected",
		Detail:  "invalid_client_secret: The client secret was rejected.",
	}}, result.FieldOutcomes)
	require.Equal(t, []string{"client_id", "client_secret"}, []string{result.Step.ExpectedValues[0].Key, result.Step.ExpectedValues[1].Key})

	workOSSecret := expectDirectWorkOSConnection(t, ti, manualClientID, false)
	retry, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{StepKey: "sign_in", Values: values, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, configuredSignInStep(manualClientID, false, false), retry.Step)
	require.Equal(t, "manual-secret", *workOSSecret)
	created, reads := fake.ApplicationCounts()
	require.Zero(t, created)
	require.Equal(t, 1, reads)
}

func TestSubmitSignInStepPersistsApplicationBeforeAssignmentAndDoesNotRecreateOnRetry(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaAssignFails)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)

	_, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{StepKey: "sign_in", Values: []*gen.IdentityProviderSetupValue{}, SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeGatewayError)
	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, testSignInAppID, stored.SignInApplicationID.String)
	require.Equal(t, "application_created", stored.SignInState.String)
	created, reads := fake.ApplicationCounts()
	require.Equal(t, 1, created)
	require.Zero(t, reads)

	_, err = ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{StepKey: "sign_in", Values: []*gen.IdentityProviderSetupValue{}, SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeGatewayError)
	created, reads = fake.ApplicationCounts()
	require.Equal(t, 1, created)
	require.Equal(t, 1, reads)
}

func TestSubmitSignInStepAdoptsExistingActiveApplication(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaExistingApp)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	workOSSecret := expectDirectWorkOSConnection(t, ti, testSignInClientID, true)

	result, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{StepKey: "sign_in", Values: []*gen.IdentityProviderSetupValue{}, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, configuredSignInStep(testSignInClientID, false, true), result.Step)
	require.Equal(t, testFakeClientSecret, *workOSSecret)
	require.Nil(t, fake.CreatedApplication())
	created, _ := fake.ApplicationCounts()
	require.Zero(t, created)
	_, updateCount := fake.UpdatedApplication()
	require.Equal(t, 1, updateCount)
}

func TestSubmitSignInStepRejectsConflictingGroupAcknowledgements(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaClaimsOmitted)
	ctx, ti := prepareProvisionedSignInApplication(t, fake)
	_, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey: "sign_in",
		Values: []*gen.IdentityProviderSetupValue{
			{Key: "groups_claim_confirmed", Value: "true"},
			{Key: "groups_source", Value: "directory"},
		},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSubmitSignInStepConfirmsTokenGroupsClaim(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaClaimsOmitted)
	ctx, ti := prepareProvisionedSignInApplication(t, fake)

	result, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "sign_in",
		Values:       []*gen.IdentityProviderSetupValue{{Key: "groups_claim_confirmed", Value: "true"}},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, []*gen.IdentityProviderFieldOutcome{{Key: "groups_claim_confirmed", Outcome: "accepted", Detail: "Okta groups claim confirmed."}}, result.FieldOutcomes)
	require.Equal(t, new("true"), result.Step.ExpectedValues[0].CurrentValue)
	require.Equal(t, new("token"), result.Step.ExpectedValues[1].CurrentValue)

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.True(t, stored.GroupsClaimConfirmed)
	require.Equal(t, "token", stored.GroupsSource.String)
}

func TestSubmitSignInStepSelectsDirectoryGroups(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaClaimsOmitted)
	ctx, ti := prepareProvisionedSignInApplication(t, fake)

	result, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "sign_in",
		Values:       []*gen.IdentityProviderSetupValue{{Key: "groups_source", Value: "directory"}},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, []*gen.IdentityProviderFieldOutcome{{Key: "groups_source", Outcome: "accepted", Detail: "Directory groups selected."}}, result.FieldOutcomes)
	require.Equal(t, new("directory"), result.Step.ExpectedValues[1].CurrentValue)

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.False(t, stored.GroupsClaimConfirmed)
	require.Equal(t, "directory", stored.GroupsSource.String)
}

func configuredSignInStep(clientID string, portalFallback, claimsProvisioned bool) *gen.IdentityProviderSetupStep {
	instructions := []string{
		"Speakeasy created the Okta sign-in application and WorkOS OIDC connection.",
		"Configure the groups claim in Okta, then confirm whether sign-in tokens or the directory will supply groups.",
	}
	deepLink := new("https://example-admin.okta.com/admin/app/oidc_client/instance/" + testSignInAppID + "/#tab-sign-on")
	var portalIntent *string
	where := "their_console"
	issuer := "https://example.okta.com"
	expectedValues := []*gen.IdentityProviderExpectedValue{
		{Key: "groups_claim_confirmed", Label: "Groups claim confirmed", Secret: false, CurrentValue: new("false")},
		{Key: "groups_source", Label: "Groups source", Secret: false, CurrentValue: nil},
	}
	var repair *gen.IdentityProviderRepair
	if claimsProvisioned {
		instructions = []string{"Speakeasy created the Okta sign-in application, identity claims, and WorkOS OIDC connection."}
		deepLink = nil
		where = "our_page"
		issuer += "/oauth2/default"
		expectedValues = []*gen.IdentityProviderExpectedValue{}
	} else {
		repair = &gen.IdentityProviderRepair{
			Title: "Repair the groups claim",
			Instructions: []string{
				"Open the Sign On tab, then find OpenID Connect ID Token.",
				"Set Groups claim type to Filter.",
				"Set the claim name to groups and Matches regex to .*.",
			},
			DeepLink:          new("https://example-admin.okta.com/admin/app/oidc_client/instance/" + testSignInAppID + "/#tab-sign-on"),
			FallbackAvailable: true,
		}
	}
	if portalFallback {
		instructions = []string{
			"Open WorkOS Admin Portal and choose the OpenID Connect connection.",
			"Copy the client secret from the Okta application directly into WorkOS Admin Portal. It is not retained by Speakeasy.",
			"Use the Client ID, issuer, and discovery URL shown below, then activate the connection.",
		}
		deepLink = new("https://example-admin.okta.com/admin/app/oidc_client/instance/" + testSignInAppID + "/#tab-general")
		portalIntent = new("sso")
		where = "their_console"
	}
	printedValues := []*gen.IdentityProviderPrintedValue{
		{Label: "Client ID", Value: clientID, Copyable: true},
		{Label: "Issuer", Value: issuer, Copyable: true},
		{Label: "Discovery URL", Value: issuer + "/.well-known/openid-configuration", Copyable: true},
	}
	if !portalFallback {
		printedValues = append(printedValues, &gen.IdentityProviderPrintedValue{Label: "Sign-in redirect URI", Value: testWorkOSRedirectURI, Copyable: true})
	}
	return &gen.IdentityProviderSetupStep{
		Key:            "sign_in",
		Title:          "Configure Okta sign-in",
		Where:          where,
		Instructions:   instructions,
		DeepLink:       deepLink,
		PrintedValues:  printedValues,
		ExpectedValues: expectedValues,
		Claims: []*gen.IdentityProviderClaim{
			{Name: "email", Purpose: "identity", CarriesAccess: false, Provisioned: false},
			{Name: "first_name", Purpose: "display", CarriesAccess: false, Provisioned: claimsProvisioned},
			{Name: "last_name", Purpose: "display", CarriesAccess: false, Provisioned: claimsProvisioned},
			{Name: "groups", Purpose: "used by access rules", CarriesAccess: true, Provisioned: claimsProvisioned},
		},
		Repair:       repair,
		PortalIntent: portalIntent,
		State:        "awaiting_verification",
		LastOutcome:  nil,
	}
}
