package admin

import (
	"encoding/json"
	"fmt"
	"net/http"

	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// adminOrganizationFeatures is the response body of GET /admin/organization.features.
//
// Like sessionInfo, this endpoint is deliberately not a Goa method so the
// standalone admin UI contract stays out of the shared OpenAPI document and the
// generated public SDKs.
type adminOrganizationFeatures struct {
	ConsentToolFilteringEnabled    bool   `json:"consent_tool_filtering_enabled"`
	HooksBrowserLoginEnabled       bool   `json:"hooks_browser_login_enabled"`
	HooksFailOpenEnabled           bool   `json:"hooks_fail_open_enabled"`
	PlatformMcpEnabled             bool   `json:"platform_mcp_enabled"`
	RemoteSessionAutoRefreshPolicy string `json:"remote_session_auto_refresh_policy"`
	SessionCaptureEnabled          bool   `json:"session_capture_enabled"`
	SkillCaptureMetadataOnly       bool   `json:"skill_capture_metadata_only"`
	SkillsEnabled                  bool   `json:"skills_enabled"`
}

func (s *Service) handleGetOrganizationFeatures(w http.ResponseWriter, r *http.Request) error {
	scheme := security.APIKeyScheme{
		Name:           constants.AdminAuthSecurityScheme,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}

	ctx, err := s.verifier.Authorize(r.Context(), "", &scheme)
	if err != nil {
		return fmt.Errorf("admin auth: %w", err)
	}

	organizationID := r.URL.Query().Get("organization_id")
	if organizationID == "" {
		return oops.C(oops.CodeBadRequest)
	}

	organizationID, err = s.canonicalAdminOrganizationID(ctx, organizationID)
	if err != nil {
		return err
	}

	readFeature := func(feature productfeatures.Feature) bool {
		enabled, err := s.productFeatures.IsFeatureEnabled(ctx, organizationID, feature)
		if err != nil {
			s.logger.WarnContext(
				ctx,
				"failed to read organization feature flag",
				attr.SlogError(err),
				attr.SlogOrganizationID(organizationID),
				attr.SlogProductFeatureName(string(feature)),
			)
			return false
		}
		return enabled
	}

	remoteSessionAutoRefreshPolicy := "disabled"
	if readFeature(productfeatures.FeatureRemoteSessionAutoRefreshEnforced) {
		remoteSessionAutoRefreshPolicy = "enforced"
	} else if readFeature(productfeatures.FeatureRemoteSessionAutoRefresh) {
		remoteSessionAutoRefreshPolicy = "user_controlled"
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(adminOrganizationFeatures{
		ConsentToolFilteringEnabled:    readFeature(productfeatures.FeatureConsentToolFiltering),
		HooksBrowserLoginEnabled:       readFeature(productfeatures.FeatureHooksBrowserLogin),
		HooksFailOpenEnabled:           readFeature(productfeatures.FeatureHooksFailOpen),
		PlatformMcpEnabled:             readFeature(productfeatures.FeaturePlatformMCP),
		RemoteSessionAutoRefreshPolicy: remoteSessionAutoRefreshPolicy,
		SessionCaptureEnabled:          readFeature(productfeatures.FeatureSessionCapture),
		SkillCaptureMetadataOnly:       readFeature(productfeatures.FeatureSkillCaptureMetadataOnly),
		SkillsEnabled:                  true,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode organization features").LogError(ctx, s.logger)
	}

	return nil
}
