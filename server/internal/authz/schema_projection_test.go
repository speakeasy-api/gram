package authz

import (
	"bufio"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	s "github.com/speakeasy-api/gram/server/internal/authz/schema"
)

// TestSchemaProjectionsMatchFrozenTables declares the organization, project,
// MCP server and MCP auth client scopes with the schema DSL and compares the
// compiled projections with the frozen baselines: scope_tables.golden and the
// runtime policy's runtime_scopes.golden. The declarations exist only in this
// test. They show that the DSL can reproduce today's tables for a slice and
// that compiled scope names line up with the authz.Scope constants; the
// production tables stay authoritative until compiled projections replace
// them.
func TestSchemaProjectionsMatchFrozenTables(t *testing.T) {
	t.Parallel()

	reg, err := s.Compile(s.Config{
		Roles:         []string{SystemRoleAdmin, SystemRoleMember},
		WriteSurfaces: []string{string(GrantSurfaceAccess), string(GrantSurfaceRiskPolicy)},
		Conditions: []s.Condition{
			{Name: "mcp_tools", SelectorKeys: []string{SelectorKeyTool, SelectorKeyDisposition}},
		},
		RetiredRuntimeScopes: []string{"mcp_approval:decide", "mcp_approval:read"},
	}, frozenSliceDeclarations()...)
	require.NoError(t, err)

	declared := []Scope{
		ScopeLogRead,
		ScopeMCPAuth, ScopeMCPBlockedAuth, ScopeMCPBlockedConnect, ScopeMCPBlockedRead, ScopeMCPBlockedWrite,
		ScopeMCPConnect, ScopeMCPRead, ScopeMCPWrite,
		ScopeOrgAdmin, ScopeOrgBlockedAdmin, ScopeOrgBlockedRead, ScopeOrgDeviceAgentSync, ScopeOrgHooksIngest, ScopeOrgRead,
		ScopePluginBlockedWrite, ScopePluginWrite,
		ScopeProjectBlockedRead, ScopeProjectBlockedWrite, ScopeProjectRead, ScopeProjectWrite,
	}
	var compiledNames []Scope
	for _, scope := range reg.Scopes() {
		compiledNames = append(compiledNames, Scope(scope.Name))
	}
	require.Equal(t, declared, compiledNames, "the registry must compile exactly the declared slice")

	var runtimeNames []string
	for _, scope := range reg.RuntimeScopes() {
		runtimeNames = append(runtimeNames, scope.Name)
	}
	wantRuntimeNames := []string{"mcp_approval:decide", "mcp_approval:read"}
	for _, scope := range declared {
		wantRuntimeNames = append(wantRuntimeNames, string(scope))
	}
	require.ElementsMatch(t, wantRuntimeNames, runtimeNames)

	frozenScopes := readGoldenRecords(t, filepath.Join(foundationCompatibilityDir, "scope_tables.golden"))
	var got, want strings.Builder
	for _, scope := range reg.Scopes() {
		require.Contains(t, RegisteredScopes(), Scope(scope.Name), "compiled scope %s has no authz.Scope constant", scope.Name)
		record, ok := frozenScopes[scope.Name]
		require.True(t, ok, "compiled scope %s is missing from the frozen scope tables", scope.Name)
		want.WriteString(record)
		got.WriteString(renderCompiledScope(scope))
	}
	require.Equal(t, want.String(), got.String())

	frozenRuntime := readGoldenLines(t, filepath.Join("..", "agents", "runtimepolicy", "testdata", "runtime_scopes.golden"))
	var gotRuntime, wantRuntime []string
	for _, scope := range reg.RuntimeScopes() {
		line, ok := frozenRuntime[scope.Name]
		require.True(t, ok, "runtime scope %s is missing from the frozen runtime scopes", scope.Name)
		wantRuntime = append(wantRuntime, line)
		gotRuntime = append(gotRuntime, fmt.Sprintf("%s lifecycle=%s safe_since=%d", scope.Name, scope.Lifecycle, scope.SafeSince))
	}
	require.Equal(t, wantRuntime, gotRuntime)
}

