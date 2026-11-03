package approval

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestHoldCallReturnsSameTokenWhileOpen(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	outcome, err := f.service.HoldCall(t.Context(), f.call(`{"title":"bug","labels":["a"]}`))
	require.NoError(t, err)
	require.False(t, outcome.Allowed)
	require.Contains(t, outcome.UserMessage, "curl -sS -X POST 'https://speakeasy.test/approvals/request' -H 'Authorization: Bearer "+outcome.Token+"'")

	// Same arguments in another key order and spacing: same request.
	require.Equal(t, outcome.Token, f.hold(t, f.call(`{ "labels": ["a"], "title": "bug" }`)))
	// Different arguments: a separate request.
	require.NotEqual(t, outcome.Token, f.hold(t, f.call(`{"title":"other"}`)))

	require.Equal(t, StatusPending, f.request(t, outcome.Token))
	again, err := f.service.HoldCall(t.Context(), f.call(`{"title":"bug","labels":["a"]}`))
	require.NoError(t, err)
	require.Equal(t, outcome.Token, again.Token)
	require.Contains(t, again.UserMessage, "approval was already requested")
	require.Contains(t, again.UserMessage, "-H 'Authorization: Bearer "+outcome.Token+"' 'https://speakeasy.test/approvals/watch?format=text'")
}

func TestApprovedCallIsReleasedExactlyOnce(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	call := f.call(`{"title":"bug"}`)
	token := f.hold(t, call)

	status, err := f.service.WatchStatus(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, StatusBlocked, status)

	requests, err := audittest.AuditLogCountByAction(t.Context(), f.db, audit.ActionRiskPolicyApprovalRequest)
	require.NoError(t, err)
	require.Equal(t, StatusPending, f.request(t, token))
	// Repeating the request leaves it as it is.
	require.Equal(t, StatusPending, f.request(t, token))
	requestsAfter, err := audittest.AuditLogCountByAction(t.Context(), f.db, audit.ActionRiskPolicyApprovalRequest)
	require.NoError(t, err)
	require.Equal(t, requests+1, requestsAfter, "only the first request is audited")
	approvals, err := audittest.AuditLogCountByAction(t.Context(), f.db, audit.ActionRiskPolicyApprovalApprove)
	require.NoError(t, err)

	decided, err := f.service.Decide(t.Context(), DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: f.approvalID(t, token), Approve: true, DeciderUserID: f.approverID, Via: DecidedViaDashboard, Note: ""})
	require.NoError(t, err)
	require.Equal(t, string(StatusApproved), decided.Status)
	approvalsAfter, err := audittest.AuditLogCountByAction(t.Context(), f.db, audit.ActionRiskPolicyApprovalApprove)
	require.NoError(t, err)
	require.Equal(t, approvals+1, approvalsAfter)
	status, err = f.service.WatchStatus(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, StatusApproved, status)

	// A different call stays held.
	require.NotEqual(t, token, f.hold(t, f.call(`{"title":"other"}`)))

	released, err := f.service.HoldCall(t.Context(), call)
	require.NoError(t, err)
	require.True(t, released.Allowed)
	status, err = f.service.WatchStatus(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, StatusApproved, status, "the watcher keeps reporting approved after the retry")

	// The approval is used up: the next identical call starts a new request.
	next := f.hold(t, call)
	require.NotEqual(t, token, next)
}

func TestDeniedCallStartsNewRequestOnRetry(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	call := f.call(`{"title":"bug"}`)
	token := f.hold(t, call)
	f.request(t, token)

	_, err := f.service.Decide(t.Context(), DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: f.approvalID(t, token), Approve: false, DeciderUserID: f.approverID, Via: DecidedViaDashboard, Note: "Use the staging project."})
	require.NoError(t, err)

	status, err := f.service.WatchStatus(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, StatusDenied, status)
	row, status, err := f.service.Request(t.Context(), RequestInput{Token: token, Reason: "again", Scope: "", ExpiresIn: 0})
	require.NoError(t, err)
	require.Equal(t, StatusDenied, status)
	require.Equal(t, "Use the staging project.", row.DecisionNote.String)

	require.NotEqual(t, token, f.hold(t, call))
}

