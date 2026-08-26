package policycore

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/message"
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

func ValidateMessageTypes(messageTypes []string) error {
	for _, messageType := range messageTypes {
		if message.IsTypeValid(messageType) {
			continue
		}
		return fmt.Errorf("message_type %q must be one of: %s", messageType, strings.Join(message.AllTypes(), ", "))
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
		recommendation, ok := recommendedscopes.For(category)
		if !ok {
			return nil, fmt.Errorf("detection scope category %q is not recognized", spec.Category)
		}
		if !recommendation.Applicable {
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
