package risk_analysis_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

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
	return seedAccountIdentityPolicyScoped(t, conn, td, approvedDomains, disabledRules, "")
}

// seedAccountIdentityPolicyScoped additionally sets a CEL scope_exempt
// predicate, for tests proving that message scoping does not suppress the
// session-scoped findings.
func seedAccountIdentityPolicyScoped(t *testing.T, conn *pgxpool.Pool, td testData, approvedDomains []string, disabledRules []string, scopeExempt string) (uuid.UUID, int64) {
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
		ScopeExempt:    pgtype.Text{String: scopeExempt, Valid: scopeExempt != ""},
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

// setAccountEmail fills the account's email through the same ingest upsert
// the OTEL path uses (COALESCE fills a previously-unknown field).
func setAccountEmail(t *testing.T, conn *pgxpool.Pool, td testData, externalAccountUUID, email string) {
	t.Helper()
	_, err := hooksrepo.New(conn).UpsertUserAccount(t.Context(), hooksrepo.UpsertUserAccountParams{
		OrganizationID:      td.orgID,
		Provider:            "anthropic",
		ExternalAccountUuid: externalAccountUUID,
		UserID:              pgtype.Text{},
		ExternalOrgID:       pgtype.Text{},
		ExternalAccountID:   pgtype.Text{},
		Email:               pgtype.Text{String: email, Valid: true},
		AccountType:         pgtype.Text{},
	})
	require.NoError(t, err)
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

func newAccountIdentityAnalyzeBatch(t *testing.T, conn *pgxpool.Pool) *risk_analysis.AnalyzeBatch {
	t.Helper()
	return risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		&risk_analysis.StubPIIScanner{},
		nil,
		nil,
		nil,
		nil,
		nil,
		newPresidioPub(),
		newGitleaksPub(),
		newCustomRulesPub(),
		mustCELEngine(t),
	)
}

func runAccountIdentityBatch(t *testing.T, ab *risk_analysis.AnalyzeBatch, td testData, policyID uuid.UUID, policyVersion int64, messageIDs []uuid.UUID, messageTypes ...string) {
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
		MessageTypes:   messageTypes,
	})
	require.NoError(t, err)
}

func accountIdentityFindings(t *testing.T, conn *pgxpool.Pool, td testData, policyID uuid.UUID) []testrepo.RiskResult {
	t.Helper()
	rows, err := testrepo.New(conn).ListRiskResultsAll(t.Context(), testrepo.ListRiskResultsAllParams{
		ProjectID:    td.projectID,
		RiskPolicyID: policyID,
	})
	require.NoError(t, err)
	var out []testrepo.RiskResult
	for _, row := range rows {
		if row.Source == "account_identity" && row.Found {
			out = append(out, row)
		}
	}
	return out
}

func TestAnalyzeBatch_AccountIdentityPersonalAccountOnePerChat(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	policyID, policyVersion := seedAccountIdentityPolicy(t, conn, td, nil, nil)

	chatID := seedAccountChat(t, conn, td, "personal", "jane@gmail.com")
	msg1 := seedChatMessage(t, conn, td, chatID)
	msg2 := seedChatMessage(t, conn, td, chatID)

	ab := newAccountIdentityAnalyzeBatch(t, conn)
	runAccountIdentityBatch(t, ab, td, policyID, policyVersion, []uuid.UUID{msg1, msg2})

	findings := accountIdentityFindings(t, conn, td, policyID)
	require.Len(t, findings, 1, "session-scoped finding should be emitted once per chat, not per message")
	assert.Equal(t, "identity.personal_account", findings[0].RuleID.String)
	assert.Equal(t, "jane@gmail.com", findings[0].Match.String)
	assert.Contains(t, []uuid.UUID{msg1, msg2}, findings[0].ChatMessageID)
}

func TestAnalyzeBatch_AccountIdentityDedupesAcrossBatches(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	policyID, policyVersion := seedAccountIdentityPolicy(t, conn, td, nil, nil)

	chatID := seedAccountChat(t, conn, td, "personal", "jane@gmail.com")
	msg1 := seedChatMessage(t, conn, td, chatID)
	msg2 := seedChatMessage(t, conn, td, chatID)

	ab := newAccountIdentityAnalyzeBatch(t, conn)
	runAccountIdentityBatch(t, ab, td, policyID, policyVersion, []uuid.UUID{msg1})
	require.Len(t, accountIdentityFindings(t, conn, td, policyID), 1)

	// A later batch of the same session must not add a second finding.
	runAccountIdentityBatch(t, ab, td, policyID, policyVersion, []uuid.UUID{msg2})
	require.Len(t, accountIdentityFindings(t, conn, td, policyID), 1)

	// Re-analyzing the message that carries the finding rewrites it in place
	// (the writer deletes and re-inserts results for in-batch messages).
	runAccountIdentityBatch(t, ab, td, policyID, policyVersion, []uuid.UUID{msg1})
	findings := accountIdentityFindings(t, conn, td, policyID)
	require.Len(t, findings, 1)
	assert.Equal(t, msg1, findings[0].ChatMessageID)
}

