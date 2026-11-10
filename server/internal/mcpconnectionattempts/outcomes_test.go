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
	require.Equal(t, early.AttemptID, next.Success.AttemptID)
	require.Equal(t, failure.End(outcomeStart.Add(48*time.Hour)), next.Failed)

	tooLate := fixSummary(outcomeStart.Add(mcpconnectionattempts.FixLookahead), mcpconnectionattempts.OutcomeConnected)
	_, ok = mcpconnectionattempts.NextSuccess(failure, []mcpconnectionattempts.AttemptSummary{tooLate}, outcomeStart.Add(30*24*time.Hour))
	require.False(t, ok, "a success at the end of the fix lookahead is too late")

	anonymous := failure
	anonymous.SubjectURN = ""
	_, ok = mcpconnectionattempts.NextSuccess(anonymous, []mcpconnectionattempts.AttemptSummary{early}, outcomeStart.Add(48*time.Hour))
	require.False(t, ok, "a failure with no person cannot be paired")
}

func TestFixFixedBy(t *testing.T) {
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
			fix := mcpconnectionattempts.Fix{Failure: failure, Failed: failure.End(outcomeStart), Success: tc.success}
			require.Equal(t, tc.want, fix.FixedBy(tc.changes))
		})
	}
}

func TestIsAdminActor(t *testing.T) {
	t.Parallel()
	require.True(t, mcpconnectionattempts.IsAdminActor("user", "user_123", false))
	require.True(t, mcpconnectionattempts.IsAdminActor("api_key", "key_1", false), "an admin's script or Terraform")
	require.False(t, mcpconnectionattempts.IsAdminActor("user", "user_staff", true), "Speakeasy staff")
	require.False(t, mcpconnectionattempts.IsAdminActor("user", "system", false), "older background writers")
	require.False(t, mcpconnectionattempts.IsAdminActor("system", "workos-role-sync", false))
	require.True(t, mcpconnectionattempts.IsAdminActor("agent", "agent_1", false), "the customer's agent")
	require.True(t, mcpconnectionattempts.IsAdminActor("workload", "workload_1", false), "a workload acts for the customer's agent")
	require.False(t, mcpconnectionattempts.IsAdminActor("agent", "agent_1", true), "Speakeasy staff")
}

func TestFixedByCountsOnlyChangesAfterTheFailingRow(t *testing.T) {
	t.Parallel()
	failure := fixSummary(outcomeStart, mcpconnectionattempts.OutcomeFailed)
	failure.FinalAt = outcomeStart.Add(20 * time.Minute)
	success := fixSummary(outcomeStart.Add(time.Hour), mcpconnectionattempts.OutcomeConnected)
	fix, ok := mcpconnectionattempts.NextSuccess(failure, []mcpconnectionattempts.AttemptSummary{success}, outcomeStart)
	require.True(t, ok)
	duringAttempt := []mcpconnectionattempts.Change{{SubjectID: "server-fix", ByAdmin: true, At: outcomeStart.Add(10 * time.Minute)}}
	require.Equal(t, mcpconnectionattempts.FixedByRetry, fix.FixedBy(duringAttempt))
	afterFailing := []mcpconnectionattempts.Change{{SubjectID: "server-fix", ByAdmin: true, At: outcomeStart.Add(30 * time.Minute)}}
	require.Equal(t, mcpconnectionattempts.FixedByAdminChange, fix.FixedBy(afterFailing))
}

