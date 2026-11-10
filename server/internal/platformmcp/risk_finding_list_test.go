package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

const findingListEmail = "reporter@example.invalid"

type findingListClickHouse struct {
	rows      []chrepo.RiskFindingListRow
	groups    []chrepo.RiskFindingChatGroup
	rules     []chrepo.RiskOverviewRuleCount
	err       error
	list      []chrepo.ListRiskFindingsParams
	groupBy   []chrepo.GroupRiskFindingsByChatParams
	ruleCalls []struct {
		params    chrepo.RiskOverviewWindowParams
		category  string
		policyIDs []string
		limit     uint64
	}
}

func (s *findingListClickHouse) ListRiskFindings(_ context.Context, p chrepo.ListRiskFindingsParams) ([]chrepo.RiskFindingListRow, error) {
	s.list = append(s.list, p)
	return s.rows, s.err
}

func (s *findingListClickHouse) GroupRiskFindingsByChat(_ context.Context, p chrepo.GroupRiskFindingsByChatParams) ([]chrepo.RiskFindingChatGroup, error) {
	s.groupBy = append(s.groupBy, p)
	return s.groups, s.err
}

func (s *findingListClickHouse) ListRiskRuleCountsByCategory(_ context.Context, p chrepo.RiskOverviewWindowParams, category string, policyIDs []string, limit uint64) ([]chrepo.RiskOverviewRuleCount, error) {
	s.ruleCalls = append(s.ruleCalls, struct {
		params    chrepo.RiskOverviewWindowParams
		category  string
		policyIDs []string
		limit     uint64
	}{p, category, policyIDs, limit})
	return s.rules, s.err
}

func (s *findingListClickHouse) calls() int {
	return len(s.list) + len(s.groupBy) + len(s.ruleCalls)
}

type findingListFixture struct {
	service      *RiskFindingListService
	project      ResolvedProject
	policyReader *findingPolicies
	clickhouse   *findingListClickHouse
	// enabled, disabled, foreign and deleted policies, in that order.
	policies []riskrepo.ListRiskFindingPoliciesRow
}

func newFindingListFixture(t *testing.T) *findingListFixture {
	t.Helper()
	project := ResolvedProject{ID: uuid.New(), Slug: "default", Name: "Project"}
	policies := []riskrepo.ListRiskFindingPoliciesRow{
		{ID: uuid.New(), ProjectID: project.ID, OrganizationID: "<ORG_ID>", Enabled: true, Score: 9.5},
		{ID: uuid.New(), ProjectID: project.ID, OrganizationID: "<ORG_ID>", Enabled: false, Score: 7},
		{ID: uuid.New(), ProjectID: uuid.New(), OrganizationID: "other", Enabled: true, Score: 10},
		{ID: uuid.New(), ProjectID: project.ID, OrganizationID: "<ORG_ID>", Enabled: true, Deleted: true, Score: 10},
	}
	clickhouse := &findingListClickHouse{rows: []chrepo.RiskFindingListRow{
		{
			ID:                 uuid.New(),
			MessageCreatedAt:   riskAnalysisTestNow.Add(-time.Hour),
			ChatMessageID:      uuid.NewString(),
			ChatID:             uuid.NewString(),
			UserID:             "user-internal",
			ExternalUserID:     findingListEmail,
			RiskPolicyID:       policies[0].ID.String(),
			RiskPolicyVersion:  2,
			RuleID:             "secret.stripe",
			Source:             "gitleaks",
			Confidence:         0.9,
			Tags:               []string{"secrets"},
			MatchRedacted:      "sk-l*********************************89",
			ExecutionID:        uuid.NewString(),
			MCPServerID:        uuid.NewString(),
			ToolsetID:          uuid.NewString(),
			ToolName:           "create_issue",
			Phase:              "request",
			MediationSurface:   "hosted_mcp",
			MCPMethod:          "tools/call",
			PrincipalKind:      "user_session",
			IdentityStamped:    true,
			EnforcementOutcome: "denied",
		},
		{ID: uuid.New(), MessageCreatedAt: riskAnalysisTestNow.Add(-2 * time.Hour), ChatID: uuid.NewString(), RiskPolicyID: policies[1].ID.String(), RuleID: "pii.email_address", Source: "presidio", Confidence: 0.6, Tags: nil, MatchRedacted: "<redacted len=24 sha=0123abcd>"},
	}}
	policyReader := &findingPolicies{rows: policies}
	codec := newRiskCursorCodec("test-key")
	service := &RiskFindingListService{projects: &findingProjects{project: project}, policies: policyReader, clickhouse: clickhouse, cursor: codec, now: func() time.Time { return riskAnalysisTestNow }}
	return &findingListFixture{service: service, project: project, policyReader: policyReader, clickhouse: clickhouse, policies: policies}
}

