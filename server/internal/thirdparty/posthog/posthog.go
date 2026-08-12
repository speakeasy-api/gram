package posthog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/posthog/posthog-go"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

type Posthog struct {
	client          posthog.Client
	disabled        bool
	localEvaluation bool
	logger          *slog.Logger
}

func New(ctx context.Context, logger *slog.Logger, posthogAPIKey string, posthogEndpoint string, posthogPersonalAPIKey string) *Posthog {
	logger = logger.With(attr.SlogComponent("posthog"))

	if posthogAPIKey == "" {
		logger.InfoContext(ctx, "posthog API key not found, disabling posthog")
		return &Posthog{
			disabled:        true,
			localEvaluation: false,
			logger:          logger,
			client:          nil,
		}
	}

	if posthogEndpoint == "" {
		logger.InfoContext(ctx, "posthog endpoint not found, disabling posthog")
		return &Posthog{
			disabled:        true,
			localEvaluation: false,
			logger:          logger,
			client:          nil,
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
			disabled:        true,
			localEvaluation: false,
			logger:          logger,
			client:          nil,
		}
	}

	return &Posthog{
		client:          client,
		disabled:        false,
		localEvaluation: posthogPersonalAPIKey != "",
		logger:          logger,
	}
}

func (p *Posthog) IsFlagEnabled(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (bool, error) {
	// If posthog is disabled, we return true so we don't block the user from using the product
	if p.disabled {
		p.logger.InfoContext(ctx, "posthog is disabled, returning false")
		return false, nil
	}

	// Forward group memberships so group-targeted flag releases evaluate the
	// same way they do for the dashboard (posthog-js registers these groups).
	var phGroups posthog.Groups
	if len(groups) > 0 {
		phGroups = posthog.NewGroups()
		for groupType, groupKey := range groups {
			phGroups.Set(groupType, groupKey)
		}
	}

	flagState, err := p.client.IsFeatureEnabled(
		posthog.FeatureFlagPayload{
			Key:        string(flag),
			DistinctId: distinctID,
			Groups:     phGroups,
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

func (p *Posthog) EvaluateFlag(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (feature.Evaluation, error) {
	if p == nil || p.disabled || p.client == nil {
		return feature.EvaluationIndeterminate, nil
	}

	if p.localEvaluation {
		return p.evaluateLocalFlag(flag, distinctID, groups)
	}

	result, err := p.client.GetFeatureFlagResult(posthog.FeatureFlagPayload{
		Key:        string(flag),
		DistinctId: distinctID,
		Groups:     posthogGroups(groups),
	})
	if errors.Is(err, posthog.ErrFlagNotFound) {
		return feature.EvaluationIndeterminate, nil
	}
	if err != nil {
		return feature.EvaluationIndeterminate, fmt.Errorf("evaluate feature flag: %w", err)
	}
	if result == nil || result.Variant != nil {
		return feature.EvaluationIndeterminate, nil
	}
	if result.Enabled {
		return feature.EvaluationEnabled, nil
	}
	return feature.EvaluationDisabled, nil
}

// FlagVariant returns the variant key a multivariate flag resolves to for
// distinctID, or "" when posthog is disabled, the flag is off, the flag does
// not exist, or it is a boolean flag (no variant). Callers map "" to their own
// fail-safe default.
func (p *Posthog) FlagVariant(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (feature.Variant, error) {
	if p == nil || p.disabled || p.client == nil {
		p.logger.InfoContext(ctx, "posthog is disabled, returning no variant")
		return "", nil
	}

	result, err := p.client.GetFeatureFlagResult(posthog.FeatureFlagPayload{
		Key:        string(flag),
		DistinctId: distinctID,
		Groups:     posthogGroups(groups),
	})
	if errors.Is(err, posthog.ErrFlagNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get feature flag variant: %w", err)
	}
	if result == nil || !result.Enabled || result.Variant == nil {
		return "", nil
	}

	return feature.Variant(*result.Variant), nil
}

func (p *Posthog) evaluateLocalFlag(flag feature.Flag, distinctID string, groups map[string]string) (feature.Evaluation, error) {
	sendFeatureFlagEvents := false
	flags, err := p.client.GetAllFlags(posthog.FeatureFlagPayloadNoKey{
		DistinctId:            distinctID,
		Groups:                posthogGroups(groups),
		OnlyEvaluateLocally:   false,
		SendFeatureFlagEvents: &sendFeatureFlagEvents,
	})
	if err != nil {
		return feature.EvaluationIndeterminate, fmt.Errorf("evaluate local feature flags: %w", err)
	}

	value, ok := flags[string(flag)]
	if !ok {
		return feature.EvaluationIndeterminate, nil
	}
	if enabled, ok := value.(bool); ok {
		if enabled {
			return feature.EvaluationEnabled, nil
		}
		return feature.EvaluationDisabled, nil
	}
	return feature.EvaluationIndeterminate, nil
}

func (p *Posthog) IsFlagEnabledLocal(ctx context.Context, flag feature.Flag, distinctID string, groups, personProperties map[string]string) (bool, error) {
	if p.disabled {
		p.logger.InfoContext(ctx, "posthog is disabled, returning false")
		return false, nil
	}
	if !p.localEvaluation {
		return false, nil
	}

	var phPersonProperties posthog.Properties
	if len(personProperties) > 0 {
		phPersonProperties = posthog.NewProperties()
		for key, value := range personProperties {
			phPersonProperties.Set(key, value)
		}
	}
	sendFeatureFlagEvents := false
	flagState, err := p.client.IsFeatureEnabled(posthog.FeatureFlagPayload{
		Key:                   string(flag),
		DistinctId:            distinctID,
		Groups:                posthogGroups(groups),
		PersonProperties:      phPersonProperties,
		OnlyEvaluateLocally:   true,
		SendFeatureFlagEvents: &sendFeatureFlagEvents,
	})
	if err != nil {
		var inconclusive *posthog.InconclusiveMatchError
		if errors.As(err, &inconclusive) || errors.Is(err, posthog.ErrFlagNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("failed to locally check feature flag: %w", err)
	}

	j, err := json.Marshal(flagState)
	if err != nil {
		return false, fmt.Errorf("failed to unmarshal feature flag: %w", err)
	}
	return string(j) == "true", nil
}

func (p *Posthog) FlagPayload(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) ([]byte, error) {
	// When posthog is disabled we have no clearance data. Return nil so callers
	// fall back to their fail-closed default (e.g. carry the current version).
	if p.disabled {
		return nil, nil
	}

	var phGroups posthog.Groups
	if len(groups) > 0 {
		phGroups = posthog.NewGroups()
		for groupType, groupKey := range groups {
			phGroups.Set(groupType, groupKey)
		}
	}

	// GetFeatureFlagPayload returns the JSON payload attached to the matched
	// release as a string, or "" when the flag is off / has no payload / does
	// not exist. Treat "" as "no clearance".
	payload, err := p.client.GetFeatureFlagPayload(posthog.FeatureFlagPayload{
		Key:        string(flag),
		DistinctId: distinctID,
		Groups:     phGroups,
	})
	if err != nil {
		return nil, fmt.Errorf("get feature flag payload: %w", err)
	}
	if payload == "" {
		return nil, nil
	}

	return []byte(payload), nil
}

func posthogGroups(groups map[string]string) posthog.Groups {
	var phGroups posthog.Groups
	if len(groups) > 0 {
		phGroups = posthog.NewGroups()
		for groupType, groupKey := range groups {
			phGroups.Set(groupType, groupKey)
		}
	}
	return phGroups
}

func (p *Posthog) IdentifyUser(ctx context.Context, distinctID string, personProperties map[string]any) error {
	if p.disabled {
		p.logger.InfoContext(ctx, "posthog is disabled, dropping identify")
		return nil
	}

	properties := posthog.NewProperties()
	for k, v := range personProperties {
		properties.Set(k, v)
	}

	if err := p.client.Enqueue(posthog.Identify{
		DistinctId: distinctID,
		Properties: properties,
	}); err != nil {
		return fmt.Errorf("failed to enqueue identify: %w", err)
	}

	return nil
}

// GroupIdentify sets properties on a PostHog group (e.g. the "organization"
// group), enabling group-based cohorts and dashboards. Last write wins per
// property, so callers can refresh values idempotently. The group key must
// match the convention the capture paths use — the organization group is
// keyed by org SLUG (see CaptureEvent), so a mismatched key would fork the
// group into two identities.
func (p *Posthog) GroupIdentify(ctx context.Context, groupType string, groupKey string, groupProperties map[string]any) error {
	if p.disabled {
		p.logger.InfoContext(ctx, "posthog is disabled, dropping group identify")
		return nil
	}

	properties := posthog.NewProperties()
	for k, v := range groupProperties {
		properties.Set(k, v)
	}

	if err := p.client.Enqueue(posthog.GroupIdentify{
		Type:       groupType,
		Key:        groupKey,
		Properties: properties,
	}); err != nil {
		return fmt.Errorf("failed to enqueue group identify: %w", err)
	}

	return nil
}

// CaptureGroupEvent captures an event with explicit group memberships, for
// server-initiated events emitted outside any request auth context (e.g.
// background workers reporting per-organization metrics). No person profile
// is created for the distinct id — these events describe the group, not a
// user.
func (p *Posthog) CaptureGroupEvent(ctx context.Context, eventName string, distinctID string, groups map[string]string, eventProperties map[string]any) error {
	if p.disabled {
		p.logger.InfoContext(ctx, "posthog is disabled, dropping event")
		return nil
	}

	phGroups := map[string]any{}
	for groupType, groupKey := range groups {
		phGroups[groupType] = groupKey
	}

	properties := posthog.NewProperties().
		Set("is_gram", true).
		Set("$process_person_profile", false)
	for k, v := range eventProperties {
		properties.Set(k, v)
	}

	if err := p.client.Enqueue(posthog.Capture{
		DistinctId: distinctID,
		Event:      eventName,
		Properties: properties,
		Groups:     phGroups,
	}); err != nil {
		return fmt.Errorf("failed to enqueue event: %w", err)
	}

	return nil
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