func renderCompiledScope(scope s.ScopeDefinition) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", scope.Name)
	fmt.Fprintf(&b, "  visibility: %s\n", scope.Visibility)
	fmt.Fprintf(&b, "  resource_kind: %s\n", scope.ResourceKind)
	fmt.Fprintf(&b, "  surface: %s\n", cmp.Or(scope.WrittenBy, "-"))
	fmt.Fprintf(&b, "  satisfied_by: %v\n", scope.SatisfiedBy)
	fmt.Fprintf(&b, "  exclusion: %s\n", cmp.Or(scope.Exclusion, "-"))
	fmt.Fprintf(&b, "  closure: %v\n", scope.Closure)
	fmt.Fprintf(&b, "  sub_scopes: %v\n", scope.SubScopes)
	fmt.Fprintf(&b, "  selector_keys: %v\n", scope.SelectorKeys)
	fmt.Fprintf(&b, "  roles: %v\n", scope.Roles)
	return b.String()
}

// readGoldenRecords splits scope_tables.golden into one record per scope.
func readGoldenRecords(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	records := make(map[string]string)
	var name string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, " ") {
			name = line
		}
		records[name] += line + "\n"
	}
	require.NoError(t, scanner.Err())
	return records
}

// readGoldenLines indexes runtime_scopes.golden by scope.
func readGoldenLines(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := make(map[string]string)
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		name, _, _ := strings.Cut(line, " ")
		lines[name] = line
	}
	return lines
}

