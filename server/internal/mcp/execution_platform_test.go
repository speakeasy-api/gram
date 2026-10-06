package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	gramMCP "github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// mintExecutionToken provisions assistant's dedicated agent through a
// dashboard root trigger and mints an execution token for thread.
func mintExecutionToken(t *testing.T, ti *testInstance, ac *contextvalues.AuthContext, assistant, thread uuid.UUID) string {
	t.Helper()
	root, err := triggerrepo.New(ti.conn).CreateTriggerInstance(t.Context(), triggerrepo.CreateTriggerInstanceParams{
		OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, DefinitionSlug: "dashboard", Name: "Dashboard",
		EnvironmentID: uuid.NullUUID{}, TargetKind: "assistant", TargetRef: assistant.String(), TargetDisplay: "Dashboard",
		ConfigJson: []byte(`{}`), Status: "active",
	})
	require.NoError(t, err)
	selector, err := authz.NewSelector(authz.ScopeProjectRead, ac.ProjectID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), Scope: string(authz.ScopeProjectRead), Selectors: selector})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, t.Context(), ti.conn)
	require.NoError(t, testExecutionIdentities.Provision(t.Context(), tx, assistantidentity.ProvisionParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, AssistantID: assistant, ActorUserID: ac.UserID}))
	require.NoError(t, tx.Commit(t.Context()))
	resolution, err := testExecutionIdentities.Resolve(t.Context(), ti.conn, ac.ActiveOrganizationID, *ac.ProjectID, assistant, root.ID)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, resolution.State)
	ceiling, err := testExecutionIdentities.SnapshotCeiling(t.Context(), ti.conn, *resolution.Identity)
	require.NoError(t, err)
	token, err := ti.assistantTokens.GenerateExecution(t.Context(), assistantidentity.Execution{
		Version: assistantidentity.ExecutionVersion, Identity: *resolution.Identity, Issuer: testExecutionIdentities.Issuer(),
		ThreadID: thread, EventID: "execution-event", HumanUserID: ac.UserID, Ceiling: ceiling,
	})
	require.NoError(t, err)
	return token
}

func TestExecutionTokenRoutes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithoutTemporal(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	assistant := createAssistant(t, ti, ac, "Execution routes")
	_, thread := mintThreadAssistantToken(t, ti, ac, assistant, "execution-routes")
	token := mintExecutionToken(t, ti, ac, assistant, thread)

	metaSlug := "execution-meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *ac.ProjectID, ac.ActiveOrganizationID, metaSlug, uuid.Nil)
	_, err := servePublicHTTP(t, ctx, ti, metaSlug, makeInitializeBody(), "unrelated-authorization", map[string]string{"Gram-Chat-Session": token})
	require.Error(t, err, "meta endpoints never admit execution tokens")

	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	endpoint := &gramMCP.ResolvedMcpEndpoint{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, UserSessionIssuerID: issuer.ID, AudienceURN: urn.NewUserSessionIssuer(issuer.ID).String(), Slug: toolset.Slug, RouteBase: "mcp"}
	admitted, tokens, _, err := ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, ti.serverURL.String(), endpoint)
	require.NoError(t, err, "issuer-gated resources admit the agent's workload")
	require.Empty(t, tokens)
	actor, ok := contextvalues.AuthenticatedActor(admitted)
	require.True(t, ok)
	require.Equal(t, urn.PrincipalTypeWorkload, actor.Type)

	foreign := *endpoint
	foreign.ToolsetID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	rejected := httptest.NewRecorder()
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), rejected, token, ti.serverURL.String(), &foreign)
	require.Error(t, err)
	require.NotEmpty(t, rejected.Header().Get("WWW-Authenticate"))

	_, err = servePlatformHTTP(t, ti, platformtools.ManagedAssistantPlatformToolsetSlug, toolsListBody(), token)
	require.ErrorContains(t, err, "not found", "an execution token grants no managed-assistant capabilities")
	require.NoError(t, assistantsrepo.New(ti.conn).CreateProjectManagedAssistant(t.Context(), assistantsrepo.CreateProjectManagedAssistantParams{ProjectID: *ac.ProjectID, AssistantID: assistant}))
	w, err := servePlatformHTTP(t, ti, platformtools.ManagedAssistantPlatformToolsetSlug, toolsListBody(), token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), platformtools.ToolNameSearchLogs)
}
