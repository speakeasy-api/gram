package plugins

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/hooksrollout"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// staticPins serves fixed rollout pins, or err when set.
type staticPins struct {
	pins hooksrollout.OrganizationPins
	err  error
}

func (s staticPins) OrganizationPins(context.Context, string) (hooksrollout.OrganizationPins, error) {
	return s.pins, s.err
}

// eligibilityService builds a publisher wired to pins and features but with no
// database: hooksRolloutEligible touches neither, so nil deps are safe here.
func eligibilityService(t *testing.T, pins hooksrollout.PinReader, features feature.Provider) *Service {
	t.Helper()
	svc := NewPublisher(testenv.NewLogger(t), nil, nil, nil, "local", "", features)
	svc.hooksPins = pins
	return svc
}

func pinAt(version int) *hooksrollout.Pin {
	return &hooksrollout.Pin{Version: version, SetBy: "operator@example.com", SetAt: time.Unix(0, 0)}
}

func currentVersion(t *testing.T) int {
	t.Helper()
	current, err := CurrentHooksGeneratorVersion()
	require.NoError(t, err)
	return current
}

func TestHooksRolloutEligible_CanaryBypassesPinsAndProvider(t *testing.T) {
	t.Parallel()

	// A failing pin reader and a nil provider prove the canary decision reads
	// neither: a canary org is eligible even when both are unavailable.
	svc := eligibilityService(t, staticPins{err: errors.New("database down")}, nil)
	for _, slug := range hooksrollout.CanaryOrganizationSlugs() {
		require.True(t, svc.hooksRolloutEligible(t.Context(), "org-any", slug), "canary slug %q must be eligible", slug)
	}
}

func TestHooksRolloutEligible_PinReadErrorFailsClosed(t *testing.T) {
	t.Parallel()

	current := currentVersion(t)
	features := &feature.InMemory{}
	features.SetFlagPayload(feature.FlagHooksRollout, "org-1", fmt.Appendf(nil, `{"version": %d}`, current))

	svc := eligibilityService(t, staticPins{err: errors.New("database down")}, features)
	require.False(t, svc.hooksRolloutEligible(t.Context(), "org-1", "not-canary"), "a pin read error must not fall through to the legacy flag")
}

func TestHooksRolloutEligible_NilPinReaderFailsClosed(t *testing.T) {
	t.Parallel()

	svc := eligibilityService(t, nil, nil)
	require.False(t, svc.hooksRolloutEligible(t.Context(), "org-1", "not-canary"))
}

func TestHooksRolloutEligible_DefaultPin(t *testing.T) {
	t.Parallel()

	current := currentVersion(t)
	for _, tc := range []struct {
		version  int
		eligible bool
	}{
		{version: current - 1, eligible: false},
		{version: current, eligible: true},
		{version: current + 1, eligible: true},
	} {
		svc := eligibilityService(t, staticPins{pins: hooksrollout.OrganizationPins{Override: nil, Default: pinAt(tc.version)}}, nil)
		require.Equal(t, tc.eligible, svc.hooksRolloutEligible(t.Context(), "org-1", "not-canary"), "default pin %d against current %d", tc.version, current)
	}
}

func TestHooksRolloutEligible_OverrideWinsOverDefault(t *testing.T) {
	t.Parallel()

	current := currentVersion(t)

	held := eligibilityService(t, staticPins{pins: hooksrollout.OrganizationPins{Override: pinAt(current - 1), Default: pinAt(current)}}, nil)
	require.False(t, held.hooksRolloutEligible(t.Context(), "org-1", "not-canary"), "an override below current holds the org back past the default")

	early := eligibilityService(t, staticPins{pins: hooksrollout.OrganizationPins{Override: pinAt(current), Default: pinAt(current - 1)}}, nil)
	require.True(t, early.hooksRolloutEligible(t.Context(), "org-1", "not-canary"), "an override at current rolls the org ahead of the default")
}

func TestHooksRolloutEligible_PinIgnoresLegacyFlag(t *testing.T) {
	t.Parallel()

	current := currentVersion(t)
	features := &feature.InMemory{}
	features.SetFlagPayload(feature.FlagHooksRollout, "org-1", fmt.Appendf(nil, `{"version": %d}`, current))

	svc := eligibilityService(t, staticPins{pins: hooksrollout.OrganizationPins{Override: nil, Default: pinAt(current - 1)}}, features)
	require.False(t, svc.hooksRolloutEligible(t.Context(), "org-1", "not-canary"), "once a default pin exists the legacy flag no longer decides")
}

func TestHooksRolloutEligible_LegacyFlagWithoutPins(t *testing.T) {
	t.Parallel()

	current := currentVersion(t)
	for _, tc := range []struct {
		version  int
		eligible bool
	}{
		{version: current - 1, eligible: false},
		{version: current, eligible: true},
		{version: current + 100, eligible: true},
	} {
		features := &feature.InMemory{}
		features.SetFlagPayload(feature.FlagHooksRollout, "org-1", fmt.Appendf(nil, `{"version": %d}`, tc.version))
		svc := eligibilityService(t, staticPins{}, features)
		require.Equal(t, tc.eligible, svc.hooksRolloutEligible(t.Context(), "org-1", "not-canary"), "legacy pin %d against current %d", tc.version, current)
	}
}

func TestHooksRolloutEligible_LegacyFlagFailsClosed(t *testing.T) {
	t.Parallel()

	nilProvider := eligibilityService(t, staticPins{}, nil)
	require.False(t, nilProvider.hooksRolloutEligible(t.Context(), "org-1", "not-canary"))

	noPayload := eligibilityService(t, staticPins{}, &feature.InMemory{})
	require.False(t, noPayload.hooksRolloutEligible(t.Context(), "org-nopayload", "not-canary"))

	malformed := &feature.InMemory{}
	malformed.SetFlagPayload(feature.FlagHooksRollout, "org-bad", []byte(`not json`))
	malformedPayload := eligibilityService(t, staticPins{}, malformed)
	require.False(t, malformedPayload.hooksRolloutEligible(t.Context(), "org-bad", "not-canary"))
}
