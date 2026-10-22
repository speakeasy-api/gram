// Font route for the stylesheet and brand fonts shared with the Platform MCP
// OAuth page. The assets themselves live in the consentpage package.

package mcp

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/speakeasy-api/gram/server/internal/consentpage"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ServeConsentFont serves one embedded brand font. Mounted at
// `GET /mcp/consent-fonts/{file}`, which is also the build's `base` and so
// the prefix the stylesheet's @font-face urls point at. Fonts are separate
// immutable responses rather than data: URIs inlined into a stylesheet that
// renders uncached on every consent view. File names are content-hashed by the build,
// so the response is immutable; an unknown name is a stale URL.
func (s *Service) ServeConsentFont(w http.ResponseWriter, r *http.Request) error {
	data, ok := consentpage.Font(chi.URLParam(r, "file"))
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}

	w.Header().Set("Content-Type", "font/woff2")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		return oops.E(oops.CodeUnexpected, err, "write consent font response").LogError(r.Context(), s.logger)
	}
	return nil
}
