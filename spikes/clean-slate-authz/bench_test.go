package spike_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/authz"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/fga"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/fixtures"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/model"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/objects"
)

var permConnect = authz.Permission[objects.MCPServer]{Type: "mcp_server", Name: "connect"}

func admitted(b *testing.B, e *authz.Engine, cred authz.Credential) context.Context {
	ctx := context.Background()
	p, err := e.Admit(ctx, cred)
	must(b, err)
	return authz.WithPrincipal(ctx, p)
}

type fgaBackend struct {
	name string
	make func(testing.TB) *fga.Backend
}

func fgaBackends() []fgaBackend {
	out := []fgaBackend{{"openfga/memory", fgaMemory}}
	if os.Getenv("SPIKE_FGA_DATABASE_URL") != "" {
		out = append(out, fgaBackend{"openfga/postgres", fgaPostgres}, fgaBackend{"openfga/postgres+cache10s", fgaPostgresCached})
	}
	return out
}

// One tool call by the agent: three layers, condition + block evaluation.
func BenchmarkToolCallCheck(b *testing.B) {
	b.Run("ours", func(b *testing.B) {
		e, _ := newMemEngine()
		ctx := admitted(b, e, agent)
		for b.Loop() {
			if err := authz.Require(ctx, e, permConnect, fixtures.ServerObj, authz.With(update)); err != nil {
				b.Fatal(err)
			}
		}
	})
	for _, fb := range fgaBackends() {
		b.Run(fb.name, func(b *testing.B) {
			be := fb.make(b)
			ctx := context.Background()
			f := fga.Facts{Tool: update.Tool, Disposition: update.Disposition}
			for b.Loop() {
				if ok, err := be.CheckLayers(ctx, fgaUsers(agent), "connect", "mcp_server:"+fixtures.Server, f); err != nil || !ok {
					b.Fatal(ok, err)
				}
			}
		})
	}
}

// Team review of one chat: chat → author → groups → reviewer grant, minus protection.
func BenchmarkChatReadCheck(b *testing.B) {
	c := chat("chat_bob")
	b.Run("ours", func(b *testing.B) {
		e, _ := newMemEngine()
		ctx := admitted(b, e, alice)
		for b.Loop() {
			if !authz.Can(ctx, e, permChatRead, c) {
				b.Fatal("denied")
			}
		}
	})
	for _, fb := range fgaBackends() {
		b.Run(fb.name, func(b *testing.B) {
			be := fb.make(b)
			ctx := context.Background()
			for b.Loop() {
				if ok, err := be.Check(ctx, "user:alice", "read", "chat:chat_bob", fga.Facts{}); err != nil || !ok {
					b.Fatal(ok, err)
				}
			}
		})
	}
}

// tools/list with 40 tools for the agent.
func BenchmarkToolsList40(b *testing.B) {
	tools := make([]model.MCPToolContext, 40)
	facts := make([]fga.Facts, 40)
	for i := range tools {
		tools[i] = model.MCPToolContext{Tool: fmt.Sprintf("tool_%d", i), Disposition: "read_only"}
		facts[i] = fga.Facts{Tool: tools[i].Tool, Disposition: tools[i].Disposition}
	}
	tools[0], facts[0] = update, fga.Facts{Tool: update.Tool, Disposition: update.Disposition}
	b.Run("ours", func(b *testing.B) {
		e, _ := newMemEngine()
		ctx := admitted(b, e, agent)
		for b.Loop() {
			n := 0
			for _, t := range tools {
				if authz.Can(ctx, e, permConnect, fixtures.ServerObj, authz.With(t)) {
					n++
				}
			}
			if n != 1 {
				b.Fatal(n)
			}
		}
	})
	for _, fb := range fgaBackends() {
		b.Run(fb.name+"/batch-per-layer", func(b *testing.B) {
			be := fb.make(b)
			ctx := context.Background()
			for b.Loop() {
				allowed := make([]bool, len(facts))
				for i := range allowed {
					allowed[i] = true
				}
				for _, u := range fgaUsers(agent) {
					res, err := be.BatchCheck(ctx, u, "connect", "mcp_server:"+fixtures.Server, facts)
					if err != nil {
						b.Fatal(err)
					}
					for i, ok := range res {
						allowed[i] = allowed[i] && ok
					}
				}
				n := 0
				for _, ok := range allowed {
					if ok {
						n++
					}
				}
				if n != 1 {
					b.Fatal(n)
				}
			}
		})
	}
}

// Admission against Postgres: what each request pays before its checks.
func BenchmarkAdmitPostgres(b *testing.B) {
	for _, tc := range []struct {
		name  string
		cred  authz.Credential
		cache bool
	}{
		{"human/uncached", alice, false},
		{"human/cached", alice, true},
		{"agent-3-layers/uncached", agent, false},
		{"agent-3-layers/cached", agent, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			e, _ := pgEngine(b, tc.cache)
			ctx := context.Background()
			for b.Loop() {
				if _, err := e.Admit(ctx, tc.cred); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
