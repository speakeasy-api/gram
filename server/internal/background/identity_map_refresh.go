package background

// Link-event refresh for the identity map. The scheduled sync bounds staleness
// at its interval; this signaler tightens it for the moments that matter — an
// account link attributed during ingest, a directory membership change — by
// triggering the schedule immediately. Triggers go through the schedule so its
// Overlap SKIP policy makes concurrent requests safe, and a per-process
// throttle coalesces chatty ingest (every attributed session upserts its
// account link) into at most one leading and one trailing trigger per
// cooldown window.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"

	"github.com/speakeasy-api/gram/server/internal/attr"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/throttle"
)

// identityMapRefreshCooldown bounds trigger frequency well below the 15m
// schedule interval while staying far above the sync's cost (a sub-second
// full rebuild).
const identityMapRefreshCooldown = 2 * time.Minute

type IdentityMapRefreshSignaler struct {
	temporalEnv *tenv.Environment
	logger      *slog.Logger
	throttle    *throttle.Throttle[struct{}, struct{}]
}

func NewIdentityMapRefreshSignaler(temporalEnv *tenv.Environment, logger *slog.Logger) *IdentityMapRefreshSignaler {
	s := &IdentityMapRefreshSignaler{
		temporalEnv: temporalEnv,
		logger:      logger.With(attr.SlogComponent("identity_map_refresh")),
		throttle:    nil,
	}
	s.throttle = throttle.New(identityMapRefreshCooldown, func(struct{}) struct{} {
		return struct{}{}
	}, func(struct{}) error {
		if err := s.trigger(context.Background()); err != nil {
			s.logger.ErrorContext(context.Background(), "trailing identity map refresh failed", attr.SlogError(err))
			return fmt.Errorf("trailing identity map refresh: %w", err)
		}
		return nil
	})
	return s
}

// SignalIdentityMapRefresh requests an immediate sync. The first request
// fires; requests within the cooldown coalesce into one trailing trigger. A
// failed trigger is logged and dropped — the schedule delivers the refresh at
// its next tick regardless.
func (s *IdentityMapRefreshSignaler) SignalIdentityMapRefresh(ctx context.Context) error {
	if !s.throttle.Do(struct{}{}) {
		return nil
	}
	if err := s.trigger(ctx); err != nil {
		return fmt.Errorf("identity map refresh trigger: %w", err)
	}
	return nil
}

func (s *IdentityMapRefreshSignaler) trigger(ctx context.Context) error {
	err := s.temporalEnv.Client().ScheduleClient().GetHandle(ctx, identityMapSyncScheduleID).Trigger(ctx, client.ScheduleTriggerOptions{
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	if err != nil {
		return fmt.Errorf("trigger identity map sync schedule: %w", err)
	}
	return nil
}

// Shutdown flushes a pending trailing trigger. Call during graceful shutdown.
func (s *IdentityMapRefreshSignaler) Shutdown(_ context.Context) error {
	s.throttle.Flush()
	return nil
}