func frozenSliceDeclarations() []s.TypeDecl {
	access := string(GrantSurfaceAccess)
	return []s.TypeDecl{
		s.NewType("organization", s.StoredKind(ResourceKindOrg), s.WrittenBy(access),
			s.Grant("reader", s.StoredAs(string(ScopeOrgRead)),
				s.Describe("Read the organization"), s.DefaultFor(SystemRoleAdmin, SystemRoleMember)),
			s.Grant("admin", s.StoredAs(string(ScopeOrgAdmin)),
				s.Describe("Administer the organization"), s.DefaultFor(SystemRoleAdmin)),
			s.Grant("device_agent_sync", s.StoredAs(string(ScopeOrgDeviceAgentSync)),
				s.Describe("Sync device agents"), s.RuntimeSafeSince(2)),
			s.Grant("hooks_ingest", s.StoredAs(string(ScopeOrgHooksIngest)),
				s.SelectorKeys(SelectorKeyProjectID),
				s.Describe("Ingest hook events"), s.RuntimeSafeSince(2)),
			s.Block("read_blocked", s.StoredAs(string(ScopeOrgBlockedRead))),
			s.Block("admin_blocked", s.StoredAs(string(ScopeOrgBlockedAdmin))),
			s.Permission("read", s.Union(s.Rel("reader"), s.Rel("admin")),
				s.Checks(string(ScopeOrgRead)), s.ExcludedBy("read_blocked")),
			s.Permission("administer", s.Rel("admin"),
				s.Checks(string(ScopeOrgAdmin)), s.ExcludedBy("admin_blocked", "read_blocked")),
			s.Permission("sync_device_agent", s.Union(s.Rel("device_agent_sync"), s.Rel("admin")),
				s.Checks(string(ScopeOrgDeviceAgentSync))),
			s.Permission("ingest_hooks", s.Union(s.Rel("hooks_ingest"), s.Rel("admin")),
				s.Checks(string(ScopeOrgHooksIngest))),
		),
		s.NewType("project", s.Parent("organization"), s.WrittenBy(access),
			s.Grant("reader", s.StoredAs(string(ScopeProjectRead)), s.AnchoredAt("organization", s.Self),
				s.Describe("Read the project"), s.DefaultFor(SystemRoleAdmin, SystemRoleMember), s.RuntimeSafeSince(1)),
			s.Grant("writer", s.StoredAs(string(ScopeProjectWrite)), s.AnchoredAt("organization", s.Self),
				s.Describe("Change the project"), s.DefaultFor(SystemRoleAdmin), s.RuntimeSafeSince(1)),
			s.Grant("plugin_publisher", s.StoredAs(string(ScopePluginWrite)), s.AnchoredAt("organization", s.Self),
				s.Describe("Publish plugins"), s.DefaultFor(SystemRoleAdmin)),
			s.Grant("log_reader", s.StoredAs(string(ScopeLogRead)), s.AnchoredAt("organization", s.Self),
				s.Describe("Read tool-call logs"), s.Sensitive()),
			s.Block("read_blocked", s.StoredAs(string(ScopeProjectBlockedRead)), s.RuntimeSafeSince(4)),
			s.Block("write_blocked", s.StoredAs(string(ScopeProjectBlockedWrite)), s.RuntimeSafeSince(4)),
			s.Block("plugin_write_blocked", s.StoredAs(string(ScopePluginBlockedWrite))),
			s.Permission("read", s.Union(s.Rel("reader"), s.Rel("writer")),
				s.Checks(string(ScopeProjectRead)), s.ExcludedBy("read_blocked")),
			s.Permission("write", s.Rel("writer"),
				s.Checks(string(ScopeProjectWrite)), s.ExcludedBy("write_blocked", "read_blocked")),
			s.Permission("publish_plugin", s.Rel("plugin_publisher"),
				s.Checks(string(ScopePluginWrite)), s.ExcludedBy("plugin_write_blocked")),
			s.Permission("read_logs", s.Rel("log_reader"), s.Checks(string(ScopeLogRead))),
			s.Alternatives("plugin_publication",
				s.Alternative("project", "publish_plugin"),
				s.Alternative("organization", "administer"),
				s.VetoBlocks()),
		),
		s.NewType("mcp_server", s.Parent("project"), s.StoredKind(ResourceKindMCP), s.WrittenBy(access),
			s.SelectorKeys(SelectorKeyProjectID, SelectorKeyTool, SelectorKeyDisposition),
			s.Grant("viewer", s.StoredAs(string(ScopeMCPRead)),
				s.AnchoredAt("organization", "project", s.Self), s.Conditions("mcp_tools"),
				s.Describe("Inspect server configuration"),
				s.DefaultFor(SystemRoleAdmin, SystemRoleMember), s.RuntimeSafeSince(1)),
			s.Grant("editor", s.StoredAs(string(ScopeMCPWrite)),
				s.AnchoredAt("organization", "project", s.Self), s.Conditions("mcp_tools"),
				s.Describe("Change server configuration"),
				s.DefaultFor(SystemRoleAdmin), s.RuntimeSafeSince(1)),
			s.Grant("connector", s.StoredAs(string(ScopeMCPConnect)),
				s.AnchoredAt("organization", "project", s.Self), s.Conditions("mcp_tools"),
				s.Describe("Connect and invoke permitted tools"),
				s.DefaultFor(SystemRoleAdmin, SystemRoleMember), s.RuntimeSafeSince(1)),
			s.Block("read_blocked", s.StoredAs(string(ScopeMCPBlockedRead)),
				s.Conditions("mcp_tools"), s.RuntimeSafeSince(4)),
			s.Block("write_blocked", s.StoredAs(string(ScopeMCPBlockedWrite)),
				s.Conditions("mcp_tools"), s.RuntimeSafeSince(4)),
			s.Block("connect_blocked", s.StoredAs(string(ScopeMCPBlockedConnect)),
				s.Conditions("mcp_tools"), s.RuntimeSafeSince(4)),
			s.Permission("read", s.Union(s.Rel("viewer"), s.Rel("editor")),
				s.Checks(string(ScopeMCPRead)), s.ExcludedBy("read_blocked")),
			s.Permission("edit", s.Rel("editor"),
				s.Checks(string(ScopeMCPWrite)), s.ExcludedBy("write_blocked")),
			s.Permission("connect", s.Union(s.Rel("connector"), s.Rel("viewer"), s.Rel("editor")),
				s.Checks(string(ScopeMCPConnect)), s.ExcludedBy("connect_blocked")),
		),
		s.NewType("mcp_auth_client", s.Parent("organization"), s.StoredKind(ResourceKindMCPAuthClient), s.WrittenBy(access),
			s.SelectorKeys(SelectorKeyClientType, SelectorKeyConnectionOwner, SelectorKeyProjectID, SelectorKeyUpstreamClient),
			s.Grant("user", s.StoredAs(string(ScopeMCPAuth)), s.StrictMatching(),
				s.Describe("Use an upstream auth client"),
				s.DefaultFor(SystemRoleAdmin, SystemRoleMember), s.RuntimeSafeSince(5)),
			s.Block("use_blocked", s.StoredAs(string(ScopeMCPBlockedAuth)), s.RuntimeSafeSince(5)),
			s.Permission("use", s.Rel("user"),
				s.Checks(string(ScopeMCPAuth)), s.ExcludedBy("use_blocked")),
		),
	}
}
