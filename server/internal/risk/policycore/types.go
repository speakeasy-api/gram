package policycore

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// MCPScope restricts a policy to MCP tool traffic. A nil *MCPScope keeps the
// policy on chat-session and hook evaluation paths only.
type MCPScope struct {
	AllServers      bool             `json:"all_servers,omitempty"`
	ToolAnnotations []string         `json:"tool_annotations,omitempty"`
	Servers         []MCPServerScope `json:"servers"`
}

// MCPServerScope selects one MCP server or gateway. A nil Tools slice follows
// the policy tool rule; a non-nil slice is a custom tool selection.
type MCPServerScope struct {
	MCPServerID uuid.UUID `json:"mcp_server_id"`
	Tools       []string  `json:"tools,omitempty"`
}

// Applies reports whether a policy scope applies to a concrete MCP server and
// tool. gatewaysContaining must be resolved from current gateway membership.
func (s *MCPScope) Applies(serverID uuid.UUID, toolName string, annotations *types.ToolAnnotations, gatewaysContaining []uuid.UUID) bool {
	if s == nil {
		return true
	}
	if s.AllServers {
		for _, server := range s.Servers {
			if server.MCPServerID == serverID {
				return s.toolMatches(server, toolName, annotations)
			}
		}
		return s.toolRuleMatches(toolName, annotations)
	}
	for _, server := range s.Servers {
		if server.MCPServerID != serverID && !slices.Contains(gatewaysContaining, server.MCPServerID) {
			continue
		}
		if s.toolMatches(server, toolName, annotations) {
			return true
		}
	}
	return false
}

func (s *MCPScope) toolMatches(server MCPServerScope, toolName string, annotations *types.ToolAnnotations) bool {
	if toolName == "" {
		return true
	}
	if server.Tools != nil {
		return slices.Contains(server.Tools, toolName)
	}
	return s.toolRuleMatches(toolName, annotations)
}

func (s *MCPScope) toolRuleMatches(toolName string, annotations *types.ToolAnnotations) bool {
	if toolName == "" || len(s.ToolAnnotations) == 0 {
		return true
	}
	return toolfilter.AnnotationsMatch(annotations, s.ToolAnnotations)
}

// Policy is the transport-neutral representation of a persisted risk policy.
type Policy struct {
	ID                     uuid.UUID
	ProjectID              uuid.UUID
	OrganizationID         string
	Name                   string
	PolicyType             string
	Sources                []string
	PresidioEntities       []string
	PresidioScoreThreshold *float64
	ApprovedEmailDomains   []string
	DetectionScopes        []DetectionScope
	PromptInjectionRules   []string
	DisabledRules          []string
	CustomRuleIDs          []string
	Enabled                bool
	Action                 string
	AudienceType           string
	AudiencePrincipalURNs  []string
	MCPScope               *MCPScope
	ShadowMCPDisposition   *string
	AutoName               bool
	UserMessage            *string
	Prompt                 *string
	ModelConfig            *ModelConfig
	Score                  float64
	Version                int64
	CreatedAt              time.Time
	UpdatedAt              time.Time
	PendingMessages        *int64
	TotalMessages          *int64
}

// DetectionScope is one category's message-level detection scope.
type DetectionScope struct {
	Category     string
	ScopeInclude *string
	ScopeExempt  *string
}

// ModelConfig is the persisted prompt-policy model configuration.
type ModelConfig struct {
	Temperature *float64 `json:"temperature,omitempty"`
	FailOpen    *bool    `json:"fail_open,omitempty"`
}

// Progress describes the message-analysis progress attached to single-policy reads.
type Progress struct {
	Total    int64
	Analyzed int64
}

// AuditSnapshot strips policy content that must not enter audit or outbox
// telemetry while preserving the administrative fields needed for change logs.
func AuditSnapshot(policy Policy) Policy {
	policy.Prompt = nil
	return policy
}

