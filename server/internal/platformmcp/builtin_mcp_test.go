package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type builtinMCPFake struct {
	calls   int
	org     string
	project uuid.UUID
	key     string
	update  builtinmcp.Update
	result  builtinmcp.Server
	err     error
}

func (f *builtinMCPFake) List(_ context.Context, org string, project uuid.UUID) ([]builtinmcp.Server, error) {
	f.calls++
	f.org = org
	f.project = project
	return []builtinmcp.Server{f.result}, f.err
}
func (f *builtinMCPFake) Get(_ context.Context, org string, project uuid.UUID, key string) (builtinmcp.Server, error) {
	f.calls++
	f.org = org
	f.project = project
	f.key = key
	return f.result, f.err
}
func (f *builtinMCPFake) Update(_ context.Context, org string, project uuid.UUID, key string, update builtinmcp.Update) (builtinmcp.Server, error) {
	f.calls++
	f.org = org
	f.project = project
	f.key = key
	f.update = update
	return f.result, f.err
}
func TestBuiltinMCPToolContracts(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "live", Version: "1"}, nil))
	absent := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "absent", Version: "1"}, nil))
	registerBuiltinMCPTools(live, &builtinMCPFake{})
	registerBuiltinMCPTools(absent, nil)
	require.Len(t, live.Descriptors(), 3)
	require.Empty(t, live.For(AudienceAssistant))
	for i, d := range live.Descriptors() {
		unavailable := absent.Descriptors()[i]
		require.Equal(t, ExternalAuthorizationOrgAdmin, d.Meta.Authorization)
		require.Equal(t, ProjectScopeExplicit, d.Meta.ProjectScope)
		require.Equal(t, externalOnly, d.Meta.Audiences)
		require.Equal(t, d.Meta, unavailable.Meta)
		require.JSONEq(t, string(d.InputSchema), string(unavailable.InputSchema))
		require.Equal(t, d.output, unavailable.output)
		require.Equal(t, d.Annotations, unavailable.Annotations)
	}
}
func TestBuiltinMCPConfigureRequiresConfirmedVersionAndExactProject(t *testing.T) {
	t.Parallel()
	principal := Principal{OrganizationID: "org_example", UserID: "user_example"}
	project := uuid.NewString()
	valid := ConfigureBuiltinMCPInput{ProjectID: project, Key: "slack", Hidden: true, ExpectedVersion: "0", Confirmed: true}
	for _, name := range []string{"project", "key", "version", "confirmation", "environment"} {
		input := valid
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			switch name {
			case "project":
				input.ProjectID = ""
			case "key":
				input.Key = ""
			case "version":
				input.ExpectedVersion = ""
			case "confirmation":
				input.Confirmed = false
			case "environment":
				invalid := ""
				input.EnvironmentID = &invalid
			}
			core := &builtinMCPFake{}
			_, err := configureBuiltinMCP(t.Context(), core, principal, input)
			require.ErrorIs(t, err, errBuiltinMCPInvalid)
			require.Zero(t, core.calls)
		})
	}
	core := &builtinMCPFake{result: builtinmcp.Server{Key: "slack", Hidden: true, Version: "1"}}
	got, err := configureBuiltinMCP(t.Context(), core, principal, valid)
	require.NoError(t, err)
	require.Equal(t, core.result, got)
	require.Equal(t, principal.OrganizationID, core.org)
	require.Equal(t, project, core.project.String())
	require.Equal(t, "slack", core.key)
	require.Equal(t, "0", core.update.ExpectedVersion)
	require.True(t, core.update.Hidden)
	require.False(t, core.update.EnvironmentID.Valid)
	environment := uuid.NewString()
	valid.EnvironmentID = &environment
	_, err = configureBuiltinMCP(t.Context(), core, principal, valid)
	require.NoError(t, err)
	require.Equal(t, environment, core.update.EnvironmentID.UUID.String())
}
func TestBuiltinMCPToolsEnforceAdminBeforeCore(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"list_builtin_mcps", "get_builtin_mcp", "configure_builtin_mcp"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := mcp.NewServer(&mcp.Implementation{Name: "builtin", Version: "1"}, nil)
			bindExternalTestPrincipal(server)
			reg := newRegistrar(server)
			reg.withExternalAuthorizer(denyExternalCallAuthorizer{err: &ExternalAuthorizationError{RequiredScope: "org:admin"}})
			core := &builtinMCPFake{}
			registerBuiltinMCPTools(reg, core)
			ct, st := mcp.NewInMemoryTransports()
			ss, err := server.Connect(t.Context(), st, nil)
			require.NoError(t, err)
			defer func() { _ = ss.Close() }()
			client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			cs, err := client.Connect(t.Context(), ct, nil)
			require.NoError(t, err)
			defer func() { _ = cs.Close() }()
			args := map[string]any{"project_id": uuid.NewString()}
			if name != "list_builtin_mcps" {
				args["key"] = "slack"
			}
			if name == "configure_builtin_mcp" {
				args["hidden"] = true
				args["expected_version"] = "0"
				args["confirmed"] = true
			}
			result, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
			require.NoError(t, err)
			require.True(t, result.IsError)
			require.Zero(t, core.calls)
		})
	}
}

func TestBuiltinMCPReadsAndUnavailable(t *testing.T) {
	t.Parallel()
	principal := Principal{OrganizationID: "org_example", UserID: "user_example"}
	project := uuid.NewString()
	for _, available := range []bool{true, false} {
		reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "builtin", Version: "1"}, nil))
		core := &builtinMCPFake{result: builtinmcp.Server{Key: "slack", Hidden: true, Version: "0", CredentialVariables: []string{"SLACK_BOT_TOKEN"}}}
		if available {
			registerBuiltinMCPTools(reg, core)
		} else {
			registerBuiltinMCPTools(reg, nil)
		}
		for _, d := range reg.Descriptors() {
			require.False(t, externalToolDiscoverable([]authz.Grant{authz.NewGrant(authz.ScopeOrgRead, principal.OrganizationID)}, true, principal, d.Meta))
			require.True(t, externalToolDiscoverable([]authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)}, true, principal, d.Meta))
			if d.Name == "configure_builtin_mcp" {
				continue
			}
			args := map[string]any{"project_id": project}
			if d.Name == "get_builtin_mcp" {
				args["key"] = "slack"
			}
			raw, err := json.Marshal(args)
			require.NoError(t, err)
			got, err := d.Invoke(contextWithPrincipal(t.Context(), principal), raw)
			if !available {
				var refusal *ToolRefusalError
				require.ErrorAs(t, err, &refusal)
				require.Contains(t, refusal.Payload, unavailableCode)
				continue
			}
			require.NoError(t, err)
			if d.Name == "get_builtin_mcp" {
				require.Equal(t, core.result, got)
			} else {
				require.Equal(t, ListBuiltinMCPsOutput{Servers: []builtinmcp.Server{core.result}}, got)
			}
			require.Equal(t, principal.OrganizationID, core.org)
			require.Equal(t, project, core.project.String())
		}
	}
}
func TestBuiltinMCPCoreRefusalsDoNotLeakDetails(t *testing.T) {
	t.Parallel()
	for _, code := range []oops.Code{oops.CodeForbidden, oops.CodeNotFound, oops.CodeInvalid, oops.CodeConflict} {
		result, handled := builtinMCPToolResult(oops.E(code, nil, "private credentials and environment"))
		require.True(t, handled)
		require.True(t, result.IsError)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		require.NotContains(t, text.Text, "private credentials")
	}
}
