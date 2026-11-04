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
		s.Block(ScopeOrgBlockedRead, s.BlockSpec{}),
		s.Block(ScopeOrgBlockedAdmin, s.BlockSpec{ImpliedBy: s.Scopes(ScopeOrgBlockedRead)}),

		s.ScopePermission("read", ScopeOrgRead, s.GrantSpec{
			Description: "Read the organization",
			DefaultFor:  adminAndMember,
		}, s.AlsoGrantedBy(ScopeOrgAdmin), s.ExcludedBy(ScopeOrgBlockedRead)),
		s.ScopePermission("administer", ScopeOrgAdmin, s.GrantSpec{
			Description: "Administer the organization",
			DefaultFor:  adminOnly,
		}, s.ExcludedBy(ScopeOrgBlockedAdmin)),
		s.ScopePermission("sync_device_agent", ScopeOrgDeviceAgentSync, s.GrantSpec{
			Description:      "Sync device agents",
			DelegatableSince: 2,
		}, s.AlsoGrantedBy(ScopeOrgAdmin)),
		s.ScopePermission("ingest_hooks", ScopeOrgHooksIngest, s.GrantSpec{
			Description:      "Ingest hook events",
			SelectorKeys:     []string{SelectorKeyProjectID},
			DelegatableSince: 2,
		}, s.AlsoGrantedBy(ScopeOrgAdmin)),
	)
}

