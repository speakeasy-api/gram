package mcp_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// tunneledAssistantSentinel is stored in the tunnel's key and resource fields
// so the managed assistant's results can be checked for leaks.
const tunneledAssistantSentinel = "managed-assistant-tunnel-sentinel"

type platformToolResult struct {
	text    string
	isError bool
}

type tunneledAssistantFixture struct {
	ti            *testInstance
	authCtx       *contextvalues.AuthContext
	token         string
	wrapperID     uuid.UUID
	foreignMCPID  uuid.UUID
	foreignProjID uuid.UUID
}

func seedTunneledWrapper(t *testing.T, ti *testInstance, projectID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	tunnel, err := tunneledmcprepo.New(ti.conn).CreateServer(t.Context(), tunneledmcprepo.CreateServerParams{
		ID:                 uuid.New(),
		ProjectID:          projectID,
		Name:               "Private inventory tunnel",
		KeyHash:            tunneledAssistantSentinel + "-hash-" + uuid.NewString(),
		KeyPrefix:          tunneledAssistantSentinel + "-prefix",
		ResourceIdentifier: pgtype.Text{String: "https://" + tunneledAssistantSentinel + ".internal", Valid: true},
	})
	require.NoError(t, err)
	wrapper, err := mcpserversrepo.New(ti.conn).CreateMCPServer(t.Context(), mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                pgtype.Text{String: "Private inventory", Valid: true},
		Slug:                pgtype.Text{String: slug, Valid: true},
		TunneledMcpServerID: uuid.NullUUID{UUID: tunnel.ID, Valid: true},
		Visibility:          "private",
	})
	require.NoError(t, err)
	return wrapper.ID
}

// newTunneledAssistantFixture provisions the project's managed assistant with
// a tunneled MCP server in its project and another one in a second project of
// the same organization. adminGrants adds the live org:admin and read grants.
func newTunneledAssistantFixture(t *testing.T, adminGrants bool) tunneledAssistantFixture {
	t.Helper()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	managedID := createAssistant(t, ti, authCtx, "Managed")
	require.NoError(t, assistantsrepo.New(ti.conn).CreateProjectManagedAssistant(t.Context(), assistantsrepo.CreateProjectManagedAssistantParams{
		ProjectID:   *authCtx.ProjectID,
		AssistantID: managedID,
	}))
	ti.features.SetFlagVariant(feature.FlagAssistantPlatformMCP, authCtx.ActiveOrganizationID, feature.VariantAssistantToolsPlatformMCP)
	ti.features.SetFlag(feature.FlagTunneledMCP, authCtx.ActiveOrganizationID, true)

	wrapperID := seedTunneledWrapper(t, ti, *authCtx.ProjectID, "private-inventory")
	foreignProject, err := projectsrepo.New(ti.conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name: "Other project", Slug: "other-" + uuid.NewString()[:8], OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	foreignMCPID := seedTunneledWrapper(t, ti, foreignProject.ID, "other-inventory")

	if adminGrants {
		grantLiveOrgAdmin(t, ti, authCtx)
		mcpSelector := authz.NewSelector(authz.ScopeMCPRead, "*")
		mcpSelector[authz.SelectorKeyProjectID] = authCtx.ProjectID.String()
		foreignMCPSelector := authz.NewSelector(authz.ScopeMCPRead, "*")
		foreignMCPSelector[authz.SelectorKeyProjectID] = foreignProject.ID.String()
		require.NoError(t, authz.PatchPrincipalGrants(
			t.Context(), ti.conn, authCtx.ActiveOrganizationID,
			urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			[]*authz.RoleGrant{
				{Scope: string(authz.ScopeProjectRead), Selectors: []authz.Selector{
					authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String()),
					authz.NewSelector(authz.ScopeProjectRead, foreignProject.ID.String()),
				}},
				{Scope: string(authz.ScopeMCPRead), Selectors: []authz.Selector{mcpSelector, foreignMCPSelector}},
			},
			nil,
		))
	}

	return tunneledAssistantFixture{
		ti:            ti,
		authCtx:       authCtx,
		token:         mintAssistantToken(t, ti, authCtx, managedID),
		wrapperID:     wrapperID,
		foreignMCPID:  foreignMCPID,
		foreignProjID: foreignProject.ID,
	}
}

func (f tunneledAssistantFixture) call(t *testing.T, name string, arguments map[string]any) (platformToolResult, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": arguments},
	})
	require.NoError(t, err)
	w, err := servePlatformHTTP(t, f.ti, platformtools.PlatformMCPReadToolsetSlug, body, f.token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), tunneledAssistantSentinel)

	var envelope struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	text := ""
	if len(envelope.Result.Content) > 0 {
		text = envelope.Result.Content[0].Text
	}
	return platformToolResult{text: text, isError: envelope.Result.IsError}, w.Body.String()
}

