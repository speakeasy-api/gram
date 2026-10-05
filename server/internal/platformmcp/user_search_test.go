package platformmcp

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

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

	codec := newSubjectReferenceCodec("user-search-test-key")
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

	rows := []telemetryrepo.UserSummary{
		userSummaryRow("pat.rivera@example.com", "pat.rivera@example.com", 4, 2),
		userSummaryRow("user-orphan", "", 3, 0),
		userSummaryRow("quinn.patel@example.com", "quinn.patel@example.com", 0, 0),
	}
	// Distinct last-seen times, so the projected timestamps below pin each
	// person's own boundary rather than one value the whole fixture shares.
	for i := range rows {
		rows[i].LastSeenUnixNano = userSearchTestNow.Add(-time.Hour - time.Duration(i)*time.Minute).UnixNano()
	}
	reader := &recordingUserSearchReader{rows: rows}
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
	require.Equal(t, userSearchTestNow.Add(-time.Hour-time.Minute).Format(time.RFC3339), output.Users[1].LastSeenAt)
	require.Equal(t, userSearchTestNow.Add(-time.Hour-2*time.Minute).Format(time.RFC3339), output.Users[2].LastSeenAt,
		"each person's last-seen is projected from their own row")

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

	service := newUserSearchService(t, &recordingUserSearchReader{}, &recordingUserSearchAuditor{}, literalIdentityGate{})
	_, err := service.SearchUsers(t.Context(), testPrincipal(), SearchUsersInput{ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "90d", Limit: 0, Cursor: ""})
	require.ErrorIs(t, err, ErrDiagnosticWindowInvalid)
}

