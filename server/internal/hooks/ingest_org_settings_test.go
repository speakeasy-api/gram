package hooks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// staticFeatures stubs the product features client with a fixed fail-open
// answer; every other feature reads as disabled.
type staticFeatures struct{ failOpen bool }

func (s staticFeatures) IsFeatureEnabled(_ context.Context, _ string, feature productfeatures.Feature) (bool, error) {
	return feature == productfeatures.FeatureHooksFailOpen && s.failOpen, nil
}

func TestIngest_OrgSettingsEffectsCarryFailOpenEnabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = staticFeatures{failOpen: true}

	result, err := ti.service.Ingest(ctx, canonicalIngestPayload("claude", "session.started", "org-settings-enabled"))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "allow", result.Decision)

	settings, ok := result.Effects["org_settings"].(map[string]any)
	require.True(t, ok, "authenticated ingest must carry org_settings effects")
	require.Equal(t, true, settings["fail_open"])
}

// TestIngest_OrgSettingsEffectsCarryFailOpenDisabled: the boolean is present
// even when false so a sender's stale cached `true` converges back.
func TestIngest_OrgSettingsEffectsCarryFailOpenDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = staticFeatures{failOpen: false}

	result, err := ti.service.Ingest(ctx, canonicalIngestPayload("claude", "session.started", "org-settings-disabled"))
	require.NoError(t, err)
	require.NotNil(t, result)

	settings, ok := result.Effects["org_settings"].(map[string]any)
	require.True(t, ok, "the setting must be present even when disabled")
	require.Equal(t, false, settings["fail_open"])
}

// TestIngest_KeylessCarriesNoOrgSettings: without credentials there is no org
// to resolve the setting for, so the acknowledgment carries no effects.
func TestIngest_KeylessCarriesNoOrgSettings(t *testing.T) {
	t.Parallel()

	_, ti := newTestHooksService(t)
	ti.service.productFeatures = staticFeatures{failOpen: true}

	result, err := ti.service.Ingest(t.Context(), canonicalIngestPayload("claude", "session.started", "org-settings-keyless"))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "allow", result.Decision)
	require.Nil(t, result.Effects)
}
