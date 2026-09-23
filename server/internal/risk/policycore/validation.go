package policycore

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/recommendedscopes"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

var (
	approvedDomainFormat = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	customRuleIDFormat   = regexp.MustCompile(`^custom\.[a-z0-9_]+$`)
)

// ValidationError separates a stable client-facing validation message from an
// underlying technical cause retained for logs and diagnostics.
type ValidationError struct {
	Message string
	Cause   error
}

func (e *ValidationError) Error() string {
	return e.Message
}

func (e *ValidationError) Unwrap() error {
	return e.Cause
}

// DetectionScopeInput preserves nullable transport fields without depending on
// a generated API type.
type DetectionScopeInput struct {
	Category     string
	ScopeInclude *string
	ScopeExempt  *string
}

// MCPScopeInput preserves transport strings until UUID and tool validation.
type MCPScopeInput struct {
	Servers []*MCPServerScopeInput
}

// MCPServerScopeInput is one server or gateway selection.
type MCPServerScopeInput struct {
	MCPServerID string
	Tools       []string
}

// NormalizeMCPScope validates and canonicalizes an MCP policy scope. An
// explicit empty server list clears the restriction and therefore returns nil.
func NormalizeMCPScope(input *MCPScopeInput) (*MCPScope, error) {
	if input == nil || len(input.Servers) == 0 {
		return nil, nil
	}

	scope := &MCPScope{Servers: make([]MCPServerScope, 0, len(input.Servers))}
	seenServers := make(map[uuid.UUID]struct{}, len(input.Servers))
	for _, server := range input.Servers {
		if server == nil {
			return nil, fmt.Errorf("MCP server scope must not be null")
		}
		serverID, err := uuid.Parse(server.MCPServerID)
		if err != nil {
			return nil, fmt.Errorf("MCP server id %q is not a valid UUID", server.MCPServerID)
		}
		if _, ok := seenServers[serverID]; ok {
			return nil, fmt.Errorf("MCP server %q specified more than once", server.MCPServerID)
		}
		seenServers[serverID] = struct{}{}

		tools := make([]string, 0, len(server.Tools))
		seenTools := make(map[string]struct{}, len(server.Tools))
		for _, rawTool := range server.Tools {
			tool := strings.TrimSpace(rawTool)
			if tool == "" {
				return nil, fmt.Errorf("MCP tool name must not be empty")
			}
			if _, ok := seenTools[tool]; ok {
				continue
			}
			seenTools[tool] = struct{}{}
			tools = append(tools, tool)
		}
		slices.Sort(tools)
		scope.Servers = append(scope.Servers, MCPServerScope{
			MCPServerID: serverID,
			Tools:       tools,
		})
	}
	slices.SortFunc(scope.Servers, func(a, b MCPServerScope) int {
		return strings.Compare(a.MCPServerID.String(), b.MCPServerID.String())
	})
	return scope, nil
}

// ValidateMCPScopeOwnership requires every selected server or gateway to
// belong to the policy's project.
func ValidateMCPScopeOwnership(scope *MCPScope, projectServerIDs []uuid.UUID) error {
	if scope == nil {
		return nil
	}
	owned := make(map[uuid.UUID]struct{}, len(projectServerIDs))
	for _, id := range projectServerIDs {
		owned[id] = struct{}{}
	}
	for _, server := range scope.Servers {
		if _, ok := owned[server.MCPServerID]; !ok {
			return fmt.Errorf("MCP server %q does not belong to the project", server.MCPServerID)
		}
	}
	return nil
}

// ValidateMCPScopeSources rejects sources that cannot be evaluated against
// individual MCP calls.
func ValidateMCPScopeSources(scope *MCPScope, sources []string) error {
	if scope == nil {
		return nil
	}
	if slices.Contains(sources, ra.SourceAccountIdentity) {
		return fmt.Errorf("source %q cannot be used by an MCP-scoped policy", ra.SourceAccountIdentity)
	}
	return nil
}

func ValidateAction(action string) error {
	switch action {
	case "flag", "block", "warn", "quarantine":
		return nil
	default:
		return fmt.Errorf("action must be one of: flag, warn, block, quarantine")
	}
}

