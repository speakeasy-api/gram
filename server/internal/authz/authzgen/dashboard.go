package authzgen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/authz/internal/registry"
)

// The dashboard reads the user's effective grants once and decides what to
// show on its own (client/dashboard/src/hooks/useRBAC.ts). Two files keep it
// in step with the server: the tables it needs, generated here from the
// registry, and a list of cases answered by the server's evaluator, which
// package authzcases writes and the dashboard's tests replay against its own
// matcher.
const dashboardTablesPath = "../client/dashboard/src/lib/authz.gen.ts"

const tsHeader = "// Code generated from registry.Definitions by authzgen. DO NOT EDIT.\n// Regenerate with `mise run gen:authz`.\n\n"

// DashboardExclusions maps each scope to the exclusion scopes a check of it
// must clear: its own exclusion, then the broader blocks that also cover it,
// the way Check.exclusionCheck expands on the server.
func DashboardExclusions(r registry.Registry) map[registry.Scope][]registry.Scope {
	exclusions := r.DeriveScopeExclusions()
	expansions := r.DeriveScopeExpansions()
	out := make(map[registry.Scope][]registry.Scope)
	for scope, exclusion := range exclusions {
		if exclusion == "" {
			continue
		}
		out[scope] = append([]registry.Scope{exclusion}, expansions[exclusion]...)
	}
	return out
}

func renderDashboardTables(r registry.Registry) []byte {
	var b strings.Builder
	b.WriteString(tsHeader)

	b.WriteString("/** The resource kind each scope's checks and grant selectors name. */\n")
	b.WriteString("export const scopeResourceKinds: Readonly<Record<string, string>> = {\n")
	kinds := r.DeriveResourceKinds()
	for _, scope := range sortedKeys(kinds) {
		fmt.Fprintf(&b, "  %s: %s,\n", strconv.Quote(string(scope)), strconv.Quote(kinds[scope]))
	}
	b.WriteString("};\n\n")

	b.WriteString("/**\n * The exclusion scopes a check of each scope must clear: its own exclusion,\n * then the broader blocks that also cover it. A block on read also takes\n * write away.\n */\n")
	b.WriteString("export const scopeExclusions: Readonly<\n  Record<string, readonly string[]>\n> = {\n")
	exclusions := DashboardExclusions(r)
	for _, scope := range sortedKeys(exclusions) {
		fmt.Fprintf(&b, "  %s: [\n", strconv.Quote(string(scope)))
		for _, exclusion := range exclusions[scope] {
			fmt.Fprintf(&b, "    %s,\n", strconv.Quote(string(exclusion)))
		}
		b.WriteString("  ],\n")
	}
	b.WriteString("};\n")
	return []byte(b.String())
}
