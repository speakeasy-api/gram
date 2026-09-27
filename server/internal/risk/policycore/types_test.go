package policycore

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestMCPScopeAppliesToDirectServerAndCurrentGatewayMembership(t *testing.T) {
	t.Parallel()

	serverID := uuid.New()
	gatewayID := uuid.New()
	otherID := uuid.New()
	scope := &MCPScope{Servers: []MCPServerScope{
		{MCPServerID: serverID, Tools: []string{"search"}},
		{MCPServerID: gatewayID},
	}}

	require.True(t, (*MCPScope)(nil).Applies(serverID, "anything", nil, nil))
	require.True(t, scope.Applies(serverID, "search", nil, nil))
	require.False(t, scope.Applies(serverID, "write", nil, nil))
	require.True(t, scope.Applies(serverID, "", nil, nil), "omitted tool filters only at server level")
	require.True(t, scope.Applies(otherID, "anything", nil, []uuid.UUID{gatewayID}))
	require.False(t, scope.Applies(otherID, "anything", nil, nil))
}

func TestMCPScopeAppliesToolRuleAndAllServersOverrides(t *testing.T) {
	t.Parallel()

	serverID := uuid.New()
	otherID := uuid.New()
	scope := &MCPScope{
		AllServers:      true,
		ToolAnnotations: []string{"destructiveHint"},
		Servers: []MCPServerScope{{
			MCPServerID: serverID,
			Tools:       []string{"safe_override"},
		}},
	}
	destructive := &types.ToolAnnotations{DestructiveHint: new(true)}

	require.True(t, scope.Applies(otherID, "delete", destructive, nil))
	require.False(t, scope.Applies(otherID, "delete", nil, nil))
	require.True(t, scope.Applies(serverID, "safe_override", nil, nil), "custom tools ignore the policy rule")
	require.False(t, scope.Applies(serverID, "delete", destructive, nil), "custom tools override all-servers rule matching")

	selected := &MCPScope{
		ToolAnnotations: []string{"readOnlyHint"},
		Servers:         []MCPServerScope{{MCPServerID: serverID}},
	}
	readOnly := &types.ToolAnnotations{ReadOnlyHint: new(true)}
	require.True(t, selected.Applies(serverID, "list", readOnly, nil))
	require.False(t, selected.Applies(serverID, "list", nil, nil))
}

func TestUnmarshalMCPScopeNormalizesEmptyAndFailsClosed(t *testing.T) {
	t.Parallel()

	require.Nil(t, unmarshalMCPScope(nil))
	require.Nil(t, unmarshalMCPScope([]byte(`null`)))
	require.Nil(t, unmarshalMCPScope([]byte(`{"servers":[]}`)))

	allServers := unmarshalMCPScope([]byte(`{"all_servers":true,"tool_annotations":["destructiveHint"],"servers":[]}`))
	require.NotNil(t, allServers)
	require.True(t, allServers.AllServers)
	require.Equal(t, []string{"destructiveHint"}, allServers.ToolAnnotations)

	for _, raw := range [][]byte{
		[]byte(`{}`),
		[]byte(`{"servers":null}`),
		[]byte(`{"tool_annotations":["unknownHint"],"servers":[]}`),
		[]byte(`not-json`),
	} {
		scope := unmarshalMCPScope(raw)
		require.NotNil(t, scope)
		require.Empty(t, scope.Servers)
	}
}

func TestUnmarshalMCPScopeFailsClosedOnExplicitEmptyTools(t *testing.T) {
	t.Parallel()

	scope := unmarshalMCPScope([]byte(`{"all_servers":true,"servers":[{"mcp_server_id":"11111111-1111-4111-8111-111111111111","tools":[]}]}`))
	require.NotNil(t, scope)
	require.False(t, scope.AllServers)
	require.Nil(t, scope.ToolAnnotations)
	require.Empty(t, scope.Servers)
}

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
	require.NotNil(t, got.ModelConfig.Temperature)
	require.InDelta(t, 0.2, *got.ModelConfig.Temperature, 0)
	require.NotNil(t, got.ModelConfig.FailOpen)
	require.False(t, *got.ModelConfig.FailOpen)
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
