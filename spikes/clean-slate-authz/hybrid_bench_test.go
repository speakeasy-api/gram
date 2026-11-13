package spike_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/authz"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/fixtures"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/model"
)

// Option D: checks run on an admitted principal (in memory, no I/O);
// admission reads Option B's tables.

func BenchmarkHybridToolCallCheck(b *testing.B) {
	e, _ := dMem(b)
	ctx := admitted(b, e, agent)
	for b.Loop() {
		if err := authz.Require(ctx, e, permConnect, fixtures.ServerObj, authz.With(update)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHybridChatReadCheck(b *testing.B) {
	e, _ := dMem(b)
	ctx := admitted(b, e, alice)
	c := chat("chat_bob")
	for b.Loop() {
		if !authz.Can(ctx, e, permChatRead, c) {
			b.Fatal("denied")
		}
	}
}

func BenchmarkHybridToolsList40(b *testing.B) {
	e, _ := dMem(b)
	ctx := admitted(b, e, agent)
	tools := make([]model.MCPToolContext, 40)
	for i := range tools {
		tools[i] = model.MCPToolContext{Tool: fmt.Sprintf("tool_%d", i), Disposition: "read_only"}
	}
	tools[0] = update
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
}

func BenchmarkHybridAdmitPostgres(b *testing.B) {
	for _, tc := range []struct {
		name string
		cred authz.Credential
	}{{"human", alice}, {"agent-3-layers", agent}} {
		b.Run(tc.name, func(b *testing.B) {
			e, _, _ := dPg(b)
			ctx := context.Background()
			for b.Loop() {
				if _, err := e.Admit(ctx, tc.cred); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkHybridRequestPostgres(b *testing.B) {
	five := []model.MCPToolContext{update, {Tool: "list_issues", Disposition: "read_only"}, {Tool: "get_issue", Disposition: "read_only"}, update, update}
	for _, tc := range []struct {
		name  string
		tools []model.MCPToolContext
	}{{"one-tool-call", five[:1]}, {"five-checks", five}} {
		b.Run(tc.name, func(b *testing.B) {
			e, _, _ := dPg(b)
			ctx := context.Background()
			for b.Loop() {
				p, err := e.Admit(ctx, agent)
				if err != nil {
					b.Fatal(err)
				}
				pctx := authz.WithPrincipal(ctx, p)
				for _, t := range tc.tools {
					if err := authz.Require(pctx, e, permConnect, fixtures.ServerObj, authz.With(t)); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
