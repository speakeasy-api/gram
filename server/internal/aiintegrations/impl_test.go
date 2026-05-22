package aiintegrations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeUsagePollStarter struct {
	calls     []fakeUsagePollStarterCall
	returnErr error
}

type fakeUsagePollStarterCall struct {
	organizationSlug string
	configID         uuid.UUID
	provider         string
}

func (f *fakeUsagePollStarter) Poll(_ context.Context, organizationSlug string, configID uuid.UUID, provider string) error {
	f.calls = append(f.calls, fakeUsagePollStarterCall{
		organizationSlug: organizationSlug,
		configID:         configID,
		provider:         provider,
	})
	return f.returnErr
}

func TestShouldResetUsagePollWatermark(t *testing.T) {
	t.Parallel()

	require.True(t, shouldResetUsagePollWatermark(false, true), "new configs should be due on the next polling tick")
	require.True(t, shouldResetUsagePollWatermark(true, true), "newly supplied API keys should be due on the next polling tick")
	require.False(t, shouldResetUsagePollWatermark(true, false), "settings-only updates should keep the existing watermark")
}

func TestStartUsagePollDelegatesToStarter(t *testing.T) {
	t.Parallel()

	configID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	starter := &fakeUsagePollStarter{}
	svc := &Service{usagePollStarter: starter}

	require.NoError(t, svc.startUsagePoll(t.Context(), "acme", configID, ProviderCursor))
	require.Equal(t, []fakeUsagePollStarterCall{{
		organizationSlug: "acme",
		configID:         configID,
		provider:         ProviderCursor,
	}}, starter.calls)
}

func TestStartUsagePollAllowsMissingStarter(t *testing.T) {
	t.Parallel()

	svc := &Service{}

	require.NoError(t, svc.startUsagePoll(t.Context(), "acme", uuid.MustParse("11111111-1111-1111-1111-111111111111"), ProviderCursor))
}

func TestStartUsagePollReturnsStarterError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("temporal unavailable")
	svc := &Service{usagePollStarter: &fakeUsagePollStarter{returnErr: expectedErr}}

	require.ErrorIs(t, svc.startUsagePoll(t.Context(), "acme", uuid.MustParse("11111111-1111-1111-1111-111111111111"), ProviderCursor), expectedErr)
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
	require.NotNil(t, view.LastPollStatus)
	require.Equal(t, "success", *view.LastPollStatus)
}

func TestBuildViewShowsPendingWithoutPollResult(t *testing.T) {
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
	require.NotNil(t, view.LastPollStatus)
	require.Equal(t, "pending", *view.LastPollStatus)
	require.Nil(t, view.LastPollError)
	require.Nil(t, view.LastPollFailedAt)
}

func TestBuildViewShowsFailedPollState(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ID:                uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		OrganizationID:    "org_123",
		Provider:          ProviderCursor,
		ProjectID:         uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Enabled:           true,
		LastPollError:     "cursor rejected the configured api key",
		LastPollFailedAt:  time.Date(2026, 5, 20, 12, 45, 0, 0, time.UTC),
		LastPollSuccessAt: time.Date(2026, 5, 20, 12, 30, 0, 0, time.UTC),
		CreatedAt:         time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC),
		UpdatedAt:         time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC),
	}

	view := buildView(cfg, cfg.ID)

	require.NotNil(t, view.LastPollStatus)
	require.Equal(t, "failed", *view.LastPollStatus)
	require.NotNil(t, view.LastPollError)
	require.Equal(t, "cursor rejected the configured api key", *view.LastPollError)
	require.NotNil(t, view.LastPollFailedAt)
	require.Equal(t, "2026-05-20T12:45:00Z", *view.LastPollFailedAt)
}
