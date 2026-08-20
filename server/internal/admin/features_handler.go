package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"goa.design/goa/v3/security"
)

// adminOrganizationFeaturesResponse is the response body of GET /admin/organization.features.
//
// Like sessionInfo, this endpoint is deliberately not a Goa method so the
// standalone admin UI contract stays out of the shared OpenAPI document and the
// generated public SDKs.
type adminOrganizationFeaturesResponse struct {
	AuthzChallengeLoggingEnabled         bool `json:"authz_challenge_logging_enabled"`
	CustomerManagedEncryptionKeysEnabled bool `json:"customer_managed_encryption_keys_enabled"`
	CustomModelKeysEnabled               bool `json:"custom_model_keys_enabled"`
	PlatformMcpEnabled                   bool `json:"platform_mcp_enabled"`
	RemoteSessionAutoRefreshEnabled      bool `json:"remote_session_auto_refresh_enabled"`
	SsoEnabled                           bool `json:"sso_enabled"`
	ScimEnabled                          bool `json:"scim_enabled"`
}

type setAdminOrganizationFeatureRequest struct {
	OrganizationID string `json:"organization_id"`
	FeatureName    string `json:"feature_name"`
	Enabled        *bool  `json:"enabled"`
}

var adminOrganizationFeatures = map[string]productfeatures.Feature{
	string(productfeatures.FeatureAuthzChallengeLogging):         productfeatures.FeatureAuthzChallengeLogging,
	string(productfeatures.FeatureCustomerManagedEncryptionKeys): productfeatures.FeatureCustomerManagedEncryptionKeys,
	string(productfeatures.FeatureCustomModelKeys):               productfeatures.FeatureCustomModelKeys,
	string(productfeatures.FeaturePlatformMCP):                   productfeatures.FeaturePlatformMCP,
	string(productfeatures.FeatureRemoteSessionAutoRefresh):      productfeatures.FeatureRemoteSessionAutoRefresh,
	string(productfeatures.FeatureSSO):                           productfeatures.FeatureSSO,
	string(productfeatures.FeatureSCIM):                          productfeatures.FeatureSCIM,
}

func (s *Service) authorizeAdminRequest(r *http.Request) (context.Context, error) {
	scheme := security.APIKeyScheme{
		Name:           constants.AdminAuthSecurityScheme,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}

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

func (s *Service) readAdminOrganizationFeatures(ctx context.Context, organizationID string) adminOrganizationFeaturesResponse {
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

	return adminOrganizationFeaturesResponse{
		AuthzChallengeLoggingEnabled:         readFeature(productfeatures.FeatureAuthzChallengeLogging),
		CustomerManagedEncryptionKeysEnabled: readFeature(productfeatures.FeatureCustomerManagedEncryptionKeys),
		CustomModelKeysEnabled:               readFeature(productfeatures.FeatureCustomModelKeys),
		PlatformMcpEnabled:                   readFeature(productfeatures.FeaturePlatformMCP),
		RemoteSessionAutoRefreshEnabled:      readFeature(productfeatures.FeatureRemoteSessionAutoRefresh),
		SsoEnabled:                           readFeature(productfeatures.FeatureSSO),
		ScimEnabled:                          readFeature(productfeatures.FeatureSCIM),
	}
}

func (s *Service) writeAdminOrganizationFeatures(w http.ResponseWriter, ctx context.Context, organizationID string) error {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.readAdminOrganizationFeatures(ctx, organizationID)); err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode organization features").LogError(ctx, s.logger)
	}
	return nil
}

func (s *Service) handleGetOrganizationFeatures(w http.ResponseWriter, r *http.Request) error {
	ctx, err := s.authorizeAdminRequest(r)
	if err != nil {
		return err
	}
	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, r.URL.Query().Get("organization_id"))
	if err != nil {
		return err
	}
	return s.writeAdminOrganizationFeatures(w, ctx, organizationID)
}

func (s *Service) handleSetOrganizationFeature(w http.ResponseWriter, r *http.Request) error {
	ctx, err := s.authorizeAdminRequest(r)
	if err != nil {
		return err
	}

	var body setAdminOrganizationFeatureRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return oops.E(oops.CodeBadRequest, err, "decode organization feature request")
	}

	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, body.OrganizationID)
	if err != nil {
		return err
	}
	feature, ok := adminOrganizationFeatures[body.FeatureName]
	if !ok || body.Enabled == nil {
		return oops.C(oops.CodeBadRequest)
	}

	if err := s.productFeatures.SetFeatureEnabled(ctx, organizationID, feature, *body.Enabled); err != nil {
		return oops.E(oops.CodeUnexpected, err, "set organization feature").LogError(ctx, s.logger,
			attr.SlogOrganizationID(organizationID),
			attr.SlogProductFeatureName(string(feature)),
		)
	}
	return s.writeAdminOrganizationFeatures(w, ctx, organizationID)
}
