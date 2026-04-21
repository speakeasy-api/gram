package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchUsersTool_ListsAndFiltersByQuery(t *testing.T) {
	t.Parallel()

	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"members":[
			{"id":"U1","name":"alice","real_name":"Alice Example","profile":{"email":"alice@example.com"}},
			{"id":"U2","name":"bob","real_name":"Bob Other","profile":{"email":"bob@other.com"}}
		]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewSearchUsersTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callSearchUsers,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"query":"alice"}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/users.list", requestPath)
	require.Contains(t, out.String(), "alice@example.com")
	require.NotContains(t, out.String(), "bob@other.com")
}