func TestRiskFindingListValidation(t *testing.T) {
	t.Parallel()

	for _, input := range []ListRiskFindingPageInput{
		{From: "yesterday"},
		{To: "later"},
		{From: riskAnalysisTestNow.Format(time.RFC3339), To: riskAnalysisTestNow.Add(-time.Hour).Format(time.RFC3339)},
		{PolicyID: "not-a-uuid"},
		{MCPServerID: "not-a-uuid"},
		{ChatID: "not-a-uuid"},
		{AssistantID: "not-a-uuid"},
		{MCPServerID: "not-a-uuid"},
		{AssistantID: uuid.NewString(), NonAssistant: true},
		{Category: "unknown-category"},
		{RuleID: strings.Repeat("r", 129)},
		{ResultID: "not-a-uuid"},
		{ExecutionID: strings.Repeat("e", 129)},
		{ExecutionID: "   "},
		{UserID: strings.Repeat("u", 257)},
		{Limit: 51},
		{Limit: -1},
		{ProjectID: uuid.NewString(), ProjectSlug: "default"},
	} {
		f := newFindingListFixture(t)
		_, err := f.service.List(t.Context(), testRiskPrincipal("user"), input)
		require.ErrorIs(t, err, ErrRiskReadInvalid, "%+v", input)
		require.Zero(t, f.policyReader.calls, "%+v", input)
		require.Zero(t, f.clickhouse.calls(), "%+v", input)
	}

	f := newFindingListFixture(t)
	_, err := f.service.List(t.Context(), Principal{UserID: "user"}, ListRiskFindingPageInput{})
	require.ErrorIs(t, err, ErrRiskReadInvalid)
	_, err = f.service.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingPageInput{Cursor: "obsolete"})
	require.ErrorIs(t, err, ErrRiskCursorInvalid)
	_, err = f.service.ListByChat(t.Context(), testRiskPrincipal("user"), ListRiskFindingsByChatInput{Limit: 51})
	require.ErrorIs(t, err, ErrRiskReadInvalid)
	_, err = f.service.ListByChat(t.Context(), testRiskPrincipal("user"), ListRiskFindingsByChatInput{Cursor: "obsolete"})
	require.ErrorIs(t, err, ErrRiskCursorInvalid)
	for _, input := range []GetRiskRuleBreakdownInput{
		{},
		{Category: "unknown-category"},
		{Category: "secrets", From: "yesterday"},
		{Category: "secrets", To: "later"},
		{Category: "secrets", From: riskAnalysisTestNow.Add(-32 * 24 * time.Hour).Format(time.RFC3339)},
		{Category: "secrets", From: riskAnalysisTestNow.Format(time.RFC3339)},
		{Category: "secrets", ProjectID: uuid.NewString(), ProjectSlug: "default"},
	} {
		_, err = f.service.RuleBreakdown(t.Context(), testRiskPrincipal("user"), input)
		require.ErrorIs(t, err, ErrRiskReadInvalid, "%+v", input)
	}
	require.Zero(t, f.policyReader.calls)
	require.Zero(t, f.clickhouse.calls())

	var nilService *RiskFindingListService
	_, err = nilService.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingPageInput{})
	require.ErrorIs(t, err, ErrUnavailable)
	require.Panics(t, func() {
		NewRiskFindingListService(nil, nil, "")
	})
}

