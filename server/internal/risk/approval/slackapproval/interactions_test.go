package slackapproval_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/slackapproval"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const testSigningSecret = "synthetic-signing-secret"

// fakeResponder records replies to clicks.
type fakeResponder struct {
	mu      sync.Mutex
	replies []slackapproval.ResponseMessage
}

func (r *fakeResponder) Respond(_ context.Context, _ string, message slackapproval.ResponseMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replies = append(r.replies, message)
	return nil
}

func (r *fakeResponder) only(t *testing.T) slackapproval.ResponseMessage {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.Len(t, r.replies, 1)
	return r.replies[0]
}

// clickFixture is a pending request whose approver has its DM.
type clickFixture struct {
	*fixture
	handler    *slackapproval.Interactions
	responder  *fakeResponder
	policyID   uuid.UUID
	approvalID uuid.UUID
	message    repo.RiskPolicyApprovalMessage
}

func newClickFixture(t *testing.T) *clickFixture {
	t.Helper()
	f := newFixture(t)
	f.slack.mapUser(f.email(t, f.approverID), "UAPPROVER")
	policy := f.addPolicy(t, userURN(f.approverID))
	approvalID := f.pendingRequest(t, policy, "")
	_, err := f.notifier.Notify(t.Context(), f.target(approvalID))
	require.NoError(t, err)
	messages := f.messages(t, approvalID)
	require.Len(t, messages, 1)
	responder := &fakeResponder{mu: sync.Mutex{}, replies: nil}
	return &clickFixture{
		fixture:    f,
		handler:    slackapproval.NewInteractions(testenv.NewLogger(t), f.cards, responder, testSigningSecret),
		responder:  responder,
		policyID:   policy.ID,
		approvalID: approvalID,
		message:    messages[0],
	}
}

// clickPayload is a block_actions payload for a button on message.
func clickPayload(message repo.RiskPolicyApprovalMessage, slackUserID, actionID string) map[string]any {
	return map[string]any{
		"type":         "block_actions",
		"team":         map[string]any{"id": message.SlackTeamID.String},
		"user":         map[string]any{"id": slackUserID},
		"container":    map[string]any{"type": "message", "channel_id": message.SlackChannelID.String, "message_ts": message.SlackMessageTs.String},
		"response_url": "https://hooks.slack.com/actions/TEXAMPLE01/1/synthetic",
		"actions":      []any{map[string]any{"action_id": actionID, "value": message.ApprovalID.String(), "type": "button"}},
	}
}

// send posts a click signed with secret at the given time.
func (f *clickFixture) send(t *testing.T, payload map[string]any, secret string, at time.Time) int {
	t.Helper()
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	body := url.Values{"payload": {string(encoded)}}.Encode()
	timestamp := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("v0:" + timestamp + ":" + body))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, slackapproval.InteractionsPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, req)
	return recorder.Code
}

func (f *clickFixture) status(t *testing.T) repo.RiskPolicyApproval {
	t.Helper()
	row, err := f.approvals.Get(t.Context(), f.organizationID, f.projectID, f.approvalID)
	require.NoError(t, err)
	return row
}

func TestClickApprovesForTheApproverItWasSentTo(t *testing.T) {
	t.Parallel()
	f := newClickFixture(t)

	code := f.send(t, clickPayload(f.message, "UAPPROVER", slackapproval.ActionApprove), testSigningSecret, time.Now())
	require.Equal(t, http.StatusOK, code)

	row := f.status(t)
	require.Equal(t, string(approval.StatusApproved), row.Status)
	require.Equal(t, string(approval.DecidedViaSlack), row.DecidedVia.String)
	require.Equal(t, userURN(f.approverID), row.DecidedByUrn.String)
	reply := f.responder.only(t)
	require.True(t, reply.ReplaceOriginal)
	require.Equal(t, "Approved: one call to slack__post_message", reply.Text)
	for _, block := range reply.Blocks {
		encoded, err := json.Marshal(block)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), `"type":"actions"`)
	}
}

