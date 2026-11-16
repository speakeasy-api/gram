package github

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func uploadTestClient(transport roundTripFunc) *Client {
	return &Client{httpClient: &http.Client{Transport: transport}, tokens: map[int64]cachedToken{1: {token: "test-token", expiresAt: time.Now().Add(time.Hour)}}}
}

func TestPushFiles_TimeoutFallbackUploadsAllContentAsBlobs(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{
		"plugin/.claude-plugin/icon.png": {0x89, 'P', 'N', 'G', 0xff, 0x00},
		"plugin/README.md":               []byte("# Plugin\nUnicode: 日本語\n"),
		"plugin/hooks/run.sh":            []byte("#!/bin/sh\necho ok\n"),
		"plugin/empty.txt":               {},
	}
	var mu sync.Mutex
	blobs := map[string][]byte{}
	var entries []map[string]string
	treeCalls := 0
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer func() { require.NoError(t, provider.Shutdown(t.Context())) }()
	ctx, parent := provider.Tracer("test").Start(t.Context(), "publish")
	defer parent.End()
	client := uploadTestClient(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(req.URL.Path, "/git/ref/heads/main"):
			return jsonResponse(t, http.StatusOK, map[string]any{"object": map[string]string{"sha": "head-sha"}}), nil
		case strings.HasSuffix(req.URL.Path, "/git/blobs"):
			var payload map[string]string
			require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
			require.Equal(t, "base64", payload["encoding"])
			decoded, err := base64.StdEncoding.DecodeString(payload["content"])
			require.NoError(t, err)
			sha := "blob-" + payload["content"]
			blobs[sha] = decoded
			return jsonResponse(t, http.StatusCreated, map[string]string{"sha": sha}), nil
		case strings.HasSuffix(req.URL.Path, "/git/trees"):
			var payload map[string]json.RawMessage
			require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
			require.Len(t, payload, 1)
			require.NotContains(t, payload, "base_tree")
			entries = nil
			require.NoError(t, json.Unmarshal(payload["tree"], &entries))
			treeCalls++
			if treeCalls == 1 {
				return jsonResponse(t, http.StatusUnprocessableEntity, map[string]string{"message": "Your request timed out"}), nil
			}
			return jsonResponse(t, http.StatusCreated, map[string]string{"sha": "tree-sha"}), nil
		case strings.HasSuffix(req.URL.Path, "/git/commits"):
			return jsonResponse(t, http.StatusCreated, map[string]string{"sha": "commit-sha"}), nil
		case req.Method == http.MethodPatch && strings.HasSuffix(req.URL.Path, "/git/refs/heads/main"):
			return jsonResponse(t, http.StatusOK, map[string]string{}), nil
		default:
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
			return jsonResponse(t, http.StatusNotFound, map[string]string{}), nil
		}
	})
	sha, err := client.PushFiles(ctx, 1, "owner", "repo", "main", "publish", files)
	require.NoError(t, err)
	require.Equal(t, "commit-sha", sha)
	require.Len(t, blobs, len(files))
	require.Len(t, entries, len(files))
	var paths []string
	var total, largest int
	for _, entry := range entries {
		path := entry["path"]
		content, ok := files[path]
		require.True(t, ok, "unexpected/orphaned file: %s", path)
		require.Equal(t, content, blobs[entry["sha"]])
		require.Equal(t, "blob", entry["type"])
		require.NotContains(t, entry, "content")
		mode := "100644"
		if strings.HasSuffix(path, ".sh") {
			mode = "100755"
		}
		require.Equal(t, mode, entry["mode"])
		paths = append(paths, path)
		total += len(content)
		largest = max(largest, len(content))
	}
	require.True(t, slices.IsSorted(paths))
	spans := recorder.Ended()
	require.Len(t, spans, len(files)+3, "local parent provider must capture uploads without global registration")
	var tree sdktrace.ReadOnlySpan
	for _, span := range spans {
		if span.Name() == "github.create_tree" && uploadSpanAttributes(span)["github.upload.strategy"].AsString() == "blobs" {
			tree = span
		}
	}
	require.NotNil(t, tree)
	require.Equal(t, parent.SpanContext().SpanID(), tree.Parent().SpanID())
	attrs := uploadSpanAttributes(tree)
	require.EqualValues(t, len(files), attrs["github.upload.file_count"].AsInt64())
	require.EqualValues(t, total, attrs["github.upload.total_bytes"].AsInt64())
	require.EqualValues(t, largest, attrs["github.upload.largest_file_bytes"].AsInt64())
	body, err := json.Marshal(map[string]any{"tree": entries})
	require.NoError(t, err)
	require.EqualValues(t, len(body), attrs["github.upload.request_bytes"].AsInt64())
	sizes := []int{len(files["plugin/.claude-plugin/icon.png"])}
	for _, content := range files {
		sizes = append(sizes, len(content))
	}
	var observedSizes []int
	for _, span := range spans {
		require.Equal(t, parent.SpanContext().TraceID(), span.SpanContext().TraceID())
		attrs := uploadSpanAttributes(span)
		require.Contains(t, attrs, "github.upload.duration_ms")
		require.GreaterOrEqual(t, attrs["github.upload.duration_ms"].AsInt64(), int64(0))
		if span.Name() == "github.create_tree" && attrs["github.upload.strategy"].AsString() == "inline" {
			require.EqualValues(t, 422, attrs["http.response.status_code"].AsInt64())
			require.Equal(t, codes.Error, span.Status().Code)
		} else {
			require.EqualValues(t, http.StatusCreated, attrs["http.response.status_code"].AsInt64())
			require.NotEqual(t, codes.Error, span.Status().Code)
		}
		if span.Name() == "github.create_blob" {
			var foundParent bool
			for _, candidate := range spans {
				if candidate.Name() == "github.create_tree" && candidate.SpanContext().SpanID() == span.Parent().SpanID() {
					foundParent = true
				}
			}
			require.True(t, foundParent)
			observedSizes = append(observedSizes, int(attrs["github.upload.content_bytes"].AsInt64()))
			require.Greater(t, attrs["github.upload.request_bytes"].AsInt64(), attrs["github.upload.content_bytes"].AsInt64())
		}
	}
	require.ElementsMatch(t, sizes, observedSizes)
}

