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
		{name: "xreadgroup with a group named streams", args: []any{"xreadgroup", "group", "streams", "consumer", "count", 1, "streams", "chat:turnframes:a", ">"}, want: []string{"chat:turnframes:a"}},
		{name: "scan match", args: []any{"scan", 0, "match", "registry:list:v1*", "count", 100}, want: []string{"registry:list:v1*"}},
		{name: "scan match all", args: []any{"scan", 0, "match", "*", "count", 100}, want: nil},
		{name: "scan match leading glob", args: []any{"scan", 0, "match", "*:toolset:*"}, want: nil},
		{name: "scan without match", args: []any{"scan", 0, "count", 100}, want: nil},
		{name: "command without key", args: []any{"get"}, want: nil},
		{name: "rename", args: []any{"rename", "toolset:a", "auth:b"}, want: []string{"toolset:a", "auth:b"}},
		{name: "smove", args: []any{"smove", "a", "b", "member"}, want: []string{"a", "b"}},
		{name: "lmove", args: []any{"lmove", "a", "b", "left", "right"}, want: []string{"a", "b"}},
		{name: "blpop", args: []any{"blpop", "a", "b", 5}, want: []string{"a", "b"}},
		{name: "brpop", args: []any{"brpop", "a", 5}, want: []string{"a"}},
		{name: "mset", args: []any{"mset", "a", "1", "b", "2"}, want: []string{"a", "b"}},
		{name: "msetnx", args: []any{"msetnx", "a", "1"}, want: []string{"a"}},
		{name: "zunionstore", args: []any{"zunionstore", "dest", 2, "a", "b", "weights", 1, 2}, want: []string{"dest", "a", "b"}},
		{name: "zinterstore", args: []any{"zinterstore", "dest", 1, "a"}, want: []string{"dest", "a"}},
		{name: "bitop", args: []any{"bitop", "and", "dest", "a", "b"}, want: []string{"dest", "a", "b"}},
		{name: "xinfo stream", args: []any{"xinfo", "stream", "chat:turnframes:a"}, want: []string{"chat:turnframes:a"}},
		{name: "xinfo help", args: []any{"xinfo", "help"}, want: nil},
		{name: "memory usage", args: []any{"memory", "usage", "toolset:a"}, want: []string{"toolset:a"}},
		{name: "memory stats", args: []any{"memory", "stats"}, want: nil},
		{name: "object encoding", args: []any{"object", "encoding", "toolset:a"}, want: []string{"toolset:a"}},
		{name: "config get", args: []any{"config", "get", "maxmemory"}, want: nil},
		{name: "flushdb async", args: []any{"flushdb", "async"}, want: nil},
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
