package orgencryption

import "time"

// SetClock replaces the clock a Cipher stamps and checks decisions with.
func (c *Cipher) SetClock(now func() time.Time) {
	c.now = now
}
