//go:build !windows

package relay

import "syscall"

// mcpNonBlockingOpenFlag keeps opening an MCP config from blocking. The path
// is repository-controlled and read on the tool-call gating path, where a
// FIFO with no writer would otherwise stall the open itself — before the
// descriptor could be rejected for not being a regular file.
const mcpNonBlockingOpenFlag = syscall.O_NONBLOCK
