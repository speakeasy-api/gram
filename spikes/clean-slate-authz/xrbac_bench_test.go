package spike_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/authz"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/fixtures"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/model"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/xrbac"
)

type xBackend struct {
	name string
	make func(testing.TB) *xrbac.Client
}

func xBackends() []xBackend {
	out := []xBackend{{"extensible/memory", func(testing.TB) *xrbac.Client { c, _, _ := xMem(); return c }}}
	if os.Getenv("SPIKE_DATABASE_URL") != "" {
		out = append(out, xBackend{"extensible/postgres", func(t testing.TB) *xrbac.Client { c, _, _ := xPg(t); return c }})
	}
	return out
}

// Option B loads authority on every client call, so each figure includes the
// load. On memory it approximates evaluation cost alone.
func BenchmarkExtensibleToolCallCheck(b *testing.B) {
	for _, xb := range xBackends() {
		b.Run(xb.name, func(b *testing.B) {
			c := xb.make(b)
			ctx, s := context.Background(), xSubject(agent)
			req := xrbac.ToolCallCheck(xrbac.ScopeMCPConnect, fixtures.XServerTarget, xrbac.ToolFacts{Name: update.Tool, Classification: update.Disposition})
			for b.Loop() {
				if err := c.Require(ctx, s, req); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkExtensibleChatReadCheck(b *testing.B) {
	for _, xb := range xBackends() {
		b.Run(xb.name, func(b *testing.B) {
			c := xb.make(b)
			ctx, s := context.Background(), xSubject(alice)
			req := xrbac.ResourceCheck(xrbac.ScopeChatRead, fixtures.XChatTarget("chat_bob"))
			for b.Loop() {
				if err := c.Require(ctx, s, req); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkExtensibleToolsList40(b *testing.B) {
	reqs := make([]xrbac.Request, 40)
	for i := range reqs {
		reqs[i] = xrbac.ToolCallCheck(xrbac.ScopeMCPConnect, fixtures.XServerTarget, xrbac.ToolFacts{Name: fmt.Sprintf("tool_%d", i), Classification: "read_only"})
	}
	reqs[0] = xrbac.ToolCallCheck(xrbac.ScopeMCPConnect, fixtures.XServerTarget, xrbac.ToolFacts{Name: update.Tool, Classification: update.Disposition})
	for _, xb := range xBackends() {
		b.Run(xb.name+"/filter", func(b *testing.B) {
			c := xb.make(b)
			ctx, s := context.Background(), xSubject(agent)
			for b.Loop() {
				got, err := c.Filter(ctx, s, reqs)
				if err != nil || len(got) != 1 {
					b.Fatal(len(got), err)
				}
			}
		})
	}
}

// One request that makes one tool-call check, end to end on Postgres:
// A = admission (cached or not) + check; B = one client call.
func BenchmarkRequestOneToolCallPostgres(b *testing.B) {
	for _, cache := range []bool{false, true} {
		b.Run(fmt.Sprintf("option-a/cache=%v", cache), func(b *testing.B) {
			e, _ := pgEngine(b, cache)
			ctx := context.Background()
			for b.Loop() {
				p, err := e.Admit(ctx, agent)
				if err != nil {
					b.Fatal(err)
				}
				if err := authz.Require(authz.WithPrincipal(ctx, p), e, permConnect, fixtures.ServerObj, authz.With(update)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	b.Run("option-b", func(b *testing.B) {
		c, _, _ := xPg(b)
		ctx, s := context.Background(), xSubject(agent)
		req := xrbac.ToolCallCheck(xrbac.ScopeMCPConnect, fixtures.XServerTarget, xrbac.ToolFacts{Name: update.Tool, Classification: update.Disposition})
		for b.Loop() {
			if err := c.Require(ctx, s, req); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// One request that makes five checks (e.g. a handler that checks the server,
// a tool and three list items): A admits once; B loads per call unless the
// handler batches them into AuthorizeAll/Filter.
func BenchmarkRequestFiveChecksPostgres(b *testing.B) {
	tools := []model.MCPToolContext{update, {Tool: "list_issues", Disposition: "read_only"}, {Tool: "get_issue", Disposition: "read_only"}, update, update}
	b.Run("option-a/cache=false", func(b *testing.B) {
		e, _ := pgEngine(b, false)
		ctx := context.Background()
		for b.Loop() {
			p, err := e.Admit(ctx, agent)
			if err != nil {
				b.Fatal(err)
			}
			pctx := authz.WithPrincipal(ctx, p)
			for _, t := range tools {
				if err := authz.Require(pctx, e, permConnect, fixtures.ServerObj, authz.With(t)); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	reqs := make([]xrbac.Request, len(tools))
	for i, t := range tools {
		reqs[i] = xrbac.ToolCallCheck(xrbac.ScopeMCPConnect, fixtures.XServerTarget, xrbac.ToolFacts{Name: t.Tool, Classification: t.Disposition})
	}
	b.Run("option-b/five-calls", func(b *testing.B) {
		c, _, _ := xPg(b)
		ctx, s := context.Background(), xSubject(agent)
		for b.Loop() {
			for _, r := range reqs {
				if err := c.Require(ctx, s, r); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	b.Run("option-b/authorize-all", func(b *testing.B) {
		c, _, _ := xPg(b)
		ctx, s := context.Background(), xSubject(agent)
		for b.Loop() {
			d, err := c.AuthorizeAll(ctx, s, reqs)
			if err != nil || d.RequireAllowed() != nil {
				b.Fatal(d, err)
			}
		}
	})
}
