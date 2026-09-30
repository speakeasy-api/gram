package platformmcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// stubUserSearchTelemetry satisfies the overview read model with a fixed
// watermark; the people-search tests only need the envelope it feeds.
type stubUserSearchTelemetry struct {
	watermark int64
}

func (s stubUserSearchTelemetry) GetMCPOutcomeBreakdown(context.Context, telemetryrepo.GetMCPOutcomeBreakdownParams) ([]telemetryrepo.MCPOutcomeBreakdownRow, error) {
	return nil, nil
}

func (s stubUserSearchTelemetry) GetTelemetryWatermark(context.Context, telemetryrepo.GetTelemetryWatermarkParams) (int64, error) {
	return s.watermark, nil
}

func (s stubUserSearchTelemetry) GetOverviewSummary(context.Context, telemetryrepo.GetOverviewSummaryParams) (*telemetryrepo.OverviewSummary, error) {
	return nil, nil
}

func (s stubUserSearchTelemetry) GetActiveCounts(context.Context, telemetryrepo.GetActiveCountsParams) (*telemetryrepo.ActiveCounts, error) {
	return nil, nil
}

// GetUnifiedActiveServerCount satisfies the overview read model; the user
// search tests do not assert on the active-server tally.
func (s stubUserSearchTelemetry) GetUnifiedActiveServerCount(context.Context, telemetryrepo.GetTopServersParams) (uint64, error) {
	return 0, nil
}

func (s stubUserSearchTelemetry) GetTopServers(context.Context, telemetryrepo.GetTopServersParams) ([]telemetryrepo.TopServer, error) {
	return nil, nil
}

func (s stubUserSearchTelemetry) GetSkillsSummary(context.Context, telemetryrepo.GetSkillsSummaryParams) ([]telemetryrepo.SkillSummaryRow, error) {
	return nil, nil
}

func (s stubUserSearchTelemetry) GetSkillBreakdown(context.Context, telemetryrepo.GetSkillBreakdownParams) ([]telemetryrepo.SkillBreakdownRow, error) {
	return nil, nil
}

// recordingUserSearchReader captures the repository parameters each call built
// and serves canned rows.
type recordingUserSearchReader struct {
	searchParams  []telemetryrepo.SearchUsersParams
	rows          []telemetryrepo.UserSummary
	metricsParams []telemetryrepo.GetUserMetricsSummaryParams
	metrics       *telemetryrepo.MetricsSummaryRow
}

func (r *recordingUserSearchReader) SearchUsers(_ context.Context, arg telemetryrepo.SearchUsersParams) ([]telemetryrepo.UserSummary, error) {
	r.searchParams = append(r.searchParams, arg)
	return r.rows, nil
}

func (r *recordingUserSearchReader) GetUserMetricsSummary(_ context.Context, arg telemetryrepo.GetUserMetricsSummaryParams) (*telemetryrepo.MetricsSummaryRow, error) {
	r.metricsParams = append(r.metricsParams, arg)
	return r.metrics, nil
}

type recordingUserSearchAuditor struct {
	attributions []string
}

func (a *recordingUserSearchAuditor) RecordUserMCPStatusRead(context.Context, Principal, string, string, string, string) error {
	return nil
}

func (a *recordingUserSearchAuditor) RecordUsageAttributionRead(_ context.Context, _ Principal, projectID, targetKind, target, maskedIdentity, window string) error {
	a.attributions = append(a.attributions, strings.Join([]string{projectID, targetKind, target, maskedIdentity, window}, "|"))
	return nil
}

type stubIdentityGate struct {
	org string
}

func (g stubIdentityGate) CanonicalOrgFor(context.Context, string) string {
	return g.org
}

var userSearchTestNow = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

const (
	userSearchTestProject = "00000000-0000-0000-0000-000000000001"
	userSearchTestMCP     = "00000000-0000-0000-0000-000000000002"
)

// newUserSearchService composes the people search without Postgres or
// ClickHouse: every dependency it reaches is a fake.
func newUserSearchService(t *testing.T, reader UserSearchReader, auditor DrilldownAuditor, gate CanonicalIdentityGate) *DiagnosticsService {
	t.Helper()

	codec, err := newSubjectReferenceCodec("user-search-test-key")
	require.NoError(t, err)
	return &DiagnosticsService{
		db:              nil,
		telemetry:       stubUserSearchTelemetry{watermark: userSearchTestNow.Add(-time.Minute).UnixNano()},
		drilldown:       nil,
		userSearch:      reader,
		references:      codec,
		sensitiveBudget: allowBudget(),
		volume:          DrilldownVolumeBudget{Rows: allowOperationLimiter{}, MetricQueries: allowOperationLimiter{}},
		auditor:         auditor,
		sessions:        nil,
		sessionCapture:  nil,
		reader:          diagnosticsProjectReader{output: ListProjectsOutput{}},
		readiness:       nil,
		budget:          allowBudget(),
		identityGate:    gate,
		now:             func() time.Time { return userSearchTestNow },
	}
}

