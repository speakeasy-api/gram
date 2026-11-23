package redisrole

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommandKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []any
		want []string
	}{
		{name: "no arguments", args: nil, want: nil},
		{name: "ping", args: []any{"ping"}, want: nil},
		{name: "hello", args: []any{"hello", 3}, want: nil},
		{name: "client setinfo", args: []any{"client", "setinfo", "lib-name", "go-redis"}, want: nil},
		{name: "get", args: []any{"get", "userInfo:u:"}, want: []string{"userInfo:u:"}},
		{name: "set with bytes key", args: []any{"set", []byte("feature:o:f:"), "v"}, want: []string{"feature:o:f:"}},
		{name: "upper-case name", args: []any{"GET", "toolset:a"}, want: []string{"toolset:a"}},
		{name: "mget", args: []any{"mget", "sessions:issued_at:s", "sessions:valid_after:u"}, want: []string{"sessions:issued_at:s", "sessions:valid_after:u"}},
		{name: "del", args: []any{"del", "a", "b"}, want: []string{"a", "b"}},
		{name: "watch", args: []any{"watch", "spend_gate:rules:o"}, want: []string{"spend_gate:rules:o"}},
		{name: "eval", args: []any{"eval", "return 1", 2, "mcpClientInfo:x", "mcpClientInfoLive:x", "arg"}, want: []string{"mcpClientInfo:x", "mcpClientInfoLive:x"}},
		{name: "evalsha string numkeys", args: []any{"evalsha", "sha", "1", "toolset:a", "arg"}, want: []string{"toolset:a"}},
		{name: "eval no keys", args: []any{"eval", "return 1", 0, "arg"}, want: nil},
		{name: "xread", args: []any{"xread", "block", 20000, "streams", "chat:turnframes:a", "chat:turnframes:b", "0", "$"}, want: []string{"chat:turnframes:a", "chat:turnframes:b"}},
		{name: "xadd", args: []any{"xadd", "chat:turnframes:a", "maxlen", "~", 2000, "*", "f", "v"}, want: []string{"chat:turnframes:a"}},
		{name: "scan match", args: []any{"scan", 0, "match", "registry:list:v1*", "count", 100}, want: []string{"registry:list:v1*"}},
		{name: "scan without match", args: []any{"scan", 0, "count", 100}, want: nil},
		{name: "command without key", args: []any{"get"}, want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, commandKeys(tc.args))
		})
	}
}

func TestKeyHead(t *testing.T) {
	t.Parallel()

	require.Equal(t, "auth:", keyHead("auth:login_nonce:secret"))
	require.Empty(t, keyHead("nocolon"))
}