func TestSearchUsers_CursorResumesOnlyTheQueryThatMintedIt(t *testing.T) {
	t.Parallel()

	rows := make([]telemetryrepo.UserSummary, 0, 3)
	// Distinct, descending last-seen times, the order the repository returns
	// them in. Only then is rows[1] identifiably the last person page 1 serves,
	// and only then can an assertion about the sealed boundary distinguish it
	// from any other row's timestamp.
	for i, key := range []string{"pat.a@example.com", "pat.b@example.com", "pat.c@example.com"} {
		row := userSummaryRow(key, key, 1, 0)
		row.LastSeenUnixNano = userSearchTestNow.Add(-time.Hour - time.Duration(i)*time.Minute).UnixNano()
		rows = append(rows, row)
	}
	reader := &recordingUserSearchReader{rows: rows}
	service := newUserSearchService(t, reader, &recordingUserSearchAuditor{}, literalIdentityGate{})
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
	// Guards the assertion that follows: were the fixture to share one
	// last_seen across its rows again, that assertion would hold for any row's
	// timestamp and pin nothing.
	require.NotEqual(t, rows[0].LastSeenUnixNano, rows[1].LastSeenUnixNano, "the fixture's boundaries must be distinguishable")
	require.NotEqual(t, rows[2].LastSeenUnixNano, rows[1].LastSeenUnixNano, "the fixture's boundaries must be distinguishable")
	require.Equal(t, rows[1].LastSeenUnixNano, reader.searchParams[1].CursorLastSeenUnixNano,
		"the boundary the last served person was displayed at travels inside the cursor, so the repository never re-derives it from rows this search excludes")

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

// TestSearchUsers_RefusesACursorWithoutASealedPosition pins that every payload
// shape short of the current one is refused rather than resumed.
//
// A cursor carrying only a group key would leave the repository to re-derive
// the boundary from a lookup that ignores this search's window and its excluded
// hook sources, which is what could repeat a person. A cursor carrying the
// boundary but no interval would leave the window to be recomputed from the
// clock, which is what could drop a person near its start before their page is
// reached. Neither has a resume path.
func TestSearchUsers_RefusesACursorWithoutASealedPosition(t *testing.T) {
	t.Parallel()

	reader := &recordingUserSearchReader{rows: []telemetryrepo.UserSummary{userSummaryRow("pat.a@example.com", "pat.a@example.com", 1, 0)}}
	service := newUserSearchService(t, reader, &recordingUserSearchAuditor{}, literalIdentityGate{})
	principal := testPrincipal()
	search, err := normalizeUserSearch(SearchUsersInput{ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "24h", Limit: 2, Cursor: ""})
	require.NoError(t, err)
	search.window, err = resolveWindow("24h", userSearchTestNow, userSearchWindowSpec)
	require.NoError(t, err)

	sealed := func(fields ...string) string {
		return "u3:" + strings.Join(fields, ":")
	}
	start := strconv.FormatInt(search.window.start.UnixNano(), 10)
	end := strconv.FormatInt(search.window.end.UnixNano(), 10)
	boundary := strconv.FormatInt(userSearchTestNow.Add(-time.Hour).UnixNano(), 10)
	for name, value := range map[string]string{
		// Shapes from before each field was sealed in.
		"key only":          "u:1:pat.a@example.com",
		"boundary only":     "u2:1:" + boundary + ":pat.a@example.com",
		"unknown prefix":    "u9:1:" + start + ":" + end + ":" + boundary + ":pat.a@example.com",
		"no interval":       sealed("1", boundary, "pat.a@example.com"),
		"zero boundary":     sealed("1", start, end, "0", "pat.a@example.com"),
		"unparseable start": sealed("1", "soon", end, boundary, "pat.a@example.com"),
		"unparseable end":   sealed("1", start, "later", boundary, "pat.a@example.com"),
		"inverted interval": sealed("1", end, start, boundary, "pat.a@example.com"),
		"zero start":        sealed("1", "0", end, boundary, "pat.a@example.com"),
		"unparseable count": sealed("many", start, end, boundary, "pat.a@example.com"),
		"empty key":         sealed("1", start, end, boundary, ""),
	} {
		stale, err := service.references.EncodeScoped(principal, subjectKindCursor, search.cursorScope(), value, userSearchTestNow)
		require.NoError(t, err, name)
		_, err = service.SearchUsers(t.Context(), principal, SearchUsersInput{ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "24h", Limit: 2, Cursor: stale})
		require.ErrorIs(t, err, ErrSubjectReferenceNotFound, name)
	}
	require.Empty(t, reader.searchParams, "a refused cursor reads nothing")
}

func TestSearchUsers_TraversalCapWithholdsTheCursor(t *testing.T) {
	t.Parallel()

	rows := make([]telemetryrepo.UserSummary, 0, maxUserSearchLimit+1)
	for index := range maxUserSearchLimit + 1 {
		key := "pat" + string(rune('a'+index)) + "@example.com"
		rows = append(rows, userSummaryRow(key, key, 1, 0))
	}
	reader := &recordingUserSearchReader{rows: rows}
	service := newUserSearchService(t, reader, &recordingUserSearchAuditor{}, literalIdentityGate{})
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
	service := newUserSearchService(t, reader, &recordingUserSearchAuditor{}, literalIdentityGate{})
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
	service := newUserSearchService(t, reader, auditor, literalIdentityGate{})
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
	service := newUserSearchService(t, reader, auditor, literalIdentityGate{})
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

	searchWindow, err := resolveWindow("7d", userSearchTestNow, userSearchWindowSpec)
	require.NoError(t, err)
	position := userSearchPosition{key: "pat.rivera@example.com", lastSeen: userSearchTestNow.UnixNano(), traversed: 1}
	cursor, err := service.references.EncodeScoped(principal, subjectKindCursor, projectUserScope(userSearchTestProject), formatUserSearchCursor(position, searchWindow), userSearchTestNow)
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

	service := newUserSearchService(t, &recordingUserSearchReader{}, &recordingUserSearchAuditor{}, literalIdentityGate{})
	for name, input := range map[string]GetUserMetricsSummaryInput{
		"missing project":   {ProjectID: "", UserReference: "opaque", Window: "", MCPID: ""},
		"missing reference": {ProjectID: userSearchTestProject, UserReference: " ", Window: "", MCPID: ""},
	} {
		_, err := service.GetUserMetricsSummary(t.Context(), testPrincipal(), input)
		require.ErrorIs(t, err, ErrUserSearchInvalid, name)
	}
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
// contract of the people search tools.
func TestUserSearchTools_DeclareOrgAdminReadsForBothAudiences(t *testing.T) {
	t.Parallel()

	live := newRegistrar(newTestMCPServer())
	registerUserSearchTools(live, newUserSearchService(t, &recordingUserSearchReader{}, &recordingUserSearchAuditor{}, literalIdentityGate{}))

	for _, name := range []string{"search_users", "get_user_metrics_summary"} {
		descriptor := descriptorByName(t, live, name)
		require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
		require.ElementsMatch(t, bothAudiences, descriptor.Meta.Audiences)
		require.True(t, descriptor.Annotations.ReadOnlyHint)
		require.NotEmpty(t, descriptor.InputSchema)
	}
	liveSearch := descriptorByName(t, live, "search_users")
	require.Contains(t, string(liveSearch.InputSchema), "\"query\"")
	require.Contains(t, string(liveSearch.InputSchema), "\"cursor\"")
	liveSummary := descriptorByName(t, live, "get_user_metrics_summary")
	require.Contains(t, string(liveSummary.InputSchema), "\"user_reference\"")
}

// unmappedIdentityReader answers like the repository does for a person whose
// email the identity map does not carry and whose every row carries a user id.
//
// The fold's id-keyed arm resolves such an email to no owner, and its other arm
// matches only rows with an empty user_id, so the canonical identity alone
// names none of this person's rows. Only the user ids the caller resolved from
// the rows themselves can. Serving metrics on exactly that condition is what
// makes this fake fail when the summary stops sending them.
type unmappedIdentityReader struct {
	person        telemetryrepo.UserSummary
	personID      string
	metrics       *telemetryrepo.MetricsSummaryRow
	searchParams  []telemetryrepo.SearchUsersParams
	metricsParams []telemetryrepo.GetUserMetricsSummaryParams
}

func (r *unmappedIdentityReader) SearchUsers(_ context.Context, arg telemetryrepo.SearchUsersParams) ([]telemetryrepo.UserSummary, error) {
	r.searchParams = append(r.searchParams, arg)
	return []telemetryrepo.UserSummary{r.person}, nil
}

func (r *unmappedIdentityReader) GetUserMetricsSummary(_ context.Context, arg telemetryrepo.GetUserMetricsSummaryParams) (*telemetryrepo.MetricsSummaryRow, error) {
	r.metricsParams = append(r.metricsParams, arg)
	if slices.Contains(arg.User.UserIDs, r.personID) {
		return r.metrics, nil
	}
	// The scope never named a row of this person's: no observations.
	return &telemetryrepo.MetricsSummaryRow{ToolCounts: map[string]uint64{}, ToolFailureCounts: map[string]uint64{}}, nil
}

// TestGetUserMetricsSummary_UnmappedEmailAgreesWithSearch covers a person the
// identity map does not carry whose rows do carry a user id.
//
// The search finds them: its group key folds an unmapped email back to the
// recorded address, so they are returned and reported active. The summary used
// to disagree, because supplying the canonical identity made the repository
// ignore the user ids the reference had been widened to and scope the read on
// the mapping that does not exist — showing activity in the search and none in
// the summary for the same person, in the same window.
func TestGetUserMetricsSummary_UnmappedEmailAgreesWithSearch(t *testing.T) {
	t.Parallel()

	const (
		unmappedEmail = "pat.rivera@example.com"
		firstID       = "user-42"
		secondID      = "user-91"
	)
	// Distinguishable from every other fixture value: two ids rather than the
	// shared one, its own activity counts, and its own last-seen moment, so an
	// assertion naming one of them cannot be satisfied by another row's value.
	person := userSummaryRow(unmappedEmail, unmappedEmail, 7, 2)
	person.RawUserIDs = []string{firstID, secondID}
	person.LastSeenUnixNano = userSearchTestNow.Add(-17 * time.Minute).UnixNano()

	reader := &unmappedIdentityReader{
		person:   person,
		personID: firstID,
		metrics: &telemetryrepo.MetricsSummaryRow{
			FirstSeenUnixNano: userSearchTestNow.Add(-31 * time.Hour).UnixNano(),
			LastSeenUnixNano:  person.LastSeenUnixNano,
			TotalToolCalls:    9,
			ToolCallSuccess:   7,
			ToolCallFailure:   2,
			ToolCounts:        map[string]uint64{"mcp__payments__refund": 9},
			ToolFailureCounts: map[string]uint64{"mcp__payments__refund": 2},
		},
	}
	// On the fold: the organization has an identity map, it just does not carry
	// this address.
	service := newUserSearchService(t, reader, &recordingUserSearchAuditor{}, stubIdentityGate{org: "organization-1"})
	principal := testPrincipal()

	// What the search says about this person.
	found, err := service.SearchUsers(t.Context(), principal, SearchUsersInput{ProjectID: userSearchTestProject, Query: "rivera", UserType: "", Window: "7d", Limit: 5, Cursor: ""})
	require.NoError(t, err)
	require.Len(t, found.Users, 1)
	// The search's vocabulary is categorical evidence rather than a state, so
	// "mixed" here is its way of saying this person has been doing things —
	// seven successful calls and two failed ones.
	require.Equal(t, "mixed", found.Users[0].Activity, "the search finds a person with observed activity")
	require.Equal(t, time.Unix(0, person.LastSeenUnixNano).UTC().Format(time.RFC3339), found.Users[0].LastSeenAt)

	// And what the summary says about the very reference the search minted.
	summary, err := service.GetUserMetricsSummary(t.Context(), principal, GetUserMetricsSummaryInput{ProjectID: userSearchTestProject, UserReference: found.Users[0].UserReference, Window: "7d", MCPID: ""})
	require.NoError(t, err)
	require.Equal(t, SubjectStateActive, summary.Activity, "the summary must not report inactive for a person the search just showed as active")
	require.Equal(t, found.Users[0].LastSeenAt, summary.LastSeenAt, "both tools must agree on when the person was last seen")
	require.Equal(t, found.Users[0].MaskedIdentity, summary.MaskedIdentity)
	require.Equal(t, int64(9), summary.ToolCalls)
	require.False(t, summary.Envelope.NoObservations)

	// The scope that got there: the fold is still applied, and the resolved ids
	// travel with it rather than being dropped in its favour.
	require.Len(t, reader.metricsParams, 1)
	params := reader.metricsParams[0]
	require.Equal(t, []string{firstID, secondID}, params.User.UserIDs, "every user id the reference was widened to survives")
	require.Equal(t, []string{unmappedEmail}, params.User.Emails)
	require.Equal(t, telemetryrepo.CanonicalUserIdentity{OrgID: "organization-1", UserID: "", EmailLower: unmappedEmail}, params.CanonicalUser)
	require.True(t, params.CanonicalUser.Enabled(), "the fold stays in the scope for the mapped identities it does resolve")
	require.False(t, params.User.IsEmpty(), "and the resolved set stays in it for the ones it does not")
}

// windowedUserSearchReader answers like the repository: only people observed
// inside the interval the parameters name, newest first, resumed strictly after
// the sealed (last_seen, key) boundary, capped at the requested limit.
type windowedUserSearchReader struct {
	people []telemetryrepo.UserSummary
	params []telemetryrepo.SearchUsersParams
}

func (r *windowedUserSearchReader) SearchUsers(_ context.Context, arg telemetryrepo.SearchUsersParams) ([]telemetryrepo.UserSummary, error) {
	r.params = append(r.params, arg)

	matched := make([]telemetryrepo.UserSummary, 0, len(r.people))
	for _, person := range r.people {
		if person.LastSeenUnixNano < arg.TimeStart || person.LastSeenUnixNano > arg.TimeEnd {
			continue
		}
		// The descending tuple comparison the repository applies:
		// (last_seen, key) < (boundary, cursor).
		if arg.Cursor != "" {
			if person.LastSeenUnixNano > arg.CursorLastSeenUnixNano {
				continue
			}
			if person.LastSeenUnixNano == arg.CursorLastSeenUnixNano && person.UserID >= arg.Cursor {
				continue
			}
		}
		matched = append(matched, person)
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].LastSeenUnixNano > matched[j].LastSeenUnixNano })
	if arg.Limit > 0 && len(matched) > arg.Limit {
		matched = matched[:arg.Limit]
	}
	return matched, nil
}

