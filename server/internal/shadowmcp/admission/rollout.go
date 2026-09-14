package admission

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

// Mode is the closed set of Shadow MCP distribution rollout behaviours.
type Mode string

const (
	// ModeLegacy preserves distribution behaviour from before Shadow MCP
	// admission was introduced.
	ModeLegacy Mode = "legacy"
	// ModeReport evaluates and records the admission result without refusing the
	// distribution write.
	ModeReport Mode = "report"
	// ModeEnforce evaluates admission and refuses a distribution write that is
	// not covered.
	ModeEnforce Mode = "enforce"
)

// RolloutConfig is the complete rollout decision shared by every distribution
// writer. Callers must not use the config when ResolveRollout returns an error.
type RolloutConfig struct {
	Mode                             Mode
	DirectRemoteDistributionDisabled bool
}

// RolloutUnavailableError reports that a rollout flag did not produce an
// authoritative closed decision. Flag identifies the unavailable control and
// Cause retains provider or payload details for logs.
type RolloutUnavailableError struct {
	Flag  feature.Flag
	Cause error
}

func (e *RolloutUnavailableError) Error() string {
	if e == nil {
		return "Shadow MCP distribution rollout unavailable"
	}
	if e.Cause == nil {
		return fmt.Sprintf("Shadow MCP distribution rollout flag %q unavailable", e.Flag)
	}
	return fmt.Sprintf("Shadow MCP distribution rollout flag %q unavailable: %v", e.Flag, e.Cause)
}

func (e *RolloutUnavailableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ResolveRollout resolves the project-scoped Shadow MCP distribution controls.
// The organization ID is the PostHog distinct ID; org and project slugs are
// supplied as the registered group memberships. Only an explicit disabled mode
// flag selects legacy behaviour. Every indeterminate state fails closed with a
// RolloutUnavailableError.
func ResolveRollout(ctx context.Context, provider feature.Provider, organizationID, orgSlug, projectSlug string) (RolloutConfig, error) {
	if organizationID == "" || orgSlug == "" || projectSlug == "" {
		return RolloutConfig{}, rolloutUnavailable(feature.FlagPlatformMCPShadowAudienceEnforcement, errors.New("project rollout identity is incomplete"))
	}
	groups := feature.OrgProjectGroups(orgSlug, projectSlug)

	modeEvaluation, err := feature.EvaluateFlag(ctx, provider, feature.FlagPlatformMCPShadowAudienceEnforcement, organizationID, groups)
	if err != nil {
		return RolloutConfig{}, rolloutUnavailable(feature.FlagPlatformMCPShadowAudienceEnforcement, err)
	}

	var mode Mode
	switch modeEvaluation {
	case feature.EvaluationDisabled:
		mode = ModeLegacy
	case feature.EvaluationEnabled:
		payload, payloadErr := provider.FlagPayload(ctx, feature.FlagPlatformMCPShadowAudienceEnforcement, organizationID, groups)
		if payloadErr != nil {
			return RolloutConfig{}, rolloutUnavailable(feature.FlagPlatformMCPShadowAudienceEnforcement, fmt.Errorf("resolve mode payload: %w", payloadErr))
		}
		mode, err = decodeRolloutMode(payload)
		if err != nil {
			return RolloutConfig{}, rolloutUnavailable(feature.FlagPlatformMCPShadowAudienceEnforcement, err)
		}
	case feature.EvaluationIndeterminate:
		return RolloutConfig{}, rolloutUnavailable(feature.FlagPlatformMCPShadowAudienceEnforcement, errors.New("mode evaluation is indeterminate"))
	default:
		return RolloutConfig{}, rolloutUnavailable(feature.FlagPlatformMCPShadowAudienceEnforcement, fmt.Errorf("unknown mode evaluation %d", modeEvaluation))
	}

	killEvaluation, err := feature.EvaluateFlag(ctx, provider, feature.FlagPlatformMCPDirectRemoteDistributionDisabled, organizationID, groups)
	if err != nil {
		return RolloutConfig{}, rolloutUnavailable(feature.FlagPlatformMCPDirectRemoteDistributionDisabled, err)
	}

	var directRemoteDistributionDisabled bool
	switch killEvaluation {
	case feature.EvaluationDisabled:
		directRemoteDistributionDisabled = false
	case feature.EvaluationEnabled:
		directRemoteDistributionDisabled = true
	case feature.EvaluationIndeterminate:
		return RolloutConfig{}, rolloutUnavailable(feature.FlagPlatformMCPDirectRemoteDistributionDisabled, errors.New("kill-switch evaluation is indeterminate"))
	default:
		return RolloutConfig{}, rolloutUnavailable(feature.FlagPlatformMCPDirectRemoteDistributionDisabled, fmt.Errorf("unknown kill-switch evaluation %d", killEvaluation))
	}

	return RolloutConfig{Mode: mode, DirectRemoteDistributionDisabled: directRemoteDistributionDisabled}, nil
}

func decodeRolloutMode(payload []byte) (Mode, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return "", errors.New("mode payload must be an object")
	}
	key, err := decoder.Token()
	if err != nil || key != "mode" {
		return "", errors.New("mode payload requires exactly one key named mode")
	}
	var mode Mode
	if err := decoder.Decode(&mode); err != nil {
		return "", fmt.Errorf("decode mode payload: %w", err)
	}
	if decoder.More() {
		return "", errors.New("mode payload requires exactly one key named mode")
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return "", errors.New("mode payload has an invalid closing delimiter")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return "", errors.New("decode mode payload: multiple JSON values")
		}
		return "", fmt.Errorf("decode mode payload trailer: %w", err)
	}

	switch mode {
	case ModeReport, ModeEnforce:
		return mode, nil
	case "":
		return "", errors.New("mode payload is missing mode")
	default:
		return "", fmt.Errorf("mode payload selects unknown mode %q", mode)
	}
}

func rolloutUnavailable(flag feature.Flag, cause error) error {
	return &RolloutUnavailableError{Flag: flag, Cause: cause}
}
