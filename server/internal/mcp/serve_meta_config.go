package mcp

import "time"

// MetaRuntimeConfig bounds per-upstream work; zero values mean the defaults.
type MetaRuntimeConfig struct {
	// MemberCallTimeout bounds one member call end to end, handshake and
	// pagination included. Kept under the proxy's own 60s per-exchange
	// timeouts so this deadline, not the proxy's, is what a slow member hits.
	MemberCallTimeout time.Duration

	// ValidationTimeout bounds a consent-page probe end to end, session close and verdict write included.
	ValidationTimeout time.Duration
}

func (c MetaRuntimeConfig) withDefaults() MetaRuntimeConfig {
	if c.MemberCallTimeout <= 0 {
		c.MemberCallTimeout = 30 * time.Second
	}
	if c.ValidationTimeout <= 0 {
		c.ValidationTimeout = 15 * time.Second
	}
	return c
}
