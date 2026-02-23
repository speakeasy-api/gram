package posthog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/posthog/posthog-go"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

type Posthog struct {
	client   posthog.Client
	disabled bool
	logger   *slog.Logger
}

func New(ctx context.Context, logger *slog.Logger, posthogAPIKey string, posthogEndpoint string, posthogPersonalAPIKey string) *Posthog {
	if posthogAPIKey == "" {
		logger.InfoContext(ctx, "posthog API key not found, disabling posthog")
		return &Posthog{
			disabled: true,
			logger:   logger,
			client:   nil,
		}
	}

	if posthogEndpoint == "" {
		logger.InfoContext(ctx, "posthog endpoint not found, disabling posthog")
		return &Posthog{
			disabled: true,
			logger:   logger,
			client:   nil,
		}
	}

	phConfig := posthog.Config{
		Endpoint: posthogEndpoint,
	}

	// Having a personal (private) API key allow posthog to maintain its own state of feature flags via polling
	if posthogPersonalAPIKey != "" {
		phConfig.PersonalApiKey = posthogPersonalAPIKey
		phConfig.DefaultFeatureFlagsPollingInterval = 1 * time.Minute
	}

	client, err := posthog.NewWithConfig(
		posthogAPIKey,
		phConfig,
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to instantiate posthog client", attr.SlogError(err))
		return &Posthog{
			disabled: true,
			logger:   logger,
			client:   nil,
		}
	}

	return &Posthog{
		client:   client,
		disabled: false,
		logger:   logger,
	}
}

func (p *Posthog) IsFlagEnabled(ctx context.Context, flag feature.Flag, distinctID string) (bool, error) {
	// If posthog is disabled, we return true so we don't block the user from using the product
	if p.disabled {
		p.logger.InfoContext(ctx, "posthog is disabled, returning false")
		return false, nil
	}

	flagState, err := p.client.IsFeatureEnabled(
		posthog.FeatureFlagPayload{
			Key:        string(flag),
			DistinctId: distinctID,
		})
	if err != nil {
		return false, fmt.Errorf("failed to check feature flag: %w", err)
	}

	// The posthog client returns interface{} for some reason so we need to convert it to a bool
	j, err := json.Marshal(flagState)
	if err != nil {
		return false, fmt.Errorf("failed to unmarshal feature flag: %w", err)
	}

	// Convert JSON string to bool
	return string(j) == "true", nil
}

func (p *Posthog) CaptureEvent(ctx context.Context, eventName string, distinctID string, eventProperties map[string]any) error {
	// If posthog is disabled, we return true so we don't block the user from using the product
	if p.disabled {
		p.logger.InfoContext(ctx, "posthog is disabled, dropping event")
		return nil
	}

	authCtx, _ := contextvalues.GetAuthContext(ctx)

	groups := map[string]any{}
	properties := posthog.NewProperties().
		Set("start_time", time.Now()).
		Set("is_gram", true)

	// Add auth context properties if available
	if authCtx != nil {
		if authCtx.ActiveOrganizationID != "" {
			properties.Set("organization_id", authCtx.ActiveOrganizationID)
		}
		if authCtx.OrganizationSlug != "" {
			groups["organization"] = authCtx.OrganizationSlug
			properties.Set("organization_slug", authCtx.OrganizationSlug)
		}
		if authCtx.ProjectSlug != nil {
			properties.Set("project_slug", *authCtx.ProjectSlug)
			if authCtx.OrganizationSlug != "" {
				groups["slug"] = authCtx.OrganizationSlug + "/" + *authCtx.ProjectSlug
			}
		}
		if authCtx.Email != nil {
			properties.Set("email", *authCtx.Email)
		}
		if authCtx.ExternalUserID != "" {
			properties.Set("external_user_id", authCtx.ExternalUserID)
		}
	}

	// Add custom event properties
	for k, v := range eventProperties {
		properties.Set(k, v)
	}

	if err := p.client.Enqueue(posthog.Capture{
		DistinctId: distinctID,
		Event:      eventName,
		Properties: properties,
		Groups:     groups,
	}); err != nil {
		return fmt.Errorf("failed to enqueue event: %w", err)
	}

	return nil
}
