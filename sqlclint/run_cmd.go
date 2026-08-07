package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	altsrc "github.com/urfave/cli-altsrc/v3"
	altyaml "github.com/urfave/cli-altsrc/v3/yaml"
	"github.com/urfave/cli/v3"

	"github.com/speakeasy-api/gram/sqlclint/catalog"
	"github.com/speakeasy-api/gram/sqlclint/ignore"
	"github.com/speakeasy-api/gram/sqlclint/query"
	"github.com/speakeasy-api/gram/sqlclint/rule"
	"github.com/speakeasy-api/gram/sqlclint/schema"
)

func newRunCommand(configPath *string) *cli.Command {
	cfg := altsrc.NewStringPtrSourcer(configPath)

	return &cli.Command{
		Name:  "run",
		Usage: "check every matched query file against the tenancy rule",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "schema-file",
				Usage:   "path to the SQL schema that defines the tenancy shape",
				Value:   "server/database/schema.sql",
				Sources: cli.NewValueSourceChain(altyaml.YAML("schema-file", cfg), cli.EnvVar("SQLCLINT_SCHEMA_FILE")),
			},
			&cli.StringFlag{
				Name:    "schema-dsn",
				Usage:   "read the tenancy shape from a live database instead of --schema-file",
				Sources: cli.NewValueSourceChain(altyaml.YAML("schema-dsn", cfg), cli.EnvVar("SQLCLINT_SCHEMA_DSN")),
			},
			&cli.StringFlag{
				Name:    "ignore-file",
				Usage:   "path globs to skip and grandfathered violations to tolerate",
				Value:   ".sqlclintignore",
				Sources: cli.NewValueSourceChain(altyaml.YAML("ignore-file", cfg), cli.EnvVar("SQLCLINT_IGNORE_FILE")),
			},
			&cli.StringSliceFlag{
				Name:    "include",
				Usage:   "glob of query files to check; repeatable",
				Sources: cli.NewValueSourceChain(altyaml.YAML("include", cfg), cli.EnvVar("SQLCLINT_INCLUDE")),
			},
			&cli.StringSliceFlag{
				Name:    "exclude",
				Usage:   "glob of query files to skip; repeatable",
				Sources: cli.NewValueSourceChain(altyaml.YAML("exclude", cfg), cli.EnvVar("SQLCLINT_EXCLUDE")),
			},
			&cli.BoolFlag{
				Name:  "write-ignore-file",
				Usage: "rewrite the ignore file from the current violations instead of failing",
			},
			&cli.BoolFlag{
				Name:  "github",
				Usage: "also emit GitHub Actions error annotations",
				Value: os.Getenv("GITHUB_ACTIONS") == "true",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return run(ctx, cmd, options{
				schemaFile:      cmd.String("schema-file"),
				schemaDSN:       cmd.String("schema-dsn"),
				ignoreFile:      cmd.String("ignore-file"),
				include:         cmd.StringSlice("include"),
				exclude:         cmd.StringSlice("exclude"),
				writeIgnoreFile: cmd.Bool("write-ignore-file"),
				github:          cmd.Bool("github"),
			})
		},
	}
}

type options struct {
	schemaFile      string
	schemaDSN       string
	ignoreFile      string
	include         []string
	exclude         []string
	writeIgnoreFile bool
	github          bool
}

