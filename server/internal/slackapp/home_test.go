package slackapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
)

func appHomeBody(tab string) string {
	return `{"type":"event_callback","team_id":"T1","event_id":"Ev2","event":{"type":"app_home_opened","user":"U1","channel":"D1","tab":"` + tab + `","event_ts":"1.0"}}`
}

func TestEventsPublishesAppHome(t *testing.T) {
	t.Parallel()
	router := &fakeRouter{workspaces: []slackdirectoryconnections.RoutingWorkspace{{
		ConnectionID: uuid.New(), OrganizationID: "org-1", TeamID: "T1", ProjectID: uuid.New(), Token: "xoxb-test",
	}}}
	dispatcher := &fakeDispatcher{}
	poster := &fakePoster{}
	events := newTestEvents(t, router, dispatcher, poster, "").WithDashboardURL("https://app.getgram.ai")

	rec := httptest.NewRecorder()
	events.ServeHTTP(rec, signedRequest(t, appHomeBody("home"), testSecret))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, dispatcher.calls)
	require.Len(t, poster.calls, 1)
	call := poster.calls[0]
	require.Equal(t, "views.publish", call.method)
	require.Equal(t, "xoxb-test", call.token)
	require.Equal(t, "U1", call.payload["user_id"])

	var view struct {
		Type   string `json:"type"`
		Blocks []struct {
			Type     string `json:"type"`
			Elements []struct {
				URL string `json:"url"`
			} `json:"elements"`
		} `json:"blocks"`
	}
	encoded, ok := call.payload["view"].(string)
	require.True(t, ok)
	require.NoError(t, json.Unmarshal([]byte(encoded), &view))
	require.Equal(t, "home", view.Type)
	require.Len(t, view.Blocks, 3)
	require.Equal(t, "https://app.getgram.ai", view.Blocks[2].Elements[0].URL)
}

func TestEventsSkipsAppHomeMessagesTab(t *testing.T) {
	t.Parallel()
	router := &fakeRouter{workspaces: []slackdirectoryconnections.RoutingWorkspace{{
		ConnectionID: uuid.New(), OrganizationID: "org-1", TeamID: "T1", ProjectID: uuid.New(), Token: "xoxb-test",
	}}}
	poster := &fakePoster{}
	events := newTestEvents(t, router, &fakeDispatcher{}, poster, "").WithDashboardURL("https://app.getgram.ai")

	rec := httptest.NewRecorder()
	events.ServeHTTP(rec, signedRequest(t, appHomeBody("messages"), testSecret))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, poster.calls)
	require.Zero(t, router.calls)
}
