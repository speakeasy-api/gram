package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/executionprofiles"
)

func TestAssistantProfileSelectionsStayPinnedAndFailClosedPerServer(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "assistant_profile_resolution")
	require.NoError(t, err)
	ctx := t.Context()
	f := newAuthClientFixture(t, pool)
	q := executionprofiles.New(pool)
	assistantID, err := q.SeedAssistantFixture(ctx, executionprofiles.SeedAssistantFixtureParams{OrganizationID: f.orgID, ProjectID: f.projectID, Name: "assistant"})
	require.NoError(t, err)
	triggerID, err := q.SeedTriggerFixture(ctx, executionprofiles.SeedTriggerFixtureParams{OrganizationID: f.orgID, ProjectID: f.projectID, Name: "trigger"})
	require.NoError(t, err)
	openServer, _ := f.server()
	for _, serverID := range []uuid.UUID{f.serverID, openServer} {
		require.NoError(t, q.SeedAssistantServerFixture(ctx, executionprofiles.SeedAssistantServerFixtureParams{AssistantID: assistantID, ProjectID: f.projectID, McpServerID: serverID}))
	}
	personal := f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))

	bind := func(execCtx executionprofiles.Context) *upstreamSelector {
		t.Helper()
		binding, err := executionprofiles.Bind(ctx, pool, executionprofiles.Target{
			OrganizationID: f.orgID, ProjectID: f.projectID, AssistantID: assistantID, TriggerID: triggerID, Context: execCtx,
		}, executionprofiles.SourceIssuance)
		require.NoError(t, err)
		selector := f.service.profileSelector(principalcredential.Credential{
			OrganizationID: f.orgID, ProjectID: f.projectID, SelectionProfileVersionID: validUUID(binding.VersionID),
		}, &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID})
		require.NotNil(t, selector)
		return selector
	}
	human := bind(executionprofiles.ContextUserBacked)
	userlessGap := bind(executionprofiles.ContextUserless)
	require.Equal(t, personal, f.resolve(human).clientID)
	require.Equal(t, authClientReasonMissingChoice, f.resolve(userlessGap).reason)

	shared := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
	userless := bind(executionprofiles.ContextUserless)
	require.Equal(t, authClientResolved, f.resolve(userless).outcome)
	require.Equal(t, shared, f.resolve(userless).clientID)
	require.Equal(t, authClientReasonMissingChoice, f.resolve(userlessGap).reason, "a running turn never gains a choice")
	require.Equal(t, personal, f.resolve(human).clientID, "a new offer does not replace a pinned choice")

	require.NoError(t, f.q.SetAuthClientEnabledFixture(ctx, authclientbackfill.SetAuthClientEnabledFixtureParams{ID: personal, Enabled: false}))
	stale := f.resolve(human)
	require.Equal(t, authClientGap, stale.outcome)
	require.Equal(t, authClientReasonClientDisabled, stale.reason, "never substitute another offer for a stale choice")
	open, err := human.resolveAuthClient(ctx, openServer)
	require.NoError(t, err)
	require.Equal(t, authClientNoAuth, open.outcome, "a stale choice does not stop a different server from working")
}