func TestDecideChecksTheDecider(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	token := f.hold(t, f.call(`{}`))
	id := f.approvalID(t, token)

	_, err := f.service.Decide(t.Context(), DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: id, Approve: true, DeciderUserID: f.approverID, Via: DecidedViaDashboard, Note: ""})
	require.ErrorIs(t, err, ErrNotPending, "a request must be filed before anyone decides it")

	f.request(t, token)
	_, err = f.service.Decide(t.Context(), DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: id, Approve: true, DeciderUserID: f.addMember(t), Via: DecidedViaDashboard, Note: ""})
	require.ErrorIs(t, err, ErrNotApprover)

	_, err = f.service.Decide(t.Context(), DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: id, Approve: true, DeciderUserID: f.approverID, Via: DecidedViaDashboard, Note: ""})
	require.NoError(t, err)
	_, err = f.service.Decide(t.Context(), DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: id, Approve: false, DeciderUserID: f.approverID, Via: DecidedViaDashboard, Note: ""})
	require.ErrorIs(t, err, ErrNotPending, "a decision is final")
}

func TestRequesterCannotApproveOwnCall(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	call := f.call(`{}`)
	call.RequesterURN = "user:" + f.approverID
	call.RequesterUserID = f.approverID
	token := f.hold(t, call)
	_, _, err := f.service.Request(t.Context(), RequestInput{Token: token, Reason: "needed", Scope: "", ExpiresIn: 0})
	require.NoError(t, err)

	_, err = f.service.Decide(t.Context(), DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: f.approvalID(t, token), Approve: true, DeciderUserID: f.approverID, Via: DecidedViaDashboard, Note: ""})
	require.ErrorIs(t, err, ErrSelfApproval)
}

func TestRequestValidatesInput(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	token := f.hold(t, f.call(`{}`))

	cases := map[string]struct {
		input RequestInput
		err   error
	}{
		"unknown token":   {input: RequestInput{Token: "apr_" + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Reason: "r", Scope: "", ExpiresIn: 0}, err: ErrNotFound},
		"malformed token": {input: RequestInput{Token: "not-a-token", Reason: "r", Scope: "", ExpiresIn: 0}, err: ErrNotFound},
		"no reason":       {input: RequestInput{Token: token, Reason: "  ", Scope: "", ExpiresIn: 0}, err: ErrInvalidRequest},
		"wider scope":     {input: RequestInput{Token: token, Reason: "r", Scope: "always", ExpiresIn: 0}, err: ErrInvalidRequest},
		"short expiry":    {input: RequestInput{Token: token, Reason: "r", Scope: "", ExpiresIn: time.Second}, err: ErrInvalidRequest},
		"long expiry":     {input: RequestInput{Token: token, Reason: "r", Scope: "", ExpiresIn: 48 * time.Hour}, err: ErrInvalidRequest},
	}
	// The group returns only after its parallel cases finish, so the status
	// check below sees every rejected request.
	t.Run("cases", func(t *testing.T) {
		t.Parallel()
		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, _, err := f.service.Request(t.Context(), tc.input)
				require.ErrorIs(t, err, tc.err)
			})
		}
	})

	status, err := f.service.WatchStatus(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, StatusBlocked, status, "rejected requests leave the call blocked")
}

func TestExpiredRequestCannotBeDecidedAndRetryStartsOver(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	call := f.call(`{}`)
	token := f.hold(t, call)

	// File the request an hour ago, so its 30-minute window has passed.
	f.shiftClock(-time.Hour)
	f.request(t, token)
	f.shiftClock(0)

	status, err := f.service.WatchStatus(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, StatusExpired, status)

	_, err = f.service.Decide(t.Context(), DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: f.approvalID(t, token), Approve: true, DeciderUserID: f.approverID, Via: DecidedViaDashboard, Note: ""})
	require.ErrorIs(t, err, ErrNotPending)

	require.NotEqual(t, token, f.hold(t, call))
	status, err = f.service.WatchStatus(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, StatusExpired, status)
}