func (r *windowedUserSearchReader) GetUserMetricsSummary(context.Context, telemetryrepo.GetUserMetricsSummaryParams) (*telemetryrepo.MetricsSummaryRow, error) {
	return &telemetryrepo.MetricsSummaryRow{ToolCounts: map[string]uint64{}, ToolFailureCounts: map[string]uint64{}}, nil
}

// TestSearchUsers_CursorAnchorsTheWindowAcrossPages pins that every page of one
// traversal reads the interval the first page read, however long the caller
// takes to ask for the next one. Re-resolving the window from the clock on each
// page slid it forward under the traversal.
func TestSearchUsers_CursorAnchorsTheWindowAcrossPages(t *testing.T) {
	t.Parallel()

	rows := make([]telemetryrepo.UserSummary, 0, 3)
	for index, key := range []string{"pat.a@example.com", "pat.b@example.com", "pat.c@example.com"} {
		row := userSummaryRow(key, key, 1, 0)
		row.LastSeenUnixNano = userSearchTestNow.Add(-time.Hour - time.Duration(index)*time.Minute).UnixNano()
		rows = append(rows, row)
	}
	reader := &recordingUserSearchReader{rows: rows}
	clock := userSearchTestNow
	service := newUserSearchService(t, reader, &recordingUserSearchAuditor{}, literalIdentityGate{})
	service.now = func() time.Time { return clock }
	principal := testPrincipal()

	first, err := service.SearchUsers(t.Context(), principal, SearchUsersInput{ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "24h", Limit: 2, Cursor: ""})
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)

	// A pause a caller can really take: the cursor is a reference, so it
	// expires after SubjectReferenceTTL and nothing longer than that is
	// reachable. That bounds how far a re-resolved window could slide, not
	// whether it slides — seven minutes is already 12% of the shortest window
	// this tool offers.
	clock = clock.Add(7 * time.Minute)
	second, err := service.SearchUsers(t.Context(), principal, SearchUsersInput{ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "24h", Limit: 2, Cursor: first.NextCursor})
	require.NoError(t, err)

	require.Len(t, reader.searchParams, 2)
	// Guards the assertions that follow: were the clock not actually moving,
	// identical bounds would prove nothing about where they came from.
	moved, err := resolveWindow("24h", clock, userSearchWindowSpec)
	require.NoError(t, err)
	require.NotEqual(t, reader.searchParams[0].TimeStart, moved.start.UnixNano(), "the clock must have moved for this test to mean anything")

	require.Equal(t, reader.searchParams[0].TimeStart, reader.searchParams[1].TimeStart, "page 2 reads the interval page 1 read, not one recomputed from the clock")
	require.Equal(t, reader.searchParams[0].TimeEnd, reader.searchParams[1].TimeEnd)
	// And the interval the caller is told it read is the same one.
	require.Equal(t, first.Envelope.ResolvedWindow.From, second.Envelope.ResolvedWindow.From)
	require.Equal(t, first.Envelope.ResolvedWindow.To, second.Envelope.ResolvedWindow.To)
	require.Equal(t, DiagnosticWindowLastDay, second.Envelope.ResolvedWindow.Window)
}

