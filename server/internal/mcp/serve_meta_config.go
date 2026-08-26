package mcp

import "time"

// MetaRuntimeConfig bounds the gateway's per-member upstream work. Zero
// values mean the built-in defaults, so an unwired config is safe.
type MetaRuntimeConfig struct {
	// MemberCallTimeout bounds one member call end to end, handshake
	// included. Kept under the proxy's own 60s per-exchange timeouts so this
	// deadline, not the proxy's, is what a slow member hits.
	MemberCallTimeout time.Duration
	// MemberProbeTimeout bounds a status probe (reserved for cached health).
	MemberProbeTimeout time.Duration
	// MaxFanout bounds concurrent member work in one request.
	MaxFanout int
}

func (c MetaRuntimeConfig) withDefaults() MetaRuntimeConfig {
	if c.MemberCallTimeout <= 0 {
		c.MemberCallTimeout = 30 * time.Second
	}
	if c.MemberProbeTimeout <= 0 {
		c.MemberProbeTimeout = 2 * time.Second
	}
	if c.MaxFanout <= 0 {
		c.MaxFanout = 4
	}
	return c
}