func TestCallFingerprintIgnoresKeyOrderAndSpacing(t *testing.T) {
	t.Parallel()
	a, err := CallFingerprint("s", "t", []byte(`{"b":1,"a":{"y":2,"x":[1,2]}}`))
	require.NoError(t, err)
	b, err := CallFingerprint("s", "t", []byte(` { "a" : { "x" : [1,2], "y" : 2 }, "b" : 1 } `))
	require.NoError(t, err)
	require.Equal(t, a, b)

	for _, other := range []struct{ server, tool, args string }{
		{"s", "t", `{"b":2,"a":{"y":2,"x":[1,2]}}`},
		{"s", "t", `{"b":1,"a":{"y":2,"x":[2,1]}}`},
		{"s", "u", `{"b":1,"a":{"y":2,"x":[1,2]}}`},
		{"r", "t", `{"b":1,"a":{"y":2,"x":[1,2]}}`},
	} {
		c, err := CallFingerprint(other.server, other.tool, []byte(other.args))
		require.NoError(t, err)
		require.NotEqual(t, a, c)
	}

	empty, err := CallFingerprint("s", "t", nil)
	require.NoError(t, err)
	emptyObject, err := CallFingerprint("s", "t", []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, empty, emptyObject, "a call without arguments matches an empty arguments object")
}

func TestEveryCoveringPolicyMustApprove(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	second := f.addPolicy(t, "Second policy")
	call := f.call(`{"title":"bug"}`)
	call.Policies = append(call.Policies, second)

	firstToken := f.hold(t, call)
	f.request(t, firstToken)
	_, err := f.service.Decide(t.Context(), DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: f.approvalID(t, firstToken), Approve: true, DeciderUserID: f.approverID, Via: DecidedViaDashboard, Note: ""})
	require.NoError(t, err)

	// The first policy approved; the second now holds the call, and the first
	// approval stays unused.
	outcome, err := f.service.HoldCall(t.Context(), call)
	require.NoError(t, err)
	require.False(t, outcome.Allowed)
	require.NotNil(t, outcome.Policy)
	require.Equal(t, second.ID, outcome.Policy.ID)
	require.NotEqual(t, firstToken, outcome.Token)
	status, err := f.service.WatchStatus(t.Context(), firstToken)
	require.NoError(t, err)
	require.Equal(t, StatusApproved, status)

	f.request(t, outcome.Token)
	_, err = f.service.Decide(t.Context(), DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: f.approvalID(t, outcome.Token), Approve: true, DeciderUserID: f.approverID, Via: DecidedViaDashboard, Note: ""})
	require.NoError(t, err)

	released, err := f.service.HoldCall(t.Context(), call)
	require.NoError(t, err)
	require.True(t, released.Allowed)
	// Both approvals were used by that one retry.
	again, err := f.service.HoldCall(t.Context(), call)
	require.NoError(t, err)
	require.False(t, again.Allowed)
	require.NotNil(t, again.Policy)
	require.Equal(t, f.policy.ID, again.Policy.ID)
}

func TestUnusedApprovalExpiresWithItsWindow(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	call := f.call(`{}`)
	token := f.hold(t, call)

	// Request and approve with the window already closed for the gateway's
	// clock, as if nobody retried in time.
	f.shiftClock(-29 * time.Minute)
	f.request(t, token)
	f.shiftClock(0)
	_, err := f.service.Decide(t.Context(), DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: f.approvalID(t, token), Approve: true, DeciderUserID: f.approverID, Via: DecidedViaDashboard, Note: ""})
	require.NoError(t, err)
	f.shiftClock(2 * time.Minute)

	status, err := f.service.WatchStatus(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, StatusExpired, status)
}

