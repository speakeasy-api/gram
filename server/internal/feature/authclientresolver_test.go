package feature_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

func TestAuthClientResolverMode(t *testing.T) {
	t.Parallel()

	flags := new(feature.InMemory)
	require.Equal(t, feature.AuthClientResolverOff, feature.AuthClientResolver(t.Context(), flags, "org_a"))
	require.Equal(t, feature.AuthClientResolverOff, feature.AuthClientResolver(t.Context(), nil, "org_a"))

	flags.SetFlag(feature.FlagAuthClientResolverShadow, "org_a", true)
	require.Equal(t, feature.AuthClientResolverShadow, feature.AuthClientResolver(t.Context(), flags, "org_a"))
	require.Equal(t, feature.AuthClientResolverOff, feature.AuthClientResolver(t.Context(), flags, "org_b"))

	flags.SetFlag(feature.FlagAuthClientResolver, "org_a", true)
	require.Equal(t, feature.AuthClientResolverEnforced, feature.AuthClientResolver(t.Context(), flags, "org_a"), "enforcement wins over shadow")
	require.Equal(t, feature.AuthClientResolverOff, feature.AuthClientResolver(t.Context(), flags, ""))
}

// enforcedErrors fails the enforced flag lookup while the shadow flag is on.
type enforcedErrors struct{ feature.InMemory }

func (e *enforcedErrors) IsFlagEnabledLocal(ctx context.Context, flag feature.Flag, distinctID string, groups, personProperties map[string]string) (bool, error) {
	if flag == feature.FlagAuthClientResolver {
		return false, errors.New("flag definitions unavailable")
	}
	enabled, err := e.InMemory.IsFlagEnabledLocal(ctx, flag, distinctID, groups, personProperties)
	if err != nil {
		return false, fmt.Errorf("in-memory flag: %w", err)
	}
	return enabled, nil
}

func TestAuthClientResolverModeOffWhenEnforcedLookupFails(t *testing.T) {
	t.Parallel()

	flags := new(enforcedErrors)
	flags.SetFlag(feature.FlagAuthClientResolverShadow, "org_a", true)
	require.Equal(t, feature.AuthClientResolverOff, feature.AuthClientResolver(t.Context(), flags, "org_a"), "an indeterminate rollout runs nothing")
}