func TestAnalyzeBatch_AccountIdentityVersionBumpReemits(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	policyID, policyVersion := seedAccountIdentityPolicy(t, conn, td, nil, nil)

	chatID := seedAccountChat(t, conn, td, "personal", "jane@gmail.com")
	msg1 := seedChatMessage(t, conn, td, chatID)
	msg2 := seedChatMessage(t, conn, td, chatID)

	ab := newAccountIdentityAnalyzeBatch(t, conn)
	runAccountIdentityBatch(t, ab, td, policyID, policyVersion, []uuid.UUID{msg1})
	require.Len(t, accountIdentityFindings(t, conn, td, policyID), 1)

	// Findings dedupe within a policy version; a bumped version re-emits.
	runAccountIdentityBatch(t, ab, td, policyID, policyVersion+1, []uuid.UUID{msg2})
	require.Len(t, accountIdentityFindings(t, conn, td, policyID), 2)
}

func TestAnalyzeBatch_AccountIdentityUnapprovedDomainMatrix(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	policyID, policyVersion := seedAccountIdentityPolicy(t, conn, td, []string{"acme.com"}, nil)

	cases := []struct {
		accountType string
		email       string
		wantRules   []string
	}{
		{"team", "alice@acme.com", nil},
		{"team", "DAVE@ACME.COM", nil},
		{"team", "bob@other.com", []string{"identity.unapproved_domain"}},
		// Exact-match semantics: subdomains must be listed explicitly.
		{"team", "carol@mail.acme.com", []string{"identity.unapproved_domain"}},
		{"personal", "eve@acme.com", []string{"identity.personal_account"}},
		{"personal", "frank@other.com", []string{"identity.personal_account", "identity.unapproved_domain"}},
	}

	msgByEmail := make(map[uuid.UUID]string)
	var messageIDs []uuid.UUID
	for _, tc := range cases {
		chatID := seedAccountChat(t, conn, td, tc.accountType, tc.email)
		msgID := seedChatMessage(t, conn, td, chatID)
		msgByEmail[msgID] = tc.email
		messageIDs = append(messageIDs, msgID)
	}

	ab := newAccountIdentityAnalyzeBatch(t, conn)
	runAccountIdentityBatch(t, ab, td, policyID, policyVersion, messageIDs)

	rulesByEmail := make(map[string][]string)
	for _, finding := range accountIdentityFindings(t, conn, td, policyID) {
		email := msgByEmail[finding.ChatMessageID]
		rulesByEmail[email] = append(rulesByEmail[email], finding.RuleID.String)
		assert.Equal(t, email, finding.Match.String)
	}

	for _, tc := range cases {
		assert.ElementsMatch(t, tc.wantRules, rulesByEmail[tc.email], "email %s", tc.email)
	}
}

func TestAnalyzeBatch_AccountIdentityEmptyDomainListInert(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	policyID, policyVersion := seedAccountIdentityPolicy(t, conn, td, nil, nil)

	// Team account with an arbitrary email: without a configured domain list
	// the unapproved_domain rule stays inert.
	chatID := seedAccountChat(t, conn, td, "team", "bob@other.com")
	msgID := seedChatMessage(t, conn, td, chatID)

	ab := newAccountIdentityAnalyzeBatch(t, conn)
	runAccountIdentityBatch(t, ab, td, policyID, policyVersion, []uuid.UUID{msgID})

	require.Empty(t, accountIdentityFindings(t, conn, td, policyID))
}

func TestAnalyzeBatch_AccountIdentityUnattributedChatEmitsNothing(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	policyID, policyVersion := seedAccountIdentityPolicy(t, conn, td, []string{"acme.com"}, nil)

	// td.chatID has no user_account_id: no identity, no finding.
	msgID := seedChatMessage(t, conn, td, td.chatID)

	ab := newAccountIdentityAnalyzeBatch(t, conn)
	runAccountIdentityBatch(t, ab, td, policyID, policyVersion, []uuid.UUID{msgID})

	require.Empty(t, accountIdentityFindings(t, conn, td, policyID))
}

func TestAnalyzeBatch_AccountIdentityDisabledRuleFiltered(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	policyID, policyVersion := seedAccountIdentityPolicy(t, conn, td, nil, []string{"identity.personal_account"})

	chatID := seedAccountChat(t, conn, td, "personal", "jane@gmail.com")
	msgID := seedChatMessage(t, conn, td, chatID)

	ab := newAccountIdentityAnalyzeBatch(t, conn)
	runAccountIdentityBatch(t, ab, td, policyID, policyVersion, []uuid.UUID{msgID})

	require.Empty(t, accountIdentityFindings(t, conn, td, policyID))
}