// The managed assistant reaches the tunneled setup handoff in its own project
// only: the model cannot name a project, the add form opens in the assistant's, an existing
// tunneled server opens its agent setup, and another project's server is
// refused like a missing one.
func TestServePlatformToolset_TunneledSetupHandoffPinnedToAssistantProject(t *testing.T) {
	t.Parallel()
	f := newTunneledAssistantFixture(t, true)
	project, err := projectsrepo.New(f.ti.conn).GetProjectByID(t.Context(), *f.authCtx.ProjectID)
	require.NoError(t, err)

	_, raw := f.call(t, "get_tunneled_mcp_setup_handoff", map[string]any{"project_id": f.foreignProjID.String()})
	require.Contains(t, raw, "additional properties 'project_id' not allowed", "the model cannot name a project")
	require.NotContains(t, raw, "setup_url")

	result, _ := f.call(t, "get_tunneled_mcp_setup_handoff", map[string]any{})
	require.False(t, result.isError, result.text)
	var add struct {
		ProjectID string `json:"project_id"`
		Intent    string `json:"intent"`
		SetupURL  string `json:"setup_url"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.text), &add))
	require.Equal(t, f.authCtx.ProjectID.String(), add.ProjectID)
	require.Equal(t, "add_tunneled_mcp", add.Intent)
	require.Contains(t, add.SetupURL, "https://dashboard.example.test/")
	require.Contains(t, add.SetupURL, "/projects/"+project.Slug+"/mcp/add/tunneled")

	result, _ = f.call(t, "get_tunneled_mcp_setup_handoff", map[string]any{"mcp_id": f.wrapperID.String()})
	require.False(t, result.isError, result.text)
	require.Contains(t, result.text, "/mcp/x/private-inventory/settings#agent-setup")

	result, _ = f.call(t, "get_tunneled_mcp_setup_handoff", map[string]any{"mcp_id": f.foreignMCPID.String()})
	// The adapter returns a refusal to the model as the tool's answer.
	require.Contains(t, result.text, `"code":"not_found"`)
	require.NotContains(t, result.text, "setup_url")
}

// The managed assistant's get_mcp read of a tunneled server carries the
// tunnel's connection state, and nothing else about the tunnel.
func TestServePlatformToolset_GetMCPReportsTunnelConnectionForAssistant(t *testing.T) {
	t.Parallel()
	f := newTunneledAssistantFixture(t, true)

	result, _ := f.call(t, "get_mcp", map[string]any{"mcp_id": f.wrapperID.String()})
	require.False(t, result.isError, result.text)
	var mcp struct {
		BackendKind string          `json:"backend_kind"`
		Tunnel      json.RawMessage `json:"tunnel"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.text), &mcp))
	require.Equal(t, "tunneled", mcp.BackendKind)
	require.JSONEq(t, `{"connection_status":"never_connected"}`, string(mcp.Tunnel))
}

// Without live organization administration the managed assistant cannot get a
// setup link, whatever the model asks for.
func TestServePlatformToolset_TunneledSetupHandoffRefusedWithoutOrgAdmin(t *testing.T) {
	t.Parallel()
	f := newTunneledAssistantFixture(t, false)

	_, raw := f.call(t, "get_tunneled_mcp_setup_handoff", map[string]any{"mcp_id": f.wrapperID.String()})
	var refusal struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &refusal))
	require.Equal(t, "permission denied", refusal.Error.Message, "the live org-admin gate refuses the call")
	require.NotContains(t, raw, "setup_url")
	require.NotContains(t, raw, "dashboard.example.test")
}

// For an organization outside the tunneled MCP rollout the add form is refused
// as not enabled, while an existing tunneled server's setup stays reachable.
func TestServePlatformToolset_TunneledSetupHandoffNotEnabledForOrganization(t *testing.T) {
	t.Parallel()
	f := newTunneledAssistantFixture(t, true)
	f.ti.features.SetFlag(feature.FlagTunneledMCP, f.authCtx.ActiveOrganizationID, false)

	result, _ := f.call(t, "get_tunneled_mcp_setup_handoff", map[string]any{})
	require.Contains(t, result.text, `"code":"not_enabled"`)
	require.NotContains(t, result.text, "setup_url")

	result, _ = f.call(t, "get_tunneled_mcp_setup_handoff", map[string]any{"mcp_id": f.wrapperID.String()})
	require.False(t, result.isError, result.text)
	require.Contains(t, result.text, "#agent-setup")
}