func TestRiskFindingListDefaultIncludesDisabledExcludesDeleted(t *testing.T) {
	t.Parallel()

	f := newFindingListFixture(t)
	principal := testRiskPrincipal("user")
	out, err := f.service.List(t.Context(), principal, ListRiskFindingPageInput{})
	require.NoError(t, err)
	require.Len(t, f.clickhouse.list, 1)
	expected := []string{f.policies[0].ID.String(), f.policies[1].ID.String()}
	slices.Sort(expected)
	require.Equal(t, expected, f.clickhouse.list[0].PolicyIDs, "enabled and disabled policies are pushed down; foreign and deleted are not")
	require.Len(t, out.Findings, 2)
	require.Equal(t, f.policies[1].ID.String(), out.Findings[1].PolicyID)
	require.Equal(t, "high", out.Findings[1].Severity, "a disabled policy's finding scores from that policy")
	require.Equal(t, f.clickhouse.rows[0].MCPServerID, out.Findings[0].MCPServerID)
	require.Equal(t, "create_issue", out.Findings[0].ToolName)
	require.Equal(t, "denied", out.Findings[0].EnforcementOutcome)

	// An explicit filter on a deleted policy short-circuits without a read.
	empty, err := f.service.List(t.Context(), principal, ListRiskFindingPageInput{PolicyID: f.policies[3].ID.String()})
	require.NoError(t, err)
	require.Empty(t, empty.Findings)
	require.Len(t, f.clickhouse.list, 1)
}

func TestRiskFindingListFiltersAndCursorBinding(t *testing.T) {
	t.Parallel()

	f := newFindingListFixture(t)
	principal := testRiskPrincipal("user")
	mcpServerID := uuid.New()
	input := ListRiskFindingPageInput{Limit: 1, MCPServerID: strings.ToUpper(mcpServerID.String()), Category: "secrets", RuleID: "secret", UserID: "reporter", UniqueMatch: true, NonAssistant: true}
	out, err := f.service.List(t.Context(), principal, input)
	require.NoError(t, err)
	require.Len(t, out.Findings, 1)
	require.NotEmpty(t, out.NextCursor)
	require.Equal(t, riskFindingListLimitations, out.Limitations)
	params := f.clickhouse.list[0]
	require.Equal(t, mcpServerID.String(), params.MCPServerID)
	require.Equal(t, "secrets", params.Category)
	require.Equal(t, "secret", params.RuleIDSubstr)
	require.Equal(t, "reporter", params.UserIDSubstr)
	require.True(t, params.UniqueMatch)
	require.True(t, params.NonAssistant)
	require.Empty(t, params.ChatID)

	encoded, err := json.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "reporter")

	// A cursor is bound to its filters, project and principal.
	cursorInput := input
	cursorInput.Cursor = out.NextCursor
	_, err = f.service.List(t.Context(), principal, ListRiskFindingPageInput{Limit: 1, Category: "pii", Cursor: out.NextCursor})
	require.ErrorIs(t, err, ErrRiskCursorInvalid)
	_, err = f.service.List(t.Context(), testRiskPrincipal("other-user"), cursorInput)
	require.ErrorIs(t, err, ErrRiskCursorInvalid)
	tampered := cursorInput
	tampered.Cursor += "x"
	_, err = f.service.List(t.Context(), principal, tampered)
	require.ErrorIs(t, err, ErrRiskCursorInvalid)
	require.Len(t, f.clickhouse.list, 1, "refused cursors must not reach storage")
}

func TestRiskFindingListLabelsAreBounded(t *testing.T) {
	t.Parallel()

	f := newFindingListFixture(t)
	f.clickhouse.rows = f.clickhouse.rows[:1]
	f.clickhouse.rows[0].RuleID = "rule\twith-control"
	f.clickhouse.rows[0].Tags = []string{strings.Repeat("t", 500)}
	out, err := f.service.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingPageInput{})
	require.NoError(t, err)
	require.Len(t, out.Findings, 1)
	require.NotContains(t, out.Findings[0].RuleID, "\t")
	require.Len(t, out.Findings[0].Tags, 1)
	require.Less(t, len(out.Findings[0].Tags[0]), 200)
}

