package slackapproval_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/slackapproval"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// cardBlocks decodes the blocks a Slack call carried.
func cardBlocks(t *testing.T, call slackCall) []map[string]any {
	t.Helper()
	var blocks []map[string]any
	require.NoError(t, json.Unmarshal([]byte(call.Form.Get("blocks")), &blocks))
	return blocks
}

// cardText joins every text and URL on a card, decoded, so assertions see
// what Slack renders rather than the JSON escaping.
func cardText(t *testing.T, call slackCall) string {
	t.Helper()
	var blocks any
	require.NoError(t, json.Unmarshal([]byte(call.Form.Get("blocks")), &blocks))
	var parts []string
	var walk func(any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			for key, value := range v {
				if text, ok := value.(string); ok && (key == "text" || key == "url" || key == "value") {
					parts = append(parts, text)
					continue
				}
				walk(value)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(blocks)
	return strings.Join(parts, "\n")
}

// buttonActions lists the action IDs of a card's buttons.
func buttonActions(blocks []map[string]any) []string {
	actions := []string{}
	for _, block := range blocks {
		if block["type"] != "actions" {
			continue
		}
		elements, _ := block["elements"].([]any)
		for _, element := range elements {
			button, _ := element.(map[string]any)
			if id, ok := button["action_id"].(string); ok {
				actions = append(actions, id)
			}
		}
	}
	return actions
}

func userURN(id string) string {
	return urn.NewPrincipal(urn.PrincipalTypeUser, id).String()
}

func TestNotifyDMsEachApprover(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	reviewer, reviewerEmail := f.addMember(t, "Reviewer")
	f.slack.mapUser(f.email(t, f.approverID), "UAPPROVER")
	f.slack.mapUser(reviewerEmail, "UREVIEWER")
	policy := f.addPolicy(t, userURN(f.approverID), userURN(reviewer))
	approvalID := f.pendingRequest(t, policy, "")

	result, err := f.notifier.Notify(t.Context(), f.target(approvalID))
	require.NoError(t, err)
	require.Equal(t, 2, result.Sent)
	require.False(t, result.ExpiresAt.IsZero())

	posts := f.slack.callsTo("chat.postMessage")
	require.Len(t, posts, 2)
	channels := []string{posts[0].Form.Get("channel"), posts[1].Form.Get("channel")}
	require.ElementsMatch(t, []string{"UAPPROVER", "UREVIEWER"}, channels)
	for _, post := range posts {
		require.Equal(t, "Bearer "+testBotToken, post.Token)
		blocks := cardBlocks(t, post)
		require.Equal(t, []string{slackapproval.ActionApprove, slackapproval.ActionDeny, "risk_approval_open"}, buttonActions(blocks))
		text := cardText(t, post)
		require.Contains(t, text, approvalID.String())
		require.Contains(t, text, "https://app.speakeasy.test/")
		require.Contains(t, text, "tab=tool-approvals")
		require.Contains(t, text, "> Tell the team the deploy finished.")
		// Agent-written text cannot ping the channel or render a link.
		require.Contains(t, text, "&lt;!channel&gt;")
		require.NotContains(t, text, "<!channel>")
		require.Contains(t, text, "The agent is told the call to slack__post_message was denied.")
	}

	messages := f.messages(t, approvalID)
	require.Len(t, messages, 2)
	for _, message := range messages {
		require.Equal(t, "slack", message.Provider)
		require.Equal(t, testTeamID, message.SlackTeamID.String)
		require.Equal(t, "D"+message.SlackUserID.String, message.SlackChannelID.String)
		require.NotEmpty(t, message.SlackMessageTs.String)
	}
}

func TestNotifyPrefersAConfirmedSlackMapping(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.mapToSlack(t, f.approverID, "UMAPPED")
	// The email would find someone else; the confirmed mapping wins.
	f.slack.mapUser(f.email(t, f.approverID), "UBYEMAIL")
	policy := f.addPolicy(t, userURN(f.approverID))
	approvalID := f.pendingRequest(t, policy, "")

	result, err := f.notifier.Notify(t.Context(), f.target(approvalID))
	require.NoError(t, err)
	require.Equal(t, 1, result.Sent)
	require.Empty(t, f.slack.callsTo("users.lookupByEmail"))
	posts := f.slack.callsTo("chat.postMessage")
	require.Len(t, posts, 1)
	require.Equal(t, "UMAPPED", posts[0].Form.Get("channel"))
}

func TestNotifyRepeatsWithoutDuplicates(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.slack.mapUser(f.email(t, f.approverID), "UAPPROVER")
	policy := f.addPolicy(t, userURN(f.approverID))
	approvalID := f.pendingRequest(t, policy, "")

	for range 2 {
		result, err := f.notifier.Notify(t.Context(), f.target(approvalID))
		require.NoError(t, err)
		require.Equal(t, 1, result.Sent)
	}
	require.Len(t, f.slack.callsTo("chat.postMessage"), 1)
	require.Len(t, f.messages(t, approvalID), 1)
}

func TestNotifyLeavesOutTheRequester(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	reviewer, reviewerEmail := f.addMember(t, "Reviewer")
	f.slack.mapUser(f.email(t, f.approverID), "UAPPROVER")
	f.slack.mapUser(reviewerEmail, "UREVIEWER")
	policy := f.addPolicy(t, userURN(f.approverID), userURN(reviewer))
	// The approver calls through their own session, so they cannot decide.
	approvalID := f.pendingRequest(t, policy, f.approverID)

	result, err := f.notifier.Notify(t.Context(), f.target(approvalID))
	require.NoError(t, err)
	require.Equal(t, 1, result.Sent)
	posts := f.slack.callsTo("chat.postMessage")
	require.Len(t, posts, 1)
	require.Equal(t, "UREVIEWER", posts[0].Form.Get("channel"))
}

func TestNotifyExpandsRoleApprovers(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	oncall, oncallEmail := f.addMember(t, "On call")
	_, bystanderEmail := f.addMember(t, "Bystander")
	f.slack.mapUser(oncallEmail, "UONCALL")
	f.slack.mapUser(bystanderEmail, "UBYSTANDER")
	role := f.addRole(t, "on-call", oncall)
	policy := f.addPolicy(t, role)
	approvalID := f.pendingRequest(t, policy, "")

	result, err := f.notifier.Notify(t.Context(), f.target(approvalID))
	require.NoError(t, err)
	require.Equal(t, 1, result.Sent)
	posts := f.slack.callsTo("chat.postMessage")
	require.Len(t, posts, 1)
	require.Equal(t, "UONCALL", posts[0].Form.Get("channel"))
}

func TestNotifyWithoutAWorkspaceSendsNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.workspaces.workspaces = []slackdirectoryconnections.MessagingWorkspace{}
	policy := f.addPolicy(t, userURN(f.approverID))
	approvalID := f.pendingRequest(t, policy, "")

	result, err := f.notifier.Notify(t.Context(), f.target(approvalID))
	require.NoError(t, err)
	require.Equal(t, 0, result.Sent)
	require.Empty(t, f.slack.callsTo("users.lookupByEmail"))
	require.Empty(t, f.slack.callsTo("chat.postMessage"))
}

func TestNotifySkipsApproversWithoutSlack(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	reviewer, reviewerEmail := f.addMember(t, "Reviewer")
	f.slack.mapUser(reviewerEmail, "UREVIEWER")
	policy := f.addPolicy(t, userURN(f.approverID), userURN(reviewer))
	approvalID := f.pendingRequest(t, policy, "")

	result, err := f.notifier.Notify(t.Context(), f.target(approvalID))
	require.NoError(t, err)
	require.Equal(t, 1, result.Sent)
	require.Len(t, f.slack.callsTo("chat.postMessage"), 1)
}

func TestNotifyRetriesOnlyWhatSlackFailedToSend(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	reviewer, reviewerEmail := f.addMember(t, "Reviewer")
	f.slack.mapUser(f.email(t, f.approverID), "UAPPROVER")
	f.slack.mapUser(reviewerEmail, "UREVIEWER")
	f.slack.failPostTo("UREVIEWER", "ratelimited")
	policy := f.addPolicy(t, userURN(f.approverID), userURN(reviewer))
	approvalID := f.pendingRequest(t, policy, "")

	_, err := f.notifier.Notify(t.Context(), f.target(approvalID))
	require.Error(t, err)
	require.Len(t, f.messages(t, approvalID), 1)

	f.slack.failPostTo("UREVIEWER", "")
	result, err := f.notifier.Notify(t.Context(), f.target(approvalID))
	require.NoError(t, err)
	require.Equal(t, 2, result.Sent)
	posts := f.slack.callsTo("chat.postMessage")
	require.Len(t, posts, 3)
	require.Len(t, slices.DeleteFunc(posts, func(c slackCall) bool { return c.Form.Get("channel") != "UREVIEWER" }), 2)
}

func TestNotifyGivesUpOnSlackRefusals(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.slack.mapUser(f.email(t, f.approverID), "UAPPROVER")
	// An install without chat:write cannot post; retrying cannot fix it.
	f.slack.failPostTo("UAPPROVER", "missing_scope")
	policy := f.addPolicy(t, userURN(f.approverID))
	approvalID := f.pendingRequest(t, policy, "")

	result, err := f.notifier.Notify(t.Context(), f.target(approvalID))
	require.NoError(t, err)
	require.Equal(t, 0, result.Sent)
	require.Empty(t, f.messages(t, approvalID))
}

func TestNotifyIgnoresDecidedRequests(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	reviewer, _ := f.addMember(t, "Reviewer")
	policy := f.addPolicy(t, userURN(reviewer))
	approvalID := f.pendingRequest(t, policy, "")
	_, err := f.approvals.Decide(t.Context(), approval.DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: approvalID, Approve: false, DeciderUserID: reviewer, DeciderDisplayName: nil, Via: approval.DecidedViaDashboard, Note: ""})
	require.NoError(t, err)

	result, err := f.notifier.Notify(t.Context(), f.target(approvalID))
	require.NoError(t, err)
	require.Equal(t, 0, result.Sent)
	require.Empty(t, f.slack.callsTo("chat.postMessage"))
}

