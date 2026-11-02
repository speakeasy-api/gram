package risk_analysis_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func newCategoryScopeAnalyzer(
	t *testing.T,
	conn *pgxpool.Pool,
	gitleaksPub gcp.Publisher[*riskv1.GitleaksAnalysis],
	customRulesPub gcp.Publisher[*riskv1.CustomRulesAnalysis],
) *risk_analysis.AnalyzeBatch {
	t.Helper()

	analyzer, err := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		nil,
		nil,
		newPresidioPub(),
		gitleaksPub,
		newPromptInjectionPub(),
		newPromptPolicyPub(),
		customRulesPub,
		newLLMPub(),
		newToolCallPub(),
		newAccountIdentityPub(),
		mustCELEngine(t),
		false,
	)
	require.NoError(t, err)
	return analyzer
}

func executeCategoryScopeBatch(t *testing.T, analyzer *risk_analysis.AnalyzeBatch, td testData, messageIDs []uuid.UUID, sources []string) {
	t.Helper()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivity(analyzer.Do)
	value, err := env.ExecuteActivity(analyzer.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		RiskPolicyID:   td.policyID,
		PolicyVersion:  td.policyVersion,
		MessageIDs:     messageIDs,
		Sources:        sources,
	})
	require.NoError(t, err)
	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, value.Get(&result))
}

func insertCategoryScopeMessage(t *testing.T, conn *pgxpool.Pool, td testData, role, content string) uuid.UUID {
	t.Helper()
	id, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      role,
		Content:   content,
	})
	require.NoError(t, err)
	return id
}

func TestAnalyzeBatch_GitleaksCategoryScopeSubsetsPublish(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	secret := "AccessKeyId ASIAZ2XY3WNBQR5TUVWX SecretAccessKey " + "wJalrXUtnFEMIbKp7MDoRZ" + "fiCYqTvHgNsQ8xLcWd"
	assistantID := insertCategoryScopeMessage(t, conn, td, "assistant", secret)
	userID := insertCategoryScopeMessage(t, conn, td, "user", secret)
	gitleaksPub, published := capturingPub[*riskv1.GitleaksAnalysis](t)
	analyzer := newCategoryScopeAnalyzer(t, conn, gitleaksPub, newCustomRulesPub())

	executeCategoryScopeBatch(t, analyzer, td, []uuid.UUID{assistantID, userID}, []string{risk_analysis.SourceGitleaks})

	require.Len(t, *published, 1)
	require.Equal(t, userID.String(), (*published)[0].GetChatMessageId())
}

func TestAnalyzeBatch_CustomCategoryScopeSubsetsPublish(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	td = seedCustomRulePolicySelection(t, conn, td, "custom.acme_token", `content.matchRegex("ACME-[A-Z0-9]{8}")`)
	policy, err := riskrepo.New(conn).GetRiskPolicy(t.Context(), riskrepo.GetRiskPolicyParams{ID: td.policyID, ProjectID: td.projectID})
	require.NoError(t, err)
	analyzerConfig, err := risk_analysis.WithDetectionScopes(policy.AnalyzerConfig, []risk_analysis.DetectionScopeConfig{{
		Category:     string(categories.CategoryCustom),
		ScopeInclude: `kind == "user_message"`,
	}})
	require.NoError(t, err)
	policy, err = riskrepo.New(conn).UpdateRiskPolicy(t.Context(), riskrepo.UpdateRiskPolicyParams{
		Name:                 policy.Name,
		Sources:              policy.Sources,
		PresidioEntities:     policy.PresidioEntities,
		AnalyzerConfig:       analyzerConfig,
		PromptInjectionRules: policy.PromptInjectionRules,
		DisabledRules:        policy.DisabledRules,
		CustomRuleIds:        policy.CustomRuleIds,
		McpScope:             policy.McpScope,
		Enabled:              policy.Enabled,
		Action:               policy.Action,
		AudienceType:         policy.AudienceType,
		AutoName:             policy.AutoName,
		UserMessage:          policy.UserMessage,
		Prompt:               policy.Prompt,
		ModelConfig:          policy.ModelConfig,
		Score:                pgtype.Float8{Float64: policy.Score, Valid: true},
		ID:                   policy.ID,
		ProjectID:            policy.ProjectID,
	})
	require.NoError(t, err)
	td.policyVersion = policy.Version

	assistantID := insertCategoryScopeMessage(t, conn, td, "assistant", "ACME-ABC12345")
	userID := insertCategoryScopeMessage(t, conn, td, "user", "ACME-ABC12345")
	customPub, published := capturingPub[*riskv1.CustomRulesAnalysis](t)
	analyzer := newCategoryScopeAnalyzer(t, conn, newGitleaksPub(), customPub)

	executeCategoryScopeBatch(t, analyzer, td, []uuid.UUID{assistantID, userID}, []string{risk_analysis.SourceCustom})

	require.Len(t, *published, 1)
	require.Equal(t, userID.String(), (*published)[0].GetChatMessageId())
}
