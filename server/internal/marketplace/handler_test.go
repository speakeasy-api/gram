package marketplace

import (
	"context"
	"net/http"
	"net/http/httptest"
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
