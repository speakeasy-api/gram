package orgencryption

import (
	"context"
	"time"
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

// RefreshDue runs one KeepWarm pass as if the clock read now.
func (s *Service) RefreshDue(ctx context.Context, now time.Time, useCases ...UseCase) int {
	warm := make(map[string]struct{}, len(useCases))
	for _, useCase := range useCases {
		warm[string(useCase)] = struct{}{}
	}
	return s.refreshDue(ctx, warm, now)
}
