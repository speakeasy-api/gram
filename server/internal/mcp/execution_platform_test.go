package mcp_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	gramMCP "github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestExecutionPlatformRoutePreservesManagedRestrictionsAfterBusinessDenial(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithoutTemporal(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	assistant := createAssistant(t, ti, ac, "Execution platform")
	_, thread := mintThreadAssistantToken(t, ti, ac, assistant, "execution-platform")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	signer, err := mcpauthz.New(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})), "https://platform.example.invalid", false)
	require.NoError(t, err)
	identities, err := assistantidentity.New("https://platform.example.invalid", false)
	require.NoError(t, err)
	ti.assistantTokens.ConfigureExecutionIdentity(signer, identities)
	selector, err := json.Marshal(authz.NewSelector(authz.ScopeProjectWrite, ac.ProjectID.String()))
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).InsertPrincipalGrantIfAbsent(t.Context(), accessrepo.InsertPrincipalGrantIfAbsentParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), Scope: string(authz.ScopeProjectWrite), Selectors: selector})
	require.NoError(t, err)
	root := uuid.New()
	require.NoError(t, identityrepo.New(ti.conn).FixtureCreateRoot(t.Context(), identityrepo.FixtureCreateRootParams{ID: root, OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, DefinitionSlug: "dashboard", TargetRef: assistant.String()}))
	tx := testenv.BeginTx(t, t.Context(), ti.conn)
	_, err = identities.Provision(t.Context(), tx, assistantidentity.ProvisionParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, AssistantID: assistant, ActorUserID: ac.UserID})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	resolution, err := identities.Resolve(t.Context(), ti.conn, ac.ActiveOrganizationID, *ac.ProjectID, assistant, root)
	require.NoError(t, err)
	_, resourceIssuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	remote, business := uuid.New(), uuid.New()
	iq := identityrepo.New(ti.conn)
	require.NoError(t, iq.FixtureCreateRemote(t.Context(), identityrepo.FixtureCreateRemoteParams{ID: remote, ProjectID: *ac.ProjectID}))
	require.NoError(t, iq.FixtureCreateMCPServer(t.Context(), identityrepo.FixtureCreateMCPServerParams{ID: business, ProjectID: *ac.ProjectID, RemoteID: uuid.NullUUID{UUID: remote, Valid: true}}))
	require.NoError(t, iq.FixtureAttachMCPServer(t.Context(), identityrepo.FixtureAttachMCPServerParams{ProjectID: *ac.ProjectID, AssistantID: assistant, ServerID: business}))
	selector, err = json.Marshal(authz.NewSelector(authz.ScopeMCPConnect, business.String()))
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).InsertPrincipalGrantIfAbsent(t.Context(), accessrepo.InsertPrincipalGrantIfAbsentParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeAgent, resolution.Identity.AgentID.String()), Scope: string(authz.ScopeMCPConnect), Selectors: selector})
	require.NoError(t, err)
	ceiling, err := identities.SnapshotCeiling(t.Context(), ti.conn, *resolution.Identity)
	require.NoError(t, err)
	execution := assistantidentity.Execution{Version: 1, Identity: *resolution.Identity, Issuer: identities.Issuer(), ThreadID: thread, EventID: "platform-event", Mode: assistantidentity.ExecutionWorkload, Ceiling: ceiling}
	token, err := ti.assistantTokens.GenerateExecution(t.Context(), execution)
	require.NoError(t, err)
	endpoint := &gramMCP.ResolvedMcpEndpoint{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, McpServerID: uuid.NullUUID{UUID: business, Valid: true}, UserSessionIssuerID: resourceIssuer.ID, AudienceURN: urn.NewUserSessionIssuer(resourceIssuer.ID).String(), Slug: "execution-resource", RouteBase: "mcp"}
	admitted, tokens, _, err := ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, ti.serverURL.String(), endpoint)
	require.NoError(t, err, "issuer-gated native resources reuse workload admission, not human impersonation")
	require.Empty(t, tokens)
	actor, ok := contextvalues.AuthenticatedActor(admitted)
	require.True(t, ok)
	require.Equal(t, urn.PrincipalTypeWorkload, actor.Type)
	wrong := *endpoint
	wrong.McpServerID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, ti.serverURL.String(), &wrong)
	require.Error(t, err)
	_, err = ti.assistantTokens.AuthorizeBusiness(t.Context(), token, uuid.New(), nil)
	require.Error(t, err)
	_, err = servePlatformHTTP(t, ti, platformtools.ManagedAssistantPlatformToolsetSlug, toolsListBody(), token)
	require.ErrorContains(t, err, "not found", "binding does not grant managed-assistant capabilities")
	require.NoError(t, assistantsrepo.New(ti.conn).CreateProjectManagedAssistant(t.Context(), assistantsrepo.CreateProjectManagedAssistantParams{ProjectID: *ac.ProjectID, AssistantID: assistant}))
	w, err := servePlatformHTTP(t, ti, platformtools.ManagedAssistantPlatformToolsetSlug, toolsListBody(), token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), platformtools.ToolNameSearchLogs)
}
