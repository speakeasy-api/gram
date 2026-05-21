package aiintegrations

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestShouldResetUsagePollWatermark(t *testing.T) {
	t.Parallel()

	require.True(t, shouldResetUsagePollWatermark(false, true), "new configs should be due on the next polling tick")
	require.True(t, shouldResetUsagePollWatermark(true, true), "newly supplied API keys should be due on the next polling tick")
	require.False(t, shouldResetUsagePollWatermark(true, false), "settings-only updates should keep the existing watermark")
}

func TestBuildViewUsesLastPollSuccessForLastPolledAt(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ID:                uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		OrganizationID:    "org_123",
		Provider:          ProviderCursor,
		ProjectID:         uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Enabled:           true,
		LastPollSuccessAt: time.Date(2026, 5, 20, 12, 30, 0, 0, time.UTC),
		CreatedAt:         time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC),
		UpdatedAt:         time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC),
	}

	view := buildView(cfg, cfg.ID)

	require.NotNil(t, view.LastPolledAt)
	require.Equal(t, "2026-05-20T12:30:00Z", *view.LastPolledAt)
}

func TestBuildViewOmitsLastPolledAtWithoutSuccessfulPoll(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ID:             uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		OrganizationID: "org_123",
		Provider:       ProviderCursor,
		ProjectID:      uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Enabled:        true,
		CreatedAt:      time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC),
	}

	view := buildView(cfg, cfg.ID)

	require.Nil(t, view.LastPolledAt)
}
