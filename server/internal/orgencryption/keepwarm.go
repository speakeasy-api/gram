package orgencryption

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	// keepWarmIdle is how long after its last use KeepWarm keeps refreshing a
	// DEK. After that the DEK expires from the process cache as usual.
	keepWarmIdle = 30 * time.Minute

	// refreshAhead is how long before DataKeyCacheTTL runs out KeepWarm
	// unwraps a DEK again.
	refreshAhead = 90 * time.Second

	// keepWarmInterval is how often KeepWarm looks for DEKs to refresh. It is
	// well under refreshAhead, so a due DEK is refreshed before it expires.
	keepWarmInterval = 30 * time.Second
)

// KeepWarm keeps the DEKs of the given use cases that callers used in the last
// keepWarmIdle in the process cache, so a request does not wait for a KMS call
// each time a DEK expires. It runs until ctx ends.
//
// A refresh is the same full unwrap a cache miss does: it reads the committed
// configuration and DEK through the pool and calls KMS. A suspended
// configuration, an unreadable DEK or an unreachable KEK fails the refresh, and
// the cached DEK then expires on schedule. The revocation bound stays
// DataKeyCacheTTL. Each warm DEK costs one KMS call per process about every
// DataKeyCacheTTL minus refreshAhead.
func (s *Service) KeepWarm(ctx context.Context, useCases ...UseCase) {
	warm := make(map[string]struct{}, len(useCases))
	for _, useCase := range useCases {
		warm[string(useCase)] = struct{}{}
	}
	if len(warm) == 0 {
		return
	}

	ticker := time.NewTicker(keepWarmInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshDue(ctx, warm, time.Now())
		}
	}
}

// refreshDue unwraps again every cached DEK of a warm use case that is due for
// refresh at now and was used within keepWarmIdle. It returns how many it
// refreshed.
func (s *Service) refreshDue(ctx context.Context, warm map[string]struct{}, now time.Time) int {
	refreshed := 0
	for _, id := range s.dataKeys.Keys() {
		cached, ok := s.dataKeys.Peek(id)
		if !ok {
			continue
		}
		if _, ok := warm[cached.useCase]; !ok {
			continue
		}
		if now.Sub(cached.unwrappedAt) < DataKeyCacheTTL-refreshAhead {
			continue
		}
		if !s.usedSince(id, now.Add(-keepWarmIdle)) {
			continue
		}
		if _, err := s.unwrapShared(ctx, cached.organizationID, id); err != nil {
			s.logger.WarnContext(ctx, "refresh data encryption key", attr.SlogOrganizationID(cached.organizationID), attr.SlogError(err))
			continue
		}
		refreshed++
	}
	s.pruneUses(now.Add(-keepWarmIdle))

	return refreshed
}

func (s *Service) noteUse(id uuid.UUID) {
	s.usesMu.Lock()
	defer s.usesMu.Unlock()
	s.lastUsed[id] = time.Now()
}

func (s *Service) usedSince(id uuid.UUID, since time.Time) bool {
	s.usesMu.Lock()
	defer s.usesMu.Unlock()
	last, ok := s.lastUsed[id]
	return ok && !last.Before(since)
}

func (s *Service) forgetUse(id uuid.UUID) {
	s.usesMu.Lock()
	defer s.usesMu.Unlock()
	delete(s.lastUsed, id)
}

// pruneUses drops uses older than before, and uses of DEKs no longer cached,
// so the map stays bounded by the cache.
func (s *Service) pruneUses(before time.Time) {
	s.usesMu.Lock()
	defer s.usesMu.Unlock()
	for id, last := range s.lastUsed {
		if last.Before(before) || !s.dataKeys.Contains(id) {
			delete(s.lastUsed, id)
		}
	}
}
