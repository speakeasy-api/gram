package admin

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// HookClientsReader reads the fleet-wide hook client grids. Implemented by
// telemetry.HookClients, which owns the ClickHouse reads.
type HookClientsReader interface {
	Sessions(ctx context.Context, windowDays int) ([]telemetry.HookClientSessionRow, error)
	InferenceTraffic(ctx context.Context, windowDays int) ([]telemetry.InferenceHookTrafficRow, error)
}

// SetHookClients enables getHookClients. Nil leaves it unavailable.
func (s *Service) SetHookClients(reader HookClientsReader) { s.hookClients = reader }

func (s *Service) GetHookClients(ctx context.Context, payload *gen.GetHookClientsPayload) (*gen.AdminHookClientsResult, error) {
	if s.hookClients == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "hook clients are unavailable")
	}

	snapshot, fromCache, err := s.hookClientsSnapshot(ctx, payload.WindowDays, payload.Refresh)
	if err != nil {
		return nil, err
	}
	sessionRows := snapshot.Sessions
	inferenceRows := snapshot.Inference

	seen := map[string]struct{}{}
	sessions := make([]*gen.AdminHookClientSessionCell, 0, len(sessionRows))
	for _, row := range sessionRows {
		seen[row.OrganizationID] = struct{}{}
		sessions = append(sessions, &gen.AdminHookClientSessionCell{
			OrganizationID: row.OrganizationID,
			Day:            row.Day,
			Client:         row.Client,
			Machines:       clampCount(row.Machines),
			Sessions:       clampCount(row.Sessions),
			MachinesAll:    clampCount(row.MachinesAll),
		})
	}
	inference := make([]*gen.AdminInferenceHookCell, 0, len(inferenceRows))
	for _, row := range inferenceRows {
		seen[row.OrganizationID] = struct{}{}
		inference = append(inference, &gen.AdminInferenceHookCell{
			OrganizationID: row.OrganizationID,
			Day:            row.Day,
			Surface:        row.Surface,
			Users:          clampCount(row.Users),
			Events:         clampCount(row.Events),
			Sessions:       clampCount(row.Sessions),
		})
	}

	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	names, err := repo.New(s.db).AdminListOrganizationNamesByIDs(ctx, ids)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list hook client organizations").LogError(ctx, s.logger)
	}
	byID := make(map[string]repo.AdminListOrganizationNamesByIDsRow, len(names))
	for _, name := range names {
		byID[name.ID] = name
	}
	organizations := make([]*gen.AdminHookClientOrganization, 0, len(ids))
	for _, id := range ids {
		organization := &gen.AdminHookClientOrganization{ID: id, Name: nil, Slug: nil}
		if name, ok := byID[id]; ok {
			organization.Name = &name.Name
			organization.Slug = &name.Slug
		}
		organizations = append(organizations, organization)
	}

	return &gen.AdminHookClientsResult{
		WindowDays:    payload.WindowDays,
		QueriedAt:     snapshot.QueriedAt.UTC().Format(time.RFC3339),
		FromCache:     fromCache,
		Organizations: organizations,
		Sessions:      sessions,
		Inference:     inference,
	}, nil
}

const hookClientsCacheTTL = time.Hour

// hookClientsSnapshot is one ClickHouse read of both grids. Organization names
// are not cached: they come from Postgres on every request, so a rename shows
// at once.
type hookClientsSnapshot struct {
	WindowDays int                                 `json:"window_days"`
	QueriedAt  time.Time                           `json:"queried_at"`
	Sessions   []telemetry.HookClientSessionRow    `json:"sessions"`
	Inference  []telemetry.InferenceHookTrafficRow `json:"inference"`
}

func hookClientsCacheKey(windowDays int) string {
	return fmt.Sprintf("admin_hook_clients:%d", windowDays)
}

func (h hookClientsSnapshot) CacheKey() string { return hookClientsCacheKey(h.WindowDays) }

func (h hookClientsSnapshot) TTL() time.Duration { return hookClientsCacheTTL }

// hookClientsSnapshot returns the cached grids for the window, or reads
// ClickHouse when there is no cached copy or refresh is set. A cache failure
// never fails the request: it only costs a fresh read.
func (s *Service) hookClientsSnapshot(ctx context.Context, windowDays int, refresh bool) (hookClientsSnapshot, bool, error) {
	if !refresh {
		cached, err := s.hookClientsCache.Get(ctx, hookClientsCacheKey(windowDays))
		if err == nil {
			return cached, true, nil
		}
	}

	queriedAt := time.Now().UTC()
	sessions, err := s.hookClients.Sessions(ctx, windowDays)
	if err != nil {
		return hookClientsSnapshot{}, false, s.hookClientsError(ctx, err, "read hook client sessions")
	}
	inference, err := s.hookClients.InferenceTraffic(ctx, windowDays)
	if err != nil {
		return hookClientsSnapshot{}, false, s.hookClientsError(ctx, err, "read inference hook traffic")
	}

	snapshot := hookClientsSnapshot{WindowDays: windowDays, QueriedAt: queriedAt, Sessions: sessions, Inference: inference}
	// Two refreshes can overlap. Keep whichever read started later, so a slow
	// read never replaces newer data or extends its TTL.
	if existing, err := s.hookClientsCache.Get(ctx, hookClientsCacheKey(windowDays)); err == nil && existing.QueriedAt.After(queriedAt) {
		return snapshot, false, nil
	}
	if err := s.hookClientsCache.Store(ctx, snapshot); err != nil {
		s.logger.WarnContext(ctx, "failed to cache hook clients", attr.SlogError(err))
	}
	return snapshot, false, nil
}

// hookClientsError keeps a shareable error's code and logs server faults,
// which only this boundary does.
func (s *Service) hookClientsError(ctx context.Context, err error, message string) error {
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		if shareable.Code == oops.CodeUnexpected {
			return shareable.LogError(ctx, s.logger)
		}
		return shareable
	}
	return oops.E(oops.CodeUnexpected, err, "%s", message).LogError(ctx, s.logger)
}

// clampCount narrows a ClickHouse count to the API's int64. Counts here are
// far below the int64 range, so clamping never loses a real value.
func clampCount(v uint64) int64 {
	out, _ := conv.ClampedUint64ToInt64(v)
	return out
}