// TestSearchUsers_ReachesAPersonNearTheOriginalWindowStart is the complaint the
// anchored interval answers. Under newest-first ordering the people nearest the
// window's start are exactly the ones a later page reaches, so a window sliding
// forward between pages drops them out of range before their page is asked for
// — and the traversal then ends on a result the caller cannot tell from having
// seen everyone.
func TestSearchUsers_ReachesAPersonNearTheOriginalWindowStart(t *testing.T) {
	t.Parallel()

	// Distinct last-seen moments spanning the window: one recent, one in the
	// middle, and one three minutes inside the original start, which a window
	// re-resolved after a pause no longer covers.
	offsets := map[string]time.Duration{
		"pat.recent@example.com": -time.Hour,
		"pat.middle@example.com": -2 * time.Hour,
		"pat.oldest@example.com": -24*time.Hour + 3*time.Minute,
	}
	people := make([]telemetryrepo.UserSummary, 0, len(offsets))
	for key, offset := range offsets {
		row := userSummaryRow(key, key, 1, 0)
		row.LastSeenUnixNano = userSearchTestNow.Add(offset).UnixNano()
		people = append(people, row)
	}
	reader := &windowedUserSearchReader{people: people}
	clock := userSearchTestNow
	service := newUserSearchService(t, reader, &recordingUserSearchAuditor{}, literalIdentityGate{})
	service.now = func() time.Time { return clock }
	principal := testPrincipal()

	served := []string{}
	cursor := ""
	for page := range 5 {
		output, err := service.SearchUsers(t.Context(), principal, SearchUsersInput{ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "24h", Limit: 1, Cursor: cursor})
		require.NoError(t, err, "page %d", page)
		for _, user := range output.Users {
			served = append(served, user.LastSeenAt)
		}
		cursor = output.NextCursor
		if cursor == "" {
			break
		}
		// The caller takes a few minutes before asking for the next page —
		// comfortably inside SubjectReferenceTTL, so the cursor stays valid,
		// and already enough to carry a re-resolved window past the oldest
		// person.
		clock = clock.Add(7 * time.Minute)
	}
	require.Empty(t, cursor, "the traversal must finish rather than be cut short")

	oldest := userSearchTestNow.Add(-24*time.Hour + 3*time.Minute).UTC().Format(time.RFC3339)
	require.Contains(t, served, oldest, "the person nearest the original window start must still be reachable on a later page")
	require.Len(t, served, len(offsets), "and everyone else with them")

	// Every page read the interval the first one did, and the clock really did
	// move underneath them.
	require.Greater(t, len(reader.params), 1)
	for index, params := range reader.params {
		require.Equal(t, reader.params[0].TimeStart, params.TimeStart, "page %d start", index)
		require.Equal(t, reader.params[0].TimeEnd, params.TimeEnd, "page %d end", index)
	}
	require.NotEqual(t, userSearchTestNow, clock, "the clock must have moved for this test to mean anything")
	require.LessOrEqual(t, reader.params[0].TimeStart, userSearchTestNow.Add(-24*time.Hour+3*time.Minute).UnixNano(),
		"the anchored interval is the one that contains the oldest person")
}