func TestLabelsSendUnknownValuesToOther(t *testing.T) {
	t.Parallel()
	require.Equal(t, "accounts.google.com", mcpconnectionattempts.ProviderLabel("accounts.google.com"))
	require.Equal(t, "id.speakeasy.com", mcpconnectionattempts.ProviderLabel("id.speakeasy.com"), "Speakeasy's own sign-in host")
	require.Empty(t, mcpconnectionattempts.ProviderLabel(""))
	for _, tenant := range []string{"acme.okta.com", "acme.my.salesforce.com", "login.acme.example", "mcp.acme.com:8443", "accounts.google.com.acme.example"} {
		require.Equal(t, mcpconnectionattempts.LabelOther, mcpconnectionattempts.ProviderLabel(tenant), tenant)
	}

	catalogApp := mcpconnectionattempts.AppOf("https://claude.ai/oauth/claude-code-client-metadata").String()
	require.Equal(t, "claude.ai", mcpconnectionattempts.AppLabel(catalogApp))
	require.Equal(t, "dcr", mcpconnectionattempts.AppLabel(mcpconnectionattempts.AppDCR.String()))
	customerApp := mcpconnectionattempts.AppOf("https://agents.acme.example/oauth/client.json").String()
	require.Equal(t, mcpconnectionattempts.AppOtherMetadataURL.String(), mcpconnectionattempts.AppLabel(customerApp), "a customer-run metadata URL keeps no host")
	require.Equal(t, mcpconnectionattempts.LabelOther, mcpconnectionattempts.AppLabel("agents.acme.example"), "a host that is not in the catalog")

	require.Equal(t, mcpconnectionattempts.FamilyCursor, mcpconnectionattempts.ClientFamilyLabel(mcpconnectionattempts.FamilyCursor))
	require.Equal(t, mcpconnectionattempts.LabelOther, mcpconnectionattempts.ClientFamilyLabel("acme-agent"))

	require.Equal(t, mcpconnectionattempts.LabelOther, mcpconnectionattempts.StepLabel("acme_step"))
	require.Equal(t, mcpconnectionattempts.LabelOther, mcpconnectionattempts.OutcomeLabel(""))
	require.Equal(t, mcpconnectionattempts.LabelOther, mcpconnectionattempts.ReasonLabel("acme said no"))
	require.Empty(t, mcpconnectionattempts.ReasonLabel(""))
	require.Equal(t, mcpconnectionattempts.LabelOther, mcpconnectionattempts.FixedByLabel("acme_fix"))
}

func TestLabelsKeepEveryValueTheWriterProduces(t *testing.T) {
	t.Parallel()
	for _, family := range []string{
		mcpconnectionattempts.FamilyCodexCLI, mcpconnectionattempts.FamilyClaudeCode, mcpconnectionattempts.FamilyClaudeAI,
		mcpconnectionattempts.FamilyOpenAIConnector, mcpconnectionattempts.FamilyCursor, mcpconnectionattempts.FamilyVSCode,
		mcpconnectionattempts.FamilyPerplexity, mcpconnectionattempts.FamilyScript, mcpconnectionattempts.FamilyBrowser,
		mcpconnectionattempts.FamilyOther, mcpconnectionattempts.FamilyCopilot, mcpconnectionattempts.FamilyZed,
		mcpconnectionattempts.FamilyKiro, mcpconnectionattempts.FamilyReplit, mcpconnectionattempts.FamilyNotion,
	} {
		require.Equal(t, family, mcpconnectionattempts.ClientFamilyLabel(family))
	}
	for _, app := range []mcpconnectionattempts.App{
		mcpconnectionattempts.AppDCR, mcpconnectionattempts.AppFirstParty, mcpconnectionattempts.AppUnverified,
		mcpconnectionattempts.AppWorkload, mcpconnectionattempts.AppCodeMode, mcpconnectionattempts.AppRecoveryLink,
		mcpconnectionattempts.AppBackground, mcpconnectionattempts.AppOtherMetadataURL,
	} {
		require.Equal(t, app.String(), mcpconnectionattempts.AppLabel(app.String()))
		require.True(t, mcpconnectionattempts.IsAppKind(app.String()))
	}
	for _, step := range []mcpconnectionattempts.Step{
		mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.StepInboundConsent,
		mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.StepProviderToken,
		mcpconnectionattempts.StepProviderRefresh, mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.StepUpstreamEMA,
		mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.StepDiscoveryListing, mcpconnectionattempts.StepDiscoveryMember,
	} {
		require.Equal(t, step.String(), mcpconnectionattempts.StepLabel(step.String()))
	}
	for _, outcome := range []mcpconnectionattempts.Outcome{
		mcpconnectionattempts.OutcomeConnected, mcpconnectionattempts.OutcomeFailed, mcpconnectionattempts.OutcomeBlocked,
		mcpconnectionattempts.OutcomeDeclined, mcpconnectionattempts.OutcomeAbandoned,
	} {
		require.Equal(t, string(outcome), mcpconnectionattempts.OutcomeLabel(string(outcome)))
	}
	for _, fix := range []mcpconnectionattempts.FixedBy{
		mcpconnectionattempts.FixedByNothing, mcpconnectionattempts.FixedByAdminChange, mcpconnectionattempts.FixedBySystemChange,
		mcpconnectionattempts.FixedByReconnect, mcpconnectionattempts.FixedByRetry,
	} {
		require.Equal(t, string(fix), mcpconnectionattempts.FixedByLabel(fix))
	}
	require.Equal(t, mcpconnectionattempts.CodeNeedsSignIn.String(), mcpconnectionattempts.ReasonLabel(mcpconnectionattempts.CodeNeedsSignIn.String()))
}

