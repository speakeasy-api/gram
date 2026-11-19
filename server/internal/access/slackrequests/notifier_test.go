package slackrequests_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/access/slackrequests"
	"github.com/speakeasy-api/gram/server/internal/slackdm"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const testSigningSecret = "synthetic-signing-secret"

func TestNotifyDMsAdministratorsButNotTheRequester(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	target := f.pendingRequest(t)

	result, err := f.notifier.Notify(t.Context(), target)
	require.NoError(t, err)
	require.Equal(t, 1, result.Sent)
	require.False(t, result.ExpiresAt.IsZero())

	posts := f.slack.callsTo("chat.postMessage")
	require.Len(t, posts, 1)
	require.Equal(t, "UADMIN", posts[0].Form.Get("channel"), "the requester is never asked to decide their own request")
	require.Contains(t, posts[0].Form.Get("blocks"), `"static_select"`)
	require.Contains(t, posts[0].Form.Get("blocks"), "request="+target.RequestID.String())

	again, err := f.notifier.Notify(t.Context(), target)
	require.NoError(t, err)
	require.Equal(t, 1, again.Sent)
	require.Len(t, f.slack.callsTo("chat.postMessage"), 1, "a retried send does not DM twice")
}

func TestEndOfDayUsesTheRequestersSlackTimeZone(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	const zone = "Pacific/Kiritimati"
	f.slack.setZone("UREQUESTER", zone)
	loc, err := time.LoadLocation(zone)
	require.NoError(t, err)
	local := time.Now().In(loc)
	end := time.Date(local.Year(), local.Month(), local.Day(), 23, 59, 0, 0, loc)
	if time.Until(end) < time.Hour {
		t.Skip("too close to midnight in the requester's time zone for End of day to be offered")
	}
	target := f.pendingRequest(t)

	_, err = f.notifier.Notify(t.Context(), target)
	require.NoError(t, err)

	posts := f.slack.callsTo("chat.postMessage")
	require.Len(t, posts, 1)
	require.Contains(t, posts[0].Form.Get("blocks"), "eod|"+strconv.FormatInt(end.Unix(), 10), "End of day is 23:59 where the requester is")
}

func TestFinishUpdatesCardsAndTellsTheRequester(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	target := f.pendingRequest(t)
	_, err := f.notifier.Notify(t.Context(), target)
	require.NoError(t, err)

	require.NoError(t, f.notifier.Finish(t.Context(), target))
	require.Empty(t, f.slack.callsTo("chat.update"), "a pending request is left alone")

	f.decide(t, target, access.AccessRequestStatusDenied)
	require.NoError(t, f.notifier.Finish(t.Context(), target))
	updates := f.slack.callsTo("chat.update")
	require.Len(t, updates, 1)
	require.Contains(t, updates[0].Form.Get("text"), "Denied")
	posts := f.slack.callsTo("chat.postMessage")
	require.Len(t, posts, 2)
	require.Equal(t, "UREQUESTER", posts[1].Form.Get("channel"))
	require.Contains(t, posts[1].Form.Get("text"), "was denied")

	require.NoError(t, f.notifier.Finish(t.Context(), target))
	require.Len(t, f.slack.callsTo("chat.postMessage"), 2, "the requester is told once")
	require.Len(t, f.messages(t, target), 2)
}

// fakeDecider records decisions and stores them like access.Service.
type fakeDecider struct {
	f      *fixture
	t      *testing.T
	mu     sync.Mutex
	inputs []access.DecideAccessRequestInput
	err    error
}

func (d *fakeDecider) DecideAccessRequestAs(_ context.Context, input access.DecideAccessRequestInput) (*gen.AccessRequest, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inputs = append(d.inputs, input)
	if d.err != nil {
		return nil, d.err
	}
	status := access.AccessRequestStatusDenied
	if input.Approve {
		status = access.AccessRequestStatusApproved
	}
	d.f.decide(d.t, slackrequests.Target{OrganizationID: input.OrganizationID, RequestID: input.RequestID}, status)
	return &gen.AccessRequest{ID: input.RequestID.String(), Status: status}, nil
}

