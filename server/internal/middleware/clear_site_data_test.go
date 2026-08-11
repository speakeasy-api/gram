package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// noFlushWriter is a ResponseWriter with no flush support of any kind, so
// http.ResponseController.Flush on it fails with ErrNotSupported.
type noFlushWriter struct {
	header http.Header
	code   int
}

func (w *noFlushWriter) Header() http.Header         { return w.header }
func (w *noFlushWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w *noFlushWriter) WriteHeader(code int)        { w.code = code }

func TestClearSiteDataOnLogout_SetsDirectiveOnOK(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	handler := ClearSiteDataOnLogout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/auth.logout", nil))

	require.Equal(t, `"cookies", "storage"`, rec.Header().Get("Clear-Site-Data"))
}

// Any success status carries the directive, not just 200 — a bodiless logout
// answers 204.
func TestClearSiteDataOnLogout_SetsDirectiveOnNoContent(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	handler := ClearSiteDataOnLogout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/auth.logout", nil))

	require.Equal(t, `"cookies", "storage"`, rec.Header().Get("Clear-Site-Data"))
}

// A handler that writes a body without calling WriteHeader still sends a 200,
// so the directive has to be attached on that path too.
func TestClearSiteDataOnLogout_SetsDirectiveOnImplicitOK(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	handler := ClearSiteDataOnLogout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(`{"session_cookie":""}`))
		require.NoError(t, err)
	}))

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/auth.logout", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, `"cookies", "storage"`, rec.Header().Get("Clear-Site-Data"))
}

// A logout that never invalidated anything must leave the caller's state alone:
// an unauthenticated request would otherwise wipe cookies and storage for a
// session that is still live in another tab.
func TestClearSiteDataOnLogout_OmitsDirectiveOnUnauthorized(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	handler := ClearSiteDataOnLogout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/auth.logout", nil))

	require.Empty(t, rec.Header().Get("Clear-Site-Data"))
}

func TestClearSiteDataOnLogout_OmitsDirectiveOnRedirect(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	handler := ClearSiteDataOnLogout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound)
	}))

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/auth.logout", nil))

	require.Empty(t, rec.Header().Get("Clear-Site-Data"))
}

// The status recorded first wins: net/http drops a second WriteHeader, so a
// late error status cannot retract a directive already on the wire, and must
// not add one either.
func TestClearSiteDataOnLogout_IgnoresStatusAfterCommit(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	handler := ClearSiteDataOnLogout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/auth.logout", nil))

	require.Empty(t, rec.Header().Get("Clear-Site-Data"))
}

func TestClearSiteDataOnLogout_SetsDirectiveBeforeFlush(t *testing.T) {
	t.Parallel()

	inner := &flushRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: false}
	w := &clearSiteDataWriter{ResponseWriter: inner, wroteHeader: false}

	w.Flush()

	require.True(t, inner.flushed)
	require.Equal(t, `"cookies", "storage"`, w.Header().Get("Clear-Site-Data"))

	// The flush committed the headers, so a later error status must not be
	// able to un-commit them — but it must not gain a directive either.
	w.WriteHeader(http.StatusInternalServerError)
	require.Equal(t, `"cookies", "storage"`, w.Header().Get("Clear-Site-Data"))
}

// A flush the underlying writer cannot serve commits nothing, so the directive
// must go back to pending rather than ride along with a later error status.
func TestClearSiteDataOnLogout_RetractsDirectiveOnFailedFlush(t *testing.T) {
	t.Parallel()

	inner := &noFlushWriter{header: http.Header{}, code: 0}
	w := &clearSiteDataWriter{ResponseWriter: inner, wroteHeader: false}

	w.Flush()
	require.Empty(t, w.Header().Get("Clear-Site-Data"))

	w.WriteHeader(http.StatusInternalServerError)
	require.Empty(t, w.Header().Get("Clear-Site-Data"))
	require.Equal(t, http.StatusInternalServerError, inner.code)
}