func TestRiskFindingListPushdownAndRedaction(t *testing.T) {
	t.Parallel()

	f := newFindingListFixture(t)
	principal := testRiskPrincipal("user")
	from := riskAnalysisTestNow.Add(-24 * time.Hour)
	out, err := f.service.List(t.Context(), principal, ListRiskFindingPageInput{Limit: 1, From: from.Format(time.RFC3339), AssistantID: uuid.Nil.String()})
	require.NoError(t, err)
	require.Equal(t, 1, f.policyReader.calls)
	require.Len(t, f.clickhouse.list, 1)
	params := f.clickhouse.list[0]
	require.Equal(t, "<ORG_ID>", params.OrganizationID)
	require.Equal(t, f.project.ID.String(), params.ProjectID)
	require.NotNil(t, params.From)
	require.Equal(t, from, *params.From)
	require.Nil(t, params.To)
	require.Equal(t, uuid.Nil.String(), params.AssistantID)
	require.EqualValues(t, 2, params.Limit)
	require.Nil(t, params.CursorTime)

	require.Len(t, out.Findings, 1)
	require.NotEmpty(t, out.NextCursor)
	encoded, err := json.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "sk-l")
	require.NotContains(t, string(encoded), findingListEmail)
	first := out.Findings[0]
	require.Equal(t, findingEvidence("sk-l*********************************89", "<ORG_ID>"), first.MatchRedacted)
	require.True(t, canonicalFindingEvidence.MatchString(first.MatchRedacted), "a partial-mask display sample is re-redacted into the canonical marker")
	require.Equal(t, f.clickhouse.rows[0].ID.String(), first.ID)
	require.Equal(t, f.policies[0].ID.String(), first.PolicyID)
	require.EqualValues(t, 2, first.PolicyVersion)
	require.Equal(t, "secrets", first.Category)
	require.Equal(t, "critical", first.Severity)
	require.InDelta(t, 9.5, first.Score, 0.001)
	require.Equal(t, f.clickhouse.rows[0].ChatID, first.ChatID)
	require.Equal(t, f.clickhouse.rows[0].ChatMessageID, first.ChatMessageID)
	require.Equal(t, riskAnalysisTestNow.Add(-time.Hour).Format(time.RFC3339Nano), first.MessageCreatedAt)
	require.Equal(t, []string{"secrets"}, first.Tags)
	require.Equal(t, riskUserReference(f.service.cursor.key, "<ORG_ID>", findingListEmail), first.UserReference)
	require.True(t, strings.HasPrefix(first.UserReference, "user:"))

	// The next page resumes strictly after the last returned row.
	f.clickhouse.rows = f.clickhouse.rows[1:]
	next, err := f.service.List(t.Context(), principal, ListRiskFindingPageInput{Limit: 1, From: from.Format(time.RFC3339), AssistantID: uuid.Nil.String(), Cursor: out.NextCursor})
	require.NoError(t, err)
	require.Len(t, next.Findings, 1)
	require.Empty(t, next.NextCursor)
	params = f.clickhouse.list[1]
	require.NotNil(t, params.CursorTime)
	require.Equal(t, riskAnalysisTestNow.Add(-time.Hour), *params.CursorTime)
	require.Equal(t, first.ID, params.CursorID.UUID.String())
	// A canonical marker stored at ingest passes through unchanged.
	require.Equal(t, "<redacted len=24 sha=0123abcd>", next.Findings[0].MatchRedacted)
	require.Empty(t, next.Findings[0].UserReference)
	require.Equal(t, []string{}, next.Findings[0].Tags)

	// An explicit policy filter narrows the pushdown to that policy, including a
	// disabled one, and an unknown policy short-circuits to an empty page.
	_, err = f.service.List(t.Context(), principal, ListRiskFindingPageInput{PolicyID: f.policies[1].ID.String()})
	require.NoError(t, err)
	require.Equal(t, []string{f.policies[1].ID.String()}, f.clickhouse.list[2].PolicyIDs)
	empty, err := f.service.List(t.Context(), principal, ListRiskFindingPageInput{PolicyID: f.policies[2].ID.String()})
	require.NoError(t, err)
	require.Empty(t, empty.Findings)
	require.Len(t, f.clickhouse.list, 3, "a foreign policy never reaches the store")

	// Chat scoping combines with other filters.
	chatID := uuid.NewString()
	_, err = f.service.List(t.Context(), principal, ListRiskFindingPageInput{ChatID: chatID, RuleID: "secret"})
	require.NoError(t, err)
	require.Equal(t, chatID, f.clickhouse.list[3].ChatID)
	require.Equal(t, "secret", f.clickhouse.list[3].RuleIDSubstr)

	f.clickhouse.err = errors.New("private database detail")
	_, err = f.service.List(t.Context(), principal, ListRiskFindingPageInput{})
	require.ErrorIs(t, err, ErrUnavailable)
	require.NotContains(t, err.Error(), "private database detail")
}