func TestNextSuccessIgnoresAnAttemptThatStartedBeforeTheFailure(t *testing.T) {
	t.Parallel()
	failure := fixSummary(outcomeStart, mcpconnectionattempts.OutcomeFailed)
	failure.FinalAt = outcomeStart.Add(20 * time.Minute)
	overlapping := fixSummary(outcomeStart.Add(5*time.Minute), mcpconnectionattempts.OutcomeConnected)
	_, ok := mcpconnectionattempts.NextSuccess(failure, []mcpconnectionattempts.AttemptSummary{overlapping}, outcomeStart.Add(48*time.Hour))
	require.False(t, ok, "a success already under way when the failure happened did not fix it")

	after := fixSummary(outcomeStart.Add(25*time.Minute), mcpconnectionattempts.OutcomeConnected)
	fix, ok := mcpconnectionattempts.NextSuccess(failure, []mcpconnectionattempts.AttemptSummary{overlapping, after}, outcomeStart.Add(48*time.Hour))
	require.True(t, ok)
	require.Equal(t, after.AttemptID, fix.Success.AttemptID)
}

func TestNextSuccessPairsServerlessAttemptsOnlyOnTheSameConnection(t *testing.T) {
	t.Parallel()
	serverless := func(started time.Time, outcome mcpconnectionattempts.Outcome, project uuid.UUID, remoteClient string) mcpconnectionattempts.AttemptSummary {
		s := fixSummary(started, outcome)
		s.ServerID, s.ServerKind, s.AppKey, s.ProjectID, s.RemoteSessionClientID, s.Touched = "", "provider_session", "background", project, remoteClient, nil
		return s
	}
	project := uuid.New()
	failure := serverless(outcomeStart, mcpconnectionattempts.OutcomeFailed, project, "slack-client")
	now := outcomeStart.Add(48 * time.Hour)

	_, ok := mcpconnectionattempts.NextSuccess(failure, []mcpconnectionattempts.AttemptSummary{serverless(outcomeStart.Add(time.Hour), mcpconnectionattempts.OutcomeConnected, project, "github-client")}, now)
	require.False(t, ok, "another provider's refresh")
	_, ok = mcpconnectionattempts.NextSuccess(failure, []mcpconnectionattempts.AttemptSummary{serverless(outcomeStart.Add(time.Hour), mcpconnectionattempts.OutcomeConnected, uuid.New(), "slack-client")}, now)
	require.False(t, ok, "the same provider in another project")
	_, ok = mcpconnectionattempts.NextSuccess(failure, []mcpconnectionattempts.AttemptSummary{serverless(outcomeStart.Add(time.Hour), mcpconnectionattempts.OutcomeConnected, project, "slack-client")}, now)
	require.True(t, ok)
}

