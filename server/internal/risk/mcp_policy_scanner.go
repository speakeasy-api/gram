package risk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/clidestructive"
	"github.com/speakeasy-api/gram/server/internal/scanners/customruleanalyzer"
	"github.com/speakeasy-api/gram/server/internal/scanners/destructivetool"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// MCPScanRequest is one materialized MCP request passed to policy detectors.
type MCPScanRequest struct {
	// Text is the complete request payload.
	Text string

	// ToolName is the called MCP tool.
	ToolName string

	// ToolsetID identifies the hosted toolset when one exists.
	ToolsetID string

	// ServerID identifies the concrete MCP server.
	ServerID string

	// UserID is trusted user provenance from mcpidentity.
	UserID string
}

// MCPPolicyScanner shares the synchronous detector registry used by Scanner.
type MCPPolicyScanner struct {
	scanner         *Scanner
	cliDestructive  *clidestructive.Scanner
	destructiveTool *destructivetool.Scanner
}

// NewMCPPolicyScanner creates an MCP policy scanner over the shared detectors.
func NewMCPPolicyScanner(scanner *Scanner, resolver destructivetool.Resolver) *MCPPolicyScanner {
	var destructive *destructivetool.Scanner
	if resolver != nil {
		destructive = destructivetool.NewScanner(resolver)
	}
	return &MCPPolicyScanner{
		scanner:         scanner,
		cliDestructive:  clidestructive.NewScanner(),
		destructiveTool: destructive,
	}
}

