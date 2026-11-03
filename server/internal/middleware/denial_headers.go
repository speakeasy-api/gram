package middleware

import (
	"fmt"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

const (
	// HeaderRequiredScope names the scope a 403 response lacked.
	HeaderRequiredScope = "Gram-Required-Scope"
	// HeaderRequiredResourceKind names the kind of resource that scope was
	// checked against, such as "project" or "mcp".
	HeaderRequiredResourceKind = "Gram-Required-Resource-Kind"
)

// DenialHeaders explains a 403. It gives every request a denial recorder and,
// when the handler answers 403 after the authz engine refused a scope, adds
// the scope and resource kind as response headers. Clients map the slug to
// the catalogue served by access.listScopes; nothing about the resource or
// the caller's grants leaves the server. Every other status is untouched.
func DenialHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := authz.WithDenialRecorder(r.Context())
			writer := &denialWriter{
				ResponseWriter: w,
				recorded:       func() (authz.Denial, bool) { return authz.RecordedDenial(ctx) },
				wroteHeader:    false,
			}
			next.ServeHTTP(writer, r.WithContext(ctx))
		})
	}
}

type denialWriter struct {
	http.ResponseWriter
	// recorded reads the request's denial recorder when the status is decided.
	recorded    func() (authz.Denial, bool)
	wroteHeader bool
}

func (w *denialWriter) WriteHeader(code int) {
	if !w.wroteHeader && code == http.StatusForbidden {
		if denial, ok := w.recorded(); ok {
			w.Header().Set(HeaderRequiredScope, string(denial.Scope))
			w.Header().Set(HeaderRequiredResourceKind, denial.ResourceKind)
		}
	}
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *denialWriter) Write(b []byte) (int, error) {
	w.wroteHeader = true
	n, err := w.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("denialWriter.Write: %w", err)
	}
	return n, nil
}

// Flush is forwarded explicitly so streaming handlers behind this middleware
// keep working; see the logging middleware for why embedding is not enough.
func (w *denialWriter) Flush() {
	if err := http.NewResponseController(w.ResponseWriter).Flush(); err == nil {
		w.wroteHeader = true
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *denialWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