func TestIsConfigChange(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"mcp-server:update", "mcp-server:update-tool-metadata", "auth-client:attach-mcp-server", "meta-mcp:add_member", "remote-session-client:revoke-sessions", "plugin:server_update", "identity-provider-connection:submit-client-id", "remote-session:refresh", "mcp_approval_request:approve", "access_challenge:resolve", "killswitch:activate", "killswitch:deactivate", "agent:suspend", "agent:reassign", "identity-provider-connection:verify", "organization:webhooks_enabled", "access_member:update_role", "mcp-server:promote-catalogue"} {
		require.True(t, mcpconnectionattempts.IsConfigChange(action), action)
	}
	for _, action := range []string{"platform-mcp-diagnostics:user_status_read", "platform-mcp-diagnostics:attribution_read", "mcp-server:view", "chat_session:access", "chat_session:handoff_export", "risk_result:reveal_payload", "risk_result:unmask", "assistant:tool_call", "update", "update:", ""} {
		require.False(t, mcpconnectionattempts.IsConfigChange(action), action)
	}
}

func TestSummarizeTakesTheProviderThatEndedTheAttempt(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.November, 5, 10, 0, 0, 0, time.UTC)
	a := mcpconnectionattempts.AttemptAt(uuid.New(), start, mcpconnectionattempts.GatewayScope("org-a", uuid.New(), uuid.New(), mcpconnectionattempts.AppDCR))
	row := func(step mcpconnectionattempts.Step, what mcpconnectionattempts.Happened, final bool, after time.Duration, opts ...mcpconnectionattempts.Option) mcpconnectionattempts.Row {
		r, err := mcpconnectionattempts.RowAt(a, step, what, final, start.Add(after), opts...)
		require.NoError(t, err)
		return r
	}
	signIn := row(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.ResultSignedIn, false, time.Minute, mcpconnectionattempts.Provider("https://accounts.google.com"))

	failed := mcpconnectionattempts.Summarize([]mcpconnectionattempts.Row{signIn,
		row(mcpconnectionattempts.StepProviderToken, mcpconnectionattempts.CodeProviderUnavailable, true, 2*time.Minute, mcpconnectionattempts.Provider("https://linear.app"))})
	require.Equal(t, "linear.app", failed.ProviderHost, "the member that ended the attempt")

	noHost := mcpconnectionattempts.Summarize([]mcpconnectionattempts.Row{signIn,
		row(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.ResultComplete, true, 2*time.Minute)})
	require.Equal(t, "accounts.google.com", noHost.ProviderHost, "an ending row with no provider falls back to the earliest")
}

func TestSummarizeBreaksAProviderTieTheSameWayAsSQL(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.November, 5, 10, 0, 0, 0, time.UTC)
	a := mcpconnectionattempts.AttemptAt(uuid.New(), start, mcpconnectionattempts.GatewayScope("org-a", uuid.New(), uuid.New(), mcpconnectionattempts.AppDCR))
	at := start.Add(time.Minute)
	first, err := mcpconnectionattempts.RowAt(a, mcpconnectionattempts.StepProviderToken, mcpconnectionattempts.CodeProviderUnavailable, true, at, mcpconnectionattempts.Provider("https://linear.app"))
	require.NoError(t, err)
	second, err := mcpconnectionattempts.RowAt(a, mcpconnectionattempts.StepProviderToken, mcpconnectionattempts.CodeProviderUnavailable, true, at, mcpconnectionattempts.Provider("https://accounts.google.com"))
	require.NoError(t, err)

	require.Equal(t, "accounts.google.com", mcpconnectionattempts.Summarize([]mcpconnectionattempts.Row{first, second}).ProviderHost, "the smaller host wins a final-row tie")
	require.Equal(t, "accounts.google.com", mcpconnectionattempts.Summarize([]mcpconnectionattempts.Row{second, first}).ProviderHost, "in any order")
}