func TestRiskFindingListFailsClosed(t *testing.T) {
	t.Parallel()

	f := newFindingListFixture(t)
	projects, ok := f.service.projects.(*findingProjects)
	require.True(t, ok)
	projects.err = ErrRiskReadNotFound
	_, err := f.service.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingPageInput{ProjectSlug: "missing"})
	require.ErrorIs(t, err, ErrRiskReadNotFound)
	require.Zero(t, f.policyReader.calls)

	f = newFindingListFixture(t)
	policy := f.policies[0]
	f.policyReader.rows = nil
	for range riskFindingPolicyLimit + 1 {
		row := policy
		row.ID = uuid.New()
		f.policyReader.rows = append(f.policyReader.rows, row)
	}
	_, err = f.service.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingPageInput{})
	require.ErrorIs(t, err, ErrUnavailable)
	require.Zero(t, f.clickhouse.calls())
}

func TestRiskFindingsByChatPagination(t *testing.T) {
	t.Parallel()

	f := newFindingListFixture(t)
	principal := testRiskPrincipal("user")
	chats := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	for i, id := range chats {
		f.clickhouse.groups = append(f.clickhouse.groups, chrepo.RiskFindingChatGroup{ChatID: id.String(), ExternalUserID: findingListEmail, FindingsCount: uint64(i + 1), LatestDetected: riskAnalysisTestNow.Add(-time.Duration(i) * time.Hour)}) // #nosec G115 -- small test loop index.
	}
	out, err := f.service.ListByChat(t.Context(), principal, ListRiskFindingsByChatInput{Limit: 2})
	require.NoError(t, err)
	require.Len(t, out.Chats, 2)
	require.NotEmpty(t, out.NextCursor)
	require.Len(t, f.clickhouse.groupBy, 1)
	expected := []string{f.policies[0].ID.String(), f.policies[1].ID.String()}
	slices.Sort(expected)
	require.Equal(t, expected, f.clickhouse.groupBy[0].PolicyIDs, "every non-deleted policy, disabled included, is rolled up")
	require.Empty(t, f.clickhouse.groupBy[0].CursorChatID)
	require.EqualValues(t, 3, f.clickhouse.groupBy[0].Limit)
	encoded, err := json.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), findingListEmail)
	require.Equal(t, chats[0].String(), out.Chats[0].ChatID)
	require.Equal(t, riskUserReference(f.service.cursor.key, "<ORG_ID>", findingListEmail), out.Chats[0].UserReference)
	require.EqualValues(t, 1, out.Chats[0].FindingsCount)
	require.Equal(t, riskAnalysisTestNow.Format(time.RFC3339Nano), out.Chats[0].LatestDetectedAt)

	// The next page resumes at the extra row's chat id inclusive.
	f.clickhouse.groups = f.clickhouse.groups[2:]
	next, err := f.service.ListByChat(t.Context(), principal, ListRiskFindingsByChatInput{Limit: 2, Cursor: out.NextCursor})
	require.NoError(t, err)
	require.Len(t, next.Chats, 1)
	require.Empty(t, next.NextCursor)
	require.Equal(t, chats[2].String(), f.clickhouse.groupBy[1].CursorChatID)
	_, err = f.service.ListByChat(t.Context(), testRiskPrincipal("other-user"), ListRiskFindingsByChatInput{Limit: 2, Cursor: out.NextCursor})
	require.ErrorIs(t, err, ErrRiskCursorInvalid)
	_, err = f.service.List(t.Context(), principal, ListRiskFindingPageInput{Cursor: out.NextCursor})
	require.ErrorIs(t, err, ErrRiskCursorInvalid, "a by-chat cursor is not a findings cursor")

	f.policyReader.rows = nil
	empty, err := f.service.ListByChat(t.Context(), principal, ListRiskFindingsByChatInput{})
	require.NoError(t, err)
	require.Empty(t, empty.Chats)
	require.Len(t, f.clickhouse.groupBy, 2, "no visible policy means nothing to roll up")
}

