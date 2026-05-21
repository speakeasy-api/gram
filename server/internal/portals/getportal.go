package portals

import (
	"context"
	"errors"
	"fmt"
	"os"

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

	displayName, err := resolveDisplayName(ctx, s, portalRow, authCtx)
	if err != nil {
		return nil, err
	}

	out := &gen.Portal{
		Enabled:     !disabled,
		ProjectSlug: conv.PtrValOr(authCtx.ProjectSlug, ""),
		DisplayName: displayName,
		Tagline:     resolveTagline(portalRow),
		LogoURL:     resolveLogoURL(portalRow),
		Servers:     make([]*gen.PortalServer, 0, len(servers)),
	}

	for _, sv := range servers {
		toolCount := toolCountFromInterface(sv.ToolCount)
		out.Servers = append(out.Servers, &gen.PortalServer{
			Slug:        sv.EndpointSlug,
			Name:        firstNonEmpty(conv.FromPGTextOrEmpty[string](sv.ServerName), conv.FromPGTextOrEmpty[string](sv.ToolsetName)),
			Description: conv.FromPGText[string](sv.ToolsetDescription),
			ToolCount:   toolCount,
			InstallURL:  fmt.Sprintf("%s/x/mcp/%s/install", s.publicBaseURL(), sv.EndpointSlug),
		})
	}

	return out, nil
}

// resolveDisplayName returns the portal's display name. Preference order:
//  1. Portal-specific display_name override (if set and non-empty).
//  2. Project name from the database.
func resolveDisplayName(ctx context.Context, s *Service, row *repo.ProjectPortal, authCtx *contextvalues.AuthContext) (string, error) {
	if row != nil {
		if name := conv.FromPGTextOrEmpty[string](row.DisplayName); name != "" {
			return name, nil
		}
	}
	// Fall back to project name.
	proj, err := projectsrepo.New(s.db).GetProjectByID(ctx, *authCtx.ProjectID)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "fetch project name").Log(ctx, s.logger)
	}
	return proj.Name, nil
}

func resolveTagline(row *repo.ProjectPortal) *string {
	if row == nil {
		return nil
	}
	return conv.FromPGText[string](row.Tagline)
}

// resolveLogoURL returns the portal's logo URL. In v1 we return an empty
// string (logo upload is a follow-up). Task 2.6 replaces this stub.
func resolveLogoURL(_ *repo.ProjectPortal) *string {
	return nil
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

// publicBaseURL returns the public-facing base URL for constructing install URLs.
// Falls back to the GRAM_SITE_URL env var or a sensible default.
func (s *Service) publicBaseURL() string {
	return conv.Default(os.Getenv("GRAM_SITE_URL"), "https://app.getgram.ai")
}