func userSummaryRow(key, email string, success, failure uint64) telemetryrepo.UserSummary {
	return telemetryrepo.UserSummary{
		UserID:                   key,
		UserEmail:                email,
		FirstSeenUnixNano:        userSearchTestNow.Add(-2 * time.Hour).UnixNano(),
		LastSeenUnixNano:         userSearchTestNow.Add(-time.Hour).UnixNano(),
		TotalChats:               3,
		TotalChatRequests:        9,
		TotalInputTokens:         1000,
		TotalOutputTokens:        500,
		TotalTokens:              1500,
		CacheReadInputTokens:     0,
		CacheCreationInputTokens: 0,
		AvgTokensPerReq:          150,
		TotalCost:                1.25,
		TotalToolCalls:           success + failure,
		ToolCallSuccess:          success,
		ToolCallFailure:          failure,
		ToolCounts:               map[string]uint64{"mcp__payments__refund": success + failure},
		ToolSuccessCounts:        map[string]uint64{"mcp__payments__refund": success},
		ToolFailureCounts:        map[string]uint64{"mcp__payments__refund": failure},
		HookSourceCounts:         map[string]uint64{"claude-code": success + failure},
		AccountTypes:             []string{"team"},
		RawUserIDs:               []string{"user-42"},
	}
}

// TestSearchUsersOutput_ProjectsOnlyAllowlistedFields pins the serialized
// shape. The projection is positive: a field that is not listed here is not
// served, so an addition has to be made deliberately here first.
func TestSearchUsersOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	window, err := resolveWindow("", userSearchTestNow, userSearchWindowSpec)
	require.NoError(t, err)
	require.Equal(t, DiagnosticWindowLastWeek, window.Window)
	output := SearchUsersOutput{
		ProjectID: userSearchTestProject,
		UserType:  UserTypeInternal,
		Envelope:  newDataEnvelope(userSearchTestNow, userSearchTestNow.Add(-time.Minute), window, true),
		Users: []UserSearchMatch{{
			UserReference:  "opaque",
			MaskedIdentity: "p***@e***",
			Activity:       "mixed",
			Errors:         "observed",
			LastSeenAt:     userSearchTestNow.Format(time.RFC3339),
		}},
		NextCursor: "opaque-cursor",
	}

	require.ElementsMatch(t, []string{
		"project_id", "user_type",
		"data", "queried_at", "data_through", "freshness", "no_observations", "resolved_window", "window", "from", "to",
		"users", "user_reference", "masked_identity", "activity", "errors", "last_seen_at",
		"next_cursor",
	}, decodeKeys(t, output))
}

func TestGetUserMetricsSummaryOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	window, err := resolveWindow("30d", userSearchTestNow, userSearchWindowSpec)
	require.NoError(t, err)
	output := GetUserMetricsSummaryOutput{
		ProjectID:       userSearchTestProject,
		Envelope:        newDataEnvelope(userSearchTestNow, userSearchTestNow.Add(-time.Minute), window, true),
		MaskedIdentity:  "p***@e***",
		Activity:        SubjectStateActive,
		FirstSeenAt:     userSearchTestNow.Format(time.RFC3339),
		LastSeenAt:      userSearchTestNow.Format(time.RFC3339),
		ToolCalls:       12,
		FailedToolCalls: 3,
		FailureRate:     0.25,
		ActiveServers:   1,
		TopTools:        []UserMetricsTool{{Tool: "mcp__payments__refund", Server: "payments", Calls: 12, Failures: 3}},
		ToolsTruncated:  false,
	}

	// Note what is absent: no email, no user id, no tokens, no cost.
	require.ElementsMatch(t, []string{
		"project_id",
		"data", "queried_at", "data_through", "freshness", "no_observations", "resolved_window", "window", "from", "to",
		"masked_identity", "activity", "first_seen_at", "last_seen_at",
		"tool_calls", "failed_tool_calls", "failure_rate", "active_servers",
		"top_tools", "tool", "server", "calls", "failures", "tools_truncated",
	}, decodeKeys(t, output))
}

