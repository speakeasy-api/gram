package platformmcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// refusingProjectReader fails the authorization boundaries a test names, so a
// refusal can be proven to stop the call before any budget, audit, or read.
type refusingProjectReader struct {
	diagnosticsProjectReader
	projectErr error
	mcpErr     error
}

func (r refusingProjectReader) ResolveProjectRead(context.Context, Principal, FindMCPInput) (ResolvedProject, error) {
	if r.projectErr != nil {
		return ResolvedProject{}, r.projectErr
	}
	return ResolvedProject{}, nil
}

func (r refusingProjectReader) GetMCP(context.Context, Principal, GetMCPInput) (MCP, error) {
	if r.mcpErr != nil {
		return MCP{}, r.mcpErr
	}
	return MCP{}, nil
}

// TestUserSearch_ChargesTheSensitiveBudgetOnlyForServedReads pins that a
// malformed, unauthorized, or unresolvable request cannot spend the caller's
// personal-data allowance: the budget is charged once the read is going to
// happen, not on the way in.
func TestUserSearch_ChargesTheSensitiveBudgetOnlyForServedReads(t *testing.T) {
	t.Parallel()

	connection := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	reader := &recordingUserSearchReader{rows: nil, metrics: &telemetryrepo.MetricsSummaryRow{ToolCounts: map[string]uint64{}}}
	service := newUserSearchService(t, reader, &recordingUserSearchAuditor{}, literalIdentityGate{})
	service.sensitiveBudget = OperationBudget{Connection: connection, Organization: allowOperationLimiter{}}
	principal := testPrincipal()

	_, err := service.SearchUsers(t.Context(), principal, SearchUsersInput{ProjectID: userSearchTestProject, Query: "pa", UserType: "", Window: "", Limit: 0, Cursor: ""})
	require.ErrorIs(t, err, ErrUserSearchInvalid)
	_, err = service.SearchUsers(t.Context(), principal, SearchUsersInput{ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "90d", Limit: 0, Cursor: ""})
	require.ErrorIs(t, err, ErrDiagnosticWindowInvalid)
	_, err = service.SearchUsers(t.Context(), principal, SearchUsersInput{ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "", Limit: 0, Cursor: "not-a-cursor"})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
	foreign, err := service.references.EncodeScoped(principal, subjectKindUser, projectUserScope(userSearchTestMCP), FormatSubjectIdentity(SubjectIdentityEmail, "pat.rivera@example.com"), userSearchTestNow)
	require.NoError(t, err)
	_, err = service.GetUserMetricsSummary(t.Context(), principal, GetUserMetricsSummaryInput{ProjectID: userSearchTestProject, UserReference: foreign, Window: "", MCPID: ""})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
	require.Empty(t, connection.keys, "refused calls never reach the sensitive budget")

	denied := errors.New("project:read denied")
	service.reader = refusingProjectReader{diagnosticsProjectReader: diagnosticsProjectReader{output: ListProjectsOutput{}}, projectErr: denied, mcpErr: nil}
	_, err = service.SearchUsers(t.Context(), principal, SearchUsersInput{ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "", Limit: 0, Cursor: ""})
	require.ErrorIs(t, err, denied)
	require.Empty(t, connection.keys, "an unauthorized call never reaches the sensitive budget")
	require.Empty(t, reader.searchParams)

	service.reader = diagnosticsProjectReader{output: ListProjectsOutput{}}
	_, err = service.SearchUsers(t.Context(), principal, SearchUsersInput{ProjectID: userSearchTestProject, Query: "pat", UserType: "", Window: "", Limit: 0, Cursor: ""})
	require.NoError(t, err)
	require.Equal(t, []string{"connection-1"}, connection.keys, "a served page is charged exactly once")
}

