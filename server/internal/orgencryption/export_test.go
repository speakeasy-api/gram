package orgencryption

import "time"

// SetClock replaces the clock a Cipher stamps and checks decisions with.
func (c *Cipher) SetClock(now func() time.Time) {
	c.now = now
}

// SetCandidatePageSize replaces how many candidates one
// NextBackfillCandidates page holds, so tests can page through a few rows.
func (m *Migrator) SetCandidatePageSize(n int32) {
	m.candidatePageSize = n
}
