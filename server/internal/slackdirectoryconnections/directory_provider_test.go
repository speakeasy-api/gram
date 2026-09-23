package slackdirectoryconnections_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
	"github.com/stretchr/testify/require"
)

func TestDirectoryProviderPaginationAndEligibility(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/users.list" || r.URL.Query().Get("limit") != "200" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			w.WriteHeader(400)
			return
		}
		if r.URL.Query().Get("cursor") == "" {
			_, _ = fmt.Fprint(w, `{"ok":true,"members":[
    {"id":"UEXAMPLE01","team_id":"TEXAMPLE01","deleted":false,"is_bot":false,"is_restricted":false,"profile":{"display_name":"First"}},
    {"id":"UEXTERNAL","team_id":"TOTHER","is_stranger":false},
    {"id":"USTRANGER","team_id":"TEXAMPLE01","is_stranger":true},
    {"id":"UGRID","enterprise_user":{"teams":["TEXAMPLE01"]}},
    {"id":"UAMBIGUOUS","team_id":"TEXAMPLE01","profile":{"team":"TOTHER"}}
   ],"response_metadata":{"next_cursor":"page-two"}}`)
		} else if r.URL.Query().Get("cursor") == "page-two" {
			_, _ = fmt.Fprint(w, `{"ok":true,"members":[
    {"id":"UEXAMPLE01","team_id":"TEXAMPLE01","deleted":false,"is_bot":false,"is_restricted":false,"is_email_confirmed":false,"profile":{"display_name":"Latest","email":"unconfirmed@example.com"}},
    {"id":"USLACKBOT","team_id":"TEXAMPLE01","is_bot":false,"is_restricted":false},
    {"id":"UINVITED","team_id":"TEXAMPLE01","deleted":false,"is_invited_user":true},
    {"id":"UGUEST","team_id":"TEXAMPLE01","deleted":false,"is_ultra_restricted":true},
    {"id":"UDELETED","team_id":"TEXAMPLE01","deleted":true},
    {"id":"UUNKNOWN","team_id":"TEXAMPLE01"}
   ],"response_metadata":{"next_cursor":""}}`)
		} else {
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	provider := slackdirectoryconnections.NewDirectoryProvider(slackapi.NewClient(server.URL, server.Client()))
	var progress slackdirectoryconnections.SyncProgress
	rows, err := provider.Fetch(t.Context(), "synthetic-token", "TEXAMPLE01", func(p slackdirectoryconnections.SyncProgress) { progress = p })
	require.NoError(t, err)
	require.Equal(t, int32(2), requests.Load())
	require.Len(t, rows, 6)
	require.Equal(t, "Latest", rows[0].DisplayName)
	require.Empty(t, rows[0].Email)
	require.Equal(t, "bot", rows[1].MemberType)
	require.Equal(t, "invited", rows[2].Status)
	require.Equal(t, "single_channel_guest", rows[3].MemberType)
	require.Equal(t, "deactivated", rows[4].Status)
	require.Equal(t, "unknown", rows[5].Status)
	require.Equal(t, "unknown", rows[5].MemberType)
	require.Equal(t, 4, progress.ExcludedExternal)
	require.Equal(t, 1, progress.Bots)
}

func TestDirectoryProviderPartialFailureReturnsNoMembers(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			_, _ = fmt.Fprint(w, `{"ok":true,"members":[{"id":"UEXAMPLE01","team_id":"TEXAMPLE01"}],"response_metadata":{"next_cursor":"next"}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":false,"error":"SENSITIVE_PROVIDER_TEXT"}`)
	}))
	defer server.Close()
	provider := slackdirectoryconnections.NewDirectoryProvider(slackapi.NewClient(server.URL, server.Client()))
	rows, err := provider.Fetch(t.Context(), "synthetic-token", "TEXAMPLE01", nil)
	require.Error(t, err)
	require.Nil(t, rows)
	require.NotContains(t, err.Error(), "SENSITIVE")
}

func TestDirectoryProviderRejectsIncompleteEnvelopeAndCursorLoop(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = fmt.Fprint(w, `{"ok":true}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"members":[],"response_metadata":{"next_cursor":"loop"}}`)
	}))
	defer server.Close()
	provider := slackdirectoryconnections.NewDirectoryProvider(slackapi.NewClient(server.URL, server.Client()))
	rows, err := provider.Fetch(t.Context(), "synthetic-token", "TEXAMPLE01", nil)
	require.Error(t, err)
	require.Nil(t, rows)
	rows, err = provider.Fetch(t.Context(), "synthetic-token", "TEXAMPLE01", nil)
	require.Error(t, err)
	require.Nil(t, rows)
	require.Equal(t, int32(3), requests.Load())
}

func TestDirectoryProviderRateLimitHonorsRetryAfter(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	var first time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			first = time.Now()
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"members":[],"response_metadata":{"next_cursor":""}}`)
	}))
	defer server.Close()
	provider := slackdirectoryconnections.NewDirectoryProvider(slackapi.NewClient(server.URL, server.Client()))
	rows, err := provider.Fetch(t.Context(), "synthetic-token", "TEXAMPLE01", nil)
	require.NoError(t, err)
	require.Empty(t, rows)
	require.Equal(t, int32(2), requests.Load())
	require.GreaterOrEqual(t, time.Since(first), time.Second)
}

func TestDirectoryProviderRateLimitWaitCancels(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "120"); w.WriteHeader(429) }))
	defer server.Close()
	provider := slackdirectoryconnections.NewDirectoryProvider(slackapi.NewClient(server.URL, server.Client()))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rows, err := provider.Fetch(ctx, "synthetic-token", "TEXAMPLE01", func(p slackdirectoryconnections.SyncProgress) {
		if p.Phase == "waiting_for_slack" {
			cancel()
		}
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, rows)
}
