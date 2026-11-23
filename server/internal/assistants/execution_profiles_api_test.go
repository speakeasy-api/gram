package assistants

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/executionprofiles"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestExecutionProfileChoiceRequiresWriteAndOneMode(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID := newRBACService(t)
	writerCtx := authztest.WithExactGrants(t, ctx, assistantWriteGrant(projectID))
	assistant := createTestAssistant(t, svc, writerCtx, "Assistant")

	readerCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeAssistantRead, assistant.ID))
	listed, err := svc.ListExecutionProfiles(readerCtx, &gen.ListExecutionProfilesPayload{AssistantID: assistant.ID})
	require.NoError(t, err)
	require.NotNil(t, listed.Profiles)

	choice := func(mutate func(*gen.SetExecutionProfileChoicePayload)) *gen.SetExecutionProfileChoicePayload {
		p := &gen.SetExecutionProfileChoicePayload{
			AssistantID: assistant.ID, TriggerInstanceID: uuid.NewString(), ExecutionContext: "userless",
			McpServerID: uuid.NewString(), Automatic: new(true),
		}
		mutate(p)
		return p
	}
	_, err = svc.SetExecutionProfileChoice(readerCtx, choice(func(*gen.SetExecutionProfileChoicePayload) {}))
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = svc.SetExecutionProfileChoice(writerCtx, choice(func(p *gen.SetExecutionProfileChoicePayload) { p.Automatic = nil }))
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = svc.SetExecutionProfileChoice(writerCtx, choice(func(p *gen.SetExecutionProfileChoicePayload) { p.NoAuth = new(true) }))
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = svc.SetExecutionProfileChoice(writerCtx, choice(func(*gen.SetExecutionProfileChoicePayload) {}))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestExecutionProfileChoiceIsVersionedAndAudited(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistants_execution_profiles")
	writerCtx := authztest.WithExactGrants(t, ctx, assistantWriteGrant(projectID))
	assistant := createTestAssistant(t, svc, writerCtx, "Assistant")
	assistantID := uuid.MustParse(assistant.ID)
	const org = "org-test"

	q := executionprofiles.New(conn)
	trigger, err := q.SeedTriggerFixture(ctx, executionprofiles.SeedTriggerFixtureParams{OrganizationID: org, ProjectID: projectID, Name: "Dashboard"})
	require.NoError(t, err)
	require.NoError(t, q.SeedTriggerBindingFixture(ctx, executionprofiles.SeedTriggerBindingFixtureParams{OrganizationID: org, ProjectID: projectID, AssistantID: uuid.NullUUID{UUID: assistantID, Valid: true}, TriggerID: trigger}))
	serverID, remoteID := uuid.New(), uuid.New()
	require.NoError(t, q.SeedRemoteMCPServerFixture(ctx, executionprofiles.SeedRemoteMCPServerFixtureParams{ID: remoteID, ProjectID: projectID, Url: "https://mcp.example.com/" + remoteID.String()[:8]}))
	require.NoError(t, q.SeedMCPServerFixture(ctx, executionprofiles.SeedMCPServerFixtureParams{ID: serverID, ProjectID: projectID, Slug: pgtype.Text{String: "s-" + serverID.String()[:8], Valid: true}, RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true}, RequiresUpstreamAuth: pgtype.Bool{Bool: true, Valid: true}}))
	require.NoError(t, q.SeedAssistantServerFixture(ctx, executionprofiles.SeedAssistantServerFixtureParams{AssistantID: assistantID, McpServerID: serverID, ProjectID: projectID}))
	var clients []uuid.UUID
	for range 2 {
		id, err := q.SeedAuthClientFixture(ctx, executionprofiles.SeedAuthClientFixtureParams{OrganizationID: org, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, ClientType: authz.AuthClientTypeStaticCredential})
		require.NoError(t, err)
		require.NoError(t, q.LinkAuthClientFixture(ctx, executionprofiles.LinkAuthClientFixtureParams{ProjectID: projectID, OrganizationID: org, McpServerID: serverID, AuthClientID: id}))
		clients = append(clients, id)
	}

	listed, err := svc.ListExecutionProfiles(writerCtx, &gen.ListExecutionProfilesPayload{AssistantID: assistant.ID})
	require.NoError(t, err)
	var userless *gen.AssistantExecutionProfile
	for _, profile := range listed.Profiles {
		if profile.ExecutionContext == "userless" {
			userless = profile
			break
		}
	}
	require.NotNil(t, userless, "the assistant's turns run under its dashboard trigger")
	require.Len(t, userless.Servers, 1)
	require.Equal(t, "selection_required", *userless.Servers[0].GapReason)

	before, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAssistantExecutionProfileUpdate)
	require.NoError(t, err)
	choice := &gen.SetExecutionProfileChoicePayload{
		AssistantID: assistant.ID, TriggerInstanceID: userless.TriggerInstanceID, ExecutionContext: "userless",
		McpServerID: serverID.String(), AuthClientID: new(clients[1].String()), ExpectedVersion: userless.Version,
	}
	updated, err := svc.SetExecutionProfileChoice(writerCtx, choice)
	require.NoError(t, err)
	require.Equal(t, userless.Version+1, updated.Version)
	require.Equal(t, clients[1].String(), *updated.Servers[0].AuthClientID)
	require.Equal(t, gen.ExecutionProfileChoiceSource("admin"), *updated.Servers[0].Source)
	after, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAssistantExecutionProfileUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	_, err = svc.SetExecutionProfileChoice(writerCtx, choice)
	requireOopsCode(t, err, oops.CodeConflict)
}
