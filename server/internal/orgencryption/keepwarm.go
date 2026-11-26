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
	warm := s.enableKeepWarm(useCases)
	if len(warm) == 0 {
		return
	}
	defer s.disableKeepWarm(warm)

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

// enableKeepWarm starts recording uses of the given use cases' DEKs and
// returns them as a set.
func (s *Service) enableKeepWarm(useCases []UseCase) map[string]struct{} {
	warm := make(map[string]struct{}, len(useCases))
	s.usesMu.Lock()
	defer s.usesMu.Unlock()
	for _, useCase := range useCases {
		if _, ok := warm[string(useCase)]; ok {
			continue
		}
		warm[string(useCase)] = struct{}{}
		s.warmUseCases[string(useCase)]++
	}
	return warm
}

// disableKeepWarm undoes enableKeepWarm when a KeepWarm loop stops. Once no
// loop keeps a use case warm, its uses are dropped and no longer recorded.
func (s *Service) disableKeepWarm(warm map[string]struct{}) {
	s.usesMu.Lock()
	defer s.usesMu.Unlock()
	for useCase := range warm {
		s.warmUseCases[useCase]--
		if s.warmUseCases[useCase] > 0 {
			continue
		}
		delete(s.warmUseCases, useCase)
		for id, use := range s.lastUsed {
			if use.useCase == useCase {
				delete(s.lastUsed, id)
			}
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

// noteUse records a use of a DEK whose use case KeepWarm keeps warm. Other
// uses are not recorded, so a process without KeepWarm keeps no map at all.
func (s *Service) noteUse(id uuid.UUID, useCase string) {
	s.usesMu.Lock()
	defer s.usesMu.Unlock()
	if s.warmUseCases[useCase] == 0 {
		return
	}
	s.lastUsed[id] = keyUse{at: time.Now(), useCase: useCase}
}

func (s *Service) usedSince(id uuid.UUID, since time.Time) bool {
	s.usesMu.Lock()
	defer s.usesMu.Unlock()
	use, ok := s.lastUsed[id]
	return ok && !use.at.Before(since)
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
	for id, use := range s.lastUsed {
		if use.at.Before(before) || !s.dataKeys.Contains(id) {
			delete(s.lastUsed, id)
		}
	}
}
