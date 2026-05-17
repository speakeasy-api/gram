package slack

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUploadFileTool_RunsThreeStepFlow(t *testing.T) {
	t.Parallel()

	fileBytes := []byte("hello slack from gram")
	fileBase64 := base64.StdEncoding.EncodeToString(fileBytes)

	var (
		getURLForm   url.Values
		uploadedBody []byte
		uploadedCT   string
		uploadedAuth string
		completeForm url.Values
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/files.getUploadURLExternal", func(w http.ResponseWriter, r *http.Request) {
		getURLForm = readForm(t, r)
		w.Header().Set("Content-Type", "application/json")
		uploadURL := "http://" + r.Host + "/upload/v1/ABC"
		resp := map[string]any{
			"ok":         true,
			"upload_url": uploadURL,
			"file_id":    "F123",
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encode response: %v", err)
		}
	})
	mux.HandleFunc("/upload/v1/ABC", func(w http.ResponseWriter, r *http.Request) {
		uploadedCT = r.Header.Get("Content-Type")
		uploadedAuth = r.Header.Get("Authorization")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upload body: %v", err)
			return
		}
		uploadedBody = body
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK - " + http.StatusText(http.StatusOK)))
	})
	mux.HandleFunc("/files.completeUploadExternal", func(w http.ResponseWriter, r *http.Request) {
		completeForm = readForm(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"files":[{"id":"F123","title":"my title"}]}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	tool := &slackTool{
		descriptor: NewUploadFileTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callUploadFile,
	}

	input := map[string]any{
		"filename":        "notes.txt",
		"content_base64":  fileBase64,
		"title":           "my title",
		"alt_text":        "screen reader text",
		"snippet_type":    "text",
		"channel_id":      "C1,C2",
		"initial_comment": "here you go",
		"thread_ts":       "1700000000.000100",
	}
	payload, err := json.Marshal(input)
	require.NoError(t, err)

	var out bytes.Buffer
	err = tool.Call(t.Context(), testSlackEnv(), bytes.NewReader(payload), &out)
	require.NoError(t, err)

	require.Equal(t, "notes.txt", getURLForm.Get("filename"))
	require.Equal(t, "21", getURLForm.Get("length"))
	require.Equal(t, "screen reader text", getURLForm.Get("alt_txt"))
	require.Equal(t, "text", getURLForm.Get("snippet_type"))

	require.Equal(t, "application/octet-stream", uploadedCT)
	require.Empty(t, uploadedAuth)
	require.Equal(t, fileBytes, uploadedBody)

	require.Equal(t, "C1,C2", completeForm.Get("channel_id"))
	require.Equal(t, "here you go", completeForm.Get("initial_comment"))
	require.Equal(t, "1700000000.000100", completeForm.Get("thread_ts"))

	var files []map[string]any
	require.NoError(t, json.Unmarshal([]byte(completeForm.Get("files")), &files))
	require.Len(t, files, 1)
	require.Equal(t, "F123", files[0]["id"])
	require.Equal(t, "my title", files[0]["title"])

	require.JSONEq(t, `{"ok":true,"files":[{"id":"F123","title":"my title"}]}`, out.String())
}

func TestUploadFileTool_RejectsBadBase64(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewUploadFileTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callUploadFile,
	}

	err := tool.Call(t.Context(), testSlackEnv(), strings.NewReader(`{"filename":"a.txt","content_base64":"!!!not-base64!!!"}`), io.Discard)
	require.Error(t, err)
	require.ErrorContains(t, err, "content_base64")
}

func TestUploadFileTool_StopsWhenStartReturnsError(t *testing.T) {
	t.Parallel()

	var completeCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/files.getUploadURLExternal", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error":"file_too_large"}`))
	})
	mux.HandleFunc("/files.completeUploadExternal", func(w http.ResponseWriter, r *http.Request) {
		completeCalls++
		w.WriteHeader(http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	tool := &slackTool{
		descriptor: NewUploadFileTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callUploadFile,
	}

	payload := map[string]any{
		"filename":       "huge.bin",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("xxx")),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	err = tool.Call(t.Context(), testSlackEnv(), bytes.NewReader(body), io.Discard)
	require.Error(t, err)
	require.ErrorContains(t, err, "file_too_large")
	require.Equal(t, 0, completeCalls)
}
