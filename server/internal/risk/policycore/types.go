package policycore

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

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
	MessageTypes           []string
	ScopeInclude           *string
	ScopeExempt            *string
	Enabled                bool
	Action                 string
	AudienceType           string
	AudiencePrincipalURNs  []string
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
	Model       *string  `json:"model,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	FailOpen    *bool    `json:"fail_open,omitempty"`
}

// Progress describes the message-analysis progress attached to single-policy reads.
type Progress struct {
	Total    int64
	Analyzed int64
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
		MessageTypes:           row.MessageTypes,
		ScopeInclude:           conv.FromPGText[string](row.ScopeInclude),
		ScopeExempt:            conv.FromPGText[string](row.ScopeExempt),
		Enabled:                row.Enabled,
		Action:                 row.Action,
		AudienceType:           row.AudienceType,
		AudiencePrincipalURNs:  audience,
		ShadowMCPDisposition:   effectiveShadowMCPDisposition(row),
		AutoName:               row.AutoName,
		UserMessage:            conv.FromPGText[string](row.UserMessage),
		Prompt:                 conv.FromPGText[string](row.Prompt),
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

func effectiveShadowMCPDisposition(row repo.RiskPolicy) *string {
	if row.Action != "block" || !slices.Contains(row.Sources, shadowmcp.SourceShadowMCP) {
		return nil
	}
	if row.ShadowMcpDisposition.Valid && row.ShadowMcpDisposition.String != "" {
		return new(row.ShadowMcpDisposition.String)
	}
	return new(shadowmcp.DispositionBlockAll)
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