func run(ctx context.Context, cmd *cli.Command, opts options) error {
	out := cmd.Root().Writer

	if len(opts.include) == 0 {
		return fmt.Errorf("no --include globs given; pass one or set include in the config file")
	}

	cat, err := catalog.Load()
	if err != nil {
		return fmt.Errorf("load rule catalog: %w", err)
	}

	var source schema.Source = schema.NewFileSource(opts.schemaFile)
	if opts.schemaDSN != "" {
		source = schema.NewDatabaseSource(opts.schemaDSN)
	}

	tables, err := source.Tables(ctx)
	if err != nil {
		return fmt.Errorf("load schema: %w", err)
	}

	ign, err := ignore.Load(opts.ignoreFile)
	if err != nil {
		return err
	}

	files, err := matchFiles(opts.include, opts.exclude, ign)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no query files matched %s", strings.Join(opts.include, ", "))
	}

	engine := rule.NewEngine(schema.NewClassifier(tables), cat)

	var (
		diagnostics []rule.Diagnostic
		violations  []ignore.Entry
		checked     int
		exempted    int
	)
	seenRefs := map[string]bool{}

	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read query file: %w", err)
		}

		for _, q := range query.Split(file, src) {
			if isTestQuery(q.Name) {
				continue
			}
			checked++
			seenRefs[q.Ref()] = true

			res := engine.Check(q)
			if res.Exempted {
				exempted++
			}

			// Structural problems are mistakes rather than debt, so they are
			// reported whatever the ignore file says.
			diagnostics = append(diagnostics, res.Structural...)

			entry, grandfathered := ign.Lookup(q.Ref())

			// An annotation is a reviewed decision and an ignore entry is untriaged
			// debt. A query holding both leaves a reader unable to tell which is
			// true, and keeps the debt entry alive after the review settled it.
			if grandfathered && res.Exempted {
				diagnostics = append(diagnostics, rule.Diagnostic{
					RuleID:  catalog.RedundantExemption,
					File:    q.File,
					Line:    q.Line,
					Query:   q.Name,
					Message: "the query is annotated and also grandfathered in the ignore file; delete the ignore entry and keep the annotation",
				})
				continue
			}

			if len(res.Scope) == 0 {
				if grandfathered {
					diagnostics = append(diagnostics, rule.Diagnostic{
						RuleID:  catalog.StaleIgnoreEntry,
						File:    q.File,
						Line:    q.Line,
						Query:   q.Name,
						Message: "the query no longer violates the tenancy rule; run `sqlclint run --write-ignore-file` and commit the removed line",
					})
				}
				continue
			}

			violations = append(violations, ignore.Entry{Ref: q.Ref(), Hash: q.Hash()})

			if !grandfathered {
				diagnostics = append(diagnostics, res.Scope...)
				continue
			}
			if entry.Hash != q.Hash() {
				diagnostics = append(diagnostics, rule.Diagnostic{
					RuleID:  catalog.ModifiedIgnoredQuery,
					File:    q.File,
					Line:    q.Line,
					Query:   q.Name,
					Message: fmt.Sprintf("the body changed since it was grandfathered (recorded %s, now %s), so the entry no longer covers this SQL", entry.Hash, q.Hash()),
				})
			}
		}
	}

	// Entries naming a query that no longer exists keep the file from reflecting
	// reality, which is the only thing that makes the ratchet meaningful.
	for ref := range ign.Entries {
		if !seenRefs[ref] {
			file, name, _ := strings.Cut(ref, "::")
			diagnostics = append(diagnostics, rule.Diagnostic{
				RuleID:  catalog.StaleIgnoreEntry,
				File:    file,
				Line:    1,
				Query:   name,
				Message: "the ignore file names a query that no longer exists; run `sqlclint run --write-ignore-file`",
			})
		}
	}

	if opts.writeIgnoreFile {
		if err := ignore.Save(opts.ignoreFile, ign.Globs, violations); err != nil {
			return err
		}
		fmt.Fprintf(out, "wrote %s: %d grandfathered %s across %d files (%d queries checked, %d annotated)\n",
			opts.ignoreFile, len(violations), plural(len(violations), "violation", "violations"), len(files), checked, exempted)
		return nil
	}

	return report(out, diagnostics, opts.github, checked, exempted, len(violations))
}

func report(out io.Writer, diagnostics []rule.Diagnostic, github bool, checked, exempted, grandfathered int) error {
	slices.SortFunc(diagnostics, func(a, b rule.Diagnostic) int {
		if a.File != b.File {
			return strings.Compare(a.File, b.File)
		}
		return a.Line - b.Line
	})

	byRule := map[string]int{}
	for _, d := range diagnostics {
		byRule[d.RuleID]++
		fmt.Fprintf(out, "%s\n    see: sqlclint rule %s\n", d, d.RuleID)
		if github {
			fmt.Fprintf(out, "::error file=%s,line=%d::[%s] %s: %s\n", d.File, d.Line, d.RuleID, d.Query, d.Message)
		}
	}

	if len(diagnostics) == 0 {
		fmt.Fprintf(out, "ok: %d queries checked, %d annotated, %d grandfathered\n", checked, exempted, grandfathered)
		return nil
	}

	fmt.Fprintf(out, "\n%d %s across %d checked queries:\n", len(diagnostics), plural(len(diagnostics), "problem", "problems"), checked)
	for _, id := range slices.Sorted(maps.Keys(byRule)) {
		fmt.Fprintf(out, "  %4d  %s\n", byRule[id], id)
	}

	return fmt.Errorf("%d %s found", len(diagnostics), plural(len(diagnostics), "problem", "problems"))
}

// matchFiles resolves the include globs, subtracts the exclude globs and the
// ignore file's path globs, and returns a sorted, deduplicated list.
func matchFiles(include, exclude []string, ign *ignore.File) ([]string, error) {
	seen := map[string]bool{}
	var out []string

	for _, pattern := range include {
		matches, err := glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("expand --include %q: %w", pattern, err)
		}
		for _, m := range matches {
			if seen[m] || matchAny(exclude, m) || ign.Skipped(m) {
				continue
			}
			seen[m] = true
			out = append(out, m)
		}
	}

	slices.Sort(out)
	return out, nil
}

// glob expands a pattern where "**" spans any number of directories, which
// filepath.Glob does not support.
func glob(pattern string) ([]string, error) {
	root, rest, hasDoubleStar := strings.Cut(pattern, "**")
	if !hasDoubleStar {
		return filepath.Glob(pattern)
	}

	root = strings.TrimSuffix(root, "/")
	if root == "" {
		root = "."
	}
	rest = strings.TrimPrefix(rest, "/")

	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if matchSuffix(rest, rel) {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	return out, nil
}

// matchSuffix matches the portion of a pattern after "**" against any trailing
// run of path segments.
func matchSuffix(pattern, name string) bool {
	segments := strings.Split(name, "/")
	for i := range segments {
		if ok, err := filepath.Match(pattern, strings.Join(segments[i:], "/")); err == nil && ok {
			return true
		}
	}
	return false
}

func matchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if matchSuffix(p, name) {
			return true
		}
	}
	return false
}

// isTestQuery reports whether a query exists only to support tests, by the
// naming conventions already used across the repository's query files.
func isTestQuery(name string) bool {
	return strings.HasSuffix(name, "ForTest") || strings.HasPrefix(name, "Seed")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
