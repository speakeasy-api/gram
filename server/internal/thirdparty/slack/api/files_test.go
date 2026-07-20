package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 64)...)

// newFileServer serves files.info pointing at its own /download path. The
// client trusts its non-default base URL's host for downloads.
func newFileServer(t *testing.T, infoResponse func(r *http.Request) string, download http.HandlerFunc) *Client {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/files.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(infoResponse(r)))
	})
	if download != nil {
		mux.HandleFunc("/download", download)
	}

	return NewClient(server.URL, server.Client())
}

func infoJSON(serverHost *string, size int64) func(r *http.Request) string {
	return func(r *http.Request) string {
		return fmt.Sprintf(`{"ok":true,"file":{"id":"F123","name":"cat.png","title":"a cat","mimetype":"image/png","size":%d,"url_private_download":"http://%s/download"}}`, size, *serverHost)
	}
}

func TestFetchImageFileDownloadsAndSniffsImage(t *testing.T) {
	t.Parallel()

	var host string
	var gotAuth, gotFileParam string
	client := newFileServer(t, func(r *http.Request) string {
		require.NoError(t, r.ParseForm())
		gotFileParam = r.PostForm.Get("file")
		return fmt.Sprintf(`{"ok":true,"file":{"id":"F123","name":"cat.png","title":"a cat","mimetype":"text/lies","size":%d,"url_private_download":"http://%s/download"}}`, len(pngBytes), host)
	}, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write(pngBytes)
	})
	host = strings.TrimPrefix(client.BaseURL(), "http://")

	img, err := client.FetchImageFile(t.Context(), "F123", "xoxb-token")
	require.NoError(t, err)
	require.Equal(t, "F123", gotFileParam)
	require.Equal(t, "Bearer xoxb-token", gotAuth)
	require.Equal(t, "F123", img.FileID)
	require.Equal(t, "cat.png", img.Name)
	// The sniffed type wins over Slack's declared mimetype.
	require.Equal(t, "image/png", img.MimeType)
	require.Equal(t, pngBytes, img.Data)
	require.True(t, strings.HasPrefix(img.DataURI(), "data:image/png;base64,"))
}

func TestFetchImageFileRejectsNonImageContent(t *testing.T) {
	t.Parallel()

	var host string
	client := newFileServer(t, infoJSON(&host, 64), func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>please sign in</body></html>"))
	})
	host = strings.TrimPrefix(client.BaseURL(), "http://")

	_, err := client.FetchImageFile(t.Context(), "F123", "tok")
	require.ErrorContains(t, err, "not an allowed image type")
}

func TestFetchImageFileRejectsDeclaredOversize(t *testing.T) {
	t.Parallel()

	downloaded := false
	var host string
	client := newFileServer(t, infoJSON(&host, MaxImageFileBytes+1), func(w http.ResponseWriter, r *http.Request) {
		downloaded = true
	})
	host = strings.TrimPrefix(client.BaseURL(), "http://")

	_, err := client.FetchImageFile(t.Context(), "F123", "tok")
	require.ErrorContains(t, err, "byte limit")
	require.False(t, downloaded, "oversized files must be rejected before download")
}

func TestFetchImageFileRejectsOversizeBody(t *testing.T) {
	t.Parallel()

	var host string
	// files.info under-declares the size; the byte cap on the actual body
	// must still hold.
	client := newFileServer(t, infoJSON(&host, 64), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n"))
		_, _ = w.Write(bytes.Repeat([]byte{0}, MaxImageFileBytes))
	})
	host = strings.TrimPrefix(client.BaseURL(), "http://")

	_, err := client.FetchImageFile(t.Context(), "F123", "tok")
	require.ErrorContains(t, err, "byte limit")
}

func TestFetchImageFileRejectsNonSlackHost(t *testing.T) {
	t.Parallel()

	downloaded := false
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	mux.HandleFunc("/files.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"file":{"id":"F123","size":64,"url_private_download":"https://evil.example.com/download"}}`))
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) { downloaded = true })

	// The download host differs from the API base host, so the default
	// slack.com host validation applies and must reject it.
	client := NewClient(server.URL, server.Client())
	_, err := client.FetchImageFile(t.Context(), "F123", "tok")
	require.ErrorContains(t, err, "not a slack.com host")
	require.False(t, downloaded)
}

func TestFetchImageFileSurfacesSlackError(t *testing.T) {
	t.Parallel()

	client := newFileServer(t, func(r *http.Request) string {
		return `{"ok":false,"error":"file_not_found"}`
	}, nil)

	_, err := client.FetchImageFile(t.Context(), "F404", "tok")
	require.ErrorContains(t, err, "file_not_found")
}

func TestFetchImageFileHonorsContextDeadline(t *testing.T) {
	t.Parallel()

	var host string
	client := newFileServer(t, infoJSON(&host, 64), func(w http.ResponseWriter, r *http.Request) {
		// Hang until the client gives up; the request context unblocks the
		// handler so the test server can shut down cleanly.
		<-r.Context().Done()
	})
	host = strings.TrimPrefix(client.BaseURL(), "http://")

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := client.FetchImageFile(ctx, "F123", "tok")
	require.Error(t, err)
	require.Less(t, time.Since(start), 5*time.Second, "deadline must cut the hung download short")
}

func TestValidateSlackFileURL(t *testing.T) {
	t.Parallel()

	valid := func(raw string) error {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		return validateSlackFileURL(u)
	}

	require.NoError(t, valid("https://files.slack.com/files-pri/T1-F1/download/cat.png"))
	require.NoError(t, valid("https://myteam.slack.com/files/cat.png"))
	require.ErrorContains(t, valid("http://files.slack.com/x"), "https")
	require.ErrorContains(t, valid("https://evilslack.com/x"), "not a slack.com host")
	require.ErrorContains(t, valid("https://files.slack.com.evil.io/x"), "not a slack.com host")
	require.ErrorContains(t, valid("https://example.com/x"), "not a slack.com host")
}
