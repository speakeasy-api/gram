package assistants

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
	"github.com/stretchr/testify/require"
)

func TestKitBootstrapMultipleRootsAndIndependentEventRevocation(t *testing.T) {
	t.Parallel()
	core, db, r, identity, config, _ := kitWorkloadDBFixture(t)
	core.encryptionClient = testenv.NewEncryptionClient(t)
	config.Origin = core.serverURL
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

	q := assistantrepo.New(db)
	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: r.ProjectID, AssistantID: r.AssistantID, RuntimeVersion: 3}))
	chatID := uuid.New()
	require.NoError(t, q.UpsertAssistantChat(t.Context(), assistantrepo.UpsertAssistantChatParams{ChatID: chatID, ProjectID: r.ProjectID, OrganizationID: identity.OrganizationID}))
	thread, err := q.UpsertAssistantThread(t.Context(), assistantrepo.UpsertAssistantThreadParams{AssistantID: r.AssistantID, ProjectID: r.ProjectID, CorrelationID: uuid.NewString(), ChatID: chatID, SourceKind: sourceKindSlack, SourceRefJson: []byte("{}")})
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
		require.True(t, b.Workload.MCPProjectGateway)

		var document struct {
			Servers map[string]struct {
				Type    string            `json:"type"`
				URL     string            `json:"url"`
				Headers map[string]string `json:"headers"`
				Auth    struct {
					Type       string            `json:"type"`
					Resource   string            `json:"resource"`
					Scope      string            `json:"scope"`
					Extensions map[string]string `json:"extensions"`
				} `json:"auth"`
			} `json:"mcpServers"`
		}
		require.NoError(t, json.Unmarshal(b.MCPJSON, &document))
		require.Len(t, document.Servers, 1, "Kit bootstrap must configure only the single project-gateway MCP entry")
		gw, ok := document.Servers[kitProjectGatewayServerName]
		require.True(t, ok)
		require.Equal(t, "streamable-http", gw.Type)
		require.Equal(t, "https://api.example/mcp/gateway/identity-tests/projects/kit-workload-trust", gw.URL)
		require.Equal(t, gw.URL, gw.Auth.Resource)
		require.Equal(t, "workload_identity", gw.Auth.Type)
		require.Equal(t, "mcp:connect", gw.Auth.Scope)
		require.Empty(t, gw.Auth.Extensions)
		require.Empty(t, gw.Headers)
		require.NotContains(t, string(b.MCPJSON), "builtin-")
		require.Contains(t, string(b.AgentsMD), "`project-gateway`")
		require.NotContains(t, string(b.AgentsMD), "builtin-")
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
	require.ErrorIs(t, core.identities.ValidateRuntime(t.Context(), db, second.Workload.Identity), assistantidentity.ErrInvalidIdentity)
	_, err = core.provisionKitWorkload(t.Context(), r, config, second.Workload.Identity, "stale-binding")
	require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
}