func TestNormalizeUserSearch_RefusesInvalidInput(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]SearchUsersInput{
		"missing project":       {ProjectID: " ", Query: "pat"},
		"empty query":           {ProjectID: userSearchTestProject, Query: "  "},
		"two-character query":   {ProjectID: userSearchTestProject, Query: "pa"},
		"two-rune query":        {ProjectID: userSearchTestProject, Query: "ませ"},
		"oversized query":       {ProjectID: userSearchTestProject, Query: strings.Repeat("x", maxUserSearchQueryLength+1)},
		"unknown user type":     {ProjectID: userSearchTestProject, Query: "pat", UserType: "agent"},
		"managed tool vocab":    {ProjectID: userSearchTestProject, Query: "pat", UserType: "employee"},
		"whitespace-only query": {ProjectID: userSearchTestProject, Query: " \t "},
	} {
		_, err := normalizeUserSearch(input)
		require.ErrorIs(t, err, ErrUserSearchInvalid, name)
	}
}

func TestNormalizeUserSearch_AppliesDefaultsAndCaps(t *testing.T) {
	t.Parallel()

	search, err := normalizeUserSearch(SearchUsersInput{ProjectID: " " + userSearchTestProject + " ", Query: " Pat ", UserType: "", Limit: 0})
	require.NoError(t, err)
	require.Equal(t, userSearchTestProject, search.projectID)
	require.Equal(t, "Pat", search.query)
	require.Equal(t, UserTypeInternal, search.userType)
	require.Equal(t, defaultUserSearchLimit, search.limit)

	search, err = normalizeUserSearch(SearchUsersInput{ProjectID: userSearchTestProject, Query: "ませて", UserType: " External ", Limit: 500})
	require.NoError(t, err)
	require.Equal(t, UserTypeExternal, search.userType)
	require.Equal(t, maxUserSearchLimit, search.limit)
}

func TestSearchUsers_MasksIdentitiesAndMintsProjectReferences(t *testing.T) {
	t.Parallel()

	reader := &recordingUserSearchReader{rows: []telemetryrepo.UserSummary{
		userSummaryRow("pat.rivera@example.com", "pat.rivera@example.com", 4, 2),
		userSummaryRow("user-orphan", "", 3, 0),
		userSummaryRow("quinn.patel@example.com", "quinn.patel@example.com", 0, 0),
	}}
	auditor := &recordingUserSearchAuditor{}
	service := newUserSearchService(t, reader, auditor, stubIdentityGate{org: "organization-1"})
	principal := testPrincipal()

	output, err := service.SearchUsers(t.Context(), principal, SearchUsersInput{ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "", Limit: 0, Cursor: ""})
	require.NoError(t, err)

	require.Len(t, reader.searchParams, 1)
	params := reader.searchParams[0]
	require.Equal(t, "user_id", params.GroupBy)
	require.Equal(t, "pat", params.IdentityContains)
	require.Equal(t, defaultUserSearchLimit+1, params.Limit)
	require.Equal(t, "organization-1", params.CanonicalIdentityOrg)
	require.NotEmpty(t, params.ExcludedHookSources, "an organization's own people never count Gram-hosted inference as their usage")
	require.Empty(t, params.UserIDs)
	require.Equal(t, telemetryrepo.MetricsDetailFull, params.MetricsDetail)

	require.Equal(t, UserTypeInternal, output.UserType)
	require.Equal(t, DiagnosticWindowLastWeek, output.Envelope.ResolvedWindow.Window)
	require.False(t, output.Envelope.NoObservations)
	require.Empty(t, output.NextCursor)
	require.Len(t, output.Users, 3)
	require.Equal(t, "p***@e***", output.Users[0].MaskedIdentity)
	require.Equal(t, "mixed", output.Users[0].Activity)
	require.Equal(t, "observed", output.Users[0].Errors)
	require.Equal(t, "u***", output.Users[1].MaskedIdentity)
	require.Equal(t, "successful", output.Users[1].Activity)
	require.Equal(t, "none_observed", output.Users[1].Errors)
	require.Equal(t, "observed", output.Users[2].Activity)
	require.Equal(t, userSearchTestNow.Add(-time.Hour).Format(time.RFC3339), output.Users[0].LastSeenAt)

	// The raw identities never leave the service in any form.
	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	for _, forbidden := range []string{"pat.rivera", "quinn.patel", "example.com", "user-orphan", "user-42", "total_cost", "tokens"} {
		require.NotContains(t, string(encoded), forbidden)
	}

	// References resolve within the project alone, and state the column each
	// identity lives in so the follow-up filters the right one.
	scope := projectUserScope(userSearchTestProject)
	subject, err := service.references.DecodeScoped(output.Users[0].UserReference, principal, subjectKindUser, scope, userSearchTestNow)
	require.NoError(t, err)
	require.Equal(t, FormatSubjectIdentity(SubjectIdentityEmail, "pat.rivera@example.com"), subject)
	subject, err = service.references.DecodeScoped(output.Users[1].UserReference, principal, subjectKindUser, scope, userSearchTestNow)
	require.NoError(t, err)
	require.Equal(t, FormatSubjectIdentity(SubjectIdentityUser, "user-orphan"), subject)

	// The page is an attribution read about many people, recorded before it
	// is served.
	require.Equal(t, []string{userSearchTestProject + "|project|" + userSearchTestProject + "|multiple|7d"}, auditor.attributions)
}