func TestArgumentsPreviewKeepsExactNumbers(t *testing.T) {
	t.Parallel()
	require.JSONEq(t, `{"id":9007199254740993,"nested":"{\"n\":12345678901234567890}"}`, string(argumentsPreview([]byte(`{"id":9007199254740993,"nested":{"n":12345678901234567890}}`))))
}

func TestArgumentsPreviewNotesOmittedArguments(t *testing.T) {
	t.Parallel()
	args := make(map[string]int, maxPreviewKeys+3)
	for i := range maxPreviewKeys + 3 {
		args[fmt.Sprintf("k%02d", i)] = i
	}
	raw, err := json.Marshal(args)
	require.NoError(t, err)

	var preview map[string]any
	require.NoError(t, json.Unmarshal(argumentsPreview(raw), &preview))
	require.Len(t, preview, maxPreviewKeys+1)
	require.Contains(t, preview, "k00")
	require.NotContains(t, preview, fmt.Sprintf("k%02d", maxPreviewKeys))
	require.Equal(t, "arguments not shown: 3", preview[omittedArgumentsKey])
}

func TestArgumentsPreviewKeepsAnArgumentNamedLikeTheNote(t *testing.T) {
	t.Parallel()
	args := make(map[string]int, maxPreviewKeys+1)
	for i := range maxPreviewKeys - 1 {
		args[fmt.Sprintf("k%02d", i)] = i
	}
	args[omittedArgumentsKey] = 100
	args[omittedArgumentsKey+"z"] = 101
	raw, err := json.Marshal(args)
	require.NoError(t, err)

	var preview map[string]any
	require.NoError(t, json.Unmarshal(argumentsPreview(raw), &preview))
	require.InDelta(t, 100, preview[omittedArgumentsKey], 0)
	require.Equal(t, "arguments not shown: 1", preview[omittedArgumentsKey+"…"])
}

func TestHeldCallIgnoresAChatThatIsNotInTheProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	call := f.call(`{"title":"unknown chat"}`)
	call.ChatID = uuid.NewString()
	row, err := repo.New(f.db).GetApprovalByToken(t.Context(), f.hold(t, call))
	require.NoError(t, err)
	require.False(t, row.ChatID.Valid)
}

func TestHeldCallRecordsTheMCPServerOnlyWhenItIsInTheProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	serverID := f.addRemoteServer(t, f.projectID)

	onServer := f.call(`{"channel":"#releases"}`)
	onServer.ServerID = serverID.String()
	row, err := repo.New(f.db).GetApprovalByToken(t.Context(), f.hold(t, onServer))
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: serverID, Valid: true}, row.McpServerID)

	// A real MCP server in another project of the organization is not recorded.
	otherProjectID, err := testrepo.New(f.db).CreateProjectFixture(t.Context(), testrepo.CreateProjectFixtureParams{
		ID:             uuid.New(),
		Name:           "Other project",
		Slug:           "other-" + uuid.NewString()[:8],
		OrganizationID: f.organizationID,
	})
	require.NoError(t, err)
	inOtherProject := f.call(`{"channel":"#random"}`)
	inOtherProject.ServerID = f.addRemoteServer(t, otherProjectID).String()
	row, err = repo.New(f.db).GetApprovalByToken(t.Context(), f.hold(t, inOtherProject))
	require.NoError(t, err)
	require.False(t, row.McpServerID.Valid)

	// A Platform MCP call carries a platform toolset id, not an MCP server.
	elsewhere := f.call(`{"channel":"#general"}`)
	elsewhere.ServerID = uuid.NewString()
	row, err = repo.New(f.db).GetApprovalByToken(t.Context(), f.hold(t, elsewhere))
	require.NoError(t, err)
	require.False(t, row.McpServerID.Valid)
}
