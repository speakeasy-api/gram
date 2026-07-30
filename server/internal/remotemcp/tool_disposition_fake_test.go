package remotemcp_test

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/remotemcp"
)

// fakeToolDispositionResolver is a canned [remotemcp.ToolDispositionResolver]
// for interceptor tests: it returns a fixed disposition map (or error) without
// touching Postgres or Redis, so the interceptor's disposition wiring and
// fail-closed behavior are exercised in isolation from the resolver's own
// storage-backed tests (those live in the mcpservers package, next to the
// metadata fixtures).
type fakeToolDispositionResolver struct {
	dispositions map[string]string
	err          error
}

func (f fakeToolDispositionResolver) Dispositions(_ context.Context, _, _ string) (map[string]string, error) {
	return f.dispositions, f.err
}

// emptyResolver resolves every tool to the empty disposition, reducing each
// check to a pure tool-name match.
func emptyResolver() remotemcp.ToolDispositionResolver {
	return fakeToolDispositionResolver{dispositions: nil, err: nil}
}
