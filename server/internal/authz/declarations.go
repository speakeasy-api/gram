package authz

import (
	"fmt"

	s "github.com/speakeasy-api/gram/server/internal/authz/schema"
)

// DefaultRegistryConfig is the compile configuration of the default registry.
func DefaultRegistryConfig() s.Config {
	return s.Config{
		Roles:         []string{SystemRoleAdmin, SystemRoleMember},
		WriteSurfaces: []string{string(GrantSurfaceAccess), string(GrantSurfaceRiskPolicy)},
		Conditions: []s.Condition{
			{Name: "mcp_tools", SelectorKeys: []string{SelectorKeyTool, SelectorKeyDisposition}},
		},
		RetiredRuntimeScopes: []string{"mcp_approval:decide", "mcp_approval:read"},
	}
}

// DefaultRegistry compiles the default declarations. A compile error is a
// programming error in the declarations and must stop startup rather than
// leave checks unevaluated.
func DefaultRegistry() (*s.Registry, error) {
	reg, err := s.Compile(DefaultRegistryConfig(), DefaultDeclarations()...)
	if err != nil {
		return nil, fmt.Errorf("compile default authorization registry: %w", err)
	}
	return reg, nil
}

// DefaultDeclarations are the resource declarations of the default registry.
// A type joins once its consumers migrate to the typed client; until then its
// checks keep running through Engine against the scope tables. Every compiled
// scope must reproduce its frozen scope table entry.
func DefaultDeclarations() []s.TypeDecl {
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
