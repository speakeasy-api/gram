package identityproviders_test

import (
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
