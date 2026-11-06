package mcpconnectionattempts_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var outcomeStart = time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)

func outcomeAttempt(t *testing.T) mcpconnectionattempts.Attempt {
	t.Helper()
	scope := mcpconnectionattempts.ServerScope("org-outcomes", uuid.New(), uuid.New(), mcpconnectionattempts.AppDCR)
	return mcpconnectionattempts.AttemptAt(uuid.New(), outcomeStart, scope)
}

func outcomeRow(t *testing.T, attempt mcpconnectionattempts.Attempt, step mcpconnectionattempts.Step, what mcpconnectionattempts.Happened, final bool, after time.Duration, opts ...mcpconnectionattempts.Option) mcpconnectionattempts.Row {
	t.Helper()
	row, err := mcpconnectionattempts.RowAt(attempt, step, what, final, outcomeStart.Add(after), opts...)
	require.NoError(t, err)
	return row
}

func TestEndUsesTheFinalRow(t *testing.T) {
	t.Parallel()
	a := outcomeAttempt(t)
	summary := mcpconnectionattempts.Summarize([]mcpconnectionattempts.Row{
		outcomeRow(t, a, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, false, 0),
		outcomeRow(t, a, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Minute),
	})
	require.Equal(t, mcpconnectionattempts.Ending{Step: "inbound_token", Outcome: mcpconnectionattempts.OutcomeConnected, Reason: "", At: outcomeStart.Add(time.Minute)}, summary.End(outcomeStart.Add(time.Minute)))
}

func TestEndIgnoresRowsAfterTheFinalRow(t *testing.T) {
	t.Parallel()
	a := outcomeAttempt(t)
	summary := mcpconnectionattempts.Summarize([]mcpconnectionattempts.Row{
		outcomeRow(t, a, mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeNeedsSignIn, false, 2*time.Minute),
		outcomeRow(t, a, mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderDenied, true, time.Minute),
		outcomeRow(t, a, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, 3*time.Minute),
	})
	require.Equal(t, mcpconnectionattempts.Ending{Step: "provider_sign_in", Outcome: mcpconnectionattempts.OutcomeDeclined, Reason: "provider_denied", At: outcomeStart.Add(time.Minute)}, summary.End(outcomeStart.Add(2*time.Hour)))
}

func TestEndStopsOnTheLastFailureAfterTheLiveWindow(t *testing.T) {
	t.Parallel()
	a := outcomeAttempt(t)
	summary := mcpconnectionattempts.Summarize([]mcpconnectionattempts.Row{
		outcomeRow(t, a, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, false, 0),
		outcomeRow(t, a, mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderOAuthInvalidClient, false, time.Minute),
	})
	stopped := mcpconnectionattempts.Ending{Step: "provider_sign_in", Outcome: mcpconnectionattempts.OutcomeFailed, Reason: "provider_oauth_invalid_client", At: outcomeStart.Add(time.Minute)}
	require.Equal(t, stopped, summary.End(outcomeStart.Add(mcpconnectionattempts.AttemptLiveWindow)))
	require.Equal(t, mcpconnectionattempts.OutcomeAbandoned, summary.End(outcomeStart.Add(mcpconnectionattempts.AttemptLiveWindow-time.Second)).Outcome, "still live")
}

func TestEndKeepsABlockedOrDeclinedReasonOnAnUnfinishedAttempt(t *testing.T) {
	t.Parallel()
	a := outcomeAttempt(t)
	blocked := mcpconnectionattempts.Summarize([]mcpconnectionattempts.Row{
		outcomeRow(t, a, mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeNeedsSignIn, false, time.Minute),
	})
	require.Equal(t, mcpconnectionattempts.OutcomeBlocked, blocked.End(outcomeStart.Add(2*time.Hour)).Outcome)

	b := outcomeAttempt(t)
	declined := mcpconnectionattempts.Summarize([]mcpconnectionattempts.Row{
		outcomeRow(t, b, mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderDenied, false, time.Minute),
	})
	require.Equal(t, mcpconnectionattempts.OutcomeDeclined, declined.End(outcomeStart.Add(2*time.Hour)).Outcome)
}

func TestEndAbandonsAnAttemptWhoseLastRowSucceeded(t *testing.T) {
	t.Parallel()
	a := outcomeAttempt(t)
	summary := mcpconnectionattempts.Summarize([]mcpconnectionattempts.Row{
		outcomeRow(t, a, mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderOAuthInvalidClient, false, 0),
		outcomeRow(t, a, mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.ResultSignedIn, false, time.Minute),
	})
	require.Equal(t, mcpconnectionattempts.Ending{Step: "provider_sign_in", Outcome: mcpconnectionattempts.OutcomeAbandoned, Reason: "", At: outcomeStart.Add(time.Minute)}, summary.End(outcomeStart.Add(24*time.Hour)))
	require.True(t, summary.SignedIn)
}