// projectType declares projects, plugin publication and tool-call logs, and
// the alternatives that pair a project permission with an organization one.
func projectType() s.TypeDecl {
	return s.NewType("project", s.Parent("organization"), s.GrantSurface(accessSurface),
		s.Block(ScopeProjectBlockedRead, s.BlockSpec{DelegatableSince: 4}),
		s.Block(ScopeProjectBlockedWrite, s.BlockSpec{
			ImpliedBy:        s.Scopes(ScopeProjectBlockedRead),
			DelegatableSince: 4,
		}),
		s.Block(ScopePluginBlockedWrite, s.BlockSpec{}),

		s.ScopePermission("read", ScopeProjectRead, s.GrantSpec{
			Description:      "Read the project",
			DefaultFor:       adminAndMember,
			AnchoredAt:       orgAndSelf,
			DelegatableSince: 1,
		}, s.AlsoGrantedBy(ScopeProjectWrite), s.ExcludedBy(ScopeProjectBlockedRead)),
		s.ScopePermission("write", ScopeProjectWrite, s.GrantSpec{
			Description:      "Change the project",
			DefaultFor:       adminOnly,
			AnchoredAt:       orgAndSelf,
			DelegatableSince: 1,
		}, s.ExcludedBy(ScopeProjectBlockedWrite)),
		s.ScopePermission("publish_plugin", ScopePluginWrite, s.GrantSpec{
			Description: "Publish plugins",
			DefaultFor:  adminOnly,
			AnchoredAt:  orgAndSelf,
		}, s.ExcludedBy(ScopePluginBlockedWrite)),
		s.ScopePermission("read_logs", ScopeLogRead, s.GrantSpec{
			Description: "Read tool-call logs",
			AnchoredAt:  orgAndSelf,
			Sensitive:   true,
		}),

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

// mcpServerType declares MCP servers. Every grant and block carries the tool
// and disposition condition.
func mcpServerType() s.TypeDecl {
	return s.NewType("mcp_server", s.Parent("project"), s.StoredKind(ResourceKindMCP), s.GrantSurface(accessSurface),
		s.SelectorKeys(SelectorKeyProjectID, SelectorKeyTool, SelectorKeyDisposition),
		s.Block(ScopeMCPBlockedRead, s.BlockSpec{Conditions: mcpToolsConditions, DelegatableSince: 4}),
		s.Block(ScopeMCPBlockedWrite, s.BlockSpec{Conditions: mcpToolsConditions, DelegatableSince: 4}),
		s.Block(ScopeMCPBlockedConnect, s.BlockSpec{Conditions: mcpToolsConditions, DelegatableSince: 4}),

		s.ScopePermission("read", ScopeMCPRead, s.GrantSpec{
			Description:      "Inspect server configuration",
			DefaultFor:       adminAndMember,
			AnchoredAt:       orgProjectAndSelf,
			Conditions:       mcpToolsConditions,
			DelegatableSince: 1,
		}, s.AlsoGrantedBy(ScopeMCPWrite), s.ExcludedBy(ScopeMCPBlockedRead)),
		s.ScopePermission("edit", ScopeMCPWrite, s.GrantSpec{
			Description:      "Change server configuration",
			DefaultFor:       adminOnly,
			AnchoredAt:       orgProjectAndSelf,
			Conditions:       mcpToolsConditions,
			DelegatableSince: 1,
		}, s.ExcludedBy(ScopeMCPBlockedWrite)),
		s.ScopePermission("connect", ScopeMCPConnect, s.GrantSpec{
			Description:      "Connect and invoke permitted tools",
			DefaultFor:       adminAndMember,
			AnchoredAt:       orgProjectAndSelf,
			Conditions:       mcpToolsConditions,
			DelegatableSince: 1,
		}, s.AlsoGrantedBy(ScopeMCPRead, ScopeMCPWrite), s.ExcludedBy(ScopeMCPBlockedConnect)),

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
		s.Block(ScopeMCPBlockedAuth, s.BlockSpec{DelegatableSince: 5}),

		s.ScopePermission("use", ScopeMCPAuth, s.GrantSpec{
			Description:      "Use an upstream auth client",
			DefaultFor:       adminAndMember,
			StrictMatching:   true,
			DelegatableSince: 5,
		}, s.ExcludedBy(ScopeMCPBlockedAuth)),
	)
}

// environmentType declares project environments.
func environmentType() s.TypeDecl {
	return s.NewType("environment", s.Parent("project"), s.GrantSurface(accessSurface),
		s.SelectorKeys(SelectorKeyProjectID),
		s.Block(ScopeEnvironmentBlockedRead, s.BlockSpec{DelegatableSince: 4}),
		s.Block(ScopeEnvironmentBlockedWrite, s.BlockSpec{
			ImpliedBy:        s.Scopes(ScopeEnvironmentBlockedRead),
			DelegatableSince: 4,
		}),

		s.ScopePermission("read", ScopeEnvironmentRead, s.GrantSpec{
			Description:      "View environments and their entries",
			DefaultFor:       adminOnly,
			AnchoredAt:       orgProjectAndSelf,
			DelegatableSince: 1,
		}, s.AlsoGrantedBy(ScopeEnvironmentWrite), s.ExcludedBy(ScopeEnvironmentBlockedRead)),
		s.ScopePermission("write", ScopeEnvironmentWrite, s.GrantSpec{
			Description:      "Add, edit, clone and remove environments",
			DefaultFor:       adminOnly,
			AnchoredAt:       orgProjectAndSelf,
			DelegatableSince: 1,
		}, s.ExcludedBy(ScopeEnvironmentBlockedWrite)),
	)
}

// riskPolicyType declares risk policies. Their grants are written by the risk
// policy surface, and evaluation subtracts bypass grants rather than using
// blocks.
func riskPolicyType() s.TypeDecl {
	return s.NewType("risk_policy", s.StoredKind(ResourceKindRiskPolicy), s.GrantSurface(riskPolicySurface),
		s.SelectorKeys(SelectorKeyServerIdentity, SelectorKeyServerURL),
		s.Grant(ScopeRiskPolicyEvaluate, s.GrantSpec{
			Description:      "Apply the policy",
			DelegatableSince: 1,
		}),
		s.Permission("evaluate", s.Minus(s.Rel(ScopeRiskPolicyEvaluate), s.Rel(ScopeRiskPolicyBypass)),
			s.AnswersScope(ScopeRiskPolicyEvaluate)),

		s.ScopePermission("bypass", ScopeRiskPolicyBypass, s.GrantSpec{Description: "Skip the policy"}),
		s.ScopePermission("block", ScopeRiskPolicyBlock, s.GrantSpec{Description: "Block with the policy"}),
	)
}

// skillType declares skills.
func skillType() s.TypeDecl {
	return s.NewType("skill", s.Parent("project"), s.GrantSurface(accessSurface),
		s.Block(ScopeSkillBlockedRead, s.BlockSpec{DelegatableSince: 4}),
		s.Block(ScopeSkillBlockedWrite, s.BlockSpec{
			ImpliedBy:        s.Scopes(ScopeSkillBlockedRead),
			DelegatableSince: 4,
		}),

		s.ScopePermission("read", ScopeSkillRead, s.GrantSpec{
			Description:      "Read skills",
			DefaultFor:       adminAndMember,
			AnchoredAt:       orgProjectAndSelf,
			DelegatableSince: 1,
		}, s.AlsoGrantedBy(ScopeSkillWrite), s.ExcludedBy(ScopeSkillBlockedRead)),
		s.ScopePermission("write", ScopeSkillWrite, s.GrantSpec{
			Description:      "Change skills",
			DefaultFor:       adminOnly,
			AnchoredAt:       orgProjectAndSelf,
			DelegatableSince: 1,
		}, s.ExcludedBy(ScopeSkillBlockedWrite)),

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
		s.Block(ScopeAssistantBlockedRead, s.BlockSpec{DelegatableSince: 4}),
		s.Block(ScopeAssistantBlockedWrite, s.BlockSpec{
			ImpliedBy:        s.Scopes(ScopeAssistantBlockedRead),
			DelegatableSince: 4,
		}),

		s.ScopePermission("read", ScopeAssistantRead, s.GrantSpec{
			Description:      "Read assistants",
			DefaultFor:       adminAndMember,
			AnchoredAt:       orgProjectAndSelf,
			DelegatableSince: 3,
		}, s.AlsoGrantedBy(ScopeAssistantWrite), s.ExcludedBy(ScopeAssistantBlockedRead)),
		s.ScopePermission("write", ScopeAssistantWrite, s.GrantSpec{
			Description:      "Change assistants",
			DefaultFor:       adminOnly,
			AnchoredAt:       orgProjectAndSelf,
			DelegatableSince: 3,
		}, s.ExcludedBy(ScopeAssistantBlockedWrite)),
	)
}

// inferenceType declares inference run through Speakeasy by workloads.
func inferenceType() s.TypeDecl {
	return s.NewType("inference", s.Parent("project"), s.GrantSurface(accessSurface),
		s.SelectorKeys(SelectorKeyProjectID),
		s.ScopePermission("execute", ScopeInferenceExecute, s.GrantSpec{
			Description:      "Run inference through Speakeasy as a workload",
			AnchoredAt:       []string{"organization", "project"},
			DelegatableSince: 6,
		}),
	)
}

// chatType declares session transcripts. The permissions answer the stored
// grants only; the service decides ownership.
func chatType() s.TypeDecl {
	return s.NewType("chat", s.Parent("project"), s.GrantSurface(accessSurface),
		s.ScopePermission("read_grant", ScopeChatRead, s.GrantSpec{
			Description: "Read session transcripts",
			AnchoredAt:  orgProjectAndSelf,
			Sensitive:   true,
		}, s.AlsoGrantedBy(ScopeChatWrite)),
		s.ScopePermission("write_grant", ScopeChatWrite, s.GrantSpec{
			Description: "Change session transcripts",
			AnchoredAt:  orgProjectAndSelf,
		}),
	)
}