// ScanMCPPolicy runs one policy against a tool_request message view.
func (s *MCPPolicyScanner) ScanMCPPolicy(ctx context.Context, policy policycore.Policy, request MCPScanRequest) ([]scanners.Finding, error) {
	if s == nil || s.scanner == nil {
		return nil, errors.New("MCP policy scanner is unavailable")
	}

	view := realtimeMessageView(request.Text, message.ToolRequest, request.ToolName)
	specs := make([]risk_analysis.DetectionScopeConfig, 0, len(policy.DetectionScopes))
	for _, scope := range policy.DetectionScopes {
		specs = append(specs, risk_analysis.DetectionScopeConfig{
			Category:     scope.Category,
			ScopeInclude: dereference(scope.ScopeInclude),
			ScopeExempt:  dereference(scope.ScopeExempt),
		})
	}
	specified, err := risk_analysis.CompileDetectionScopes(s.scanner.celEng, specs)
	if err != nil {
		return nil, fmt.Errorf("compile MCP detection scopes: %w", err)
	}
	categoryScope := risk_analysis.NewCategoryScope(s.scanner.recommended, specified)

	if policy.PolicyType == risk_analysis.PolicyTypePromptBased {
		return s.scanPromptPolicy(ctx, policy, request, view, categoryScope)
	}

	exclusionRows, err := s.scanner.repo.ListEnabledExclusionsForPolicy(ctx, repo.ListEnabledExclusionsForPolicyParams{
		ProjectID:    policy.ProjectID,
		RiskPolicyID: uuid.NullUUID{UUID: policy.ID, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list MCP policy exclusions: %w", err)
	}
	exclusions := risk_analysis.NewExclusionSet(exclusionRows)
	disabled := risk_analysis.NewDisabledRuleSet(policy.DisabledRules)
	filter := func(source string, findings []scanners.Finding) []scanners.Finding {
		if !categoryScope.SourceInScope(view, source) {
			return nil
		}
		return categoryScope.FilterFindings(view, exclusions.FilterFindings(disabled.FilterFindings(findings)))
	}

	var findings []scanners.Finding
	var scanErr error
	for _, source := range policy.Sources {
		if !categoryScope.SourceInScope(view, source) {
			continue
		}
		switch source {
		case risk_analysis.SourceGitleaks:
			result, err := s.scanner.scanGitleaks(ctx, request.Text)
			findings = append(findings, filter(source, result.Findings)...)
			if err != nil || !result.Completed {
				scanErr = errors.Join(scanErr, completionError(source, err))
			}
		case risk_analysis.SourcePresidio:
			if s.scanner.piiScanner == nil {
				scanErr = errors.Join(scanErr, errors.New("presidio scanner is unavailable"))
				continue
			}
			entities := risk_analysis.FilterPinnedEntities(policy.PresidioEntities)
			if len(policy.PresidioEntities) > 0 && len(entities) == 0 {
				continue
			}
			threshold := risk_analysis.DefaultPresidioScoreThreshold
			if policy.PresidioScoreThreshold != nil && *policy.PresidioScoreThreshold > 0 && *policy.PresidioScoreThreshold <= 1 {
				threshold = *policy.PresidioScoreThreshold
			}
			results, err := s.scanner.piiScanner.AnalyzeBatch(ctx, []string{request.Text}, entities, threshold, func() {})
			if err != nil || len(results) != 1 || !results[0].Completed {
				scanErr = errors.Join(scanErr, completionError(source, err))
			}
			if len(results) == 1 {
				findings = append(findings, filter(source, filterMCPPresidioFindings(results[0].Findings, entities, threshold))...)
			}
		case risk_analysis.SourcePromptInjection:
			result, _, err := s.scanner.piScanner.ScanWithVerdict(ctx, request.Text, policy.OrganizationID, policy.ProjectID.String(), request.UserID, judgemessage.New(message.ToolRequest, request.ToolName, request.Text))
			findings = append(findings, filter(source, result.Findings)...)
			if err != nil || !result.Completed {
				scanErr = errors.Join(scanErr, completionError(source, err))
			}
		case risk_analysis.SourceCLIDestructive:
			result := s.cliDestructive.Scan(ctx, []clidestructive.ToolCall{{Name: request.ToolName, Arguments: request.Text}})
			findings = append(findings, filter(source, result.Findings)...)
			if !result.Completed {
				scanErr = errors.Join(scanErr, completionError(source, nil))
			}
		case shadowmcp.SourceDestructiveTool:
			if s.destructiveTool == nil || request.ToolsetID == "" {
				scanErr = errors.Join(scanErr, errors.New("destructive tool detector lacks hosted toolset attribution"))
				continue
			}
			arguments, err := argumentsWithToolsetID(request.Text, request.ToolsetID)
			if err != nil {
				scanErr = errors.Join(scanErr, fmt.Errorf("prepare destructive tool input: %w", err))
				continue
			}
			detected := s.destructiveTool.Scan(ctx, policy.OrganizationID, []destructivetool.ToolCall{{Name: "MCP:" + request.ToolName, Arguments: arguments}})
			findings = append(findings, filter(source, detected)...)
		case shadowmcp.SourceShadowMCP:
			// Mediation itself proves the server is registered with Gram.
			continue
		case risk_analysis.SourceAccountIdentity:
			// Account identity is evaluated over session attribution, not one MCP request.
			continue
		default:
			scanErr = errors.Join(scanErr, fmt.Errorf("unsupported MCP policy source %q", source))
		}
	}

	if len(policy.CustomRuleIDs) > 0 && categoryScope.SourceInScope(view, risk_analysis.SourceCustom) {
		toolCalls := []customruleanalyzer.ScanToolCall{{Name: request.ToolName, Arguments: request.Text}}
		result, err := s.scanner.customRuleScanner.Scan(ctx, customruleanalyzer.ScanRequest{
			ProjectID:     policy.ProjectID,
			CustomRuleIDs: policy.CustomRuleIDs,
			Content:       view.Content,
			Kind:          view.Type,
			ToolCalls:     toolCalls,
		})
		findings = append(findings, filter(risk_analysis.SourceCustom, result.Findings)...)
		if err != nil || !result.Completed {
			scanErr = errors.Join(scanErr, completionError(risk_analysis.SourceCustom, err))
		}
	}

	return deduplicateMCPFindings(findings), scanErr
}

func (s *MCPPolicyScanner) scanPromptPolicy(ctx context.Context, policy policycore.Policy, request MCPScanRequest, view risk_analysis.MessageView, scope risk_analysis.CategoryScope) ([]scanners.Finding, error) {
	if !scope.SourceInScope(view, promptpolicy.Source) {
		return nil, nil
	}
	if !s.scanner.projectFlagEnabled(ctx, policy.OrganizationID, policy.ProjectID, feature.FlagPromptPolicies) {
		return nil, errors.New("prompt policy detector is disabled")
	}
	if s.scanner.promptPolicy == nil {
		return nil, errors.New("prompt policy detector is unavailable")
	}
	config := promptpolicy.Config{Temperature: nil, FailOpen: true}
	if policy.ModelConfig != nil {
		config.Temperature = policy.ModelConfig.Temperature
		if policy.ModelConfig.FailOpen != nil {
			config.FailOpen = *policy.ModelConfig.FailOpen
		}
	}
	prompt := ""
	if policy.Prompt != nil {
		prompt = *policy.Prompt
	}
	result, _ := s.scanner.promptPolicy.ScanWithVerdict(ctx, policy.OrganizationID, policy.ProjectID.String(), request.UserID, prompt, config, judgemessage.New(message.ToolRequest, request.ToolName, request.Text))
	findings := scope.FilterFindings(view, risk_analysis.NewDisabledRuleSet(policy.DisabledRules).FilterFindings(result.Findings))
	if !result.Completed {
		return findings, completionError(promptpolicy.Source, nil)
	}
	return findings, nil
}

func completionError(source string, err error) error {
	if err != nil {
		return fmt.Errorf("%s scan: %w", source, err)
	}
	return fmt.Errorf("%s scan incomplete", source)
}

func filterMCPPresidioFindings(findings []scanners.Finding, entities []string, threshold float64) []scanners.Finding {
	out := make([]scanners.Finding, 0, len(findings))
	for _, finding := range findings {
		entity := strings.ToUpper(strings.TrimPrefix(finding.RuleID, "pii."))
		if risk_analysis.IsEntityFindingDropped(entity) || finding.Confidence < threshold || (len(entities) > 0 && !slices.Contains(entities, entity)) {
			continue
		}
		out = append(out, finding)
	}
	return out
}

func argumentsWithToolsetID(raw, toolsetID string) (string, error) {
	input := map[string]any{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &input); err != nil {
			return "", fmt.Errorf("decode tool arguments: %w", err)
		}
	}
	input[shadowmcp.XGramToolsetIDField] = toolsetID
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("encode tool arguments: %w", err)
	}
	return string(encoded), nil
}

func deduplicateMCPFindings(findings []scanners.Finding) []scanners.Finding {
	type key struct {
		source string
		rule   string
		start  int
		end    int
		match  string
	}
	seen := make(map[key]struct{}, len(findings))
	out := make([]scanners.Finding, 0, len(findings))
	for _, finding := range findings {
		k := key{source: finding.Source, rule: finding.RuleID, start: finding.StartPos, end: finding.EndPos, match: finding.Match}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, finding)
	}
	return out
}

func dereference(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