func TestClickDenies(t *testing.T) {
	t.Parallel()
	f := newClickFixture(t)

	require.Equal(t, http.StatusOK, f.send(t, clickPayload(f.message, "UAPPROVER", slackapproval.ActionDeny), testSigningSecret, time.Now()))
	require.Equal(t, string(approval.StatusDenied), f.status(t).Status)
	require.Equal(t, "Denied: the call to slack__post_message", f.responder.only(t).Text)
}

func TestClickRejectsForgedAndStaleSignatures(t *testing.T) {
	t.Parallel()
	f := newClickFixture(t)
	payload := clickPayload(f.message, "UAPPROVER", slackapproval.ActionApprove)

	require.Equal(t, http.StatusUnauthorized, f.send(t, payload, "another-secret", time.Now()))
	require.Equal(t, http.StatusUnauthorized, f.send(t, payload, testSigningSecret, time.Now().Add(-6*time.Minute)))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, slackapproval.InteractionsPath, strings.NewReader("payload={}"))
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)

	require.Equal(t, string(approval.StatusPending), f.status(t).Status)
	require.Empty(t, f.responder.replies)
}

func TestClickWithoutASigningSecretIsRefused(t *testing.T) {
	t.Parallel()
	f := newClickFixture(t)
	f.handler = slackapproval.NewInteractions(testenv.NewLogger(t), f.cards, f.responder, "")

	// An empty secret would let anyone compute a valid signature.
	require.Equal(t, http.StatusNotFound, f.send(t, clickPayload(f.message, "UAPPROVER", slackapproval.ActionApprove), "", time.Now()))
	require.Equal(t, string(approval.StatusPending), f.status(t).Status)
}

func TestClickFromAnotherSlackUserIsRefused(t *testing.T) {
	t.Parallel()
	f := newClickFixture(t)

	require.Equal(t, http.StatusOK, f.send(t, clickPayload(f.message, "USOMEONEELSE", slackapproval.ActionApprove), testSigningSecret, time.Now()))
	require.Equal(t, string(approval.StatusPending), f.status(t).Status)
	reply := f.responder.only(t)
	require.Equal(t, "ephemeral", reply.ResponseType)
	require.False(t, reply.ReplaceOriginal)
	require.Contains(t, reply.Text, "Only the approver this message was sent to")
}

func TestClickByARemovedApproverIsRefused(t *testing.T) {
	t.Parallel()
	f := newClickFixture(t)
	someoneElse, _ := f.addMember(t, "New approver")
	f.setApprovers(t, f.policyID, userURN(someoneElse))

	require.Equal(t, http.StatusOK, f.send(t, clickPayload(f.message, "UAPPROVER", slackapproval.ActionApprove), testSigningSecret, time.Now()))
	require.Equal(t, string(approval.StatusPending), f.status(t).Status)
	require.Contains(t, f.responder.only(t).Text, "no longer an approver")
}

func TestClickByTheRequesterIsRefused(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	policy := f.addPolicy(t, userURN(f.approverID))
	// The approver made the call through their own session. They get no DM,
	// so plant one to prove the click path refuses them too.
	approvalID := f.pendingRequest(t, policy, f.approverID)
	message, err := repo.New(f.db).InsertApprovalMessage(t.Context(), repo.InsertApprovalMessageParams{
		OrganizationID:  f.organizationID,
		ProjectID:       f.projectID,
		ApprovalID:      approvalID,
		Provider:        "slack",
		Kind:            "dm",
		RecipientUserID: f.approverID,
		SlackTeamID:     conv.ToPGText(testTeamID),
		SlackUserID:     conv.ToPGText("UAPPROVER"),
		SlackChannelID:  conv.ToPGText("DUAPPROVER"),
		SlackMessageTs:  conv.ToPGText("1700000000.000001"),
	})
	require.NoError(t, err)
	responder := &fakeResponder{mu: sync.Mutex{}, replies: nil}
	click := &clickFixture{fixture: f, handler: slackapproval.NewInteractions(testenv.NewLogger(t), f.cards, responder, testSigningSecret), responder: responder, policyID: policy.ID, approvalID: approvalID, message: message}

	require.Equal(t, http.StatusOK, click.send(t, clickPayload(message, "UAPPROVER", slackapproval.ActionApprove), testSigningSecret, time.Now()))
	require.Equal(t, string(approval.StatusPending), click.status(t).Status)
	require.Contains(t, responder.only(t).Text, "You made this call")
}