func TestRiskRuleBreakdown(t *testing.T) {
	t.Parallel()

	f := newFindingListFixture(t)
	principal := testRiskPrincipal("user")
	f.clickhouse.rules = []chrepo.RiskOverviewRuleCount{{RuleID: "secret.stripe", Source: "gitleaks", Findings: 7}, {RuleID: "secret.aws", Source: "gitleaks", Findings: 3}}
	out, err := f.service.RuleBreakdown(t.Context(), principal, GetRiskRuleBreakdownInput{Category: "secrets"})
	require.NoError(t, err)
	require.Equal(t, "secrets", out.Category)
	require.Equal(t, riskAnalysisTestNow.Format(time.RFC3339Nano), out.To)
	require.Equal(t, time.Date(2026, time.September, 9, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), out.From, "defaults to the start of the day six days before to")
	require.EqualValues(t, 10, out.Total)
	require.False(t, out.Truncated)
	require.Equal(t, []RiskRuleCount{{RuleID: "secret.stripe", Source: "gitleaks", Findings: 7}, {RuleID: "secret.aws", Source: "gitleaks", Findings: 3}}, out.Rules)
	require.Equal(t, 1, f.policyReader.calls)
	require.Len(t, f.clickhouse.ruleCalls, 1)
	call := f.clickhouse.ruleCalls[0]
	require.Equal(t, "secrets", call.category)
	require.EqualValues(t, riskRuleBreakdownLimit+1, call.limit, "one row past the bound detects truncation without an unbounded fetch")
	require.Equal(t, f.project.ID.String(), call.params.ProjectID)
	require.Equal(t, riskAnalysisTestNow, call.params.To)
	expectedPolicies := []string{f.policies[0].ID.String(), f.policies[1].ID.String()}
	slices.Sort(expectedPolicies)
	require.Equal(t, expectedPolicies, call.policyIDs, "every non-deleted policy, disabled included, is pushed down; foreign and deleted ones are not")

	explicit, err := f.service.RuleBreakdown(t.Context(), principal, GetRiskRuleBreakdownInput{Category: "pii", From: riskAnalysisTestNow.Add(-31 * 24 * time.Hour).Format(time.RFC3339), To: riskAnalysisTestNow.Format(time.RFC3339)})
	require.NoError(t, err)
	require.Equal(t, riskAnalysisTestNow.Add(-31*24*time.Hour).Format(time.RFC3339Nano), explicit.From)

	f.clickhouse.rules = nil
	for i := range riskRuleBreakdownLimit + 1 {
		f.clickhouse.rules = append(f.clickhouse.rules, chrepo.RiskOverviewRuleCount{RuleID: "rule-" + strconv.Itoa(i), Source: "gitleaks", Findings: 2})
	}
	out, err = f.service.RuleBreakdown(t.Context(), principal, GetRiskRuleBreakdownInput{Category: "secrets"})
	require.NoError(t, err)
	require.True(t, out.Truncated)
	require.Len(t, out.Rules, riskRuleBreakdownLimit)
	require.EqualValues(t, 2*riskRuleBreakdownLimit, out.Total)

	f.policyReader.rows = nil
	empty, err := f.service.RuleBreakdown(t.Context(), principal, GetRiskRuleBreakdownInput{Category: "secrets"})
	require.NoError(t, err)
	require.Empty(t, empty.Rules)
	require.Len(t, f.clickhouse.ruleCalls, 3, "no visible policy means nothing to count")
	f.policyReader.rows = f.policies

	f.clickhouse.err = errors.New("private database detail")
	_, err = f.service.RuleBreakdown(t.Context(), principal, GetRiskRuleBreakdownInput{Category: "secrets"})
	require.ErrorIs(t, err, ErrUnavailable)
	require.NotContains(t, err.Error(), "private database detail")
}

