package policycore

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestProjectPreservesPolicyReadSemantics(t *testing.T) {
	t.Parallel()

	threshold := 0.75
	analyzerConfig, err := ra.WithPresidioScoreThreshold(nil, &threshold)
	require.NoError(t, err)
	analyzerConfig, err = ra.WithApprovedEmailDomains(analyzerConfig, []string{"example.com"})
	require.NoError(t, err)
	analyzerConfig, err = ra.WithDetectionScopes(analyzerConfig, []ra.DetectionScopeConfig{{
		Category:     "secrets",
		ScopeInclude: `kind == "user_message"`,
	}})
	require.NoError(t, err)

	createdAt := time.Date(2026, time.August, 25, 1, 2, 3, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	row := repo.RiskPolicy{
		ID:                   uuid.New(),
		ProjectID:            uuid.New(),
		OrganizationID:       "<ORG_ID>",
		Enabled:              true,
		Name:                 "Policy",
		PolicyType:           ra.PolicyTypeStandard,
		Sources:              []string{shadowmcp.SourceShadowMCP},
		PresidioEntities:     []string{"EMAIL_ADDRESS"},
		AnalyzerConfig:       analyzerConfig,
		PromptInjectionRules: []string{"rule"},
		DisabledRules:        []string{"disabled"},
		CustomRuleIds:        []string{"custom.rule"},
		MessageTypes:         []string{"user_message"},
		ScopeInclude:         pgtype.Text{String: `kind == "user_message"`, Valid: true},
		Action:               "block",
		AudienceType:         "targeted",
		AutoName:             true,
		UserMessage:          pgtype.Text{String: "Blocked", Valid: true},
		ModelConfig:          []byte(`{"model":"model","temperature":0.2,"fail_open":false}`),
		Score:                5,
		Version:              3,
		CreatedAt:            pgtype.Timestamptz{Time: createdAt, Valid: true},
		UpdatedAt:            pgtype.Timestamptz{Time: updatedAt, Valid: true},
	}

	got := Project(row, nil, &Progress{Total: 7, Analyzed: 9})
	require.Equal(t, row.ID, got.ID)
	require.Equal(t, []string{}, got.AudiencePrincipalURNs)
	require.NotNil(t, got.ShadowMCPDisposition)
	require.Equal(t, shadowmcp.DispositionBlockAll, *got.ShadowMCPDisposition)
	require.NotNil(t, got.PresidioScoreThreshold)
	require.InDelta(t, threshold, *got.PresidioScoreThreshold, 0)
	require.Equal(t, []string{"example.com"}, got.ApprovedEmailDomains)
	require.Equal(t, []DetectionScope{{Category: "secrets", ScopeInclude: new(`kind == "user_message"`)}}, got.DetectionScopes)
	require.NotNil(t, got.PendingMessages)
	require.Equal(t, int64(0), *got.PendingMessages)
	require.NotNil(t, got.TotalMessages)
	require.Equal(t, int64(7), *got.TotalMessages)
	require.Equal(t, createdAt, got.CreatedAt)
	require.Equal(t, updatedAt, got.UpdatedAt)
	require.NotNil(t, got.ModelConfig)
	require.NotNil(t, got.ModelConfig.Model)
	require.Equal(t, "model", *got.ModelConfig.Model)
}

func TestAuditSnapshotRemovesPrompt(t *testing.T) {
	t.Parallel()

	prompt := "sensitive policy prompt"
	policy := Policy{Name: "policy", Prompt: &prompt}

	snapshot := AuditSnapshot(policy)
	require.Nil(t, snapshot.Prompt)
	require.Equal(t, "policy", snapshot.Name)
	require.Equal(t, &prompt, policy.Prompt)
}

func TestProjectToleratesMalformedConfigAndOmitsListProgress(t *testing.T) {
	t.Parallel()

	got := Project(repo.RiskPolicy{
		Action:         "flag",
		AnalyzerConfig: []byte(`{"broken"`),
		ModelConfig:    []byte(`{"broken"`),
	}, []string{"user:one"}, nil)

	require.Nil(t, got.PresidioScoreThreshold)
	require.Nil(t, got.ApprovedEmailDomains)
	require.Nil(t, got.DetectionScopes)
	require.Nil(t, got.ModelConfig)
	require.Nil(t, got.PendingMessages)
	require.Nil(t, got.TotalMessages)
	require.Equal(t, []string{"user:one"}, got.AudiencePrincipalURNs)
	require.Nil(t, got.ShadowMCPDisposition)
}