// Project maps a database policy row into its canonical transport-neutral form.
func Project(row repo.RiskPolicy, audiencePrincipalURNs []string, progress *Progress) Policy {
	var pendingMessages, totalMessages *int64
	if progress != nil {
		total := progress.Total
		pending := max(progress.Total-progress.Analyzed, 0)
		totalMessages = &total
		pendingMessages = &pending
	}

	audience := slices.Clone(audiencePrincipalURNs)
	if audience == nil {
		audience = []string{}
	}

	return Policy{
		ID:                     row.ID,
		ProjectID:              row.ProjectID,
		OrganizationID:         row.OrganizationID,
		Name:                   row.Name,
		PolicyType:             row.PolicyType,
		Sources:                row.Sources,
		PresidioEntities:       row.PresidioEntities,
		PresidioScoreThreshold: ra.PresidioScoreThresholdPtr(row.AnalyzerConfig),
		ApprovedEmailDomains:   ra.ApprovedEmailDomainsFromConfig(row.AnalyzerConfig),
		DetectionScopes:        projectDetectionScopes(row.AnalyzerConfig),
		PromptInjectionRules:   row.PromptInjectionRules,
		DisabledRules:          row.DisabledRules,
		CustomRuleIDs:          row.CustomRuleIds,
		Enabled:                row.Enabled,
		Action:                 row.Action,
		AudienceType:           row.AudienceType,
		AudiencePrincipalURNs:  audience,
		ShadowMCPDisposition:   shadowMCPDisposition(row),
		AutoName:               row.AutoName,
		UserMessage:            conv.FromPGText[string](row.UserMessage),
		Prompt:                 conv.FromPGText[string](row.Prompt),
		MCPScope:               unmarshalMCPScope(row.McpScope),
		ModelConfig:            unmarshalModelConfig(row.ModelConfig),
		Score:                  row.Score,
		Version:                row.Version,
		CreatedAt:              row.CreatedAt.Time,
		UpdatedAt:              row.UpdatedAt.Time,
		PendingMessages:        pendingMessages,
		TotalMessages:          totalMessages,
	}
}

func projectDetectionScopes(analyzerConfig []byte) []DetectionScope {
	specs := ra.DetectionScopesFromConfig(analyzerConfig)
	if len(specs) == 0 {
		return nil
	}
	out := make([]DetectionScope, 0, len(specs))
	for _, spec := range specs {
		out = append(out, DetectionScope{
			Category:     spec.Category,
			ScopeInclude: conv.PtrEmpty(spec.ScopeInclude),
			ScopeExempt:  conv.PtrEmpty(spec.ScopeExempt),
		})
	}
	return out
}

func shadowMCPDisposition(row repo.RiskPolicy) *string {
	disposition := shadowmcp.EffectiveDisposition(row.ShadowMcpDisposition, row.Sources, row.Action)
	if disposition == "" {
		return nil
	}
	return &disposition
}

func unmarshalMCPScope(raw []byte) *MCPScope {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var scope MCPScope
	if err := json.Unmarshal(raw, &scope); err != nil {
		return failClosedMCPScope()
	}
	if scope.Servers == nil {
		return failClosedMCPScope()
	}
	for _, annotation := range scope.ToolAnnotations {
		if !isKnownMCPToolAnnotation(annotation) {
			return failClosedMCPScope()
		}
	}
	for i := range scope.Servers {
		if len(scope.Servers[i].Tools) == 0 {
			scope.Servers[i].Tools = nil
		}
	}
	if !scope.AllServers && len(scope.Servers) == 0 {
		if len(scope.ToolAnnotations) == 0 {
			return nil
		}
		return failClosedMCPScope()
	}
	return &scope
}

func failClosedMCPScope() *MCPScope {
	return &MCPScope{AllServers: false, ToolAnnotations: nil, Servers: []MCPServerScope{}}
}

func unmarshalModelConfig(raw []byte) *ModelConfig {
	if len(raw) == 0 {
		return nil
	}
	var config ModelConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil
	}
	return &config
}