func TestSearchUsers_ExternalGroupsByExternalUserIDWithoutFolding(t *testing.T) {
	t.Parallel()

	reader := &recordingUserSearchReader{rows: []telemetryrepo.UserSummary{userSummaryRow("customer-7781", "", 1, 0)}}
	service := newUserSearchService(t, reader, &recordingUserSearchAuditor{}, stubIdentityGate{org: "organization-1"})
	principal := testPrincipal()

	output, err := service.SearchUsers(t.Context(), principal, SearchUsersInput{ProjectID: userSearchTestProject, Query: "7781", UserType: UserTypeExternal, Window: "30d", Limit: 5, Cursor: ""})
	require.NoError(t, err)

	params := reader.searchParams[0]
	require.Equal(t, "external_user_id", params.GroupBy)
	require.Empty(t, params.CanonicalIdentityOrg, "external ids never fold: the identity map is email-keyed")
	require.Empty(t, params.ExcludedHookSources, "an external user's hosted completions are their usage")
	require.Equal(t, DiagnosticWindowLastMonth, output.Envelope.ResolvedWindow.Window)

	subject, err := service.references.DecodeScoped(output.Users[0].UserReference, principal, subjectKindUser, projectUserScope(userSearchTestProject), userSearchTestNow)
	require.NoError(t, err)
	require.Equal(t, FormatSubjectIdentity(SubjectIdentityExternal, "customer-7781"), subject)
}

func TestSearchUsers_RefusesWindowBeyondAMonth(t *testing.T) {
	t.Parallel()

	service := newUserSearchService(t, &recordingUserSearchReader{}, &recordingUserSearchAuditor{}, nil)
	_, err := service.SearchUsers(t.Context(), testPrincipal(), SearchUsersInput{ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "90d", Limit: 0, Cursor: ""})
	require.ErrorIs(t, err, ErrDiagnosticWindowInvalid)
}

func TestSearchUsers_CursorResumesOnlyTheQueryThatMintedIt(t *testing.T) {
	t.Parallel()

	rows := make([]telemetryrepo.UserSummary, 0, 3)
	for _, key := range []string{"pat.a@example.com", "pat.b@example.com", "pat.c@example.com"} {
		rows = append(rows, userSummaryRow(key, key, 1, 0))
	}
	reader := &recordingUserSearchReader{rows: rows}
	service := newUserSearchService(t, reader, &recordingUserSearchAuditor{}, nil)
	principal := testPrincipal()

	first, err := service.SearchUsers(t.Context(), principal, SearchUsersInput{ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "24h", Limit: 2, Cursor: ""})
	require.NoError(t, err)
	require.Len(t, first.Users, 2, "the extra row decides that another page exists and is not served")
	require.NotEmpty(t, first.NextCursor)
	require.NotContains(t, first.NextCursor, "pat.b", "the cursor carries an identity and must be opaque")

	// Resumed with the same query, the cursor hands the repository the last
	// key it served and carries the traversal count inside the sealed token.
	_, err = service.SearchUsers(t.Context(), principal, SearchUsersInput{ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "24h", Limit: 2, Cursor: first.NextCursor})
	require.NoError(t, err)
	require.Len(t, reader.searchParams, 2)
	require.Equal(t, "pat.b@example.com", reader.searchParams[1].Cursor)

	// The same position replayed against a different query, window, or user
	// type is a different page of a different question.
	for name, input := range map[string]SearchUsersInput{
		"different query":     {ProjectID: userSearchTestProject, Query: "quinn", UserType: "", Window: "24h", Limit: 2, Cursor: first.NextCursor},
		"different window":    {ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "7d", Limit: 2, Cursor: first.NextCursor},
		"different user type": {ProjectID: userSearchTestProject, Query: "pat", UserType: UserTypeExternal, Window: "24h", Limit: 2, Cursor: first.NextCursor},
		"different project":   {ProjectID: userSearchTestMCP, Query: "pat", UserType: "", Window: "24h", Limit: 2, Cursor: first.NextCursor},
	} {
		_, err := service.SearchUsers(t.Context(), principal, input)
		require.ErrorIs(t, err, ErrSubjectReferenceNotFound, name)
	}

	// Case is folded into the scope because the match ignores it.
	_, err = service.SearchUsers(t.Context(), principal, SearchUsersInput{ProjectID: userSearchTestProject, Query: "PAT", UserType: "", Window: "24h", Limit: 2, Cursor: first.NextCursor})
	require.NoError(t, err)
}