func uploadSpanAttributes(span sdktrace.ReadOnlySpan) map[string]attribute.Value {
	attrs := map[string]attribute.Value{}
	for _, attr := range span.Attributes() {
		attrs[string(attr.Key)] = attr.Value
	}
	return attrs
}

func TestPushFiles_UploadFailureDoesNotCommit(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"blobs", "trees"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			defer func() { require.NoError(t, provider.Shutdown(t.Context())) }()
			ctx, parent := provider.Tracer("test").Start(t.Context(), "publish")
			defer parent.End()
			var requests []string
			client := uploadTestClient(func(req *http.Request) (*http.Response, error) {
				requests = append(requests, req.URL.Path)
				switch {
				case strings.HasSuffix(req.URL.Path, "/git/ref/heads/main"):
					return jsonResponse(t, http.StatusOK, map[string]any{"object": map[string]string{"sha": "head"}}), nil
				case strings.HasSuffix(req.URL.Path, "/git/"+stage):
					return jsonResponse(t, http.StatusUnprocessableEntity, map[string]string{"message": "private upstream detail"}), nil
				case strings.HasSuffix(req.URL.Path, "/git/blobs"):
					return jsonResponse(t, http.StatusCreated, map[string]string{"sha": "blob"}), nil
				default:
					t.Errorf("unexpected request after failure: %s", req.URL.Path)
					return jsonResponse(t, http.StatusInternalServerError, map[string]string{}), nil
				}
			})
			sha, err := client.PushFiles(ctx, 1, "owner", "repo", "main", "publish", map[string][]byte{"file.bin": {0xff, 0x00}})
			require.ErrorContains(t, err, "422")
			require.Empty(t, sha)
			for _, path := range requests {
				require.NotContains(t, path, "/git/commits")
				require.NotContains(t, path, "/git/refs/")
				if stage == "blobs" {
					require.NotContains(t, path, "/git/trees")
				}
			}
			for _, span := range recorder.Ended() {
				if span.Name() == "github.create_tree" || stage == "blobs" {
					require.Equal(t, codes.Error, span.Status().Code)
					require.NotContains(t, span.Status().Description, "private")
				}
				if span.Name() == "github.create_"+strings.TrimSuffix(stage, "s") {
					require.EqualValues(t, 422, uploadSpanAttributes(span)["http.response.status_code"].AsInt64())
				}
			}
		})
	}
}