func TestRiskFindingListToolsMCPInProcess(t *testing.T) {
	t.Parallel()

	f := newFindingListFixture(t)
	f.clickhouse.rows = append(f.clickhouse.rows, f.clickhouse.rows[1])
	server := mcp.NewServer(&mcp.Implementation{Name: "finding-list-test", Version: "1"}, nil)
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerRiskFindingListTools(reg, &budgetedRiskFindingList{service: f.service, budget: allowBudget()})
	for _, name := range []string{riskFindingListToolName, riskFindingByChatToolName, riskRuleBreakdownToolName} {
		descriptor := descriptorByName(t, reg, name)
		require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization)
		require.Equal(t, ProjectScopeDefaultable, descriptor.Meta.ProjectScope)
		require.ElementsMatch(t, bothAudiences, descriptor.Meta.Audiences)
		require.True(t, descriptor.Annotations.ReadOnlyHint)
		require.True(t, validRiskTelemetryTool(name))
		require.True(t, validOptionalFeedbackTool(name))
		require.NotContains(t, descriptor.Description, "unavailable in this deployment")
		require.Contains(t, string(descriptor.InputSchema), `"project_slug"`)
	}
	findingDescriptor := descriptorByName(t, reg, riskFindingListToolName)
	require.Contains(t, string(findingDescriptor.InputSchema), `"secrets"`, "categories are enumerated so the model picks a real key")
	require.Contains(t, string(findingDescriptor.InputSchema), `"mcp_server_id"`)
	require.Contains(t, string(findingDescriptor.InputSchema), `"result_id"`)
	require.Contains(t, string(findingDescriptor.InputSchema), `"execution_id"`)
	outputSchema, err := json.Marshal(inferOutputSchema[ListRiskFindingPageOutput](riskFindingListToolName))
	require.NoError(t, err)
	require.Contains(t, string(outputSchema), "the mediated MCP execution (tool call, resource read or prompt get) that raised this finding")
	require.Contains(t, findingDescriptor.Description, "mediation_surface")
	require.Contains(t, string(descriptorByName(t, reg, riskRuleBreakdownToolName).InputSchema), `"required":["category"]`)

	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			return next(ContextWithPrincipal(ctx, testRiskPrincipal("user")), method, req)
		}
	})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(t.Context(), st, nil)
	require.NoError(t, err)
	defer func() { _ = ss.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "finding-list-client", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), ct, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()
	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, tools.Tools, 3)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: riskFindingListToolName, Arguments: map[string]any{"limit": 2}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "sk-l", "stored display samples never cross the MCP boundary")
	require.NotContains(t, string(encoded), findingListEmail)
	var out ListRiskFindingPageOutput
	require.NoError(t, json.Unmarshal(must(json.Marshal(result.StructuredContent)), &out))
	require.Len(t, out.Findings, 2)
	require.NotEmpty(t, out.NextCursor)

	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: riskFindingListToolName, Arguments: map[string]any{"limit": 500}})
	require.NoError(t, err)
	require.True(t, result.IsError, "schema bounds are enforced before the handler runs")
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: riskFindingListToolName, Arguments: map[string]any{"cursor": "obsolete"}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, `"code":"invalid_request"`)
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: riskRuleBreakdownToolName, Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, result.IsError, "category is required")
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: riskFindingByChatToolName, Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, result.IsError)

	calls := f.clickhouse.calls()
	reg.withExternalAuthorizer(denyExternalCallAuthorizer{err: &ExternalAuthorizationError{RequiredScope: "org:admin", cause: ErrForbidden}})
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: riskFindingListToolName, Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Equal(t, calls, f.clickhouse.calls(), "authorization denial must not reach storage")
}

func must(value []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return value
}

func TestRiskFindingListToolsStub(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "finding-list-stub", Version: "1"}, nil)
	reg := newRegistrar(server)
	registerRiskFindingListTools(reg, nil)
	for name, arguments := range map[string]string{riskFindingListToolName: `{}`, riskFindingByChatToolName: `{}`, riskRuleBreakdownToolName: `{"category":"secrets"}`} {
		d := descriptorByName(t, reg, name)
		require.Contains(t, d.Description, "unavailable in this deployment")
		require.ElementsMatch(t, bothAudiences, d.Meta.Audiences)
		_, err := d.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(arguments))
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal)
		require.Contains(t, refusal.Payload, unavailableCode)
	}

	// The whole-server composition serves the stubs without a Postgres reader
	// and exactly once each.
	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	names := map[string]int{}
	for _, d := range registrar.Descriptors() {
		names[d.Name]++
	}
	for _, name := range []string{riskFindingListToolName, riskFindingByChatToolName, riskRuleBreakdownToolName} {
		require.Equal(t, 1, names[name], name)
	}
}

func TestRiskFindingListReaderRequiresBudget(t *testing.T) {
	t.Parallel()

	f := newFindingListFixture(t)
	reader := (&PostgresReader{}).WithRiskFindingList(f.service, allowBudget())
	limited, ok := reader.riskFindingList.(*budgetedRiskFindingList)
	require.True(t, ok, "production reader must attach a metered service")
	require.Same(t, f.service, limited.service)
	require.True(t, limited.valid())

	reader.WithRiskFindingList(f.service, OperationBudget{})
	require.False(t, reader.riskFindingList.valid(), "missing production budget must disable the tools")
	require.Nil(t, (&PostgresReader{}).WithRiskFindingList(nil, allowBudget()).riskFindingList)
}

