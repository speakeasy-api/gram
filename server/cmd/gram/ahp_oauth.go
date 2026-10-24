package gram

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
)

// AHP is available to any live member with a project grant, independently of
// the Platform MCP feature gate. Project admission is enforced separately.
type ahpOAuthGate struct{}

func (ahpOAuthGate) Enabled(context.Context, string) (bool, error) { return true, nil }

func configureAHPOAuth(config platformMCPConfig) error {
	authorizer := platformmcp.NewLiveOrgAdminAuthorizer(config.DB, config.Authz)
	projects := platformmcp.NewAHPProjects(config.DB, config.Authz)
	store := platformmcp.NewPostgresAHPOAuthStore(config.DB)
	signer := sessiontokens.NewSigner(config.JWTSigningKey)
	service, err := platformmcp.NewOAuthHTTP(platformmcp.OAuthHTTPConfig{
		BaseURL:            config.ServerURL,
		Environment:        config.Environment,
		Cache:              cache.NewRedisCacheAdapter(config.Redis),
		Store:              store,
		Identity:           config.Identity,
		Gate:               ahpOAuthGate{},
		Authorizer:         authorizer,
		Organizations:      platformmcp.NewLiveOrganizationSelector(config.DB, authorizer),
		Workspaces:         nil,
		Signer:             signer,
		Encryption:         config.Encryption,
		Telemetry:          nil,
		Logger:             config.Logger,
		GuardianPolicy:     config.GuardianPolicy,
		MeterProvider:      config.MeterProvider,
		IDPCallbackBaseURL: config.OutboundCallbackOrigin,
		AHPProjects:        projects,
	})
	if err != nil {
		return fmt.Errorf("configure AHP OAuth: %w", err)
	}
	authenticator, err := platformmcp.NewAHPAuthenticator(signer, store, config.Encryption, config.ServerURL, projects)
	if err != nil {
		return fmt.Errorf("configure AHP authenticator: %w", err)
	}
	config.Hooks.WithAHPOAuth(authenticator, config.ServerURL)
	service.Attach(config.Mux)
	return nil
}
