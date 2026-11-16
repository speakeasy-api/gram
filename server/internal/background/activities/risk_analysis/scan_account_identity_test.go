package risk_analysis_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// seedAccountIdentityPolicy creates an enabled flag policy with the
// account_identity source, an optional approved-domains config, and optional
// disabled rules.
func seedAccountIdentityPolicy(t *testing.T, conn *pgxpool.Pool, td testData, approvedDomains []string, disabledRules []string) (uuid.UUID, int64) {
	t.Helper()

	analyzerConfig, err := risk_analysis.WithApprovedEmailDomains(nil, approvedDomains)
	require.NoError(t, err)

	policyID, err := uuid.NewV7()
	require.NoError(t, err)
	policy, err := riskrepo.New(conn).CreateRiskPolicy(t.Context(), riskrepo.CreateRiskPolicyParams{
		ID:             policyID,
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		Name:           "account identity policy",
		Sources:        []string{"account_identity"},
		AnalyzerConfig: analyzerConfig,
		DisabledRules:  disabledRules,
		Enabled:        true,
		Action:         "flag",
		AudienceType:   "everyone",
		AutoName:       false,
		UserMessage:    pgtype.Text{},
	})
	require.NoError(t, err)
	return policyID, policy.Version
}

// seedAccountChat creates a user_accounts row (accountType/email may be empty
// for an unclassified account) and a chat linked to it, returning the chat id.
func seedAccountChat(t *testing.T, conn *pgxpool.Pool, td testData, accountType, email string) uuid.UUID {
	t.Helper()
	chatID, _ := seedAccountChatWithAccount(t, conn, td, accountType, email)
	return chatID
}

// seedAccountChatWithAccount additionally returns the account's external uuid
// (the ingest upsert key), for tests that enrich the account across batches.
func seedAccountChatWithAccount(t *testing.T, conn *pgxpool.Pool, td testData, accountType, email string) (uuid.UUID, string) {
	t.Helper()
	ctx := t.Context()

	externalAccountUUID := uuid.NewString()
	account, err := hooksrepo.New(conn).UpsertUserAccount(ctx, hooksrepo.UpsertUserAccountParams{
		OrganizationID:      td.orgID,
		Provider:            "anthropic",
		ExternalAccountUuid: externalAccountUUID,
		UserID:              pgtype.Text{},
		ExternalOrgID:       pgtype.Text{},
		ExternalAccountID:   pgtype.Text{},
		Email:               pgtype.Text{String: email, Valid: email != ""},
		AccountType:         pgtype.Text{String: accountType, Valid: accountType != ""},
	})
	require.NoError(t, err)

	chatID, err := uuid.NewV7()
	require.NoError(t, err)
	_, err = hooksrepo.New(conn).UpsertClaudeCodeSession(ctx, hooksrepo.UpsertClaudeCodeSessionParams{
		ID:             chatID,
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		UserID:         pgtype.Text{},
		ExternalUserID: pgtype.Text{},
		UserAccountID:  uuid.NullUUID{UUID: account.ID, Valid: true},
		Title:          pgtype.Text{String: "account identity chat", Valid: true},
	})
	require.NoError(t, err)
	return chatID, externalAccountUUID
}

func seedChatMessage(t *testing.T, conn *pgxpool.Pool, td testData, chatID uuid.UUID) uuid.UUID {
	t.Helper()
	msgID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "hello",
	})
	require.NoError(t, err)
	return msgID
}

func newAccountIdentityAnalyzeBatch(t *testing.T, conn *pgxpool.Pool, accountIdentityPub gcp.Publisher[*riskv1.AccountIdentityAnalysis]) *risk_analysis.AnalyzeBatch {
	t.Helper()
	ab, err := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		nil,
		nil,
		newPresidioPub(),
		newGitleaksPub(),
		newPromptInjectionPub(),
		newPromptPolicyPub(),
		newCustomRulesPub(),
		newLLMPub(),
		newToolCallPub(),
		accountIdentityPub,
		mustCELEngine(t),
		false,
	)
	require.NoError(t, err)
	return ab
}

func runAccountIdentityBatch(t *testing.T, ab *risk_analysis.AnalyzeBatch, td testData, policyID uuid.UUID, policyVersion int64, messageIDs []uuid.UUID) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)

	_, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		RiskPolicyID:   policyID,
		PolicyVersion:  policyVersion,
		MessageIDs:     messageIDs,
		Sources:        []string{"account_identity"},
	})
	require.NoError(t, err)
}

func TestAnalyzeBatch_AccountIdentityPublishesAnalysisRequest(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	policyID, policyVersion := seedAccountIdentityPolicy(t, conn, td, nil, nil)

	chatID := seedAccountChat(t, conn, td, "personal", "jane@gmail.com")
	msg1 := seedChatMessage(t, conn, td, chatID)

	pub, published := capturingAccountIdentityPub(t)
	ab := newAccountIdentityAnalyzeBatch(t, conn, pub)
	runAccountIdentityBatch(t, ab, td, policyID, policyVersion, []uuid.UUID{msg1})

	require.Len(t, *published, 1)
	request := (*published)[0]
	assert.Equal(t, "personal", request.GetAccountType())
	assert.Equal(t, "jane@gmail.com", request.GetEmail())
	assert.Equal(t, td.orgID, request.GetOrganizationId())
	assert.Equal(t, td.projectID.String(), request.GetProjectId())
	assert.Equal(t, policyID.String(), request.GetRiskPolicyId())
	assert.Equal(t, policyVersion, request.GetRiskPolicyVersion())
	assert.Equal(t, chatID.String(), request.GetChatId())
	assert.Equal(t, msg1.String(), request.GetChatMessageId())
}

func TestWithApprovedEmailDomains_PreservesPresidioConfig(t *testing.T) {
	t.Parallel()

	threshold := 0.75
	base, err := risk_analysis.WithPresidioScoreThreshold(nil, &threshold)
	require.NoError(t, err)

	withDomains, err := risk_analysis.WithApprovedEmailDomains(base, []string{"acme.com"})
	require.NoError(t, err)
	require.Equal(t, []string{"acme.com"}, risk_analysis.ApprovedEmailDomainsFromConfig(withDomains))
	require.InDelta(t, threshold, risk_analysis.PresidioScoreThresholdFromConfig(withDomains), 1e-9)

	// Clearing the domains drops the account_identity section but keeps presidio.
	cleared, err := risk_analysis.WithApprovedEmailDomains(withDomains, nil)
	require.NoError(t, err)
	require.Empty(t, risk_analysis.ApprovedEmailDomainsFromConfig(cleared))
	require.InDelta(t, threshold, risk_analysis.PresidioScoreThresholdFromConfig(cleared), 1e-9)
}
