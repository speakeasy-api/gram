//go:build !linux

package agent

func hideFromStdioServer() error { return nil }
