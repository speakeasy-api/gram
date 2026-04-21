package slack

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/stretchr/testify/require"
)

func readForm(t *testing.T, r *http.Request) url.Values {
	t.Helper()
	bodyBytes, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	form, err := url.ParseQuery(string(bodyBytes))
	require.NoError(t, err)
	return form
}

func testSlackEnv() toolconfig.ToolCallEnv {
	return toolconfig.ToolCallEnv{
		UserConfig: toolconfig.CIEnvFrom(map[string]string{
			slackBotTokenEnvVar: "xoxb-test-token",
		}),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}
}

func TestSlackTool_MissingTokenReturnsHelpfulError(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewReadUserProfileTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callReadUserProfile,
	}

	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{"user_id":"U123"}`), io.Discard)
	require.Error(t, err)
	require.ErrorContains(t, err, slackBotTokenEnvVar)
	require.ErrorContains(t, err, slackTokenEnvVar)
}