func TestSummarizePrefersTheAppsClientAndCountsRepeats(t *testing.T) {
	t.Parallel()
	a := outcomeAttempt(t)
	first := outcomeRow(t, a, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, false, 0)
	first.ClientFamily = mcpconnectionattempts.FamilyBrowser
	last := outcomeRow(t, a, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Minute)
	last.ClientFamily, last.Repeats, last.AuthClientID = mcpconnectionattempts.FamilyCursor, 3, "auth-client-1"
	summary := mcpconnectionattempts.Summarize([]mcpconnectionattempts.Row{first, last})
	require.Equal(t, mcpconnectionattempts.FamilyCursor, summary.ClientFamily)
	require.Equal(t, uint64(4), summary.Weight)
	require.Contains(t, summary.Touched, "auth-client-1")
	require.Contains(t, summary.Touched, first.ServerID)
}

func fixSummary(started time.Time, outcome mcpconnectionattempts.Outcome, touched ...string) mcpconnectionattempts.AttemptSummary {
	return mcpconnectionattempts.AttemptSummary{
		OrganizationID: "org-fix", AttemptID: uuid.New(), StartedAt: started, HasFinal: true,
		FinalStep: "inbound_token", FinalOutcome: string(outcome), FinalReason: "", FinalAt: started, LastStep: "inbound_token", LastReason: "", LastAt: started,
		SubjectURN: urn.NewUserSubject("user-fix").String(), AppKey: "dcr", ServerID: "server-fix",
		ProviderHost: "", ClientFamily: "", Weight: 1, SignedIn: false, Touched: append([]string{"server-fix"}, touched...),
	}
}

func TestNextSuccessTakesTheEarliestWithinTheWindow(t *testing.T) {
	t.Parallel()
	failure := fixSummary(outcomeStart, mcpconnectionattempts.OutcomeFailed)
	early := fixSummary(outcomeStart.Add(time.Hour), mcpconnectionattempts.OutcomeConnected)
	later := fixSummary(outcomeStart.Add(2*time.Hour), mcpconnectionattempts.OutcomeConnected)
	before := fixSummary(outcomeStart.Add(-time.Hour), mcpconnectionattempts.OutcomeConnected)
	failedAgain := fixSummary(outcomeStart.Add(30*time.Minute), mcpconnectionattempts.OutcomeFailed)
	otherServer := fixSummary(outcomeStart.Add(10*time.Minute), mcpconnectionattempts.OutcomeConnected)
	otherServer.ServerID = "server-other"

	next, ok := mcpconnectionattempts.NextSuccess(failure, []mcpconnectionattempts.AttemptSummary{later, before, failedAgain, otherServer, early}, outcomeStart.Add(48*time.Hour))
	require.True(t, ok)
	require.Equal(t, early.AttemptID, next.AttemptID)

	tooLate := fixSummary(outcomeStart.Add(mcpconnectionattempts.FixLookahead), mcpconnectionattempts.OutcomeConnected)
	_, ok = mcpconnectionattempts.NextSuccess(failure, []mcpconnectionattempts.AttemptSummary{tooLate}, outcomeStart.Add(30*24*time.Hour))
	require.False(t, ok, "a success at the end of the fix lookahead is too late")

	anonymous := failure
	anonymous.SubjectURN = ""
	_, ok = mcpconnectionattempts.NextSuccess(anonymous, []mcpconnectionattempts.AttemptSummary{early}, outcomeStart.Add(48*time.Hour))
	require.False(t, ok, "a failure with no person cannot be paired")
}