type fakeResponder struct {
	mu      sync.Mutex
	replies []slackdm.ResponseMessage
}

func (r *fakeResponder) Respond(_ context.Context, _ string, message slackdm.ResponseMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replies = append(r.replies, message)
	return nil
}

func (r *fakeResponder) only(t *testing.T) slackdm.ResponseMessage {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.Len(t, r.replies, 1)
	return r.replies[0]
}

type clickFixture struct {
	*fixture
	target    slackrequests.Target
	handler   *slackrequests.Interactions
	decider   *fakeDecider
	responder *fakeResponder
	channel   string
	ts        string
}

func newClickFixture(t *testing.T) *clickFixture {
	t.Helper()
	f := newFixture(t)
	target := f.pendingRequest(t)
	_, err := f.notifier.Notify(t.Context(), target)
	require.NoError(t, err)
	message := f.messages(t, target)[0]
	decider := &fakeDecider{f: f, t: t, mu: sync.Mutex{}, inputs: nil, err: nil}
	responder := &fakeResponder{mu: sync.Mutex{}, replies: nil}
	return &clickFixture{
		fixture:   f,
		target:    target,
		handler:   slackrequests.NewInteractions(testenv.NewLogger(t), f.cards, decider, responder, testSigningSecret),
		decider:   decider,
		responder: responder,
		channel:   message.SlackChannelID.String,
		ts:        message.SlackMessageTs.String,
	}
}

func (c *clickFixture) click(t *testing.T, slackUserID, actionID, value string) int {
	t.Helper()
	return c.clickWithWindow(t, slackUserID, actionID, value, "")
}

// clickWithWindow clicks with the time menu showing window; "" leaves the
// menu out of the click's state, as Slack does when nobody changed it.
func (c *clickFixture) clickWithWindow(t *testing.T, slackUserID, actionID, value, window string) int {
	t.Helper()
	state := map[string]any{"values": map[string]any{}}
	if window != "" {
		state = map[string]any{"values": map[string]any{"access_request": map[string]any{
			"access_request.window": map[string]any{"type": "static_select", "selected_option": map[string]any{"value": window}},
		}}}
	}
	payload, err := json.Marshal(map[string]any{
		"type":         "block_actions",
		"team":         map[string]any{"id": testTeamID},
		"user":         map[string]any{"id": slackUserID},
		"container":    map[string]any{"channel_id": c.channel, "message_ts": c.ts},
		"response_url": "https://hooks.slack.com/actions/T/1/x",
		"actions":      []map[string]any{{"action_id": actionID, "value": value}},
		"state":        state,
	})
	require.NoError(t, err)
	body := url.Values{"payload": {string(payload)}}.Encode()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(testSigningSecret))
	_, _ = mac.Write([]byte("v0:" + timestamp + ":" + body))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, slackdm.InteractionsPath, strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	recorder := httptest.NewRecorder()
	c.handler.ServeHTTP(recorder, req)
	return recorder.Code
}

func TestClickApprovesForTheAdministratorItWasSentTo(t *testing.T) {
	t.Parallel()
	c := newClickFixture(t)
	before := time.Now()

	require.Equal(t, http.StatusOK, c.clickWithWindow(t, "UADMIN", "access_request.allow", c.target.RequestID.String(), "2h"))
	require.Len(t, c.decider.inputs, 1)
	input := c.decider.inputs[0]
	require.True(t, input.Approve)
	require.Equal(t, c.adminID, input.DeciderUserID)
	require.Equal(t, access.AccessRequestDecidedViaSlack, input.Via)
	deadline, err := time.Parse(time.RFC3339, input.GrantExpiresAt)
	require.NoError(t, err)
	require.WithinDuration(t, before.Add(2*time.Hour), deadline, 5*time.Second)

	reply := c.responder.only(t)
	require.True(t, reply.ReplaceOriginal)
	require.Contains(t, reply.Text, "Approved")
}

