// Package authzgen renders the files generated from the authorization model,
// authz.Model. `mise run gen:authz` writes them; TestGeneratedFilesAreCurrent
// fails CI when one is stale.
package authzgen

import (
	"fmt"
	"go/format"
	"go/token"
	"regexp"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// File is one generated file, with its path relative to the server directory.
type File struct {
	Path    string
	Content []byte
}

const header = "// Code generated from authz.Model by authzgen. DO NOT EDIT.\n// Regenerate with `mise run gen:authz`.\n\n"

// Render renders every file generated from r.
func Render(r authz.Registry) ([]File, error) {
	scopes, err := renderScopeConstants(r)
	if err != nil {
		return nil, err
	}
	refs, err := renderRefs(r)
	if err != nil {
		return nil, err
	}
	return []File{
		{Path: "internal/authz/scopes_gen.go", Content: scopes},
		{Path: "internal/authz/refs_gen.go", Content: refs},
	}, nil
}

// identifierAcronyms are scope name parts written in capitals in Go
// identifiers.
var identifierAcronyms = map[string]string{"mcp": "MCP"}

var scopeWordSeparator = regexp.MustCompile(`[:_]`)

// ScopeConstantName turns a stored scope into its Go constant name:
// mcp:blocked_read becomes ScopeMCPBlockedRead.
func ScopeConstantName(scope authz.Scope) string {
	var b strings.Builder
	b.WriteString("Scope")
	for _, word := range scopeWordSeparator.Split(string(scope), -1) {
		if word == "" {
			continue
		}
		if acronym, ok := identifierAcronyms[word]; ok {
			b.WriteString(acronym)
			continue
		}
		b.WriteString(strings.ToUpper(word[:1]) + word[1:])
	}
	return b.String()
}

// gosecSensitiveName matches constant names gosec's hardcoded-credential rule
// flags although they hold scope names.
var gosecSensitiveName = regexp.MustCompile(`(?i)pass|secret|token|credential`)

func renderScopeConstants(r authz.Registry) ([]byte, error) {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("package authz\n\n")
	b.WriteString("// Every scope the model declares, in declaration order. Blocks follow the\n// permission they subtract from.\nconst (\n")
	seen := make(map[string]authz.Scope)
	var dup error
	line := func(scope authz.Scope, doc string) {
		name := ScopeConstantName(scope)
		if !token.IsIdentifier(name) && dup == nil {
			dup = fmt.Errorf("scope %q renders as %q, which is not a Go identifier", scope, name)
		}
		if previous, ok := seen[name]; ok && dup == nil {
			dup = fmt.Errorf("scopes %q and %q both render as %s", previous, scope, name)
		}
		seen[name] = scope
		fmt.Fprintf(&b, "\t// %s %s\n", name, doc)
		suffix := ""
		if gosecSensitiveName.MatchString(name) {
			suffix = " //nolint:gosec // scope name, not a credential"
		}
		fmt.Fprintf(&b, "\t%s Scope = %q%s\n", name, string(scope), suffix)
	}
	line(r.Root.Scope, "is the engine's own bypass. It is internal and never granted to people.")
	for _, s := range r.Scopes() {
		if s.Block {
			block, _ := s.Permission.Block()
			line(s.Scope, fmt.Sprintf("stores exceptions for %s. Internal: granted, never checked directly.", block.Subject))
			continue
		}
		line(s.Scope, fmt.Sprintf("is %q: %s", s.Permission.Label, s.Permission.Description))
	}
	b.WriteString(")\n")
	if dup != nil {
		return nil, dup
	}
	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("format scopes_gen.go: %w", err)
	}
	return formatted, nil
}
