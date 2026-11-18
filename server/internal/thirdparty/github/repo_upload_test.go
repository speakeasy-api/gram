package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestCreateTree_BoundsUploadsAndCancelsOnFailure(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started := make(chan struct{}, 12)
	fail := make(chan struct{})
	var calls, active, peak, canceled atomic.Int32
	client := uploadTestClient(func(req *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(req.URL.Path, "/git/blobs") {
			t.Errorf("unexpected request after blob failure: %s", req.URL.Path)
			return jsonResponse(t, http.StatusInternalServerError, map[string]string{}), nil
		}
		ordinal := calls.Add(1)
		current := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); current > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, current) {
				break
			}
		}
		started <- struct{}{}
		if ordinal == 1 {
			select {
			case <-fail:
				return jsonResponse(t, http.StatusUnprocessableEntity, map[string]string{"message": "upload failed"}), nil
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}
		<-req.Context().Done()
		canceled.Add(1)
		return nil, req.Context().Err()
	})
	files := make(map[string][]byte)
	for i := range 12 {
		files[fmt.Sprintf("file-%02d.txt", i)] = []byte{0xff, 0x00}
	}
	done := make(chan error, 1)
	go func() {
		_, err := client.createTree(ctx, 1, "owner", "repo", files)
		done <- err
	}()
	// Keep the first wave in flight until all four upload slots are occupied.
	for range 4 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("timed out waiting for concurrent uploads")
		}
	}
	close(fail)
	select {
	case err := <-done:
		require.ErrorContains(t, err, "422")
	case <-ctx.Done():
		t.Fatal("upload failure did not cancel sibling requests")
	}
	require.EqualValues(t, 4, peak.Load())
	require.GreaterOrEqual(t, canceled.Load(), int32(3))
	require.Zero(t, active.Load())
}

func TestPushFiles_SmallTextStaysInline(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{"README.md": []byte("Hello 日本語\n"), "run.sh": []byte("#!/bin/sh\n"), "empty.txt": {}}
	treeCalls, commits, updates := 0, 0, 0
	client := uploadTestClient(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/git/ref/heads/main"):
			return jsonResponse(t, 200, map[string]any{"object": map[string]string{"sha": "head"}}), nil
		case strings.HasSuffix(req.URL.Path, "/git/trees"):
			treeCalls++
			var payload map[string]json.RawMessage
			require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
			require.Len(t, payload, 1)
			var entries []map[string]string
			require.NoError(t, json.Unmarshal(payload["tree"], &entries))
			require.Len(t, entries, len(files))
			var paths []string
			for _, entry := range entries {
				content, exists := files[entry["path"]]
				require.True(t, exists)
				require.Contains(t, entry, "content")
				require.Equal(t, string(content), entry["content"])
				require.NotContains(t, entry, "sha")
				require.Equal(t, "blob", entry["type"])
				mode := "100644"
				if strings.HasSuffix(entry["path"], ".sh") {
					mode = "100755"
				}
				require.Equal(t, mode, entry["mode"])
				paths = append(paths, entry["path"])
			}
			require.True(t, slices.IsSorted(paths))
			return jsonResponse(t, 201, map[string]string{"sha": "tree"}), nil
		case strings.HasSuffix(req.URL.Path, "/git/commits"):
			commits++
			return jsonResponse(t, 201, map[string]string{"sha": "commit"}), nil
		case req.Method == http.MethodPatch:
			updates++
			return jsonResponse(t, 200, map[string]string{}), nil
		default:
			t.Errorf("unexpected additional request: %s", req.URL.Path)
			return jsonResponse(t, 500, map[string]string{}), nil
		}
	})
	sha, err := client.PushFiles(t.Context(), 1, "owner", "repo", "main", "publish", files)
	require.NoError(t, err)
	require.Equal(t, "commit", sha)
	require.Equal(t, 1, treeCalls)
	require.Equal(t, 1, commits)
	require.Equal(t, 1, updates)
}

func TestPushFiles_FallbackOnlyOnceAndOnlyForTreeTimeout(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		status       int
		message      string
		blobFails    bool
		trees, blobs int
	}{
		{"other422", 422, "invalid path", false, 1, 0},
		{"timeout500", 500, "Your request timed out", false, 1, 0},
		{"fallbackBlobFails", 422, "Your request timed out", true, 1, 1},
		{"fallbackTreeTimesOut", 422, "Your request timed out", false, 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			trees, blobs := 0, 0
			client := uploadTestClient(func(req *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(req.URL.Path, "/git/ref/heads/main"):
					return jsonResponse(t, 200, map[string]any{"object": map[string]string{"sha": "head"}}), nil
				case strings.HasSuffix(req.URL.Path, "/git/trees"):
					trees++
					return jsonResponse(t, tc.status, map[string]string{"message": tc.message}), nil
				case strings.HasSuffix(req.URL.Path, "/git/blobs"):
					blobs++
					if tc.blobFails {
						return jsonResponse(t, 422, map[string]string{"message": "blob rejected"}), nil
					}
					return jsonResponse(t, 201, map[string]string{"sha": "blob"}), nil
				default:
					t.Errorf("unexpected commit/ref request after failure: %s", req.URL.Path)
					return jsonResponse(t, 500, map[string]string{}), nil
				}
			})
			sha, err := client.PushFiles(t.Context(), 1, "owner", "repo", "main", "publish", map[string][]byte{"file.txt": []byte("text")})
			require.Error(t, err)
			require.Empty(t, sha)
			require.Equal(t, tc.trees, trees)
			require.Equal(t, tc.blobs, blobs)
		})
	}
}

func TestCreateTree_InjectedTracerWithoutLocalParent(t *testing.T) {
	t.Parallel()
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprintf("remote=%t", remote), func(t *testing.T) {
			t.Parallel()
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			defer func() { require.NoError(t, provider.Shutdown(t.Context())) }()
			ctx := t.Context()
			parent := trace.NewSpanContext(trace.SpanContextConfig{
				TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, Remote: true, TraceFlags: trace.FlagsSampled,
			})
			if remote {
				ctx = trace.ContextWithRemoteSpanContext(ctx, parent)
			}
			client := uploadTestClient(func(req *http.Request) (*http.Response, error) {
				return jsonResponse(t, 201, map[string]string{"sha": "sha"}), nil
			})
			client.tracer = provider.Tracer("test")
			_, err := client.createTree(ctx, 1, "owner", "repo", map[string][]byte{"binary": {0xff}})
			require.NoError(t, err)
			spans := recorder.Ended()
			require.Len(t, spans, 2)
			var tree sdktrace.ReadOnlySpan
			for _, span := range spans {
				if span.Name() == "github.create_tree" {
					tree = span
				}
			}
			require.NotNil(t, tree)
			require.Equal(t, "inline", uploadSpanAttributes(tree)["github.upload.strategy"].AsString())
			if remote {
				require.Equal(t, parent.TraceID(), tree.SpanContext().TraceID())
				require.Equal(t, parent.SpanID(), tree.Parent().SpanID())
			} else {
				require.False(t, tree.Parent().IsValid())
			}
			for _, span := range spans {
				if span.Name() == "github.create_blob" {
					require.Equal(t, tree.SpanContext().SpanID(), span.Parent().SpanID())
				}
			}
		})
	}
}
