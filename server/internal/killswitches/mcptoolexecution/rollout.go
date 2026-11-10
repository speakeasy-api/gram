package mcptoolexecution

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
)

// RolloutMode controls whether an organization skips evaluation, observes it
// without transport enforcement, or enforces the authoritative result.
type RolloutMode uint8

const (
	RolloutModeOff RolloutMode = iota
	RolloutModeShadow
	RolloutModeEnforce
)

// ResolveRolloutMode uses only locally cached feature-flag state so the serving
// path never adds a remote feature-provider dependency. Missing or disabled
// flags resolve to off. Enforce wins if both flags are enabled.
func ResolveRolloutMode(ctx context.Context, flags feature.Provider, organizationID string) (RolloutMode, error) {
	if flags == nil {
		return RolloutModeOff, nil
	}

	enforce, err := flags.IsFlagEnabledLocal(ctx, feature.FlagMCPKillswitchEnforce, organizationID, nil, nil)
	if err != nil {
		return RolloutModeOff, fmt.Errorf("resolve MCP killswitch enforce flag: %w", err)
	}
	if enforce {
		return RolloutModeEnforce, nil
	}

	shadow, err := flags.IsFlagEnabledLocal(ctx, feature.FlagMCPKillswitchShadow, organizationID, nil, nil)
	if err != nil {
		return RolloutModeOff, fmt.Errorf("resolve MCP killswitch shadow flag: %w", err)
	}
	if shadow {
		return RolloutModeShadow, nil
	}

	return RolloutModeOff, nil
}

func evaluateForRollout(
	ctx context.Context,
	flags feature.Provider,
	organizationID string,
	evaluate func() (killswitches.TransportDisposition, error),
) (killswitches.TransportDisposition, error) {
	mode, err := ResolveRolloutMode(ctx, flags, organizationID)
	if err != nil {
		return killswitches.NewInfrastructureRejectionDisposition(), fmt.Errorf("resolve MCP killswitch rollout mode: %w", err)
	}
	if mode == RolloutModeOff {
		return killswitches.NewContinueDisposition(), nil
	}

	disposition, evaluationErr := evaluate()
	if mode == RolloutModeShadow {
		return killswitches.NewContinueDisposition(), nil
	}
	return disposition, evaluationErr
}
