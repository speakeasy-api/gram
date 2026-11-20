// Package authzgen renders the files generated from the authorization model,
// registry.Definitions. `mise run gen:authz` writes them; TestGeneratedFilesAreCurrent
// fails CI when one is stale.
//
// It imports the registry and nothing from package authz. Everything it
// writes into authz it spells by name, so it builds, and can rewrite the
// generated files, even when they are missing or do not compile. The
// dashboard's cases, which are answered by the evaluator, are written by
// package authzcases for that reason.
package authzgen

import (
	"fmt"
	"go/format"
	"go/token"
	"regexp"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/authz/internal/registry"
)

// File is one generated file, with its path relative to the server directory.
type File struct {
	Path    string
	Content []byte
}

const header = "// Code generated from registry.Definitions by authzgen. DO NOT EDIT.\n// Regenerate with `mise run gen:authz`.\n\n"

// Files validates the registry and renders every file generated from it. It is
// what `mise run gen:authz` writes first; authzcases writes the rest.
func Files() ([]File, error) {
	if err := registry.Definitions.Validate(); err != nil {
		return nil, fmt.Errorf("the authorization registry is invalid: %w", err)
	}
	return Render(registry.Definitions)
}

// Render renders every file generated from r.
func Render(r registry.Registry) ([]File, error) {
	scopes, err := renderScopeConstants(r)
	if err != nil {
		return nil, err
	}
	refs, err := renderRefs(r)
	if err != nil {
		return nil, err
	}
	tables, err := renderTables(r)
	if err != nil {
		return nil, err
	}
	ids, err := renderIDs(r)
	if err != nil {
		return nil, err
	}
	vocabulary, err := renderVocabulary(registry.VocabularyFiles())
	if err != nil {
		return nil, err
	}
	return append(vocabulary, []File{
		{Path: "internal/authz/ids_gen.go", Content: ids},
		{Path: "internal/authz/scopes_gen.go", Content: scopes},
		{Path: "internal/authz/refs_gen.go", Content: refs},
		{Path: "internal/authz/tables_gen.go", Content: tables},
		{Path: dashboardTablesPath, Content: renderDashboardTables(r)},
	}...), nil
}

// identifierAcronyms are scope name parts written in capitals in Go
// identifiers.
var identifierAcronyms = map[string]string{"mcp": "MCP"}

var scopeWordSeparator = regexp.MustCompile(`[:_]`)

// ScopeConstantName turns a stored scope into its Go constant name:
// mcp:blocked_read becomes ScopeMCPBlockedRead.
func ScopeConstantName(scope registry.Scope) string {
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

func renderScopeConstants(r registry.Registry) ([]byte, error) {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("package authz\n\n")
	b.WriteString("// Every scope the model declares, in declaration order. Blocks follow the\n// permission they subtract from.\nconst (\n")
	seen := make(map[string]registry.Scope)
	var dup error
	line := func(scope registry.Scope, doc string) {
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

// vocabularyOutputs names the generated file of each vocabulary file. The
// default is <name>_gen.go; scopes.go differs because scopes_gen.go is the
// scope constants.
var vocabularyOutputs = map[string]string{"scopes.go": "scopes_vocabulary_gen.go"}

// renderVocabulary writes the registry's vocabulary into package authz: the
// declarations of each vocabulary file as they stand, under package authz.
// The evaluator gets the same names and values as the model with no import
// between them.
func renderVocabulary(files []registry.VocabularyFile) ([]File, error) {
	out := make([]File, 0, len(files))
	for _, f := range files {
		const packageLine = "package registry\n"
		_, declarations, ok := strings.Cut(f.Source, packageLine)
		if !ok {
			return nil, fmt.Errorf("registry %s has no %q line", f.Name, strings.TrimSpace(packageLine))
		}
		if strings.Contains("\n"+declarations, "\nimport") {
			return nil, fmt.Errorf("registry %s must not import anything: it is copied into package authz", f.Name)
		}
		name, ok := vocabularyOutputs[f.Name]
		if !ok {
			name = strings.TrimSuffix(f.Name, ".go") + "_gen.go"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "// Code generated from registry/%s by authzgen. DO NOT EDIT.\n// Regenerate with `mise run gen:authz`.\n\n", f.Name)
		b.WriteString("package authz\n")
		b.WriteString(declarations)
		formatted, err := format.Source([]byte(b.String()))
		if err != nil {
			return nil, fmt.Errorf("format %s: %w", name, err)
		}
		out = append(out, File{Path: "internal/authz/" + name, Content: formatted})
	}
	return out, nil
}