func TestFixedByOf(t *testing.T) {
	t.Parallel()
	failure := fixSummary(outcomeStart, mcpconnectionattempts.OutcomeFailed, "auth-client-fix")
	success := fixSummary(outcomeStart.Add(time.Hour), mcpconnectionattempts.OutcomeConnected)
	signedIn := success
	signedIn.SignedIn = true
	between := outcomeStart.Add(30 * time.Minute)

	tests := []struct {
		name    string
		success mcpconnectionattempts.AttemptSummary
		changes []mcpconnectionattempts.Change
		want    mcpconnectionattempts.FixedBy
	}{
		{name: "a person's change on the server", success: signedIn, changes: []mcpconnectionattempts.Change{{SubjectID: "server-fix", ByAdmin: true, At: between}}, want: mcpconnectionattempts.FixedByAdminChange},
		{name: "a person's change wins over Speakeasy's", success: success, changes: []mcpconnectionattempts.Change{{SubjectID: "auth-client-fix", ByAdmin: false, At: between}, {SubjectID: "server-fix", ByAdmin: true, At: between}}, want: mcpconnectionattempts.FixedByAdminChange},
		{name: "Speakeasy's change on the auth client", success: signedIn, changes: []mcpconnectionattempts.Change{{SubjectID: "auth-client-fix", ByAdmin: false, At: between}}, want: mcpconnectionattempts.FixedBySystemChange},
		{name: "signing in again", success: signedIn, changes: nil, want: mcpconnectionattempts.FixedByReconnect},
		{name: "a plain retry", success: success, changes: nil, want: mcpconnectionattempts.FixedByRetry},
		{name: "a change on something else", success: success, changes: []mcpconnectionattempts.Change{{SubjectID: "server-unrelated", ByAdmin: true, At: between}}, want: mcpconnectionattempts.FixedByRetry},
		{name: "a change before the failure", success: success, changes: []mcpconnectionattempts.Change{{SubjectID: "server-fix", ByAdmin: true, At: outcomeStart.Add(-time.Minute)}}, want: mcpconnectionattempts.FixedByRetry},
		{name: "a change after the success", success: success, changes: []mcpconnectionattempts.Change{{SubjectID: "server-fix", ByAdmin: true, At: outcomeStart.Add(2 * time.Hour)}}, want: mcpconnectionattempts.FixedByRetry},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, mcpconnectionattempts.FixedByOf(failure, failure.End(outcomeStart), tc.success, tc.changes))
		})
	}
}

func TestIsAdminActor(t *testing.T) {
	t.Parallel()
	require.True(t, mcpconnectionattempts.IsAdminActor("user", "user_123"))
	require.True(t, mcpconnectionattempts.IsAdminActor("api_key", "key_1"), "an admin's script or Terraform")
	require.False(t, mcpconnectionattempts.IsAdminActor("user", "system"), "older background writers")
	require.False(t, mcpconnectionattempts.IsAdminActor("system", "workos-role-sync"))
	require.False(t, mcpconnectionattempts.IsAdminActor("agent", "agent_1"))
}

func TestFixedByCountsOnlyChangesAfterTheFailingRow(t *testing.T) {
	t.Parallel()
	failure := fixSummary(outcomeStart, mcpconnectionattempts.OutcomeFailed)
	failure.FinalAt = outcomeStart.Add(20 * time.Minute)
	success := fixSummary(outcomeStart.Add(time.Hour), mcpconnectionattempts.OutcomeConnected)
	duringAttempt := []mcpconnectionattempts.Change{{SubjectID: "server-fix", ByAdmin: true, At: outcomeStart.Add(10 * time.Minute)}}
	require.Equal(t, mcpconnectionattempts.FixedByRetry, mcpconnectionattempts.FixedByOf(failure, failure.End(outcomeStart), success, duringAttempt))
	afterFailing := []mcpconnectionattempts.Change{{SubjectID: "server-fix", ByAdmin: true, At: outcomeStart.Add(30 * time.Minute)}}
	require.Equal(t, mcpconnectionattempts.FixedByAdminChange, mcpconnectionattempts.FixedByOf(failure, failure.End(outcomeStart), success, afterFailing))
}

func TestLabelsSendUnknownValuesToOther(t *testing.T) {
	t.Parallel()
	require.Equal(t, "accounts.google.com", mcpconnectionattempts.ProviderLabel("accounts.google.com"))
	require.Empty(t, mcpconnectionattempts.ProviderLabel(""))
	for _, tenant := range []string{"acme.okta.com", "acme.my.salesforce.com", "login.acme.example", "mcp.acme.com:8443", "accounts.google.com.acme.example"} {
		require.Equal(t, mcpconnectionattempts.LabelOther, mcpconnectionattempts.ProviderLabel(tenant), tenant)
	}

	catalogApp := mcpconnectionattempts.AppOf("https://claude.ai/oauth/claude-code-client-metadata").String()
	require.Equal(t, "claude.ai", mcpconnectionattempts.AppLabel(catalogApp))
	require.Equal(t, "dcr", mcpconnectionattempts.AppLabel(mcpconnectionattempts.AppDCR.String()))
	customerApp := mcpconnectionattempts.AppOf("https://agents.acme.example/oauth/client.json").String()
	require.Equal(t, mcpconnectionattempts.LabelOther, mcpconnectionattempts.AppLabel(customerApp), "a customer-run metadata URL")
	require.Equal(t, mcpconnectionattempts.LabelOther, mcpconnectionattempts.AppLabel("agents.acme.example"), "a host that is not in the catalog")

	require.Equal(t, mcpconnectionattempts.FamilyCursor, mcpconnectionattempts.ClientFamilyLabel(mcpconnectionattempts.FamilyCursor))
	require.Equal(t, mcpconnectionattempts.LabelOther, mcpconnectionattempts.ClientFamilyLabel("acme-agent"))
}
