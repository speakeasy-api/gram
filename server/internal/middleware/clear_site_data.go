package middleware

import (
	"fmt"
	"net/http"
)

const clearSiteDataHeader = "Clear-Site-Data"
const clearSiteDataLogout = `"cache", "cookies", "storage"`

func ClearSiteDataOnLogout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&clearSiteDataWriter{ResponseWriter: w, wroteHeader: false}, r)
	})
}

type clearSiteDataWriter struct {
	http.ResponseWriter

	wroteHeader bool
}

func (w *clearSiteDataWriter) WriteHeader(code int) {
	// 1xx other than 101 is informational: net/http sends it and leaves the
	// final status open, so the directive stays pending for the next call.
	if code >= 100 && code < 200 && code != 101 {
		w.ResponseWriter.WriteHeader(code)
		return
	}

	if !w.wroteHeader {
		w.wroteHeader = true
		if code >= 200 && code < 300 {
			w.Header().Set(clearSiteDataHeader, clearSiteDataLogout)
		}
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *clearSiteDataWriter) Write(b []byte) (int, error) {
	// A body write with no preceding WriteHeader makes net/http send 200.
	if !w.wroteHeader {
		w.wroteHeader = true
		w.Header().Set(clearSiteDataHeader, clearSiteDataLogout)
	}

	n, err := w.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("write logout response: %w", err)
	}

	return n, nil
}

// Flush must be forwarded explicitly: embedding the http.ResponseWriter
// interface hides the underlying writer's Flush from type asserts, which
// silently disables streaming for every handler behind this middleware.
func (w *clearSiteDataWriter) Flush() {
	// A successful flush commits the headers with an implicit 200, so the
	// directive has to be in the map before it runs. An unsupported or failed
	// flush commits nothing, and leaving the directive behind would attach it
	// to whatever status is written later — including an error status.
	pending := !w.wroteHeader
	if pending {
		w.Header().Set(clearSiteDataHeader, clearSiteDataLogout)
	}

	// ResponseController finds flush support through FlushError and Unwrap
	// chains that a direct http.Flusher assert would miss.
	if err := http.NewResponseController(w.ResponseWriter).Flush(); err != nil {
		if pending {
			w.Header().Del(clearSiteDataHeader)
		}
		return
	}

	w.wroteHeader = true
}

// Unwrap lets http.ResponseController reach controls of the underlying
// writer that this wrapper does not forward.
func (w *clearSiteDataWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
