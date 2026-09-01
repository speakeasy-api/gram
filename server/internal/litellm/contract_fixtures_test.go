package litellm

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/hooks"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const contractFixtureDir = "fixtures/litellm-v1.94.0/"

type contractFixtureCase struct {
	name               string
	file               string
	requestText        string
	responseText       string
	sessionID          string
	model              string
	sourceEmail        string
	userIDAttribute    string
	endUserIDAttribute string
	requestHasTools    bool
	requestHasHistory  bool
	responseHasTools   bool
	actor              hooks.ResolvedActor
	maxLines           int
	blockedReason      string
}

type contractFixtureManifest struct {
	Files     map[string]string `json:"files"`
	OTELFiles map[string]string `json:"otel_files"`
}

func readJSONLines(t *testing.T, file string) [][]byte {
	t.Helper()
	raw := testenv.ReadFixture(t, contractFixtureDir+file)
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	lines := make([][]byte, 0, 2)
	for scanner.Scan() {
		lines = append(lines, bytes.Clone(scanner.Bytes()))
	}
	require.NoError(t, scanner.Err())
	require.NotEmpty(t, lines)
	return lines
}

func postContractFixture(t *testing.T, client *http.Client, url string, raw []byte) (map[string]any, map[string]any) {
	t.Helper()
	var callback map[string]any
	require.NoError(t, json.Unmarshal(raw, &callback))

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/rpc/litellm.ingest/beta/litellm_basic_guardrail_api", bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gram-Key", "fixture-gram-key")
	req.Header.Set("Gram-Project", "fixture-project")
	response, err := client.Do(req)
	require.NoError(t, err)
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode, string(responseBody))
	var result map[string]any
	require.NoError(t, json.Unmarshal(responseBody, &result))
	return callback, result
}

func requireRecordedCallbackShape(t *testing.T, name string, callback map[string]any) {
	t.Helper()
	for _, key := range []string{
		"input_type", "litellm_call_id", "litellm_trace_id", "structured_messages", "images", "tools", "texts",
		"request_data", "request_headers", "litellm_version", "additional_provider_specific_params", "tool_calls", "model",
	} {
		require.Contains(t, callback, key, "%s missing %s", name, key)
	}
	require.IsType(t, "", callback["input_type"], name)
	require.IsType(t, "", callback["litellm_call_id"], name)
	require.IsType(t, "", callback["litellm_trace_id"], name)
	require.IsType(t, map[string]any{}, callback["request_data"], name)
	require.Equal(t, "1.94.0", callback["litellm_version"], name)
	require.IsType(t, map[string]any{}, callback["additional_provider_specific_params"], name)
	for _, key := range []string{"structured_messages", "images", "tools", "texts", "tool_calls"} {
		if callback[key] != nil {
			require.IsType(t, []any{}, callback[key], "%s field %s", name, key)
		}
	}
	if callback["request_headers"] != nil {
		require.IsType(t, map[string]any{}, callback["request_headers"], name)
	}
}

func TestContractFixtureManifest(t *testing.T) {
	t.Parallel()
	var manifest contractFixtureManifest
	require.NoError(t, json.Unmarshal(testenv.ReadFixture(t, contractFixtureDir+"manifest.json"), &manifest))
	require.NotEmpty(t, manifest.Files)
	require.NotEmpty(t, manifest.OTELFiles)

	entries, err := os.ReadDir(contractFixtureDir)
	require.NoError(t, err)
	fixtureFiles := make([]string, 0, len(manifest.Files))
	otelFixtureFiles := make([]string, 0, len(manifest.OTELFiles))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".jsonl") {
			fixtureFiles = append(fixtureFiles, entry.Name())
		}
		if strings.HasPrefix(entry.Name(), "otlp-") && (strings.HasSuffix(entry.Name(), ".json") || strings.HasSuffix(entry.Name(), ".pb")) {
			otelFixtureFiles = append(otelFixtureFiles, entry.Name())
		}
	}
	manifestFiles := make([]string, 0, len(manifest.Files))
	for filename, expected := range manifest.Files {
		manifestFiles = append(manifestFiles, filename)
		sum := sha256.Sum256(testenv.ReadFixture(t, contractFixtureDir+filename))
		require.Equal(t, expected, fmt.Sprintf("%x", sum), filename)
	}
	require.ElementsMatch(t, manifestFiles, fixtureFiles)
	manifestOTELFiles := make([]string, 0, len(manifest.OTELFiles))
	for filename, expected := range manifest.OTELFiles {
		manifestOTELFiles = append(manifestOTELFiles, filename)
		sum := sha256.Sum256(testenv.ReadFixture(t, contractFixtureDir+filename))
		require.Equal(t, expected, fmt.Sprintf("%x", sum), filename)
	}
	require.ElementsMatch(t, manifestOTELFiles, otelFixtureFiles)
}