func TestFinishReplacesEveryDMWithTheOutcome(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	reviewer, reviewerEmail := f.addMember(t, "Reviewer")
	f.slack.mapUser(f.email(t, f.approverID), "UAPPROVER")
	f.slack.mapUser(reviewerEmail, "UREVIEWER")
	policy := f.addPolicy(t, userURN(f.approverID), userURN(reviewer))
	approvalID := f.pendingRequest(t, policy, "")
	_, err := f.notifier.Notify(t.Context(), f.target(approvalID))
	require.NoError(t, err)

	// Still pending: nothing to update yet.
	require.NoError(t, f.notifier.Finish(t.Context(), f.target(approvalID)))
	require.Empty(t, f.slack.callsTo("chat.update"))

	_, err = f.approvals.Decide(t.Context(), approval.DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: approvalID, Approve: true, DeciderUserID: reviewer, DeciderDisplayName: nil, Via: approval.DecidedViaDashboard, Note: "Go ahead."})
	require.NoError(t, err)
	require.NoError(t, f.notifier.Finish(t.Context(), f.target(approvalID)))

	updates := f.slack.callsTo("chat.update")
	require.Len(t, updates, 2)
	for _, update := range updates {
		require.Equal(t, "Approved: one call to slack__post_message", update.Form.Get("text"))
		require.Empty(t, buttonActions(cardBlocks(t, update)))
		require.Contains(t, cardText(t, update), "*Approved* by Reviewer")
		require.Contains(t, cardText(t, update), "Go ahead.")
	}
}
