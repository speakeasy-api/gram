package admin

import (
	"context"
	"fmt"
	"net/http"

	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func (s *Service) authorizeAdminRequest(r *http.Request) (context.Context, error) {
	scheme := security.APIKeyScheme{Name: constants.AdminAuthSecurityScheme}
	ctx, err := s.verifier.Authorize(r.Context(), "", &scheme)
	if err != nil {
		return nil, fmt.Errorf("admin auth: %w", err)
	}
	return ctx, nil
}

func (s *Service) canonicalAdminOrganizationForRequest(ctx context.Context, organizationID string) (string, error) {
	if organizationID == "" {
		return "", oops.C(oops.CodeBadRequest)
	}
	return s.canonicalAdminOrganizationID(ctx, organizationID)
}