func TestSearchUsers_TraversalCapWithholdsTheCursor(t *testing.T) {
	t.Parallel()

	rows := make([]telemetryrepo.UserSummary, 0, maxUserSearchLimit+1)
	for index := range maxUserSearchLimit + 1 {
		key := "pat" + string(rune('a'+index)) + "@example.com"
		rows = append(rows, userSummaryRow(key, key, 1, 0))
	}
	reader := &recordingUserSearchReader{rows: rows}
	service := newUserSearchService(t, reader, &recordingUserSearchAuditor{}, nil)
	principal := testPrincipal()

	cursor := ""
	served := 0
	for range 10 {
		output, err := service.SearchUsers(t.Context(), principal, SearchUsersInput{ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "", Limit: maxUserSearchLimit, Cursor: cursor})
		require.NoError(t, err)
		served += len(output.Users)
		cursor = output.NextCursor
		if cursor == "" {
			break
		}
	}
	require.Equal(t, maxUserSearchTraversal, served, "the traversal cap bounds the people handed over, not only the pages")
	require.Empty(t, cursor)
}

func TestGetUserMetricsSummary_SummarizesOneReferencedPersonWithoutIdentity(t *testing.T) {
	t.Parallel()

	reader := &recordingUserSearchReader{
		rows: []telemetryrepo.UserSummary{userSummaryRow("pat.rivera@example.com", "Pat.Rivera@Example.com", 4, 2)},
		metrics: &telemetryrepo.MetricsSummaryRow{
			FirstSeenUnixNano: userSearchTestNow.Add(-48 * time.Hour).UnixNano(),
			LastSeenUnixNano:  userSearchTestNow.Add(-time.Hour).UnixNano(),
			TotalToolCalls:    12,
			ToolCallSuccess:   9,
			ToolCallFailure:   3,
			TotalCost:         4.5,
			TotalTokens:       9000,
			ToolCounts: map[string]uint64{
				"mcp__payments__refund":    6,
				"mcp__payments__list":      3,
				"tools:http:tickets:close": 2,
				"Read":                     1,
			},
			ToolSuccessCounts: map[string]uint64{"mcp__payments__refund": 3, "mcp__payments__list": 3, "tools:http:tickets:close": 2, "Read": 1},
			ToolFailureCounts: map[string]uint64{"mcp__payments__refund": 3},
		},
	}
	auditor := &recordingUserSearchAuditor{}
	service := newUserSearchService(t, reader, auditor, stubIdentityGate{org: "organization-1"})
	principal := testPrincipal()
	reference, err := service.references.EncodeScoped(principal, subjectKindUser, projectUserScope(userSearchTestProject), FormatSubjectIdentity(SubjectIdentityEmail, "pat.rivera@example.com"), userSearchTestNow)
	require.NoError(t, err)

	output, err := service.GetUserMetricsSummary(t.Context(), principal, GetUserMetricsSummaryInput{ProjectID: userSearchTestProject, UserReference: reference, Window: "", MCPID: ""})
	require.NoError(t, err)

	// The referenced identity is widened to every identity the person's rows
	// carry, the same way the search grouped them, and folded when the
	// organization is on the fold.
	require.Len(t, reader.searchParams, 1)
	require.Equal(t, []string{"pat.rivera@example.com"}, reader.searchParams[0].UserIDs)
	require.Equal(t, telemetryrepo.MetricsDetailBasic, reader.searchParams[0].MetricsDetail)
	require.Len(t, reader.metricsParams, 1)
	metricsParams := reader.metricsParams[0]
	require.Equal(t, []string{"user-42"}, metricsParams.User.UserIDs)
	require.Equal(t, []string{"pat.rivera@example.com"}, metricsParams.User.Emails)
	require.Equal(t, telemetryrepo.CanonicalUserIdentity{OrgID: "organization-1", UserID: "", EmailLower: "pat.rivera@example.com"}, metricsParams.CanonicalUser)
	require.Empty(t, metricsParams.ExternalUserID)
	require.NotEmpty(t, metricsParams.ExcludedHookSources)

	require.Equal(t, "p***@e***", output.MaskedIdentity)
	require.Equal(t, SubjectStateActive, output.Activity)
	require.Equal(t, DiagnosticWindowLastWeek, output.Envelope.ResolvedWindow.Window)
	require.Equal(t, int64(12), output.ToolCalls)
	require.Equal(t, int64(3), output.FailedToolCalls)
	require.InDelta(t, 0.25, output.FailureRate, 0.0001)
	require.Equal(t, int64(2), output.ActiveServers, "payments and tickets; the native Read tool names no server")
	require.False(t, output.ToolsTruncated)
	require.Equal(t, []UserMetricsTool{
		{Tool: "mcp__payments__refund", Server: "payments", Calls: 6, Failures: 3},
		{Tool: "mcp__payments__list", Server: "payments", Calls: 3, Failures: 0},
		{Tool: "tools:http:tickets:close", Server: "tickets", Calls: 2, Failures: 0},
		{Tool: "Read", Server: "", Calls: 1, Failures: 0},
	}, output.TopTools)

	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	for _, forbidden := range []string{"pat.rivera", "example.com", "user-42", "4.5", "9000"} {
		require.NotContains(t, string(encoded), forbidden)
	}

	require.Equal(t, []string{userSearchTestProject + "|project|" + userSearchTestProject + "|p***@e***|7d"}, auditor.attributions)
}

