package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListUsergroupMembersTool_PostsToUsergroupsUsersList(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"users":[]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewListUsergroupMembersTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callListUsergroupMembers,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"usergroup":"S123",
		"include_disabled":true,
		"team_id":"T123"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/usergroups.users.list", requestPath)
	require.Equal(t, "S123", requestPayload.Get("usergroup"))
	require.Equal(t, "true", requestPayload.Get("include_disabled"))
	require.Equal(t, "T123", requestPayload.Get("team_id"))
}

func TestListUsergroupMembersTool_RequiresUsergroup(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewListUsergroupMembersTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callListUsergroupMembers,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), &bytes.Buffer{})
	require.Error(t, err)
	require.ErrorContains(t, err, "usergroup")
}
