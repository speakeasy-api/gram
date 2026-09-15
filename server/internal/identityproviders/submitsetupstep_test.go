package identityproviders_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
	beforeAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionUpdated)
	require.NoError(t, err)

	result, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "sign_in",
		Values:       []*gen.IdentityProviderSetupValue{},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, configuredSignInStep(testSignInClientID), result.Step)
	require.Empty(t, result.FieldOutcomes)
	require.Nil(t, result.NextStepKey)
	require.Equal(t, map[string]any{
		"name":       "oidc_client",
		"label":      "Speakeasy",
		"signOnMode": "OPENID_CONNECT",
		"credentials": map[string]any{
			"oauthClient": map[string]any{
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
				"redirect_uris":    []any{"https://api.workos.com/sso/callback"},
				"consent_method":   "TRUSTED",
			},
		},
	}, fake.CreatedApplication())
	assignedAppID, assignedGroupID := fake.Assignment()
	require.Equal(t, testSignInAppID, assignedAppID)
	require.Equal(t, testEveryoneGroupID, assignedGroupID)
	require.NoError(t, fake.ValidationError())

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, testClientID, stored.ClientID.String)
	require.Equal(t, testSignInAppID, stored.SignInApplicationID.String)
	require.Equal(t, "application_created", stored.SignInState.String)
	var evidence map[string]any
	require.NoError(t, json.Unmarshal(stored.SignInEvidence, &evidence))
	require.Equal(t, map[string]any{"client_id": testSignInClientID}, evidence)
	require.NotContains(t, string(stored.SignInEvidence), testFakeClientSecret)

	afterAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionUpdated)
	require.NoError(t, err)
	require.Equal(t, beforeAudits+1, afterAudits)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionUpdated)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "application_created", afterSnapshot["sign_in_state"])
	auditJSON := string(record.Metadata) + string(record.BeforeSnapshot) + string(record.AfterSnapshot)
	for _, sensitive := range []string{testAccessToken, testClientID, testSignInClientID, testSignInAppID, testFakeClientSecret} {
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
	fake := newFakeOktaServer(t, fakeOktaNoProvision)
	fake.SetSignInClientID(manualClientID)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)

	setup, err := ti.service.DescribeSetup(ctx, &gen.DescribeSetupPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "their_console", setup.Steps[1].Where)
	require.Equal(t, []*gen.IdentityProviderExpectedValue{{Key: "client_id", Label: "Client ID", Secret: false, CurrentValue: nil}}, setup.Steps[1].ExpectedValues)

	invalidValues := [][]*gen.IdentityProviderSetupValue{
		{},
		{{Key: "other", Value: "not-accepted"}},
		{{Key: "client_id", Value: manualClientID}, {Key: "client_id", Value: "second-public-client-example"}},
		{{Key: "client_id", Value: "  "}},
	}
	for _, values := range invalidValues {
		_, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{StepKey: "sign_in", Values: values, SessionToken: nil, ApikeyToken: nil})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	result, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "sign_in",
		Values:       []*gen.IdentityProviderSetupValue{{Key: "client_id", Value: "  " + manualClientID + "  "}},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, manualClientID, fake.ResolvedClientID())
	require.Nil(t, fake.CreatedApplication())
	assignedAppID, assignedGroupID := fake.Assignment()
	require.Empty(t, assignedAppID)
	require.Empty(t, assignedGroupID)
	require.Equal(t, configuredSignInStep(manualClientID), result.Step)
	require.Equal(t, []*gen.IdentityProviderFieldOutcome{{Key: "client_id", Outcome: "accepted", Detail: "Okta sign-in Client ID saved."}}, result.FieldOutcomes)
	require.NoError(t, fake.ValidationError())

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, testSignInAppID, stored.SignInApplicationID.String)
	require.Equal(t, "application_created", stored.SignInState.String)
	var evidence map[string]any
	require.NoError(t, json.Unmarshal(stored.SignInEvidence, &evidence))
	require.Equal(t, map[string]any{"client_id": manualClientID}, evidence)
	require.NotContains(t, string(stored.SignInEvidence), testFakeClientSecret)

	retry, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "sign_in",
		Values:       []*gen.IdentityProviderSetupValue{{Key: "client_id", Value: manualClientID}},
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

func TestSubmitSignInStepDoesNotPersistWhenEveryoneAssignmentFails(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaAssignFails)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)

	_, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{StepKey: "sign_in", Values: []*gen.IdentityProviderSetupValue{}, SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeGatewayError)
	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.False(t, stored.SignInApplicationID.Valid)
	require.False(t, stored.SignInState.Valid)
	require.Empty(t, stored.SignInEvidence)
}

func TestSubmitSignInStepRejectsConflictingGroupAcknowledgements(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
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

	fake := newFakeOktaServer(t, fakeOktaPassed)
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

	fake := newFakeOktaServer(t, fakeOktaPassed)
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

func configuredSignInStep(clientID string) *gen.IdentityProviderSetupStep {
	return &gen.IdentityProviderSetupStep{
		Key:   "sign_in",
		Title: "Configure Okta sign-in",
		Where: "their_console",
		Instructions: []string{
			"Open WorkOS Admin Portal and choose the OpenID Connect connection.",
			"Copy the Okta-generated client secret directly from Okta into WorkOS Admin Portal. It never enters Speakeasy.",
			"Use the Client ID, issuer, and discovery URL shown below, then activate the connection.",
		},
		DeepLink: new("https://example-admin.okta.com/admin/app/oidc_client/instance/" + testSignInAppID + "/#tab-general"),
		PrintedValues: []*gen.IdentityProviderPrintedValue{
			{Label: "Client ID", Value: clientID, Copyable: true},
			{Label: "Issuer", Value: "https://example.okta.com", Copyable: true},
			{Label: "Discovery URL", Value: "https://example.okta.com/.well-known/openid-configuration", Copyable: true},
			{Label: "WorkOS redirect URI", Value: "https://api.workos.com/sso/callback", Copyable: true},
		},
		ExpectedValues: []*gen.IdentityProviderExpectedValue{
			{Key: "groups_claim_confirmed", Label: "Groups claim confirmed", Secret: false, CurrentValue: new("false")},
			{Key: "groups_source", Label: "Groups source", Secret: false, CurrentValue: nil},
		},
		Claims: []*gen.IdentityProviderClaim{
			{Name: "email", Purpose: "identity", CarriesAccess: false},
			{Name: "name", Purpose: "display", CarriesAccess: false},
			{Name: "groups", Purpose: "used by access rules", CarriesAccess: true},
			{Name: "department", Purpose: "reporting", CarriesAccess: false},
			{Name: "title", Purpose: "reporting", CarriesAccess: false},
		},
		Repair: &gen.IdentityProviderRepair{
			Title: "Repair the groups claim",
			Instructions: []string{
				"Open the Sign On tab, then find OpenID Connect ID Token.",
				"Set Groups claim type to Filter.",
				"Set the claim name to groups and Matches regex to .*.",
			},
			DeepLink:          new("https://example-admin.okta.com/admin/app/oidc_client/instance/" + testSignInAppID + "/#tab-sign-on"),
			FallbackAvailable: true,
		},
		PortalIntent: new("sso"),
		State:        "awaiting_verification",
		LastOutcome:  nil,
	}
}