func TestRiskFindingListBudgetChargesBeforeEveryRead(t *testing.T) {
	t.Parallel()

	principal := Principal{ConnectionID: "connection", OrganizationID: "organization"}
	for _, tc := range []struct {
		name    string
		allowed bool
		err     error
		wantErr error
	}{
		{name: "allowed", allowed: true},
		{name: "denied", wantErr: ErrOperationRateLimited},
		{name: "unavailable", err: errors.New("store unavailable"), wantErr: ErrOperationBudgetUnavailable},
	} {
		connection := &recordingOperationLimiter{result: ratelimit.Result{Allowed: tc.allowed}, err: tc.err}
		organization := &recordingOperationLimiter{result: ratelimit.Result{Allowed: tc.allowed}, err: tc.err}
		f := newFindingListFixture(t)
		limited := &budgetedRiskFindingList{service: f.service, budget: OperationBudget{Connection: connection, Organization: organization}}
		require.True(t, limited.valid())

		_, err := limited.List(t.Context(), principal, ListRiskFindingPageInput{})
		require.ErrorIs(t, err, tc.wantErr, tc.name)
		_, err = limited.ListByChat(t.Context(), principal, ListRiskFindingsByChatInput{})
		require.ErrorIs(t, err, tc.wantErr, tc.name)
		_, err = limited.RuleBreakdown(t.Context(), principal, GetRiskRuleBreakdownInput{Category: "secrets"})
		require.ErrorIs(t, err, tc.wantErr, tc.name)
		require.Len(t, connection.keys, 3, tc.name)
		if tc.wantErr != nil {
			require.Zero(t, f.policyReader.calls, tc.name)
			continue
		}
		require.Equal(t, 3, f.policyReader.calls, tc.name)
	}

	limited := &budgetedRiskFindingList{service: newFindingListFixture(t).service, budget: OperationBudget{}}
	require.False(t, limited.valid())
	_, err := limited.List(t.Context(), principal, ListRiskFindingPageInput{})
	require.ErrorIs(t, err, ErrOperationBudgetUnavailable)
}

func TestRiskFindingListBudgetPreservesServiceError(t *testing.T) {
	t.Parallel()

	f := newFindingListFixture(t)
	limited := &budgetedRiskFindingList{service: f.service, budget: allowBudget()}
	_, err := limited.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingPageInput{Limit: 99})
	require.ErrorIs(t, err, ErrRiskReadInvalid)
	require.ErrorContains(t, err, "list risk finding page")
}

func TestRiskFindingListMCPServerFilter(t *testing.T) {
	t.Parallel()

	serverID := uuid.NewString()

	f := newFindingListFixture(t)
	_, err := f.service.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingPageInput{MCPServerID: strings.ToUpper(serverID)})
	require.NoError(t, err)
	require.Len(t, f.clickhouse.list, 1)
	require.Equal(t, serverID, f.clickhouse.list[0].MCPServerID, "the analytics store filters on the canonical server id")
}

func TestRiskFindingListResultAndExecutionFilters(t *testing.T) {
	t.Parallel()

	resultID := uuid.New()
	f := newFindingListFixture(t)
	_, err := f.service.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingPageInput{ResultID: strings.ToUpper(resultID.String()), ExecutionID: "  exec-1  "})
	require.NoError(t, err)
	require.Len(t, f.clickhouse.list, 1)
	require.Equal(t, uuid.NullUUID{UUID: resultID, Valid: true}, f.clickhouse.list[0].ResultID)
	require.Equal(t, "exec-1", f.clickhouse.list[0].ExecutionID)
}

func TestRiskFindingListCanonicalizesUUIDExecutionID(t *testing.T) {
	t.Parallel()

	executionID := uuid.NewString()
	f := newFindingListFixture(t)
	_, err := f.service.List(t.Context(), testRiskPrincipal("user"), ListRiskFindingPageInput{ExecutionID: strings.ToUpper(executionID)})
	require.NoError(t, err)
	require.Len(t, f.clickhouse.list, 1)
	require.Equal(t, executionID, f.clickhouse.list[0].ExecutionID)
}

func TestRiskFindingListCursorBindsResultAndExecutionFilters(t *testing.T) {
	t.Parallel()

	unfiltered := riskFindingFilters{}
	require.NotEqual(t, unfiltered.cursorKind(), riskFindingFilters{ExecutionID: "exec-1"}.cursorKind())
	require.NotEqual(t, unfiltered.cursorKind(), riskFindingFilters{ResultID: uuid.NewString()}.cursorKind())
}