func ValidateSources(sources []string) error {
	allowed := []string{
		ra.SourceGitleaks,
		ra.SourcePresidio,
		shadowmcp.SourceShadowMCP,
		shadowmcp.SourceDestructiveTool,
		ra.SourceCLIDestructive,
		ra.SourcePromptInjection,
		ra.SourceAccountIdentity,
	}
	for _, source := range sources {
		if !slices.Contains(allowed, source) {
			return fmt.Errorf("source %q is not a recognized policy source", source)
		}
	}
	return nil
}

func ValidateSourceAction(sources []string, action string) error {
	if action == "flag" {
		return nil
	}
	for _, source := range []string{shadowmcp.SourceDestructiveTool, ra.SourceCLIDestructive, ra.SourceAccountIdentity} {
		if slices.Contains(sources, source) {
			return fmt.Errorf("source %q supports flagging only", source)
		}
	}
	return nil
}

func ValidateCustomRuleIDs(ids []string) error {
	for _, id := range ids {
		if !customRuleIDFormat.MatchString(id) {
			return fmt.Errorf("custom rule id %q must match custom.[a-z0-9_]+", id)
		}
	}
	return nil
}

func NormalizeApprovedEmailDomains(domains []string) ([]string, error) {
	out := make([]string, 0, len(domains))
	seen := make(map[string]struct{}, len(domains))
	for _, raw := range domains {
		domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "@")
		if domain == "" {
			continue
		}
		if !approvedDomainFormat.MatchString(domain) {
			return nil, fmt.Errorf("approved email domain %q is not a valid domain", raw)
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		out = append(out, domain)
	}
	return out, nil
}

func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if len([]rune(name)) > 100 {
		return fmt.Errorf("name must be at most 100 characters")
	}
	return nil
}

func ValidatePolicyType(policyType string) error {
	switch policyType {
	case ra.PolicyTypeStandard, ra.PolicyTypePromptBased:
		return nil
	default:
		return fmt.Errorf("policy_type must be one of: standard, prompt_based")
	}
}

// knownCategory reports whether the category is one Gram defines.
func knownCategory(category categories.Category) bool {
	return slices.ContainsFunc(categories.All(), func(def categories.Definition) bool {
		return def.Category == category
	})
}

// ValidateDetectionScopes validates and normalizes category-level message
// scopes into the analyzer-config storage shape.
func ValidateDetectionScopes(eng *celenv.Engine, specs []*DetectionScopeInput) ([]ra.DetectionScopeConfig, error) {
	out := make([]ra.DetectionScopeConfig, 0, len(specs))
	seen := make(map[categories.Category]bool, len(specs))
	for _, spec := range specs {
		if spec == nil {
			return nil, fmt.Errorf("detection scope must not be null")
		}
		category := categories.Category(spec.Category)
		if !knownCategory(category) {
			return nil, fmt.Errorf("detection scope category %q is not recognized", spec.Category)
		}
		// The registry says whether a category has a recommended scope, not
		// whether it may carry one: `custom` has no recommendation but a rule
		// written over `content` still needs an explicit scope to narrow it.
		if recommendation, ok := recommendedscopes.For(category); ok && !recommendation.Applicable {
			return nil, fmt.Errorf("category %q is session-scoped; message detection scopes do not apply", spec.Category)
		}
		if seen[category] {
			return nil, fmt.Errorf("detection scope category %q specified more than once", spec.Category)
		}
		seen[category] = true

		include := strings.TrimSpace(valueOrEmpty(spec.ScopeInclude))
		exempt := strings.TrimSpace(valueOrEmpty(spec.ScopeExempt))
		if _, err := ra.CompileScope(eng, include, exempt); err != nil {
			return nil, &ValidationError{
				Message: fmt.Sprintf("detection scope for %q does not compile", spec.Category),
				Cause:   err,
			}
		}
		out = append(out, ra.DetectionScopeConfig{
			Category:     string(category),
			ScopeInclude: include,
			ScopeExempt:  exempt,
		})
	}
	return out, nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
