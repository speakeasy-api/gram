---
name: glint
description: Conventions for authoring or editing analyzers in the `glint/` Go static-analysis package — Gram's custom golangci-lint plugin built on `go/analysis`. Activate this skill whenever the task involves adding, modifying, or testing a `glint` analyzer (new rule key, new diagnostic, settings struct, fixture under `glint/testdata/`, wiring in `BuildAnalyzers`), even if the user does not say "glint" explicitly — phrases like "add a lint rule", "write a custom analyzer", "go/analysis", or "enforce X via golangci-lint" should trigger it.
metadata:
  relevant_files:
    - "glint/**/*.go"
---

`glint` is Gram's package of custom `go/analysis` analyzers, and `gcl` is the golangci-lint custom-build configuration that loads `glint` as a plugin. Together they automate enforcement of project coding conventions and bug-prevention rules so the same feedback isn't re-litigated in PR review.

- Plugin entry point: [glint/plugin.go](../../../glint/plugin.go) — registers the plugin via `register.Plugin("glint", New)`, defines the `settings`/`ruleSettings` structs, and lists every analyzer in `BuildAnalyzers`.
- gcl wiring: [server/.custom-gcl.yml](../../../server/.custom-gcl.yml) — declares `github.com/speakeasy-api/gram/glint` as the imported plugin module for the custom golangci-lint binary.

The current set of analyzers and their rule keys is the source of truth in `BuildAnalyzers`. Read that function before adding a new one.

## Quick start

When adding a new analyzer:

1. Pick a kebab-case rule key and a matching `snake_case.go` file name in `glint/`. See [Naming conventions](#naming-conventions).
2. Define `<rule>Settings` with `Disabled bool`. Add nothing else unless there's a concrete user-facing reason. See [Settings and ignore mechanisms](#settings-and-ignore-mechanisms).
3. Implement detection in `new<Rule>Analyzer(rule <rule>Settings) *analysis.Analyzer`. Prefer type-based checks over AST shape, and AST shape over string matching. See [Detection methodology](#detection-methodology).
4. Write the diagnostic with imperative tone and the offending identifier inlined via `%q`. See [Diagnostic message style](#diagnostic-message-style).
5. Wire the rule into [glint/plugin.go](../../../glint/plugin.go): add a `ruleSettings` field with the kebab-case JSON tag, then an `if !p.settings.Rules.<X>.Disabled` block in `BuildAnalyzers`.
6. Add `<rule>_test.go` with `analysistest.Run(...)` and a fixture under `glint/testdata/src/<fixture>/`. Update `disabledAllRulesPlugin()` and the count in `TestBuildAnalyzersAllEnabled` in [glint/plugin_test.go](../../../glint/plugin_test.go). See [Testing](#testing).

## Detection methodology

Strongly prefer type-based detection over AST-shape matching, and AST-shape matching over source-string matching. Type-based checks are robust against renames, aliases, vendored packages, and import-path quirks; AST shape is robust against most refactors but blind to type identity; raw string matching is fragile and should only be reached for when the signal genuinely lives in source text rather than the type system.

### Prefer types via `pass.TypesInfo`

[glint/no_repo_fields_in_service.go](../../../glint/no_repo_fields_in_service.go) walks struct fields, resolves each field's type via `pass.TypesInfo.TypeOf`, then narrows through `*types.Pointer` → `*types.Named` to identify a sqlc-generated repo handle:

```go
fieldType := pass.TypesInfo.TypeOf(field.Type)
if fieldType == nil {
    continue
}

ptr, ok := fieldType.(*types.Pointer)
if !ok {
    continue
}

named, ok := ptr.Elem().(*types.Named)
if !ok {
    continue
}

if !isSqlcGenerated(pass.Fset, named.Obj(), cache) {
    continue
}
```

Reach for type assertions on `types.Type` (`*types.Named`, `*types.Pointer`, `*types.Slice`, `*types.Map`, `*types.Interface`); use `pass.TypesInfo.Uses[ident]` / `pass.TypesInfo.Defs[ident]` to resolve identifiers to their declared `types.Object`; inspect method receivers via `Recv()` when checking method-set rules. The plugin advertises `register.LoadModeTypesInfo` from `GetLoadMode` in [glint/plugin.go](../../../glint/plugin.go), so type information is guaranteed to be loaded.

### AST shape is acceptable when types are not load-bearing

When the rule is fundamentally about the _shape_ of code rather than its types, AST matching is fine. [glint/no_anonymous_defer.go](../../../glint/no_anonymous_defer.go) walks `*ast.DeferStmt` and asserts the call target is `*ast.FuncLit`:

```go
ast.Inspect(file, func(node ast.Node) bool {
    deferStmt, ok := node.(*ast.DeferStmt)
    if !ok {
        return true
    }

    if _, ok := deferStmt.Call.Fun.(*ast.FuncLit); !ok {
        return true
    }

    pass.ReportRangef(deferStmt, "%s", message)
    return true
})
```

There's no `*types.Type` that captures "anonymous deferred function" — the property only exists at the AST level — so AST matching is the right tool.

### Source-string matching is a last resort

Raw `strings.Contains` / regex over file content is fragile and almost never warranted. The only string-matching helper currently in the package is `isSqlcGenerated` in [glint/no_repo_fields_in_service.go](../../../glint/no_repo_fields_in_service.go), and only because the "sqlc-generated" signal lives in a generated-file header comment that has no type-system representation:

```go
f, err := parser.ParseFile(token.NewFileSet(), pos.Filename, nil, parser.ParseComments|parser.PackageClauseOnly)
if err == nil && ast.IsGenerated(f) {
    for _, cg := range f.Comments {
        if strings.Contains(cg.Text(), "sqlc") {
            result = true
            break
        }
    }
}
```

Note the helper caches results by filename, parses with `parser.PackageClauseOnly` to skip body parsing, and uses `ast.IsGenerated` to confirm the standard generated-code header before substring-matching. If you find yourself reaching for string matching, justify in a comment why type-based detection won't work — future readers will assume it was a last resort.

## Settings and ignore mechanisms

Each analyzer accepts exactly one common setting: `Disabled bool`. Don't add allowlist fields (`Ignored []string`, regex includes/excludes, package globs, etc.) — they're a maintenance burden, they obscure the rule's contract, and golangci-lint already understands `//nolint` directives as the standard opt-out path.

```go
type noRepoFieldsInServiceSettings struct {
    Disabled bool `json:"disabled"`
}
```

The default opt-out for end users is the standard `//nolint:glint` directive (or `//nolint:glint:<rule-key>` for a specific rule). golangci-lint applies this without any analyzer-side wiring. If a particular violation is genuinely intentional in repo code, add `//nolint:glint:<rule-key>` with a brief comment explaining why.

The only setting-shape exception today is **narrow message customization**: two analyzers (`no-anonymous-defer`, `enforce-o11y-conventions`) expose a `Message string` that gets appended to the default diagnostic when set. From [glint/no_anonymous_defer.go](../../../glint/no_anonymous_defer.go):

```go
type noAnonymousDeferSettings struct {
    Disabled bool   `json:"disabled"`
    Message  string `json:"message"`
}
```

```go
message := noAnonymousDeferDefaultMessage
if rule.Message != "" {
    message += ": " + rule.Message
}
```

Add a `Message` field only if there's a concrete reason for end-user customization. Anything more elaborate than a single appended-suffix string warrants a discussion before being added.

## Diagnostic message style

Diagnostics tell the reader what to **do**, not just what's wrong. Imperative present-tense, action-oriented, with the offending identifier inlined where it aids resolution. No trailing punctuation, no leading capital, match the tone of the existing analyzers.

<bad-example>

```go
pass.ReportRangef(field, "Repo field detected in service struct.")
```

Past-tense observation, capitalized, trailing period — describes the symptom but doesn't tell the reader what to change.

</bad-example>

<good-example>

```go
pass.ReportRangef(field, "field %q in %s has type %s which is sqlc-generated; services should use *pgxpool.Pool and create repo instances in methods",
    field.Names[0].Name, s.name, fieldType)
```

(from [glint/no_repo_fields_in_service.go](../../../glint/no_repo_fields_in_service.go))

Names the offending field with `%q`, explains why it's wrong, and tells the reader what shape the code should take instead.

</good-example>

<good-example>

```go
pass.ReportRangef(deferStmt, "%s", message) // message = "avoid anonymous deferred functions"
```

(from [glint/no_anonymous_defer.go](../../../glint/no_anonymous_defer.go))

Imperative ("avoid"), short, no trailing punctuation.

</good-example>

When the diagnostic has a mechanical fix, attach a `SuggestedFix` so editor quick-fix actions and `golangci-lint --fix` can apply it. See [Suggested fixes](#suggested-fixes).

### `Analyzer.Doc`

Set the `Doc` field on every `*analysis.Analyzer`. It's what `go vet -<rule>` and IDE tooling show users when surfacing the rule. Keep it short and descriptive, and where the diagnostic has a single canonical default message, reuse the same constant for both so they stay in sync. From [glint/no_sql_err_no_rows.go](../../../glint/no_sql_err_no_rows.go):

```go
const (
    noSqlErrNoRowsAnalyzer       = "nosqlerrnorows"
    noSqlErrNoRowsDefaultMessage = "use github.com/jackc/pgx/v5.ErrNoRows instead of database/sql.ErrNoRows"
)

return &analysis.Analyzer{
    Name: noSqlErrNoRowsAnalyzer,
    Doc:  noSqlErrNoRowsDefaultMessage,
    // ...
}
```

## Suggested fixes

When the diagnostic has a mechanical fix that's always safe to apply, attach `SuggestedFixes` to the `analysis.Diagnostic` so editor quick-fix actions and `golangci-lint --fix` can apply it. [glint/no_sql_err_no_rows.go](../../../glint/no_sql_err_no_rows.go) is the worked example:

```go
pass.Report(analysis.Diagnostic{
    Pos:      occ.Pos(),
    End:      occ.End(),
    Category: noSqlErrNoRowsAnalyzer,
    Message:  noSqlErrNoRowsDefaultMessage,
    SuggestedFixes: []analysis.SuggestedFix{{
        Message:   noSqlErrNoRowsFixMessage,
        TextEdits: edits,
    }},
})
```

Things to keep in mind when assembling `TextEdit`s ([upstream rules](https://github.com/golang/tools/blob/master/go/analysis/doc/suggested_fixes.md)):

- Each `TextEdit` applies to a single file; `End` must not be earlier in the file than `Pos`; for a pure insertion, set `End` equal to `Pos` (or `token.NoPos`). Edits within the same diagnostic must not overlap.
- Build replacement text from AST nodes via `go/format.Node` rather than hand-concatenating strings, so the output stays gofmt-clean. For trivial selector or identifier replacements, raw byte literals are fine.
- If the fix involves cross-occurrence work (e.g. removing an import once _all_ call sites have been rewritten), attach the cross-cutting edits to **every** diagnostic's `SuggestedFix`. `analysistest`'s three-way merge dedupes identical edits, but splitting them risks an editor quick-fix branch removing the import while sibling occurrences remain unfixed. See the explanatory comment in [glint/no_sql_err_no_rows.go](../../../glint/no_sql_err_no_rows.go) for the reasoning.

### Share editing helpers in subpackages

Common edit logic belongs in a shared subpackage under `glint/`, not duplicated across analyzers. Today's example is [glint/imports/](../../../glint/imports/), which exposes reusable helpers for emitting `analysis.TextEdit` values that add or remove imports:

- `LocalName(file, path, defaultName) (string, bool)` — resolves the local identifier (alias or default) for an imported package, or reports that the import is absent.
- `Add(file, path) (analysis.TextEdit, bool)` — emits an edit that inserts a new import into the file's first grouped import block.
- `Remove(fset, file, path) (analysis.TextEdit, bool)` — emits an edit that deletes the import line for `path`.

When you find yourself writing AST-rewriting helpers that another analyzer would plausibly need, add them under `glint/<helper>/` with a package-level docstring describing the intended use in `SuggestedFixes`, and call them from your analyzer's `Run` function. Future analyzers should compose the helper rather than reimplement it.

### Testing fixes

Test fix application with `analysistest.RunWithSuggestedFixes` instead of `analysistest.Run`. The harness applies the suggested fixes to the input file and compares the result against a `<fixture>.go.golden` file in the same fixture directory:

```go
func TestNoSqlErrNoRows(t *testing.T) {
    t.Parallel()

    testdata := analysistest.TestData()
    analysistest.RunWithSuggestedFixes(t, testdata, newNoSqlErrNoRowsAnalyzer(noSqlErrNoRowsSettings{}), "nosqlerrnorows")
}
```

## Naming conventions

There are three parallel namings to keep aligned for each analyzer: the **rule key** (user-facing, in YAML/JSON settings and `//nolint` directives), the **Go identifiers** (constant, settings struct, constructor), and the **file name**.

### Rule key (kebab-case, user-facing)

- Prohibition rules use a `no-` prefix: `no-anonymous-defer`, `no-repo-fields-in-service`, `no-sql-err-no-rows`, `no-direct-chat-message-insert`.
- Other rules use a descriptive predicate that reads naturally as an assertion about correct code: `enforce-o11y-conventions`, `service-has-attach-func`, `service-has-service-assertion`, `audit-event-urn-naming`, `audit-event-typed-snapshot`. Use a verb-led key when the rule is fundamentally about an action (`enforce-...`); use a `<subject>-has-<property>` or `<subject>-<aspect>` shape when the rule is an invariant check.

### Go identifiers (lowerCamel mirroring the rule key)

The analyzer name constant, settings struct, and constructor mirror the rule key in lowerCamel form, with Go-style initialisms (`SQL`, `URN`, `O11y`):

| Rule key                    | Constant                         | Settings                         | Constructor                         |
| --------------------------- | -------------------------------- | -------------------------------- | ----------------------------------- |
| `no-anonymous-defer`        | `noAnonymousDeferAnalyzer`       | `noAnonymousDeferSettings`       | `newNoAnonymousDeferAnalyzer`       |
| `no-repo-fields-in-service` | `noRepoFieldsInServiceAnalyzer`  | `noRepoFieldsInServiceSettings`  | `newNoRepoFieldsInServiceAnalyzer`  |
| `no-sql-err-no-rows`        | `noSqlErrNoRowsAnalyzer`         | `noSqlErrNoRowsSettings`         | `newNoSqlErrNoRowsAnalyzer`         |
| `enforce-o11y-conventions`  | `enforceO11yConventionsAnalyzer` | `enforceO11yConventionsSettings` | `newEnforceO11yConventionsAnalyzer` |
| `service-has-attach-func`   | `serviceHasAttachFuncAnalyzer`   | `serviceHasAttachFuncSettings`   | `newServiceHasAttachFuncAnalyzer`   |
| `audit-event-urn-naming`    | `auditEventURNNamingAnalyzer`    | `auditEventURNNamingSettings`    | `newAuditEventURNNamingAnalyzer`    |

The analyzer-name string constant is also the `Name` field of the returned `*analysis.Analyzer`, and it's the directory name `analysistest` looks for under `glint/testdata/src/`. Keep them identical so the test harness, the diagnostic source, and the rule key all line up.

### File names (snake_case mirroring the rule key)

Each analyzer lives in `glint/<rule-key-with-underscores>.go` with a sibling `_test.go`:

- `no-anonymous-defer` → `glint/no_anonymous_defer.go` + `glint/no_anonymous_defer_test.go`
- `no-repo-fields-in-service` → `glint/no_repo_fields_in_service.go` + `glint/no_repo_fields_in_service_test.go`
- `audit-event-urn-naming` → `glint/audit_event_urn_naming.go` + `glint/audit_event_urn_naming_test.go`

### `ruleSettings` JSON tag

In `glint/plugin.go`, the field on `ruleSettings` is UpperCamel and the `json:"..."` tag is the kebab-case rule key:

```go
NoRepoFieldsInService noRepoFieldsInServiceSettings `json:"no-repo-fields-in-service"`
```

The JSON tag is what end users write under `linters-settings.glint.rules.<key>.disabled` in `.golangci.yml`, so it must match the rule key exactly.

## Testing

Every analyzer ships with an `analysistest`-based test plus a fixture directory.

### Default-settings test

Use `golang.org/x/tools/go/analysis/analysistest` and pass the constructor with zero-value settings. The fourth argument is the directory name `analysistest` looks up under `glint/testdata/src/`:

```go
func TestNoAnonymousDefer(t *testing.T) {
    t.Parallel()

    testdata := analysistest.TestData()
    analysistest.Run(t, testdata, newNoAnonymousDeferAnalyzer(noAnonymousDeferSettings{}), "noanonymousdefer")
}
```

### Fixtures

Fixtures live at `glint/testdata/src/<fixture-name>/<file>.go`. Mark expected diagnostics with `// want "<regex>"` comments on the offending line:

```go
package noanonymousdefer

func bad() {
    defer func() {}() // want "avoid anonymous deferred functions"
}
```

For diagnostics whose text contains regex metacharacters (`*`, `(`, `.`, etc.), escape them in the `// want` directive and use backticks instead of double quotes when the message itself contains quotes:

```go
repo *repo.Queries // want `field "repo" in Service has type \*serviceannotationrepofield/repo.Queries which is sqlc-generated; services should use \*pgxpool.Pool and create repo instances in methods`
```

### Non-default settings

For each non-default setting variant (e.g. a custom `Message`), add a separate test function and a separate fixture directory whose `// want` text reflects the customized output:

```go
func TestNoAnonymousDeferCustomMessage(t *testing.T) {
    t.Parallel()

    testdata := analysistest.TestData()
    analysistest.Run(
        t,
        testdata,
        newNoAnonymousDeferAnalyzer(noAnonymousDeferSettings{Message: "use a named deferred helper instead"}),
        "noanonymousdefercustommessage",
    )
}
```

### Disabled-rule path

The disabled-rule test surface is centralized in [glint/plugin_test.go](../../../glint/plugin_test.go), which exposes a `disabledAllRulesPlugin()` helper returning a `*plugin` with every rule's `Disabled: true`. Two assertions live there:

- `TestBuildAnalyzersAllDisabled` — `BuildAnalyzers` returns an empty slice when everything is disabled.
- `TestBuildAnalyzersAllEnabled` — asserts the expected analyzer count for a zero-value `*plugin{}`. Bump this number whenever a new analyzer is added.

Each analyzer's own test file then adds a per-rule registration test: start from `disabledAllRulesPlugin()`, flip only the rule under test back on, and assert `BuildAnalyzers` returns exactly that one analyzer with the expected name. From [glint/no_repo_fields_in_service_test.go](../../../glint/no_repo_fields_in_service_test.go):

```go
func TestBuildAnalyzersSkipsDisabledNoRepoFieldsInService(t *testing.T) {
    t.Parallel()

    p := disabledAllRulesPlugin()
    p.settings.Rules.NoRepoFieldsInService.Disabled = false

    analyzers, err := p.BuildAnalyzers()
    require.NoError(t, err)
    require.Len(t, analyzers, 1)
    require.Equal(t, noRepoFieldsInServiceAnalyzer, analyzers[0].Name)
}
```

When adding a new analyzer, extend `disabledAllRulesPlugin()` with the new `Disabled: true` field, increment the expected count in `TestBuildAnalyzersAllEnabled`, and add the per-rule registration test alongside the analyzer's own unit tests.
