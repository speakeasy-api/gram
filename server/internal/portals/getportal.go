package portals

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/portals"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/portals/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func (s *Service) GetPortal(ctx context.Context, payload *gen.GetPortalPayload) (*gen.Portal, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{
		Scope:      authz.ScopeProjectRead,
		ResourceID: authCtx.ProjectID.String(),
	}); err != nil {
		return nil, err
	}

	r := repo.New(s.db)
	row, err := r.GetPortalByProjectID(ctx, *authCtx.ProjectID)
	disabled := false
	var portalRow *repo.ProjectPortal
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		disabled = true
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load portal").Log(ctx, s.logger)
	default:
		disabled = !row.Enabled
		portalRow = &row
	}

	preview := payload.Preview != nil && *payload.Preview
	if disabled {
		if !preview {
			return nil, oops.C(oops.CodeNotFound)
		}
		// preview=true: require ScopeProjectWrite to bypass.
		if err := s.authz.Require(ctx, authz.Check{
			Scope:      authz.ScopeProjectWrite,
			ResourceID: authCtx.ProjectID.String(),
		}); err != nil {
			return nil, oops.C(oops.CodeNotFound)
		}
	}

	servers, err := r.ListPortalServerCards(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list portal servers").Log(ctx, s.logger)
	}

	// Fetch the project once: needed for both display_name and logo_url fallbacks.
	proj, err := projectsrepo.New(s.db).GetProjectByID(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "fetch project").Log(ctx, s.logger)
	}

	out := &gen.Portal{
		Enabled:     !disabled,
		ProjectSlug: conv.PtrValOr(authCtx.ProjectSlug, ""),
		DisplayName: resolveDisplayName(portalRow, proj),
		Tagline:     resolveTagline(portalRow),
		LogoURL:     s.resolveLogoURL(portalRow, proj),
		Servers:     make([]*gen.PortalServer, 0, len(servers)),
	}

	for _, sv := range servers {
		toolCount := toolCountFromInterface(sv.ToolCount)
		out.Servers = append(out.Servers, &gen.PortalServer{
			Slug:        sv.EndpointSlug,
			Name:        firstNonEmpty(conv.FromPGTextOrEmpty[string](sv.ServerName), conv.FromPGTextOrEmpty[string](sv.ToolsetName)),
			Description: conv.FromPGText[string](sv.ToolsetDescription),
			ToolCount:   toolCount,
			InstallURL:  fmt.Sprintf("%s/x/mcp/%s/install", s.siteURL, sv.EndpointSlug),
		})
	}

	return out, nil
}

// resolveDisplayName returns the portal's display name. Preference order:
//  1. Portal-specific display_name override (if set and non-empty).
//  2. Project name from the database.
func resolveDisplayName(row *repo.ProjectPortal, proj projectsrepo.Project) string {
	if row != nil {
		if name := conv.FromPGTextOrEmpty[string](row.DisplayName); name != "" {
			return name
		}
	}
	return proj.Name
}

func resolveTagline(row *repo.ProjectPortal) *string {
	if row == nil {
		return nil
	}
	return conv.FromPGText[string](row.Tagline)
}

// resolveLogoURL returns the portal's logo URL.
// Preference order:
//  1. Portal-specific logo_asset_id (if set).
//  2. Project-level logo_asset_id (if set).
//  3. No logo.
//
// Asset URLs are served via the management API assets.serveImage endpoint
// following the same pattern as mcpmetadata.
func (s *Service) resolveLogoURL(row *repo.ProjectPortal, proj projectsrepo.Project) *string {
	var assetID uuid.UUID
	switch {
	case row != nil && row.LogoAssetID.Valid:
		assetID = row.LogoAssetID.UUID
	case proj.LogoAssetID.Valid:
		assetID = proj.LogoAssetID.UUID
	default:
		return nil
	}
	u := s.assetServeURL(assetID)
	return &u
}

// assetServeURL constructs a management API URL that serves the given asset
// via the assets.serveImage endpoint.
func (s *Service) assetServeURL(assetID uuid.UUID) string {
	return fmt.Sprintf("%s/rpc/assets.serveImage?id=%s", s.siteURL, assetID.String())
}

// toolCountFromInterface converts the interface{} value returned by sqlc for
// the cardinality() expression into an int. sqlc cannot infer the type of
// aggregate expressions, so it widens to interface{}.
func toolCountFromInterface(v any) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case int32:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