// TestGetUserMetricsSummary_ReauthorizesTheMCPBehindADrilldownReference pins
// that a list_mcp_usage_users reference is only honored while the caller can
// still see the server it was minted against, and that a project-wide
// reference is audited against the project even when an mcp_id rides along.
func TestGetUserMetricsSummary_ReauthorizesTheMCPBehindADrilldownReference(t *testing.T) {
	t.Parallel()

	reader := &recordingUserSearchReader{rows: nil, metrics: &telemetryrepo.MetricsSummaryRow{LastSeenUnixNano: userSearchTestNow.UnixNano(), ToolCounts: map[string]uint64{}}}
	auditor := &recordingUserSearchAuditor{}
	service := newUserSearchService(t, reader, auditor, literalIdentityGate{})
	principal := testPrincipal()
	window, err := resolveWindow("24h", userSearchTestNow, drilldownWindowSpec)
	require.NoError(t, err)
	drilldown, err := service.references.EncodeScoped(principal, subjectKindUser, mcpUsageUserScope(drilldownTarget{identity: serverIdentity{mcpServerID: userSearchTestMCP}, projectID: userSearchTestProject, window: window}), FormatSubjectIdentity(SubjectIdentityEmail, "pat.rivera@example.com"), userSearchTestNow)
	require.NoError(t, err)

	revoked := errors.New("mcp:read revoked")
	service.reader = refusingProjectReader{diagnosticsProjectReader: diagnosticsProjectReader{output: ListProjectsOutput{}}, projectErr: nil, mcpErr: revoked}
	_, err = service.GetUserMetricsSummary(t.Context(), principal, GetUserMetricsSummaryInput{ProjectID: userSearchTestProject, UserReference: drilldown, Window: "24h", MCPID: userSearchTestMCP})
	require.ErrorIs(t, err, revoked, "a still-valid handle does not outlive access to the server it was minted against")
	require.Empty(t, auditor.attributions)
	require.Empty(t, reader.metricsParams)

	// A project-wide reference never needs the MCP, so a revoked server beside
	// it neither blocks the read nor changes what is audited.
	projectWide, err := service.references.EncodeScoped(principal, subjectKindUser, projectUserScope(userSearchTestProject), FormatSubjectIdentity(SubjectIdentityEmail, "pat.rivera@example.com"), userSearchTestNow)
	require.NoError(t, err)
	_, err = service.GetUserMetricsSummary(t.Context(), principal, GetUserMetricsSummaryInput{ProjectID: userSearchTestProject, UserReference: projectWide, Window: "24h", MCPID: userSearchTestMCP})
	require.NoError(t, err)
	require.Equal(t, []string{userSearchTestProject + "|project|" + userSearchTestProject + "|p***@e***|24h"}, auditor.attributions, "the audit names the scope that resolved the reference, not the mcp_id that was supplied")
}

// TestUserSummaryIdentity_TrustsTheFoldedIdsOverTheTextShape pins that the
// column a reference names comes from what the summary folded rather than
// from whether the key looks like an email.
func TestUserSummaryIdentity_TrustsTheFoldedIdsOverTheTextShape(t *testing.T) {
	t.Parallel()

	kind, identifier := userSummaryIdentity(UserTypeInternal, telemetryrepo.UserSummary{UserID: "pat.rivera@example.com", RawUserIDs: []string{"user-42"}})
	require.Equal(t, SubjectIdentityEmail, kind)
	require.Equal(t, "pat.rivera@example.com", identifier)

	kind, _ = userSummaryIdentity(UserTypeInternal, telemetryrepo.UserSummary{UserID: "user-77", RawUserIDs: []string{"user-77"}})
	require.Equal(t, SubjectIdentityUser, kind)

	kind, _ = userSummaryIdentity(UserTypeInternal, telemetryrepo.UserSummary{UserID: "svc@legacy-id", RawUserIDs: []string{"svc@legacy-id"}})
	require.Equal(t, SubjectIdentityUser, kind, "a user id containing @ is still a user id when the summary folded it as one")

	kind, _ = userSummaryIdentity(UserTypeInternal, telemetryrepo.UserSummary{UserID: "user-orphan", RawUserIDs: nil})
	require.Equal(t, SubjectIdentityUser, kind)

	kind, _ = userSummaryIdentity(UserTypeExternal, telemetryrepo.UserSummary{UserID: "end-user@example.com", RawUserIDs: nil})
	require.Equal(t, SubjectIdentityExternal, kind, "external grouping is one column regardless of shape")
}
