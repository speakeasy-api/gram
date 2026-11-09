package dashboardlinks

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// contractPath is the file the dashboard checks against its own routes.
const contractPath = "../../../client/dashboard/src/server-links.json"

// builders calls every builder with the dashboard's own parameter names, so
// its output reads as the route template the dashboard declares.
var builders = map[string]func() Link{
	"Self":                           Self,
	"Block":                          func() Link { return Block(":id") },
	"SharedSkill":                    func() Link { return SharedSkill(":token") },
	"RiskPolicyChallengeAcknowledge": func() Link { return RiskPolicyChallengeAcknowledge(":ackToken") },
	"RiskPolicyBypassRequest":        func() Link { return RiskPolicyBypassRequest(":requestToken") },
	"OrganizationHome":               func() Link { return OrganizationHome(":orgSlug") },
	"Billing":                        func() Link { return Billing(":orgSlug") },
	"Domains":                        func() Link { return Domains(":orgSlug") },
	"Identity":                       func() Link { return Identity(":orgSlug") },
	"DeviceAgent":                    func() Link { return DeviceAgent(":orgSlug") },
	"DataExports":                    func() Link { return DataExports(":orgSlug") },
	"EventFeed":                      func() Link { return EventFeed(":orgSlug") },
	"AgentManagement":                func() Link { return AgentManagement(":orgSlug") },
	"AccessRoles":                    func() Link { return AccessRoles(":orgSlug") },
	"AccessGrant":                    func() Link { return AccessGrant(":orgSlug", ":userId", ":scope", ":resourceId", ":projectId") },
	"AccessChallenges":               func() Link { return AccessChallenges(":orgSlug") },
	"RequestAccess":                  func() Link { return RequestAccess(":orgSlug", ":scope", ":resourceId", ":resourceName") },
	"Assistants":                     func() Link { return Assistants(":orgSlug", ":projectSlug") },
	"NewAssistant":                   func() Link { return NewAssistant(":orgSlug", ":projectSlug", ":disposition") },
	"Assistant":                      func() Link { return Assistant(":orgSlug", ":projectSlug", ":assistantId") },
	"AssistantThread":                func() Link { return AssistantThread(":orgSlug", ":projectSlug", ":assistantId", ":threadId") },
	"AssistantSessions":              func() Link { return AssistantSessions(":orgSlug", ":projectSlug", ":assistantId") },
	"Chat":                           func() Link { return Chat(":orgSlug", ":projectSlug", ":chatId") },
	"Deployment":                     func() Link { return Deployment(":orgSlug", ":projectSlug", ":deploymentId") },
	"Dashboard":                      func() Link { return Dashboard(":orgSlug", ":projectSlug", ":dashboardId") },
	"SignalsIntelligence":            func() Link { return SignalsIntelligence(":orgSlug", ":projectSlug") },
	"Logs":                           func() Link { return Logs(":orgSlug", ":projectSlug") },
	"Plugins":                        func() Link { return Plugins(":orgSlug", ":projectSlug") },
	"MCPServer":                      func() Link { return MCPServer(":orgSlug", ":projectSlug", ":mcpServerSlug") },
	"MCPServerInspect":               func() Link { return MCPServerInspect(":orgSlug", ":projectSlug", ":mcpServerSlug") },
	"MCPServerAuthentication": func() Link {
		return MCPServerAuthentication(":orgSlug", ":projectSlug", ":mcpServerSlug", ":setupStep")
	},
	"MCPGateway":          func() Link { return MCPGateway(":orgSlug", ":projectSlug", ":gatewayId") },
	"ShadowAIMCP":         func() Link { return ShadowAIMCP(":orgSlug", ":projectSlug", ":serverSlug") },
	"RiskPolicies":        func() Link { return RiskPolicies(":orgSlug", ":projectSlug") },
	"MCPApprovals":        func() Link { return MCPApprovals(":orgSlug", ":projectSlug") },
	"ToolApprovalRequest": func() Link { return ToolApprovalRequest(":orgSlug", ":projectSlug", ":requestId") },
	"RiskEvents":          func() Link { return RiskEvents(":orgSlug", ":projectSlug") },
}

func readContract(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(contractPath))
	require.NoError(t, err)
	var contract struct {
		Links map[string]string `json:"links"`
	}
	require.NoError(t, json.Unmarshal(raw, &contract))
	require.NotEmpty(t, contract.Links)
	return contract.Links
}

// template reads a built URL back as a route template: the path, query and
// fragment unescaped, so ":orgSlug" reads as written.
func template(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	out := u.Path
	if u.RawQuery != "" {
		query, err := url.QueryUnescape(u.RawQuery)
		require.NoError(t, err)
		out += "?" + query
	}
	if u.Fragment != "" {
		fragment, err := url.QueryUnescape(u.Fragment)
		require.NoError(t, err)
		out += "#" + fragment
	}
	return out
}

// exportedBuilders lists every exported function in the package that returns
// a Link, so a new builder cannot skip the contract.
func exportedBuilders(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	var names []string
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		require.NoError(t, err)
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() || fn.Type.Results == nil {
				continue
			}
			result, ok := fn.Type.Results.List[0].Type.(*ast.Ident)
			if ok && result.Name == "Link" {
				names = append(names, fn.Name.Name)
			}
		}
	}
	slices.Sort(names)
	return names
}

func TestEveryBuilderIsInTheContract(t *testing.T) {
	t.Parallel()
	contract := readContract(t)

	exported := exportedBuilders(t)
	require.NotEmpty(t, exported)
	inContract := slices.Sorted(maps.Keys(contract))
	called := slices.Sorted(maps.Keys(builders))
	require.Equal(t, exported, inContract, "every builder needs an entry in %s, and every entry a builder", contractPath)
	require.Equal(t, exported, called, "every builder needs a placeholder call in this test")
}

func TestBuildersMatchTheContract(t *testing.T) {
	t.Parallel()
	contract := readContract(t)
	site, err := url.Parse("https://app.example.test")
	require.NoError(t, err)

	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want, ok := contract[name]
			require.True(t, ok, "%s is missing from %s", name, contractPath)

			built := build().URL(site)
			require.True(t, strings.HasPrefix(built, "https://app.example.test/"), built)
			require.Equal(t, want, template(t, built))
			require.Equal(t, want, template(t, build().Path()))
		})
	}
}

func TestURLWithoutSiteIsEmpty(t *testing.T) {
	t.Parallel()
	require.Empty(t, Billing("org").URL(nil))
	require.Nil(t, Billing("org").On(nil))
}

func TestOptionalQueryValuesAreLeftOut(t *testing.T) {
	t.Parallel()
	require.Equal(t, "/org/request-access", RequestAccess("org", "", "", "").Path())
	require.Equal(t, "/org/access/roles?grant_user=u&scope=s", AccessGrant("org", "u", "s", "", "").Path())
	require.Equal(t, "/org/projects/p/mcp/srv/settings#authentication", MCPServerAuthentication("org", "p", "srv", "").Path())
}

func TestSegmentsAreEscaped(t *testing.T) {
	t.Parallel()
	require.Equal(t, "/org/projects/p/deployments/a%20b", Deployment("org", "p", "a b").Path())
}
