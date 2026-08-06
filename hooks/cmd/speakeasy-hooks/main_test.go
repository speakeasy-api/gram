package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServerAliasArgsKeepsOnlyConfigBeforeSentinel(t *testing.T) {
	t.Parallel()
	got := serverAliasArgs([]string{
		"speakeasy-hooks", "server",
		"--config=/plug/speakeasy.json",
		"--idle-timeout=45m",
	})
	// Identity contract: the installed hook commands run
	// `--config=<path> agenthooks client ...`, so a supervised server must
	// present exactly `--config=<path>` pre-sentinel to be the server those
	// clients rendezvous with. Everything else parses post-sentinel.
	require.Equal(t, []string{
		"speakeasy-hooks",
		"--config=/plug/speakeasy.json",
		"agenthooks", "server",
		"--idle-timeout=45m",
	}, got)
}

func TestServerAliasArgsBareServer(t *testing.T) {
	t.Parallel()
	got := serverAliasArgs([]string{"speakeasy-hooks", "server"})
	require.Equal(t, []string{"speakeasy-hooks", "agenthooks", "server"}, got)
}
