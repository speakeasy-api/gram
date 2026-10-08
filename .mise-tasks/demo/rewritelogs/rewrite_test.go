package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRewriteDemoLogsCopiesSeededEmail(t *testing.T) {
	t.Parallel()

	payload := decodePayloadForTest(t, `{
		"resourceLogs": [{
			"resource": {"attributes": [
				{"key": "gram.demo.user_email", "value": {"stringValue": "ada@local.getgram.ai"}},
				{"key": "service.name", "value": {"stringValue": "claude-code"}}
			]},
			"scopeLogs": [{
				"scope": {"name": "com.anthropic.claude_code.events", "version": "1"},
				"logRecords": [{
					"timeUnixNano": 1700000000000000001,
					"body": {"stringValue": "hello"},
					"attributes": [
						{"key": "user.email", "value": {"stringValue": "real@keyboard.example"}},
						{"key": "session.id", "value": {"stringValue": "s1"}}
					]
				}]
			}, {
				"scope": {"name": "codex.tracing"},
				"logRecords": [{"body": {"stringValue": "internal"}}]
			}]
		}, {
			"resource": {"attributes": []},
			"scopeLogs": [{
				"scope": {"name": "com.anthropic.claude_code"},
				"logRecords": [{"body": {"stringValue": "unmarked"}}]
			}]
		}]
	}`)

	rewritten, kept := rewriteDemoLogs(payload)
	require.Equal(t, 1, kept)

	resources := rewritten["resourceLogs"].([]any)
	require.Len(t, resources, 1)
	scopes := resources[0].(map[string]any)["scopeLogs"].([]any)
	require.Len(t, scopes, 1)
	records := scopes[0].(map[string]any)["logRecords"].([]any)
	require.Len(t, records, 1)
	record := records[0].(map[string]any)
	require.Equal(t, json.Number("1700000000000000001"), record["timeUnixNano"])
	require.Equal(t, map[string]any{"stringValue": "hello"}, record["body"])

	emailCount := 0
	for _, raw := range record["attributes"].([]any) {
		if raw.(map[string]any)["key"] == "user.email" {
			emailCount++
		}
	}
	require.Equal(t, 1, emailCount)
	email, ok := attrString(record["attributes"].([]any), "user.email")
	require.True(t, ok)
	require.Equal(t, "ada@local.getgram.ai", email)
	session, ok := attrString(record["attributes"].([]any), "session.id")
	require.True(t, ok)
	require.Equal(t, "s1", session)
}

func TestRewriteDemoLogsKeepsCodexAgentScope(t *testing.T) {
	t.Parallel()

	payload := decodePayloadForTest(t, `{
		"resourceLogs": [{
			"resource": {"attributes": [
				{"key": "gram.demo.user_email", "value": {"string_value": "grace@local.getgram.ai"}}
			]},
			"scopeLogs": [{
				"scope": {"name": "codex_otel.log_only"},
				"logRecords": [{"attributes": []}]
			}]
		}]
	}`)

	_, kept := rewriteDemoLogs(payload)
	require.Equal(t, 1, kept)
	resources := payload["resourceLogs"].([]any)
	records := resources[0].(map[string]any)["scopeLogs"].([]any)[0].(map[string]any)["logRecords"].([]any)
	email, ok := attrString(records[0].(map[string]any)["attributes"].([]any), "user.email")
	require.True(t, ok)
	require.Equal(t, "grace@local.getgram.ai", email)
}

func TestRewriteHandlerForwardsRewrittenExport(t *testing.T) {
	t.Parallel()

	var gotBody []byte
	var gotKey, gotProject string
	var readErr error
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Gram-Key")
		gotProject = r.Header.Get("Gram-Project")
		gotBody, readErr = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"partialSuccess":{}}`))
	}))
	t.Cleanup(upstream.Close)

	handler := newRewriteHandler(upstream.URL, upstream.Client())
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	_, err := gz.Write([]byte(`{
		"resourceLogs": [{
			"resource": {"attributes": [
				{"key": "gram.demo.user_email", "value": {"stringValue": "ada@local.getgram.ai"}}
			]},
			"scopeLogs": [{
				"scope": {"name": "com.anthropic.claude_code"},
				"logRecords": [{"attributes": [{"key": "session.id", "value": {"stringValue": "s1"}}]}]
			}]
		}]
	}`))
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", &compressed)
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Gram-Key", "gram_local_test")
	req.Header.Set("Gram-Project", "default")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, readErr)
	require.Equal(t, "gram_local_test", gotKey)
	require.Equal(t, "default", gotProject)
	forwarded := decodePayloadForTest(t, string(gotBody))
	resources := forwarded["resourceLogs"].([]any)
	records := resources[0].(map[string]any)["scopeLogs"].([]any)[0].(map[string]any)["logRecords"].([]any)
	email, ok := attrString(records[0].(map[string]any)["attributes"].([]any), "user.email")
	require.True(t, ok)
	require.Equal(t, "ada@local.getgram.ai", email)
}

func TestRewriteHandlerDropsUnmarkedExport(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unmarked export was forwarded")
	}))
	t.Cleanup(upstream.Close)

	handler := newRewriteHandler(upstream.URL, upstream.Client())
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewBufferString(`{"resourceLogs":[]}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"partialSuccess":{}}`, rec.Body.String())
}

func decodePayloadForTest(t *testing.T, raw string) map[string]any {
	t.Helper()
	payload, err := decodePayload([]byte(raw))
	require.NoError(t, err)
	return payload
}
