package mcp

import "time"

// MetaRuntimeConfig bounds per-upstream work; zero values mean the defaults.
type MetaRuntimeConfig struct {
	// MemberCallTimeout bounds member negotiation and calls, including all
	// catalog pages. It stays below the proxy's 60s per-exchange timeouts.
	// Legacy cleanup has a separate bounded deadline.
	MemberCallTimeout time.Duration

	// ValidationTimeout budgets consent verification, legacy cleanup, and the verdict write.
	ValidationTimeout time.Duration

	// AutoVerifyWait is how long a remote login callback holds its redirect for the probe a fresh grant starts, so a fast member's verdict is on the first render.
	AutoVerifyWait time.Duration

	// RecheckInterval is how long an idle grant with no refresh token goes between keepalive re-checks of its stored verdict.
	// Unlike the other fields, zero or negative disables the sweep; the CLI flag carries the default.
	RecheckInterval time.Duration
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
