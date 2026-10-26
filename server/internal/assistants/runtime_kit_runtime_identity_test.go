package assistants

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestKitBootstrapMultipleRootsAndIndependentEventRevocation(t *testing.T) {
	t.Parallel()
	core, db, r, identity, config, _ := kitWorkloadDBFixture(t)
	core.encryptionClient = testenv.NewEncryptionClient(t)
	config.Origin = core.serverURL
	config.PersistedMCP = true
	// Two additional roots, alongside the normally provisioned dashboard root.
	for i := range 2 {
		tx := testenv.BeginTx(t, t.Context(), db)
		require.NoError(t, core.identities.LockTarget(t.Context(), tx, r.ProjectID, "assistant", r.AssistantID.String()))
		trigger, err := triggerrepo.New(tx).CreateTriggerInstance(t.Context(), triggerrepo.CreateTriggerInstanceParams{OrganizationID: identity.OrganizationID, ProjectID: r.ProjectID, DefinitionSlug: "cron", Name: "Scheduled task", TargetKind: "assistant", TargetRef: r.AssistantID.String(), TargetDisplay: "Assistant", ConfigJson: []byte(`{"schedule":"0 * * * *"}`), Status: "active"})
		require.NoError(t, err, "root %d", i)
		require.NoError(t, core.identities.BindRootTrigger(t.Context(), tx, r.ProjectID, trigger.ID))
		require.NoError(t, tx.Commit(t.Context()))
	}
	roots, err := usersessionsrepo.New(db).ListAssistantWorkloadRuntimeRoots(t.Context(), usersessionsrepo.ListAssistantWorkloadRuntimeRootsParams{ProjectID: r.ProjectID, OrganizationID: identity.OrganizationID, AssistantID: r.AssistantID})
	require.NoError(t, err)
	require.Len(t, roots, 3)
	event, err := core.identities.Resolve(t.Context(), db, identity.OrganizationID, r.ProjectID, r.AssistantID, roots[0])
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, event.State)

	backend, err := remotemcprepo.New(db).CreateServer(t.Context(), remotemcprepo.CreateServerParams{ID: uuid.New(), ProjectID: r.ProjectID, Name: pgtype.Text{String: "Remote", Valid: true}, Slug: pgtype.Text{String: "remote", Valid: true}, TransportType: "streamable-http", Url: "https://tools.example/mcp"})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(db).CreateMCPServer(t.Context(), mcpserversrepo.CreateMCPServerParams{ID: uuid.New(), ProjectID: r.ProjectID, Name: pgtype.Text{String: "Tools", Valid: true}, Slug: pgtype.Text{String: "tools", Valid: true}, RemoteMcpServerID: uuid.NullUUID{UUID: backend.ID, Valid: true}, Visibility: "private"})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(db).CreateMCPEndpoint(t.Context(), mcpendpointsrepo.CreateMCPEndpointParams{ProjectID: r.ProjectID, McpServerID: uuid.NullUUID{UUID: server.ID, Valid: true}, Slug: "tools"})
	require.NoError(t, err)
	q := assistantrepo.New(db)
	require.NoError(t, q.SetAttachedMCPServerIssuerForTest(t.Context(), assistantrepo.SetAttachedMCPServerIssuerForTestParams{ID: server.ID, ProjectID: r.ProjectID, UserSessionIssuerID: uuid.NullUUID{UUID: config.MCPUserSessionIssuerID, Valid: true}}))
	_, err = q.AddAssistantMcpServers(t.Context(), []assistantrepo.AddAssistantMcpServersParams{{AssistantID: r.AssistantID, ProjectID: r.ProjectID, McpServerID: server.ID}})
	require.NoError(t, err)
	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: r.ProjectID, AssistantID: r.AssistantID, RuntimeVersion: 3}))
	chat := uuid.New()
	require.NoError(t, q.UpsertAssistantChat(t.Context(), assistantrepo.UpsertAssistantChatParams{ChatID: chat, ProjectID: r.ProjectID, OrganizationID: identity.OrganizationID}))
	thread, err := q.UpsertAssistantThread(t.Context(), assistantrepo.UpsertAssistantThreadParams{AssistantID: r.AssistantID, ProjectID: r.ProjectID, CorrelationID: uuid.NewString(), ChatID: chat, SourceKind: sourceKindSlack, SourceRefJson: []byte("{}")})
	require.NoError(t, err)
	require.NoError(t, q.CreateAssistantRuntime(t.Context(), assistantrepo.CreateAssistantRuntimeParams{ID: r.ID, AssistantThreadID: thread, AssistantID: r.AssistantID, ProjectID: r.ProjectID, Backend: runtimeBackendGKE, BackendMetadataJson: []byte("{}"), State: "starting", UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}}))
	require.NoError(t, q.SetKitRuntimeMetadataFixture(t.Context(), assistantrepo.SetKitRuntimeMetadataFixtureParams{ProjectID: r.ProjectID, RuntimeID: r.ID, RuntimeVersion: 3, Metadata: []byte("{}")}))
	bootstrap := func() KitLocalBootstrap {
		t.Helper()
		tx := testenv.BeginTx(t, t.Context(), db)
		r.LifecycleTx = tx
		b, err := core.kitCompositionBootstrap(t.Context(), r, config)
		require.NoError(t, err)
		require.NoError(t, tx.Commit(t.Context()))
		r.LifecycleTx = nil
		require.Equal(t, identity, b.Workload.Identity)
		require.NotEmpty(t, b.Workload.ServiceAccount)
		return b
	}
	first := bootstrap()
	// Withdrawing a root revokes that event, not the assistant's runtime identity.
	admissions, err := workloadrepo.New(db).ListWorkloadAdmissions(t.Context(), workloadrepo.ListWorkloadAdmissionsParams{OrganizationID: identity.OrganizationID, ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true}})
	require.NoError(t, err)
	withdrawn := false
	for _, a := range admissions {
		if a.WorkloadIssuerID == event.Identity.IssuerID && a.Subject == event.Identity.Subject {
			_, err = workloadrepo.New(db).SoftDeleteWorkloadAdmission(t.Context(), workloadrepo.SoftDeleteWorkloadAdmissionParams{OrganizationID: identity.OrganizationID, ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true}, ID: a.ID})
			require.NoError(t, err)
			withdrawn = true
		}
	}
	require.True(t, withdrawn)
	require.ErrorIs(t, core.identities.Validate(t.Context(), db, *event.Identity), assistantidentity.ErrInvalidIdentity)
	require.NoError(t, core.identities.ValidateRuntime(t.Context(), db, identity))
	second := bootstrap()
	require.NotEqual(t, first.Workload.Subject, second.Workload.Subject)
	require.NoError(t, core.revokeKitWorkload(t.Context(), db, kitWorkloadMetadata(t, r, first.Workload)))
	require.NoError(t, core.identities.ValidateRuntime(t.Context(), db, second.Workload.Identity))
	// Replacement selecting the same agent is still a different authority.
	iq := identityrepo.New(db)
	require.NoError(t, iq.TombstoneAssistantBinding(t.Context(), identityrepo.TombstoneAssistantBindingParams{ProjectID: r.ProjectID, AssistantID: r.AssistantID}))
	_, err = iq.CreateAssistantBinding(t.Context(), identityrepo.CreateAssistantBindingParams{OrganizationID: identity.OrganizationID, ProjectID: r.ProjectID, AssistantID: r.AssistantID, AgentID: identity.AgentID})
	require.NoError(t, err)
	require.ErrorIs(t, core.identities.ValidateRuntime(t.Context(), db, identity), assistantidentity.ErrInvalidIdentity)
	_, err = core.provisionKitWorkload(t.Context(), r, config, identity, "stale-binding")
	require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
}
