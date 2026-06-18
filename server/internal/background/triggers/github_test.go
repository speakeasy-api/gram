package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGitHubConfigSchemaConstrainsEventTypeItems(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("github")
	require.True(t, ok)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(definition.ConfigSchema, &schema))

	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)

	eventTypes, ok := properties["event_types"].(map[string]any)
	require.True(t, ok)

	items, ok := eventTypes["items"].(map[string]any)
	require.True(t, ok)

	enumValues, ok := items["enum"].([]any)
	require.True(t, ok)
	require.Len(t, enumValues, len(supportedGitHubEventTypes))
	for _, evt := range supportedGitHubEventTypes {
		require.Contains(t, enumValues, evt)
	}
}

func TestGitHubDecodeConfigRejectsUnsupportedEventType(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("github")
	require.True(t, ok)

	_, err := definition.DecodeConfig(map[string]any{
		"event_types": []any{"bogus"},
	})
	require.Error(t, err)
}

func TestGitHubSignatureVerification(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("github")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	body := []byte(`{"action":"opened","repository":{"full_name":"octocat/Hello-World"}}`)
	mac := hmac.New(sha256.New, []byte("shh"))
	mac.Write(body)
	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	headers.Set("X-GitHub-Event", "pull_request")
	headers.Set("X-GitHub-Delivery", "del-1")

	require.NoError(t, definition.AuthenticateWebhook(body, headers, map[string]string{
		"GITHUB_WEBHOOK_SECRET": "shh",
	}, config))
}

func TestGitHubSignatureRejectsBadSecret(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("github")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	body := []byte(`{}`)
	mac := hmac.New(sha256.New, []byte("correct"))
	mac.Write(body)
	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))

	err = definition.AuthenticateWebhook(body, headers, map[string]string{
		"GITHUB_WEBHOOK_SECRET": "wrong",
	}, config)
	require.Error(t, err)
}

func TestGitHubIngestPullRequest(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("github")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	body := []byte(`{"action":"opened","number":42,"pull_request":{"number":42},"repository":{"full_name":"octocat/Hello-World"}}`)
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "pull_request")
	headers.Set("X-GitHub-Delivery", "del-pr-1")

	result, err := definition.HandleWebhook(body, headers, config)
	require.NoError(t, err)
	require.NotNil(t, result.Event)

	require.Equal(t, "del-pr-1", result.Event.EventID)
	require.Equal(t, "github:octocat/Hello-World/pr:42", result.Event.CorrelationID)

	event, ok := result.Event.Event.(githubTriggerEvent)
	require.True(t, ok)
	require.Equal(t, "pull_request", event.EventType)
	require.Equal(t, "opened", event.Action)
	require.Equal(t, "octocat/Hello-World", event.Repo)
	require.Equal(t, 42, event.Number)
}

func TestGitHubIngestPullRequestReviewFoldsOntoPR(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("github")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	// A review event carries the PR number under pull_request; correlation
	// must route to the PR's conversation so the assistant sees the whole
	// review thread as one context.
	body := []byte(`{"action":"submitted","pull_request":{"number":42},"repository":{"full_name":"octocat/Hello-World"}}`)
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "pull_request_review")
	headers.Set("X-GitHub-Delivery", "del-review-1")

	result, err := definition.HandleWebhook(body, headers, config)
	require.NoError(t, err)
	require.NotNil(t, result.Event)
	require.Equal(t, "github:octocat/Hello-World/pr:42", result.Event.CorrelationID)
}

func TestGitHubIngestIssueCommentFoldsOntoIssue(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("github")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	body := []byte(`{"action":"created","issue":{"number":7},"repository":{"full_name":"octocat/Hello-World"}}`)
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "issue_comment")
	headers.Set("X-GitHub-Delivery", "del-comment-1")

	result, err := definition.HandleWebhook(body, headers, config)
	require.NoError(t, err)
	require.NotNil(t, result.Event)
	require.Equal(t, "github:octocat/Hello-World/issue:7", result.Event.CorrelationID)
}

func TestGitHubIngestPushKeysOnBranch(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("github")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	body := []byte(`{"ref":"refs/heads/main","repository":{"full_name":"octocat/Hello-World"}}`)
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "push")
	headers.Set("X-GitHub-Delivery", "del-push-1")

	result, err := definition.HandleWebhook(body, headers, config)
	require.NoError(t, err)
	require.NotNil(t, result.Event)
	require.Equal(t, "github:octocat/Hello-World/branch:main", result.Event.CorrelationID)

	event, ok := result.Event.Event.(githubTriggerEvent)
	require.True(t, ok)
	require.Equal(t, "main", event.Ref)
}

func TestGitHubIngestDefaultFallsBackToRepo(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("github")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	body := []byte(`{"action":"created","repository":{"full_name":"octocat/Hello-World"}}`)
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "star")
	headers.Set("X-GitHub-Delivery", "del-star-1")

	result, err := definition.HandleWebhook(body, headers, config)
	require.NoError(t, err)
	require.NotNil(t, result.Event)
	require.Equal(t, "github:octocat/Hello-World", result.Event.CorrelationID)
}

func TestGitHubIngestRejectsMissingEventHeader(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("github")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	_, err = definition.HandleWebhook([]byte(`{}`), http.Header{}, config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing X-GitHub-Event")
}

func TestGitHubFilterDefaultDeny(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("github")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	match, err := config.Filter(githubTriggerEvent{EventType: "push"})
	require.NoError(t, err)
	require.True(t, match)

	match, err = config.Filter(githubTriggerEvent{EventType: "bogus"})
	require.NoError(t, err)
	require.False(t, match)
}

func TestGitHubFilterCEL(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("github")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"filter": `event.action == "opened"`,
	})
	require.NoError(t, err)

	match, err := config.Filter(githubTriggerEvent{EventType: "pull_request", Action: "opened"})
	require.NoError(t, err)
	require.True(t, match)

	match, err = config.Filter(githubTriggerEvent{EventType: "pull_request", Action: "closed"})
	require.NoError(t, err)
	require.False(t, match)
}
