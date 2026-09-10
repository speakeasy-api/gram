package mcp

import "time"

// MetaRuntimeConfig bounds per-upstream work; zero values mean the defaults.
type MetaRuntimeConfig struct {
	// MemberCallTimeout bounds one member call end to end, handshake and
	// pagination included. Kept under the proxy's own 60s per-exchange
	// timeouts so this deadline, not the proxy's, is what a slow member hits.
	MemberCallTimeout time.Duration

	// ValidationTimeout bounds a consent-page probe's handshake and session close.
	ValidationTimeout time.Duration

	// AutoVerifyWait is how long a remote login callback holds its redirect for the probe a fresh grant starts, so a fast member's verdict is on the first render.
	AutoVerifyWait time.Duration
}

func (c MetaRuntimeConfig) withDefaults() MetaRuntimeConfig {
	if c.MemberCallTimeout <= 0 {
		c.MemberCallTimeout = 30 * time.Second
	}
	if c.ValidationTimeout <= 0 {
		c.ValidationTimeout = 15 * time.Second
	}
	if c.AutoVerifyWait <= 0 {
		c.AutoVerifyWait = 3 * time.Second
	}
	return c
}
