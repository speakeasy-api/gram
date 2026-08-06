package mcp

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ServeFavicon serves /favicon.ico on MCP custom domains. MCP clients that
// fall back to a server's domain favicon for its display icon (Cursor among
// them) pick up the logo configured in the domain's MCP metadata this way.
// The route only responds on custom domains — without a custom-domain
// context it 404s, leaving the platform domain's own favicon untouched.
func (s *Service) ServeFavicon(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	domainCtx := customdomains.FromContext(ctx)
	if domainCtx == nil {
		return oops.C(oops.CodeNotFound)
	}

	logoID, err := s.mcpMetadataRepo.GetLogoForCustomDomain(ctx, uuid.NullUUID{UUID: domainCtx.DomainID, Valid: true})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.C(oops.CodeNotFound)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to resolve custom domain logo").LogError(ctx, s.logger)
	}
	if !logoID.Valid {
		return oops.C(oops.CodeNotFound)
	}

	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.Redirect(w, r, assets.ServeImageURL(s.serverURL, logoID.UUID), http.StatusFound)
	return nil
}