func TestAllowWithAnUntouchedMenuGivesOneHour(t *testing.T) {
	t.Parallel()
	c := newClickFixture(t)
	before := time.Now()

	c.click(t, "UADMIN", "access_request.allow", c.target.RequestID.String())
	require.Len(t, c.decider.inputs, 1)
	deadline, err := time.Parse(time.RFC3339, c.decider.inputs[0].GrantExpiresAt)
	require.NoError(t, err)
	require.WithinDuration(t, before.Add(time.Hour), deadline, 5*time.Second)
}

func TestPickingATimeDecidesNothing(t *testing.T) {
	t.Parallel()
	c := newClickFixture(t)

	require.Equal(t, http.StatusOK, c.clickWithWindow(t, "UADMIN", "access_request.window", "", "4h"))
	require.Empty(t, c.decider.inputs)
	require.Empty(t, c.responder.replies)
}

func TestClickDenies(t *testing.T) {
	t.Parallel()
	c := newClickFixture(t)

	require.Equal(t, http.StatusOK, c.click(t, "UADMIN", "access_request.deny", c.target.RequestID.String()))
	require.False(t, c.decider.inputs[0].Approve)
	require.Contains(t, c.responder.only(t).Text, "Denied")
}

func TestClickEndOfDayUsesTheCardsDeadline(t *testing.T) {
	t.Parallel()
	c := newClickFixture(t)
	deadline := time.Now().Add(3 * time.Hour).Truncate(time.Second)

	c.clickWithWindow(t, "UADMIN", "access_request.allow", c.target.RequestID.String(), "eod|"+strconv.FormatInt(deadline.Unix(), 10))
	got, err := time.Parse(time.RFC3339, c.decider.inputs[0].GrantExpiresAt)
	require.NoError(t, err)
	require.True(t, deadline.Equal(got))
}

func TestClickEndOfDayAfterTheDayEndedIsRefused(t *testing.T) {
	t.Parallel()
	c := newClickFixture(t)

	c.clickWithWindow(t, "UADMIN", "access_request.allow", c.target.RequestID.String(), "eod|"+strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10))
	require.Empty(t, c.decider.inputs)
	reply := c.responder.only(t)
	require.Equal(t, "ephemeral", reply.ResponseType)
	require.Contains(t, reply.Text, "Pick a duration")
}

func TestClickFromAnotherSlackUserIsRefused(t *testing.T) {
	t.Parallel()
	c := newClickFixture(t)

	c.click(t, "UREQUESTER", "access_request.allow", c.target.RequestID.String())
	require.Empty(t, c.decider.inputs)
	require.Contains(t, c.responder.only(t).Text, "Only the administrator")
}

func TestClickByAFormerAdministratorIsRefused(t *testing.T) {
	t.Parallel()
	c := newClickFixture(t)
	c.decider.err = access.ErrAccessRequestNotAdmin

	c.click(t, "UADMIN", "access_request.allow", c.target.RequestID.String())
	reply := c.responder.only(t)
	require.Equal(t, "ephemeral", reply.ResponseType)
	require.Contains(t, reply.Text, "no longer an organization administrator")
}

func TestClickWithAForgedSignatureIsRejected(t *testing.T) {
	t.Parallel()
	c := newClickFixture(t)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, slackdm.InteractionsPath, strings.NewReader("payload=%7B%7D"))
	req.Header.Set("X-Slack-Request-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	req.Header.Set("X-Slack-Signature", "v0=bad")
	recorder := httptest.NewRecorder()
	c.handler.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Empty(t, c.decider.inputs)
}

func TestClickOnTheLinkButtonIsIgnored(t *testing.T) {
	t.Parallel()
	c := newClickFixture(t)

	require.Equal(t, http.StatusOK, c.click(t, "UADMIN", "access_request.open", ""))
	require.Empty(t, c.decider.inputs)
	require.Empty(t, c.responder.replies)
}
