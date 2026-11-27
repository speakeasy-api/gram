package orgencryption

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SetClock replaces the clock a Cipher stamps and checks decisions with.
func (c *Cipher) SetClock(now func() time.Time) {
	c.now = now
}

// SetCandidatePageSize replaces how many candidates one
// NextBackfillCandidates page holds, so tests can page through a few rows.
func (m *Migrator) SetCandidatePageSize(n int32) {
	m.candidatePageSize = n
}

// EnableKeepWarm records uses of the given use cases' DEKs, as KeepWarm does
// when it starts, without starting its loop. The returned func stops
// recording, as KeepWarm does when it stops.
func (s *Service) EnableKeepWarm(useCases ...UseCase) func() {
	warm := s.enableKeepWarm(useCases)
	return func() { s.disableKeepWarm(warm) }
}

// AgeDataKey makes a cached DEK look as if KMS returned it age ago.
func (s *Service) AgeDataKey(id uuid.UUID, age time.Duration) {
	cached, ok := s.dataKeys.Peek(id)
	if !ok {
		return
	}
	cached.unwrappedAt = time.Now().Add(-age)
	s.dataKeys.Add(id, cached)
}

// RecordedUses counts the DEK uses KeepWarm tracks.
func (s *Service) RecordedUses() int {
	s.usesMu.Lock()
	defer s.usesMu.Unlock()
	return len(s.lastUsed)
}

// RefreshDue runs one KeepWarm pass as if the clock read now.
func (s *Service) RefreshDue(ctx context.Context, now time.Time, useCases ...UseCase) int {
	warm := make(map[string]struct{}, len(useCases))
	for _, useCase := range useCases {
		warm[string(useCase)] = struct{}{}
	}
	return s.refreshDue(ctx, warm, now)
}
