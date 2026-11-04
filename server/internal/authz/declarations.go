package authz

import (
	"fmt"

	s "github.com/speakeasy-api/gram/server/internal/authz/schema"
)

// DefaultRegistryConfig is the compile configuration of the default registry.
func DefaultRegistryConfig() s.Config {
	return s.Config{
		Roles:         []string{SystemRoleAdmin, SystemRoleMember},
		GrantSurfaces: []string{string(GrantSurfaceAccess), string(GrantSurfaceRiskPolicy)},
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
	return []s.TypeDecl{
		organizationType(),
		projectType(),
		mcpServerType(),
		mcpAuthClientType(),
		environmentType(),
		riskPolicyType(),
		skillType(),
		assistantType(),
		inferenceType(),
		chatType(),
	}
}

var (
	accessSurface      = string(GrantSurfaceAccess)
	riskPolicySurface  = string(GrantSurfaceRiskPolicy)
	adminAndMember     = []string{SystemRoleAdmin, SystemRoleMember}
	adminOnly          = []string{SystemRoleAdmin}
	orgAndSelf         = []string{"organization", s.Self}
	orgProjectAndSelf  = []string{"organization", "project", s.Self}
	mcpToolsConditions = []string{"mcp_tools"}
)

// organizationType declares the organization and its organization-wide
// scopes.
func organizationType() s.TypeDecl {
	return s.NewType("organization", s.StoredKind(ResourceKindOrg), s.GrantSurface(accessSurface),
		s.Grant("reader", s.GrantSpec{
			StoredAs:    string(ScopeOrgRead),
			Description: "Read the organization",
			DefaultFor:  adminAndMember,
		}),
		s.Grant("admin", s.GrantSpec{
			StoredAs:    string(ScopeOrgAdmin),
			Description: "Administer the organization",
			DefaultFor:  adminOnly,
		}),
		s.Grant("device_agent_sync", s.GrantSpec{
			StoredAs:         string(ScopeOrgDeviceAgentSync),
			Description:      "Sync device agents",
			DelegatableSince: 2,
		}),
		s.Grant("hooks_ingest", s.GrantSpec{
			StoredAs:         string(ScopeOrgHooksIngest),
			Description:      "Ingest hook events",
			SelectorKeys:     []string{SelectorKeyProjectID},
			DelegatableSince: 2,
		}),

		s.Block("read_blocked", s.BlockSpec{StoredAs: string(ScopeOrgBlockedRead)}),
		s.Block("admin_blocked", s.BlockSpec{
			StoredAs:  string(ScopeOrgBlockedAdmin),
			ImpliedBy: []string{"read_blocked"},
		}),

		s.Permission("read", s.Union(s.Rel("reader"), s.Rel("admin")),
			s.AnswersScope(string(ScopeOrgRead)), s.ExcludedBy("read_blocked")),
		s.Permission("administer", s.Rel("admin"),
			s.AnswersScope(string(ScopeOrgAdmin)), s.ExcludedBy("admin_blocked")),
		s.Permission("sync_device_agent", s.Union(s.Rel("device_agent_sync"), s.Rel("admin")),
			s.AnswersScope(string(ScopeOrgDeviceAgentSync))),
		s.Permission("ingest_hooks", s.Union(s.Rel("hooks_ingest"), s.Rel("admin")),
			s.AnswersScope(string(ScopeOrgHooksIngest))),
	)
}

// projectType declares projects, plugin publication and tool-call logs, and
// the alternatives that pair a project permission with an organization one.
func projectType() s.TypeDecl {
	return s.NewType("project", s.Parent("organization"), s.GrantSurface(accessSurface),
		s.Grant("reader", s.GrantSpec{
			StoredAs:         string(ScopeProjectRead),
			Description:      "Read the project",
			DefaultFor:       adminAndMember,
			AnchoredAt:       orgAndSelf,
			DelegatableSince: 1,
		}),
		s.Grant("writer", s.GrantSpec{
			StoredAs:         string(ScopeProjectWrite),
			Description:      "Change the project",
			DefaultFor:       adminOnly,
			AnchoredAt:       orgAndSelf,
			DelegatableSince: 1,
		}),
		s.Grant("plugin_publisher", s.GrantSpec{
			StoredAs:    string(ScopePluginWrite),
			Description: "Publish plugins",
			DefaultFor:  adminOnly,
			AnchoredAt:  orgAndSelf,
		}),
		s.Grant("log_reader", s.GrantSpec{
			StoredAs:    string(ScopeLogRead),
			Description: "Read tool-call logs",
			AnchoredAt:  orgAndSelf,
			Sensitive:   true,
		}),

		s.Block("read_blocked", s.BlockSpec{
			StoredAs:         string(ScopeProjectBlockedRead),
			DelegatableSince: 4,
		}),
		s.Block("write_blocked", s.BlockSpec{
			StoredAs:         string(ScopeProjectBlockedWrite),
			ImpliedBy:        []string{"read_blocked"},
			DelegatableSince: 4,
		}),
		s.Block("plugin_write_blocked", s.BlockSpec{StoredAs: string(ScopePluginBlockedWrite)}),

		s.Permission("read", s.Union(s.Rel("reader"), s.Rel("writer")),
			s.AnswersScope(string(ScopeProjectRead)), s.ExcludedBy("read_blocked")),
		s.Permission("write", s.Rel("writer"),
			s.AnswersScope(string(ScopeProjectWrite)), s.ExcludedBy("write_blocked")),
		s.Permission("publish_plugin", s.Rel("plugin_publisher"),
			s.AnswersScope(string(ScopePluginWrite)), s.ExcludedBy("plugin_write_blocked")),
		s.Permission("read_logs", s.Rel("log_reader"), s.AnswersScope(string(ScopeLogRead))),

		s.Alternatives("plugin_publication", s.AlternativesSpec{
			Options: []s.PermissionRef{
				{Type: "project", Permission: "publish_plugin"},
				{Type: "organization", Permission: "administer"},
			},
			Blocks:          s.AnyBlockVetoes,
			AcrossInstances: false,
		}),
		s.Alternatives("read_or_organization_read", s.AlternativesSpec{
			Options: []s.PermissionRef{
				{Type: "organization", Permission: "read"},
				{Type: "project", Permission: "read"},
			},
			Blocks:          s.BlocksApplyPerOption,
			AcrossInstances: false,
		}),
		s.Alternatives("write_or_administer", s.AlternativesSpec{
			Options: []s.PermissionRef{
				{Type: "project", Permission: "write"},
				{Type: "organization", Permission: "administer"},
			},
			Blocks:          s.BlocksApplyPerOption,
			AcrossInstances: false,
		}),
	)
}

// mcpServerType declares MCP servers. Every relation carries the tool and
// disposition condition.
func mcpServerType() s.TypeDecl {
	return s.NewType("mcp_server", s.Parent("project"), s.StoredKind(ResourceKindMCP), s.GrantSurface(accessSurface),
		s.SelectorKeys(SelectorKeyProjectID, SelectorKeyTool, SelectorKeyDisposition),
		s.Grant("viewer", s.GrantSpec{
			StoredAs:         string(ScopeMCPRead),
			Description:      "Inspect server configuration",
			DefaultFor:       adminAndMember,
			AnchoredAt:       orgProjectAndSelf,
			Conditions:       mcpToolsConditions,
			DelegatableSince: 1,
		}),
		s.Grant("editor", s.GrantSpec{
			StoredAs:         string(ScopeMCPWrite),
			Description:      "Change server configuration",
			DefaultFor:       adminOnly,
			AnchoredAt:       orgProjectAndSelf,
			Conditions:       mcpToolsConditions,
			DelegatableSince: 1,
		}),
		s.Grant("connector", s.GrantSpec{
			StoredAs:         string(ScopeMCPConnect),
			Description:      "Connect and invoke permitted tools",
			DefaultFor:       adminAndMember,
			AnchoredAt:       orgProjectAndSelf,
			Conditions:       mcpToolsConditions,
			DelegatableSince: 1,
		}),

		s.Block("read_blocked", s.BlockSpec{
			StoredAs:         string(ScopeMCPBlockedRead),
			Conditions:       mcpToolsConditions,
			DelegatableSince: 4,
		}),
		s.Block("write_blocked", s.BlockSpec{
			StoredAs:         string(ScopeMCPBlockedWrite),
			Conditions:       mcpToolsConditions,
			DelegatableSince: 4,
		}),
		s.Block("connect_blocked", s.BlockSpec{
			StoredAs:         string(ScopeMCPBlockedConnect),
			Conditions:       mcpToolsConditions,
			DelegatableSince: 4,
		}),

		s.Permission("read", s.Union(s.Rel("viewer"), s.Rel("editor")),
			s.AnswersScope(string(ScopeMCPRead)), s.ExcludedBy("read_blocked")),
		s.Permission("edit", s.Rel("editor"),
			s.AnswersScope(string(ScopeMCPWrite)), s.ExcludedBy("write_blocked")),
		s.Permission("connect", s.Union(s.Rel("connector"), s.Rel("viewer"), s.Rel("editor")),
			s.AnswersScope(string(ScopeMCPConnect)), s.ExcludedBy("connect_blocked")),

		s.Alternatives("project_read_or_edit", s.AlternativesSpec{
			Options: []s.PermissionRef{
				{Type: "project", Permission: "read"},
				{Type: "mcp_server", Permission: "edit"},
			},
			Blocks:          s.BlocksApplyPerOption,
			AcrossInstances: false,
		}),
	)
}

// mcpAuthClientType declares upstream MCP auth clients.
func mcpAuthClientType() s.TypeDecl {
	return s.NewType("mcp_auth_client", s.Parent("organization"), s.StoredKind(ResourceKindMCPAuthClient), s.GrantSurface(accessSurface),
		s.SelectorKeys(SelectorKeyClientType, SelectorKeyConnectionOwner, SelectorKeyProjectID, SelectorKeyUpstreamClient),
		s.Grant("user", s.GrantSpec{
			StoredAs:         string(ScopeMCPAuth),
			Description:      "Use an upstream auth client",
			DefaultFor:       adminAndMember,
			StrictMatching:   true,
			DelegatableSince: 5,
		}),

		s.Block("use_blocked", s.BlockSpec{
			StoredAs:         string(ScopeMCPBlockedAuth),
			DelegatableSince: 5,
		}),

		s.Permission("use", s.Rel("user"),
			s.AnswersScope(string(ScopeMCPAuth)), s.ExcludedBy("use_blocked")),
	)
}

// environmentType declares project environments.
func environmentType() s.TypeDecl {
	return s.NewType("environment", s.Parent("project"), s.GrantSurface(accessSurface),
		s.SelectorKeys(SelectorKeyProjectID),
		s.Grant("reader", s.GrantSpec{
			StoredAs:         string(ScopeEnvironmentRead),
			Description:      "View environments and their entries",
			DefaultFor:       adminOnly,
			AnchoredAt:       orgProjectAndSelf,
			DelegatableSince: 1,
		}),
		s.Grant("writer", s.GrantSpec{
			StoredAs:         string(ScopeEnvironmentWrite),
			Description:      "Add, edit, clone and remove environments",
			DefaultFor:       adminOnly,
			AnchoredAt:       orgProjectAndSelf,
			DelegatableSince: 1,
		}),

		s.Block("read_blocked", s.BlockSpec{
			StoredAs:         string(ScopeEnvironmentBlockedRead),
			DelegatableSince: 4,
		}),
		s.Block("write_blocked", s.BlockSpec{
			StoredAs:         string(ScopeEnvironmentBlockedWrite),
			ImpliedBy:        []string{"read_blocked"},
			DelegatableSince: 4,
		}),

		s.Permission("read", s.Union(s.Rel("reader"), s.Rel("writer")),
			s.AnswersScope(string(ScopeEnvironmentRead)), s.ExcludedBy("read_blocked")),
		s.Permission("write", s.Rel("writer"),
			s.AnswersScope(string(ScopeEnvironmentWrite)), s.ExcludedBy("write_blocked")),
	)
}

// riskPolicyType declares risk policies. Their grants are written by the risk
// policy surface, and evaluation subtracts bypass grants rather than using
// blocks.
func riskPolicyType() s.TypeDecl {
	return s.NewType("risk_policy", s.StoredKind(ResourceKindRiskPolicy), s.GrantSurface(riskPolicySurface),
		s.SelectorKeys(SelectorKeyServerIdentity, SelectorKeyServerURL),
		s.Grant("included", s.GrantSpec{
			StoredAs:         string(ScopeRiskPolicyEvaluate),
			Description:      "Apply the policy",
			DelegatableSince: 1,
		}),
		s.Grant("bypassed", s.GrantSpec{
			StoredAs:    string(ScopeRiskPolicyBypass),
			Description: "Skip the policy",
		}),
		s.Grant("blocker", s.GrantSpec{
			StoredAs:    string(ScopeRiskPolicyBlock),
			Description: "Block with the policy",
		}),

		s.Permission("evaluate", s.Minus(s.Rel("included"), s.Rel("bypassed")),
			s.AnswersScope(string(ScopeRiskPolicyEvaluate))),
		s.Permission("bypass", s.Rel("bypassed"), s.AnswersScope(string(ScopeRiskPolicyBypass))),
		s.Permission("block", s.Rel("blocker"), s.AnswersScope(string(ScopeRiskPolicyBlock))),
	)
}

// skillType declares skills.
func skillType() s.TypeDecl {
	return s.NewType("skill", s.Parent("project"), s.GrantSurface(accessSurface),
		s.Grant("reader", s.GrantSpec{
			StoredAs:         string(ScopeSkillRead),
			Description:      "Read skills",
			DefaultFor:       adminAndMember,
			AnchoredAt:       orgProjectAndSelf,
			DelegatableSince: 1,
		}),
		s.Grant("writer", s.GrantSpec{
			StoredAs:         string(ScopeSkillWrite),
			Description:      "Change skills",
			DefaultFor:       adminOnly,
			AnchoredAt:       orgProjectAndSelf,
			DelegatableSince: 1,
		}),

		s.Block("read_blocked", s.BlockSpec{
			StoredAs:         string(ScopeSkillBlockedRead),
			DelegatableSince: 4,
		}),
		s.Block("write_blocked", s.BlockSpec{
			StoredAs:         string(ScopeSkillBlockedWrite),
			ImpliedBy:        []string{"read_blocked"},
			DelegatableSince: 4,
		}),

		s.Permission("read", s.Union(s.Rel("reader"), s.Rel("writer")),
			s.AnswersScope(string(ScopeSkillRead)), s.ExcludedBy("read_blocked")),
		s.Permission("write", s.Rel("writer"),
			s.AnswersScope(string(ScopeSkillWrite)), s.ExcludedBy("write_blocked")),

		s.Alternatives("project_or_skill_read", s.AlternativesSpec{
			Options: []s.PermissionRef{
				{Type: "skill", Permission: "read"},
				{Type: "skill", Permission: "read"},
			},
			Blocks:          s.AnyBlockVetoes,
			AcrossInstances: true,
		}),
	)
}

// assistantType declares assistants.
func assistantType() s.TypeDecl {
	return s.NewType("assistant", s.Parent("project"), s.GrantSurface(accessSurface),
		s.SelectorKeys(SelectorKeyProjectID),
		s.Grant("reader", s.GrantSpec{
			StoredAs:         string(ScopeAssistantRead),
			Description:      "Read assistants",
			DefaultFor:       adminAndMember,
			AnchoredAt:       orgProjectAndSelf,
			DelegatableSince: 3,
		}),
		s.Grant("writer", s.GrantSpec{
			StoredAs:         string(ScopeAssistantWrite),
			Description:      "Change assistants",
			DefaultFor:       adminOnly,
			AnchoredAt:       orgProjectAndSelf,
			DelegatableSince: 3,
		}),

		s.Block("read_blocked", s.BlockSpec{
			StoredAs:         string(ScopeAssistantBlockedRead),
			DelegatableSince: 4,
		}),
		s.Block("write_blocked", s.BlockSpec{
			StoredAs:         string(ScopeAssistantBlockedWrite),
			ImpliedBy:        []string{"read_blocked"},
			DelegatableSince: 4,
		}),

		s.Permission("read", s.Union(s.Rel("reader"), s.Rel("writer")),
			s.AnswersScope(string(ScopeAssistantRead)), s.ExcludedBy("read_blocked")),
		s.Permission("write", s.Rel("writer"),
			s.AnswersScope(string(ScopeAssistantWrite)), s.ExcludedBy("write_blocked")),
	)
}

// inferenceType declares inference run through Speakeasy by workloads.
func inferenceType() s.TypeDecl {
	return s.NewType("inference", s.Parent("project"), s.GrantSurface(accessSurface),
		s.SelectorKeys(SelectorKeyProjectID),
		s.Grant("executor", s.GrantSpec{
			StoredAs:         string(ScopeInferenceExecute),
			Description:      "Run inference through Speakeasy as a workload",
			AnchoredAt:       []string{"organization", "project"},
			DelegatableSince: 6,
		}),

		s.Permission("execute", s.Rel("executor"), s.AnswersScope(string(ScopeInferenceExecute))),
	)
}

// chatType declares session transcripts. The permissions answer the stored
// grants only; the service decides ownership.
func chatType() s.TypeDecl {
	return s.NewType("chat", s.Parent("project"), s.GrantSurface(accessSurface),
		s.Grant("reader", s.GrantSpec{
			StoredAs:    string(ScopeChatRead),
			Description: "Read session transcripts",
			AnchoredAt:  orgProjectAndSelf,
			Sensitive:   true,
		}),
		s.Grant("writer", s.GrantSpec{
			StoredAs:    string(ScopeChatWrite),
			Description: "Change session transcripts",
			AnchoredAt:  orgProjectAndSelf,
		}),

		s.Permission("read_grant", s.Union(s.Rel("reader"), s.Rel("writer")), s.AnswersScope(string(ScopeChatRead))),
		s.Permission("write_grant", s.Rel("writer"), s.AnswersScope(string(ScopeChatWrite))),
	)
}