func TestGetUserMetricsSummary_ReportsInactiveWithoutObservations(t *testing.T) {
	t.Parallel()

	reader := &recordingUserSearchReader{rows: nil, metrics: &telemetryrepo.MetricsSummaryRow{ToolCounts: map[string]uint64{}, ToolFailureCounts: map[string]uint64{}}}
	service := newUserSearchService(t, reader, &recordingUserSearchAuditor{}, nil)
	principal := testPrincipal()
	reference, err := service.references.EncodeScoped(principal, subjectKindUser, projectUserScope(userSearchTestProject), FormatSubjectIdentity(SubjectIdentityUser, "user-77"), userSearchTestNow)
	require.NoError(t, err)

	output, err := service.GetUserMetricsSummary(t.Context(), principal, GetUserMetricsSummaryInput{ProjectID: userSearchTestProject, UserReference: reference, Window: "1h", MCPID: ""})
	require.NoError(t, err)
	require.Equal(t, SubjectStateInactive, output.Activity)
	require.True(t, output.Envelope.NoObservations)
	require.Empty(t, output.FirstSeenAt)
	require.Empty(t, output.TopTools)
	require.Zero(t, output.ToolCalls)
	// A raw user id key still scopes the read to that id when no email was
	// ever observed beside it.
	require.Equal(t, []string{"user-77"}, reader.metricsParams[0].User.UserIDs)
	require.Empty(t, reader.metricsParams[0].User.Emails)
}

func TestGetUserMetricsSummary_ExternalReferenceFiltersTheExternalColumn(t *testing.T) {
	t.Parallel()

	reader := &recordingUserSearchReader{metrics: &telemetryrepo.MetricsSummaryRow{LastSeenUnixNano: userSearchTestNow.UnixNano(), TotalToolCalls: 1, ToolCounts: map[string]uint64{"tools:http:tickets:close": 1}}}
	service := newUserSearchService(t, reader, &recordingUserSearchAuditor{}, stubIdentityGate{org: "organization-1"})
	principal := testPrincipal()
	reference, err := service.references.EncodeScoped(principal, subjectKindUser, projectUserScope(userSearchTestProject), FormatSubjectIdentity(SubjectIdentityExternal, "customer-7781"), userSearchTestNow)
	require.NoError(t, err)

	output, err := service.GetUserMetricsSummary(t.Context(), principal, GetUserMetricsSummaryInput{ProjectID: userSearchTestProject, UserReference: reference, Window: "", MCPID: ""})
	require.NoError(t, err)
	require.Empty(t, reader.searchParams, "an external id is one column; there is no identity set to widen")
	params := reader.metricsParams[0]
	require.Equal(t, "customer-7781", params.ExternalUserID)
	require.True(t, params.User.IsEmpty())
	require.False(t, params.CanonicalUser.Enabled())
	require.Empty(t, params.ExcludedHookSources)
	require.Equal(t, "c***", output.MaskedIdentity)
}

