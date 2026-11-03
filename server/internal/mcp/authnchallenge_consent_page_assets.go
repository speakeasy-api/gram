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
// `GET /mcp/consent-fonts/{file}`; see consentpage.WriteFont. Fonts are
// separate immutable responses rather than data: URIs inlined into a
// stylesheet that renders uncached on every consent view.
func (s *Service) ServeConsentFont(w http.ResponseWriter, r *http.Request) error {
	if err := consentpage.WriteFont(w, chi.URLParam(r, "file")); err != nil {
		return oops.E(oops.CodeUnexpected, err, "write consent font response").LogError(r.Context(), s.logger)
	}
	return nil
}