func TestContractFixtures(t *testing.T) {
	t.Parallel()
	cases := []contractFixtureCase{
		{
			name: "openai chat email identity and tools", file: "openai-chat-tools.jsonl",
			requestText: "latest chat block one\nlatest chat block two", responseText: "chat answer",
			sessionID: "fixture-chat-session", model: "fixture-openai", sourceEmail: "fixture-user@example.test",
			userIDAttribute: "fixture-email-user", requestHasTools: true, requestHasHistory: true, responseHasTools: true,
			actor: hooks.ResolvedActor{UserID: "fixture-resolved-user", Email: "fixture-user@example.test"},
		},
		{
			name: "openai responses email-less identity and tools", file: "openai-responses-tools.jsonl",
			requestText: "latest responses prompt", responseText: "responses answer",
			sessionID: "fixture-responses-session", model: "fixture-responses", userIDAttribute: "fixture-email-less-user",
			requestHasTools: true, requestHasHistory: true, responseHasTools: true,
			actor: hooks.ResolvedActor{UserID: "", Email: ""},
		},
		{
			name: "anthropic messages master identity and tools", file: "anthropic-messages-tools.jsonl",
			requestText: "latest anthropic prompt", responseText: "anthropic answer",
			sessionID: "fixture-anthropic-session", model: "fixture-anthropic", userIDAttribute: "default_user_id",
			requestHasTools: true, requestHasHistory: true, responseHasTools: true,
			actor: hooks.ResolvedActor{UserID: "", Email: ""},
		},
		{
			name: "pass-through text fallback", file: "passthrough-text.jsonl",
			requestText: "pass-through prompt", sessionID: "fixture-passthrough-text-trace", model: "",
			actor: hooks.ResolvedActor{UserID: "", Email: ""},
		},
		{
			name: "streaming end of stream", file: "streaming-chat.jsonl",
			requestText: "streaming prompt", responseText: "streamed answer",
			sessionID: "fixture-stream-session", model: "fixture-openai", sourceEmail: "fixture-user@example.test",
			userIDAttribute: "fixture-email-user",
			actor:           hooks.ResolvedActor{UserID: "fixture-resolved-user", Email: "fixture-user@example.test"},
		},
		{
			name: "end-user id is inert", file: "end-user-identity.jsonl",
			requestText: "end-user prompt", responseText: "chat answer",
			sessionID: "fixture-end-user-identity-trace", model: "fixture-openai",
			userIDAttribute: "fixture-email-less-user", endUserIDAttribute: "fixture-end-user-id", responseHasTools: true,
			actor: hooks.ResolvedActor{UserID: "", Email: ""},
		},
		{
			name: "shared virtual key identity", file: "shared-key-identity.jsonl",
			requestText: "shared-key prompt", responseText: "chat answer",
			sessionID: "fixture-shared-key-identity-trace", model: "fixture-openai", responseHasTools: true,
			actor: hooks.ResolvedActor{UserID: "", Email: ""},
		},
		{
			name: "blocked response contract", file: "openai-chat-tools.jsonl",
			requestText: "latest chat block one\nlatest chat block two", sessionID: "fixture-chat-session", model: "fixture-openai",
			sourceEmail: "fixture-user@example.test", userIDAttribute: "fixture-email-user",
			requestHasTools: true, requestHasHistory: true, actor: hooks.ResolvedActor{UserID: "", Email: ""},
			maxLines: 1, blockedReason: "fixture policy block",
		},
	}

	for _, tc := range cases {
		authCtx := testAuthContext()
		result := allowResult(tc.actor)
		if tc.blockedReason != "" {
			result.Result.Decision = "deny"
			result.Result.Message = &tc.blockedReason
		}
		ingester := &captureIngester{result: result, err: nil, calls: nil}
		service := unitService(t, ingester, authCtx)
		mux := goahttp.NewMuxer()
		Attach(mux, service)
		server := httptest.NewServer(mux)

		lines := readJSONLines(t, tc.file)
		if tc.maxLines > 0 {
			lines = lines[:tc.maxLines]
		}
		for index, raw := range lines {
			callback, response := postContractFixture(t, server.Client(), server.URL, raw)
			requireRecordedCallbackShape(t, tc.name, callback)
			inputType, ok := callback["input_type"].(string)
			require.True(t, ok, tc.name)
			if tc.blockedReason == "" {
				require.Equal(t, map[string]any{"action": "NONE"}, response, tc.name)
			} else {
				require.Equal(t, map[string]any{"action": "BLOCKED", "blocked_reason": tc.blockedReason}, response, tc.name)
			}
			action, ok := response["action"].(string)
			require.True(t, ok, tc.name)
			require.Contains(t, []string{"BLOCKED", "NONE"}, action, tc.name)
			require.Equal(t, tc.blockedReason != "", response["blocked_reason"] != nil, tc.name)

			require.Len(t, ingester.calls, index+1, tc.name)
			call := ingester.calls[index]
			require.Nil(t, call.payload.Data.ToolCall, tc.name)
			require.Nil(t, call.payload.Raw, tc.name)
			require.Equal(t, "litellm", call.payload.Source.Adapter, tc.name)
			require.Equal(t, "1.94.0", *call.payload.Source.AdapterVersion, tc.name)
			require.Nil(t, call.payload.Source.UserEmail, tc.name)
			require.Equal(t, tc.sessionID, *call.payload.Session.ID, tc.name)
			require.Equal(t, callback["litellm_call_id"], *call.payload.Session.TurnID, tc.name)
			if tc.model == "" {
				require.Nil(t, call.payload.Session.Model, tc.name)
			} else {
				require.Equal(t, tc.model, *call.payload.Session.Model, tc.name)
			}
			require.NotEqual(t, authCtx.UserID, call.auth.UserID, tc.name)
			if call.auth.Email != nil {
				require.NotEqual(t, *authCtx.Email, *call.auth.Email, tc.name)
			}
			require.Empty(t, call.auth.ExternalUserID, tc.name)
			require.False(t, call.auth.OrgWidePluginHooksKey, tc.name)
			require.False(t, call.options.AllowSessionIdentityFallback, tc.name)
			require.Equal(t, callback["litellm_call_id"], call.options.SourceAttributes[attr.LiteLLMCallIDKey], tc.name)
			require.Equal(t, callback["litellm_trace_id"], call.options.SourceAttributes[attr.LiteLLMTraceIDKey], tc.name)
			if tc.userIDAttribute == "" {
				require.NotContains(t, call.options.SourceAttributes, attr.LiteLLMUserIDKey, tc.name)
			} else {
				require.Equal(t, tc.userIDAttribute, call.options.SourceAttributes[attr.LiteLLMUserIDKey], tc.name)
			}
			if tc.endUserIDAttribute == "" {
				require.NotContains(t, call.options.SourceAttributes, attr.LiteLLMEndUserIDKey, tc.name)
			} else {
				require.Equal(t, tc.endUserIDAttribute, call.options.SourceAttributes[attr.LiteLLMEndUserIDKey], tc.name)
				require.Empty(t, call.auth.UserID, tc.name)
				require.Nil(t, call.auth.Email, tc.name)
			}

			switch inputType {
			case "request":
				require.Equal(t, "prompt.submitted", call.payload.Event.Type, tc.name)
				require.Equal(t, tc.requestText, *call.payload.Data.Prompt.Text, tc.name)
				require.Nil(t, call.payload.Data.Message, tc.name)
				require.Nil(t, call.options.OutputToolCalls, tc.name)
				if tc.requestHasTools {
					require.NotEmpty(t, callback["tools"], tc.name)
				}
				if tc.requestHasHistory {
					hasHistoricalToolCall := false
					messages, ok := callback["structured_messages"].([]any)
					require.True(t, ok, tc.name)
					for _, message := range messages {
						structured, ok := message.(map[string]any)
						if ok && structured["role"] == "assistant" && structured["tool_calls"] != nil {
							hasHistoricalToolCall = true
						}
					}
					require.True(t, hasHistoricalToolCall, tc.name)
				}
			case "response":
				require.Equal(t, "assistant.responded", call.payload.Event.Type, tc.name)
				require.Equal(t, tc.responseText, *call.payload.Data.Message.Text, tc.name)
				require.Equal(t, "assistant", *call.payload.Data.Message.Role, tc.name)
				require.Nil(t, call.payload.Data.Prompt, tc.name)
				if tc.responseHasTools {
					require.Equal(t, callback["tool_calls"], call.options.OutputToolCalls, tc.name)
				} else {
					require.Empty(t, call.options.OutputToolCalls, tc.name)
				}
			default:
				require.Fail(t, fmt.Sprintf("%s: unexpected input type", tc.name), inputType)
			}
		}
		server.Close()
	}
}
