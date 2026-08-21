package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

type supportHandoffIssuer interface {
	Issue(ctx context.Context, organizationID string) (string, error)
}

func (s *Service) handleOpenOrganizationInDashboard(w http.ResponseWriter, r *http.Request) error {
	scheme := security.APIKeyScheme{
		Name:           constants.AdminAuthSecurityScheme,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}
	ctx, err := s.verifier.Authorize(r.Context(), "", &scheme)
	if err != nil {
		return fmt.Errorf("admin auth: %w", err)
	}
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return oops.C(oops.CodeUnauthorized)
	}

	organizationID := strings.TrimSpace(r.URL.Query().Get("organization_id"))
	if organizationID == "" {
		return oops.E(oops.CodeInvalid, nil, "organization_id is required")
	}
	if s.dashboardURL == nil || s.dashboardURL.Scheme == "" || s.dashboardURL.Host == "" {
		return oops.E(oops.CodeUnexpected, nil, "dashboard URL is not configured").LogError(ctx, s.logger)
	}

	organization, err := orgRepo.New(s.db).GetOrganizationMetadata(ctx, organizationID)
	switch err {
	case nil:
	default:
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "load support target").LogError(ctx, s.logger)
	}
	if organization.DisabledAt.Valid {
		return oops.C(oops.CodeNotFound)
	}

	token, err := s.supportHandoffIssuer.Issue(ctx, organization.ID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "issue support handoff").LogError(ctx, s.logger)
	}

	query := url.Values{}
	query.Set("support_handoff", token)
	query.Set("redirect", "/"+organization.Slug)
	destination := url.URL{
		Scheme:   s.dashboardURL.Scheme,
		Host:     s.dashboardURL.Host,
		Path:     "/rpc/auth.login",
		RawQuery: query.Encode(),
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r.WithContext(ctx), destination.String(), http.StatusSeeOther)
	return nil
}
