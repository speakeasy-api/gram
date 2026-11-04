package slackapp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

const testSecret = "signing-secret"

type fakeRouter struct {
	workspaces []slackdirectoryconnections.RoutingWorkspace
	calls      int
}

func (r *fakeRouter) RoutingWorkspacesForTeam(context.Context, string) ([]slackdirectoryconnections.RoutingWorkspace, error) {
	r.calls++
	return r.workspaces, nil
}

type fakeDispatcher struct{ calls int }

func (d *fakeDispatcher) ProcessSpeakeasySlack(context.Context, triggerrepo.TriggerInstance, []byte, http.Header, string) (*triggers.WebhookIngressResult, error) {
	d.calls++
	return nil, nil
}

type postedCall struct {
	method  string
	payload map[string]any
	token   string
}

type fakePoster struct{ calls []postedCall }

func (p *fakePoster) CallWithToken(_ context.Context, method string, payload map[string]any, token string) ([]byte, error) {
	p.calls = append(p.calls, postedCall{method: method, payload: payload, token: token})
	return []byte(`{"ok":true}`), nil
}

func newTestEvents(t *testing.T, router *fakeRouter, dispatcher *fakeDispatcher, poster *fakePoster, mapped string) *Events {
	t.Helper()
	events := NewEvents(testenv.NewLogger(t), nil, router, dispatcher, poster, testSecret, nil)
	events.resolveSender = func(context.Context, slackrepo.ResolveSlackMappingUserParams) (string, error) {
		if mapped == "" {
			return "", pgx.ErrNoRows
		}
		return mapped, nil
	}
	return events
}

func signedRequest(t *testing.T, body string, secret string) *http.Request {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("v0:" + ts + ":" + body))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, EventsPath, strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	return req
}

const mentionBody = `{"type":"event_callback","team_id":"T1","event_id":"Ev1","event":{"type":"app_mention","user":"U1","channel":"C1","ts":"1.0","text":"<@B1> what MCPs are in my project?"}}`

func TestEventsRejectsBadSignature(t *testing.T) {
	t.Parallel()
	router := &fakeRouter{}
	events := newTestEvents(t, router, &fakeDispatcher{}, &fakePoster{}, "")
	rec := httptest.NewRecorder()
	events.ServeHTTP(rec, signedRequest(t, mentionBody, "wrong-secret"))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Zero(t, router.calls)
}

func TestEventsAnswersURLVerification(t *testing.T) {
	t.Parallel()
	events := newTestEvents(t, &fakeRouter{}, &fakeDispatcher{}, &fakePoster{}, "")
	rec := httptest.NewRecorder()
	events.ServeHTTP(rec, signedRequest(t, `{"type":"url_verification","challenge":"abc123"}`, testSecret))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "abc123", rec.Body.String())
}

func TestEventsIgnoresUnaddressedEvents(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"channel message": `{"type":"event_callback","team_id":"T1","event":{"type":"message","user":"U1","channel":"C1","ts":"1.0","text":"hi"}}`,
		"bot mention":     `{"type":"event_callback","team_id":"T1","event":{"type":"app_mention","user":"U1","bot_id":"B2","channel":"C1","ts":"1.0","text":"hi"}}`,
		"edited DM":       `{"type":"event_callback","team_id":"T1","event":{"type":"message","subtype":"message_changed","user":"U1","channel":"D1","ts":"1.0"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			router := &fakeRouter{}
			events := newTestEvents(t, router, &fakeDispatcher{}, &fakePoster{}, "")
			rec := httptest.NewRecorder()
			events.ServeHTTP(rec, signedRequest(t, body, testSecret))
			require.Equal(t, http.StatusOK, rec.Code)
			require.Zero(t, router.calls)
		})
	}
}

func TestEventsIgnoresUnroutedWorkspace(t *testing.T) {
	t.Parallel()
	router := &fakeRouter{}
	dispatcher := &fakeDispatcher{}
	poster := &fakePoster{}
	events := newTestEvents(t, router, dispatcher, poster, "user-1")
	rec := httptest.NewRecorder()
	events.ServeHTTP(rec, signedRequest(t, mentionBody, testSecret))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, router.calls)
	require.Zero(t, dispatcher.calls)
	require.Empty(t, poster.calls)
}

func TestEventsRefusesUnlinkedSender(t *testing.T) {
	t.Parallel()
	router := &fakeRouter{workspaces: []slackdirectoryconnections.RoutingWorkspace{{
		ConnectionID: uuid.New(), OrganizationID: "org-1", TeamID: "T1", ProjectID: uuid.New(), Token: "xoxb-test",
	}}}
	dispatcher := &fakeDispatcher{}
	poster := &fakePoster{}
	events := newTestEvents(t, router, dispatcher, poster, "")
	rec := httptest.NewRecorder()
	events.ServeHTTP(rec, signedRequest(t, mentionBody, testSecret))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, dispatcher.calls)
	require.Len(t, poster.calls, 1)
	require.Equal(t, "chat.postEphemeral", poster.calls[0].method)
	require.Equal(t, "xoxb-test", poster.calls[0].token)
	require.Equal(t, "U1", poster.calls[0].payload["user"])
	require.Equal(t, "C1", poster.calls[0].payload["channel"])
	require.Equal(t, UnlinkedReply, poster.calls[0].payload["text"])
}
