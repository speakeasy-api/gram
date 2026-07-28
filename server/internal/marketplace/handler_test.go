package marketplace

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// spyResolver records whether Resolve was called and returns ErrNotFound.
// Used to assert that malformed tokens are rejected before the DB lookup.
type spyResolver struct {
	called bool
}

func (r *spyResolver) Resolve(_ context.Context, _ string) (Upstream, error) {
	r.called = true
	return Upstream{}, ErrNotFound
}

type fixedResolver struct{}

func (*fixedResolver) Resolve(_ context.Context, token string) (Upstream, error) {
	return Upstream{
		Token:       token,
		Owner:       "speakeasy-plugins",
		Repo:        "test-plugins",
		AccessToken: "github-token",
	}, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Token-format check is a cheap pre-filter that keeps the resolver's DB
// lookup off the hot path for anyone hammering the proxy with random URLs.
// The 256-bit token entropy makes brute-force infeasible; this guards the
// DB from random-string flooding.
func TestMalformedTokensSkipResolver(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		req  *http.Request
	}{
		{"info/refs: no .git suffix", httptest.NewRequest(http.MethodGet, "/marketplace/short/info/refs?service=git-upload-pack", nil)},
		{"info/refs: bad chars before .git", httptest.NewRequest(http.MethodGet, "/marketplace/bad!chars1234567890123456789012345678901234567.git/info/refs?service=git-upload-pack", nil)},
		{"upload-pack: too short", httptest.NewRequest(http.MethodPost, "/marketplace/short.git/git-upload-pack", nil)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spy := &spyResolver{}
			s := &Server{
				resolver: spy,
				logger:   testenv.NewLogger(t),
			}
			rec := httptest.NewRecorder()
			s.Routes().ServeHTTP(rec, tc.req)

			require.Equal(t, http.StatusNotFound, rec.Code)
			require.False(t, spy.called, "resolver must not be called for malformed tokens")
		})
	}
}

func TestUploadPackRequestIsReplayable(t *testing.T) {
	t.Parallel()

	wantBody := []byte("0032want 0123456789012345678901234567890123456789\n0000")
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotBody, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Equal(t, wantBody, gotBody)
			require.NotNil(t, req.GetBody)

			replayedBody, err := req.GetBody()
			require.NoError(t, err)
			defer func() { require.NoError(t, replayedBody.Close()) }()
			gotReplayedBody, err := io.ReadAll(replayedBody)
			require.NoError(t, err)
			require.Equal(t, wantBody, gotReplayedBody)
			_, markedIdempotent := req.Header["Idempotency-Key"]
			require.True(t, markedIdempotent)

			return &http.Response{
				Status:           "200 OK",
				StatusCode:       http.StatusOK,
				Proto:            "HTTP/1.1",
				ProtoMajor:       1,
				ProtoMinor:       1,
				Header:           make(http.Header),
				Body:             io.NopCloser(strings.NewReader("packfile")),
				ContentLength:    8,
				TransferEncoding: nil,
				Close:            false,
				Uncompressed:     false,
				Trailer:          nil,
				Request:          req,
				TLS:              nil,
			}, nil
		}),
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       0,
	}
	server := NewServer(&fixedResolver{}, client, testenv.NewLogger(t))
	req := httptest.NewRequest(
		http.MethodPost,
		"/marketplace/"+strings.Repeat("a", 43)+".git/git-upload-pack",
		bytes.NewReader(wantBody),
	)
	// Incoming server requests do not provide GetBody.
	req.GetBody = nil
	rec := httptest.NewRecorder()

	server.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "packfile", rec.Body.String())
}

func TestUploadPackRejectsLargeRequestBody(t *testing.T) {
	t.Parallel()

	clientCalled := false
	client := &http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			clientCalled = true
			return nil, nil
		}),
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       0,
	}
	server := NewServer(&fixedResolver{}, client, testenv.NewLogger(t))
	req := httptest.NewRequest(
		http.MethodPost,
		"/marketplace/"+strings.Repeat("a", 43)+".git/git-upload-pack",
		bytes.NewReader(make([]byte, maxUploadPackRequestBytes+1)),
	)
	rec := httptest.NewRecorder()

	server.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.False(t, clientCalled)
}
