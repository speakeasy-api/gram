package mcp

import "time"

// MetaRuntimeConfig bounds the meta MCP's per-member upstream work. Zero
// values mean the built-in defaults, so an unwired config is safe.
type MetaRuntimeConfig struct {
	// MemberCallTimeout bounds one member call end to end, handshake and
	// pagination included. Kept under the proxy's own 60s per-exchange
	// timeouts so this deadline, not the proxy's, is what a slow member hits.
	MemberCallTimeout time.Duration
}

func (c MetaRuntimeConfig) withDefaults() MetaRuntimeConfig {
	if c.MemberCallTimeout <= 0 {
		c.MemberCallTimeout = 30 * time.Second
	}
	return c
}