func TestAnalyzeBatch_AccountIdentityBypassesMessageTypeFilter(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	policyID, policyVersion := seedAccountIdentityPolicy(t, conn, td, nil, nil)

	// The batch carries only user messages while the policy is scoped to tool
	// requests: every message is filtered out of the content pipeline, but the
	// session-scoped identity check must still evaluate and flag the chat.
	chatID := seedAccountChat(t, conn, td, "personal", "jane@gmail.com")
	msgID := seedChatMessage(t, conn, td, chatID)

	ab := newAccountIdentityAnalyzeBatch(t, conn)
	runAccountIdentityBatch(t, ab, td, policyID, policyVersion, []uuid.UUID{msgID}, "tool_request")

	findings := accountIdentityFindings(t, conn, td, policyID)
	require.Len(t, findings, 1)
	assert.Equal(t, "identity.personal_account", findings[0].RuleID.String)
	assert.Equal(t, msgID, findings[0].ChatMessageID)
}

func TestAnalyzeBatch_AccountIdentityBypassesCELScope(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	// scope_exempt=true exempts every message from the content pipeline; the
	// session-scoped identity check is deliberately not subject to it.
	policyID, policyVersion := seedAccountIdentityPolicyScoped(t, conn, td, nil, nil, "true")

	chatID := seedAccountChat(t, conn, td, "personal", "jane@gmail.com")
	msgID := seedChatMessage(t, conn, td, chatID)

	ab := newAccountIdentityAnalyzeBatch(t, conn)
	runAccountIdentityBatch(t, ab, td, policyID, policyVersion, []uuid.UUID{msgID})

	findings := accountIdentityFindings(t, conn, td, policyID)
	require.Len(t, findings, 1)
	assert.Equal(t, "identity.personal_account", findings[0].RuleID.String)
}

func TestAnalyzeBatch_AccountIdentityHonorsExclusions(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	policyID, policyVersion := seedAccountIdentityPolicy(t, conn, td, nil, nil)

	// A rule_id exclusion must suppress the session-scoped finding even
	// though it bypasses message scoping.
	_, err := riskrepo.New(conn).CreateRiskExclusion(t.Context(), riskrepo.CreateRiskExclusionParams{
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		RiskPolicyID:   uuid.NullUUID{UUID: policyID, Valid: true},
		MatchType:      "rule_id",
		MatchValue:     "identity.personal_account",
		RuleIDFilter:   pgtype.Text{},
		SourceFilter:   pgtype.Text{},
		Enabled:        true,
	})
	require.NoError(t, err)

	chatID := seedAccountChat(t, conn, td, "personal", "jane@gmail.com")
	msgID := seedChatMessage(t, conn, td, chatID)

	ab := newAccountIdentityAnalyzeBatch(t, conn)
	runAccountIdentityBatch(t, ab, td, policyID, policyVersion, []uuid.UUID{msgID})

	require.Empty(t, accountIdentityFindings(t, conn, td, policyID))
}

func TestAnalyzeBatch_AccountIdentityLateFieldEmitsRemainingRule(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	policyID, policyVersion := seedAccountIdentityPolicy(t, conn, td, []string{"acme.com"}, nil)

	// Identity fields arrive incrementally: the account is classified personal
	// before its email is known, so batch 1 can only fire the personal rule.
	chatID, accountUUID := seedAccountChatWithAccount(t, conn, td, "personal", "")
	msg1 := seedChatMessage(t, conn, td, chatID)
	msg2 := seedChatMessage(t, conn, td, chatID)

	ab := newAccountIdentityAnalyzeBatch(t, conn)
	runAccountIdentityBatch(t, ab, td, policyID, policyVersion, []uuid.UUID{msg1})
	findings := accountIdentityFindings(t, conn, td, policyID)
	require.Len(t, findings, 1)
	require.Equal(t, "identity.personal_account", findings[0].RuleID.String)

	// The email lands later (ingest upserts fill fields as batches carry them).
	// Dedupe is per rule, so the next batch must still emit the domain rule —
	// without duplicating the personal rule.
	setAccountEmail(t, conn, td, accountUUID, "jane@gmail.com")

	runAccountIdentityBatch(t, ab, td, policyID, policyVersion, []uuid.UUID{msg2})
	findings = accountIdentityFindings(t, conn, td, policyID)
	require.Len(t, findings, 2)
	rules := []string{findings[0].RuleID.String, findings[1].RuleID.String}
	require.ElementsMatch(t, []string{"identity.personal_account", "identity.unapproved_domain"}, rules)
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