func TestGetUserMetricsSummary_AcceptsDrilldownReferenceOnlyWithItsScope(t *testing.T) {
	t.Parallel()

	reader := &recordingUserSearchReader{rows: nil, metrics: &telemetryrepo.MetricsSummaryRow{LastSeenUnixNano: userSearchTestNow.UnixNano(), ToolCounts: map[string]uint64{}}}
	auditor := &recordingUserSearchAuditor{}
	service := newUserSearchService(t, reader, auditor, nil)
	principal := testPrincipal()
	window, err := resolveWindow("24h", userSearchTestNow, drilldownWindowSpec)
	require.NoError(t, err)
	// Minted the way list_mcp_usage_users mints: bound to the MCP and its window.
	reference, err := service.references.EncodeScoped(principal, subjectKindUser, mcpUsageUserScope(drilldownTarget{identity: serverIdentity{mcpServerID: userSearchTestMCP}, projectID: userSearchTestProject, window: window}), FormatSubjectIdentity(SubjectIdentityEmail, "pat.rivera@example.com"), userSearchTestNow)
	require.NoError(t, err)

	_, err = service.GetUserMetricsSummary(t.Context(), principal, GetUserMetricsSummaryInput{ProjectID: userSearchTestProject, UserReference: reference, Window: "24h", MCPID: ""})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound, "without the MCP that minted it the reference is unknown")
	_, err = service.GetUserMetricsSummary(t.Context(), principal, GetUserMetricsSummaryInput{ProjectID: userSearchTestProject, UserReference: reference, Window: "7d", MCPID: userSearchTestMCP})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound, "a different window is a different scope")

	output, err := service.GetUserMetricsSummary(t.Context(), principal, GetUserMetricsSummaryInput{ProjectID: userSearchTestProject, UserReference: reference, Window: "24h", MCPID: userSearchTestMCP})
	require.NoError(t, err)
	require.Equal(t, "p***@e***", output.MaskedIdentity)
	require.Equal(t, []string{userSearchTestProject + "|mcp|" + userSearchTestMCP + "|p***@e***|24h"}, auditor.attributions)
}

func TestGetUserMetricsSummary_RefusesForeignAndMalformedReferences(t *testing.T) {
	t.Parallel()

	reader := &recordingUserSearchReader{metrics: &telemetryrepo.MetricsSummaryRow{ToolCounts: map[string]uint64{}}}
	auditor := &recordingUserSearchAuditor{}
	service := newUserSearchService(t, reader, auditor, nil)
	principal := testPrincipal()
	otherProject := "00000000-0000-0000-0000-000000000009"
	reference, err := service.references.EncodeScoped(principal, subjectKindUser, projectUserScope(otherProject), FormatSubjectIdentity(SubjectIdentityEmail, "pat.rivera@example.com"), userSearchTestNow)
	require.NoError(t, err)

	// A reference from another project, another organization, another
	// session, a cursor presented as a person, and garbage all resolve to the
	// same not-found.
	_, err = service.GetUserMetricsSummary(t.Context(), principal, GetUserMetricsSummaryInput{ProjectID: userSearchTestProject, UserReference: reference, Window: "", MCPID: ""})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)

	own, err := service.references.EncodeScoped(principal, subjectKindUser, projectUserScope(userSearchTestProject), FormatSubjectIdentity(SubjectIdentityEmail, "pat.rivera@example.com"), userSearchTestNow)
	require.NoError(t, err)
	foreign := principal
	foreign.OrganizationID = "organization-2"
	_, err = service.GetUserMetricsSummary(t.Context(), foreign, GetUserMetricsSummaryInput{ProjectID: userSearchTestProject, UserReference: own, Window: "", MCPID: ""})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
	reauthorized := principal
	reauthorized.Generation = "generation-2"
	_, err = service.GetUserMetricsSummary(t.Context(), reauthorized, GetUserMetricsSummaryInput{ProjectID: userSearchTestProject, UserReference: own, Window: "", MCPID: ""})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)

	cursor, err := service.references.EncodeScoped(principal, subjectKindCursor, projectUserScope(userSearchTestProject), formatUserSearchCursor("pat.rivera@example.com", 1), userSearchTestNow)
	require.NoError(t, err)
	_, err = service.GetUserMetricsSummary(t.Context(), principal, GetUserMetricsSummaryInput{ProjectID: userSearchTestProject, UserReference: cursor, Window: "", MCPID: ""})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
	_, err = service.GetUserMetricsSummary(t.Context(), principal, GetUserMetricsSummaryInput{ProjectID: userSearchTestProject, UserReference: "not-a-reference", Window: "", MCPID: ""})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)

	// Nothing was read or audited for any refused reference.
	require.Empty(t, reader.metricsParams)
	require.Empty(t, auditor.attributions)
}

func TestGetUserMetricsSummary_RequiresProjectAndReference(t *testing.T) {
	t.Parallel()

	service := newUserSearchService(t, &recordingUserSearchReader{}, &recordingUserSearchAuditor{}, nil)
	for name, input := range map[string]GetUserMetricsSummaryInput{
		"missing project":   {ProjectID: "", UserReference: "opaque", Window: "", MCPID: ""},
		"missing reference": {ProjectID: userSearchTestProject, UserReference: " ", Window: "", MCPID: ""},
	} {
		_, err := service.GetUserMetricsSummary(t.Context(), testPrincipal(), input)
		require.ErrorIs(t, err, ErrUserSearchInvalid, name)
	}
}