func TestClickOnADecidedRequestShowsTheOutcome(t *testing.T) {
	t.Parallel()
	f := newClickFixture(t)
	reviewer, _ := f.addMember(t, "Reviewer")
	f.setApprovers(t, f.policyID, userURN(f.approverID), userURN(reviewer))
	_, err := f.approvals.Decide(t.Context(), approval.DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: f.approvalID, Approve: false, DeciderUserID: reviewer, DeciderDisplayName: nil, Via: approval.DecidedViaDashboard, Note: ""})
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, f.send(t, clickPayload(f.message, "UAPPROVER", slackapproval.ActionApprove), testSigningSecret, time.Now()))
	require.Equal(t, string(approval.StatusDenied), f.status(t).Status)
	reply := f.responder.only(t)
	require.True(t, reply.ReplaceOriginal)
	require.Equal(t, "Denied: the call to slack__post_message", reply.Text)
}

func TestClickOnAnUnknownMessageIsRefused(t *testing.T) {
	t.Parallel()
	f := newClickFixture(t)
	unknown := f.message
	unknown.SlackMessageTs = conv.ToPGText("1600000000.000001")

	require.Equal(t, http.StatusOK, f.send(t, clickPayload(unknown, "UAPPROVER", slackapproval.ActionApprove), testSigningSecret, time.Now()))
	require.Contains(t, f.responder.only(t).Text, "no longer available")
	require.Equal(t, string(approval.StatusPending), f.status(t).Status)
}

func TestClickOnTheLinkButtonIsIgnored(t *testing.T) {
	t.Parallel()
	f := newClickFixture(t)

	require.Equal(t, http.StatusOK, f.send(t, clickPayload(f.message, "UAPPROVER", "risk_approval_open"), testSigningSecret, time.Now()))
	require.Empty(t, f.responder.replies)
	require.Equal(t, string(approval.StatusPending), f.status(t).Status)
}

func TestHTTPResponderOnlyPostsToSlack(t *testing.T) {
	t.Parallel()
	responder := slackapproval.NewHTTPResponder(http.DefaultClient)
	message := slackapproval.ResponseMessage{ResponseType: "ephemeral", ReplaceOriginal: false, Text: "hello", Blocks: nil}
	for _, target := range []string{
		"http://hooks.slack.com/actions/T/1/x",
		"https://hooks.slack.com.example.com/actions/T/1/x",
		"https://example.com/actions/T/1/x",
		"not a url",
	} {
		require.Error(t, responder.Respond(t.Context(), target, message), target)
	}
}

// roundTripFunc answers requests without a network.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHTTPResponderPostsTheReplyToSlack(t *testing.T) {
	t.Parallel()
	var method, target, contentType, body string
	status := http.StatusOK
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		method, target, contentType, body = r.Method, r.URL.String(), r.Header.Get("Content-Type"), string(raw)
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("ok")), Header: http.Header{}, Request: r}, nil
	})}
	responder := slackapproval.NewHTTPResponder(client)
	responseURL := "https://hooks.slack.com/actions/T/1/x"
	message := slackapproval.ResponseMessage{ResponseType: "", ReplaceOriginal: true, Text: "Approved", Blocks: nil}

	require.NoError(t, responder.Respond(t.Context(), responseURL, message))
	require.Equal(t, http.MethodPost, method)
	require.Equal(t, responseURL, target)
	require.Equal(t, "application/json", contentType)
	require.JSONEq(t, `{"replace_original":true,"text":"Approved"}`, body)

	status = http.StatusNotFound
	require.Error(t, responder.Respond(t.Context(), responseURL, message), "a refused reply is an error")
}