func TestUserSearchService_UnavailableWithoutComposition(t *testing.T) {
	t.Parallel()

	var nilService *DiagnosticsService
	_, err := nilService.SearchUsers(t.Context(), testPrincipal(), SearchUsersInput{ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "", Limit: 0, Cursor: ""})
	require.ErrorIs(t, err, ErrUnavailable)

	withoutReader := newUserSearchService(t, nil, &recordingUserSearchAuditor{}, nil)
	_, err = withoutReader.GetUserMetricsSummary(t.Context(), testPrincipal(), GetUserMetricsSummaryInput{ProjectID: userSearchTestProject, UserReference: "opaque", Window: "", MCPID: ""})
	require.ErrorIs(t, err, ErrUnavailable)
	require.False(t, withoutReader.userSearchValid())
	// A deployment without ClickHouse composes with a nil reader, which must
	// leave the tools on the unavailable stub path rather than registering a
	// live handler that fails on every call.
	require.False(t, withoutReader.WithUserSearch(nil).userSearchValid())
	require.True(t, withoutReader.WithUserSearch(&recordingUserSearchReader{}).userSearchValid())
}

func TestToolServer_NamesOnlyServersItCanDerive(t *testing.T) {
	t.Parallel()

	require.Equal(t, "payments", toolServer("mcp__payments__refund"))
	require.Equal(t, "payments", toolServer("mcp__payments__list__all"))
	require.Equal(t, "tickets", toolServer("tools:http:tickets:close"))
	require.Empty(t, toolServer("mcp__payments"))
	require.Empty(t, toolServer("Read"))
	require.Empty(t, toolServer("tools:bogus"))
	require.Empty(t, toolServer(""))
}

func TestUserMetricsTools_OrdersBrokenToolsFirstAndReportsTruncation(t *testing.T) {
	t.Parallel()

	counts := map[string]uint64{}
	failures := map[string]uint64{}
	for index := range maxUserMetricsTools + 5 {
		name := "mcp__server" + string(rune('a'+index)) + "__tool"
		counts[name] = uint64(index + 1)
		failures[name] = uint64(index + 1)
	}
	tools, servers, truncated := userMetricsTools(&telemetryrepo.MetricsSummaryRow{ToolCounts: counts, ToolFailureCounts: failures})
	require.True(t, truncated)
	require.Len(t, tools, maxUserMetricsTools)
	require.Equal(t, int64(maxUserMetricsTools+5), servers, "servers are counted before the tool list is capped")
	require.Equal(t, int64(maxUserMetricsTools+5), tools[0].Failures, "the most-failing tool leads")
}

// TestUserSearchTools_DeclareOrgAdminReadsForBothAudiences pins the manifest
// contract on both the live and the unavailable registration, so a tool does
// not appear on and disappear from a surface as the rollout flips.
func TestUserSearchTools_DeclareOrgAdminReadsForBothAudiences(t *testing.T) {
	t.Parallel()

	_, unavailable := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	live := newRegistrar(newTestMCPServer())
	registerUserSearchTools(live, nil)

	for _, registrar := range []*Registrar{unavailable, live} {
		for _, name := range []string{"search_users", "get_user_metrics_summary"} {
			descriptor := descriptorByName(t, registrar, name)
			require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization)
			require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
			require.ElementsMatch(t, bothAudiences, descriptor.Meta.Audiences)
			require.True(t, descriptor.Annotations.ReadOnlyHint)
			require.NotEmpty(t, descriptor.InputSchema)
		}
	}
	liveSearch := descriptorByName(t, live, "search_users")
	require.Contains(t, string(liveSearch.InputSchema), "\"query\"")
	require.Contains(t, string(liveSearch.InputSchema), "\"cursor\"")
	liveSummary := descriptorByName(t, live, "get_user_metrics_summary")
	require.Contains(t, string(liveSummary.InputSchema), "\"user_reference\"")
}

// TestUserSearchTools_UnavailableStubsRefuseReadably calls the stubs through
// a real MCP session with every required input supplied, so the refusal under
// test is the tool's structured one rather than a transport schema error.
func TestUserSearchTools_UnavailableStubsRefuseReadably(t *testing.T) {
	t.Parallel()

	server, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	bindExternalTestPrincipal(server)
	registrar.withExternalAuthorizer(allowExternalCallAuthorizer{})

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "user-search-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	for name, arguments := range map[string]map[string]any{
		"search_users":             {"project_id": userSearchTestProject, "query": "pat"},
		"get_user_metrics_summary": {"project_id": userSearchTestProject, "user_reference": "opaque"},
	} {
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: arguments})
		require.NoError(t, err, name)
		require.True(t, result.IsError, name)
		require.Len(t, result.Content, 1, name)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok, name)
		var refusal featureUnavailableResult
		require.NoError(t, json.Unmarshal([]byte(text.Text), &refusal), name)
		require.Equal(t, unavailableCode, refusal.Code, name)
		require.Equal(t, "user_search", refusal.Feature, name)
	}
}
