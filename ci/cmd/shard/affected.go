package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// module holds the fields of `go list -m -json` output that shard cares about.
type module struct {
	Path string
	Dir  string
}

// selection says which test packages a run covers.
type selection struct {
	// pkgs are the packages to test.
	pkgs []pkg

	// everything is true when pkgs is the whole suite, and reason says why.
	everything bool
	reason     string
}

// readChanged reads a list of changed files: one path per line, relative to
// the module root, with forward slashes (the output of `git diff --name-only`
// run there).
func readChanged(file string) ([]string, error) {
	f, err := os.Open(filepath.Clean(file))
	if err != nil {
		return nil, fmt.Errorf("open changed files list: %w", err)
	}
	defer func() { _ = f.Close() }()

	var changed []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			changed = append(changed, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read changed files list: %w", err)
	}

	return changed, nil
}

// currentModule returns the main module of the working directory.
func currentModule(stderr io.Writer) (module, error) {
	cmd := exec.Command("go", "list", "-m", "-json")
	cmd.Stderr = stderr

	out, err := cmd.Output()
	if err != nil {
		return module{}, fmt.Errorf("go list -m: %w", err)
	}

	var m module
	if err := json.NewDecoder(bytes.NewReader(out)).Decode(&m); err != nil {
		return module{}, fmt.Errorf("decode go list -m output: %w", err)
	}

	return m, nil
}

// matchesAny reports whether file is one of entries or lies below one that
// ends in a slash.
func matchesAny(file string, entries []string) bool {
	for _, entry := range entries {
		if file == entry || (strings.HasSuffix(entry, "/") && strings.HasPrefix(file, entry)) {
			return true
		}
	}

	return false
}

// packageDir returns the directory, relative to the module root, of the
// package a changed file belongs to. Go files belong to the package in their
// directory, which may no longer exist if the change deleted it. Any other
// file (testdata, an embedded template, a query file) belongs to the nearest
// directory at or above it that hasGo says holds a package. ok is false when
// there is none.
func packageDir(file string, hasGo func(dir string) bool) (dir string, ok bool) {
	dir = path.Dir(file)

	if strings.HasSuffix(file, ".go") {
		return dir, true
	}

	for {
		if hasGo(dir) {
			return dir, true
		}
		if dir == "." {
			return "", false
		}
		dir = path.Dir(dir)
	}
}

// affected narrows pkgs to the test packages a change can reach in one step:
// the packages the changed files belong to and the packages that import one
// of those directly, tests included. Packages further down the import graph
// are left out on purpose, which is what makes the selection small; a run
// that needs certainty tests everything instead.
//
// Tests can depend on a file without importing anything. fullOn lists the
// files every package may read that way, such as a database schema: a change
// to one selects the whole suite, as does a file that cannot be pinned to a
// package. readers maps a file to the directories of the few packages whose
// tests read it, which then count as touched.
func affected(pkgs []pkg, mod string, changed []string, fullOn []string, readers map[string][]string, hasGo func(dir string) bool) selection {
	touched := map[string]bool{}

	for _, file := range changed {
		if matchesAny(file, fullOn) {
			return selection{pkgs: pkgs, everything: true, reason: file + " affects every package"}
		}

		for _, dir := range readers[file] {
			touched[path.Join(mod, dir)] = true
		}

		dir, ok := packageDir(file, hasGo)
		if !ok {
			return selection{pkgs: pkgs, everything: true, reason: file + " does not belong to a Go package"}
		}

		touched[path.Join(mod, dir)] = true
	}

	var kept []pkg
	for _, p := range pkgs {
		if touched[p.ImportPath] || slices.ContainsFunc(slices.Concat(p.Imports, p.TestImports, p.XTestImports), func(imp string) bool {
			return touched[imp]
		}) {
			kept = append(kept, p)
		}
	}

	return selection{pkgs: kept, everything: false, reason: ""}
}

// dirHasGo reports whether a directory below root holds Go source files.
func dirHasGo(root string) func(dir string) bool {
	return func(dir string) bool {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			return false
		}

		return slices.ContainsFunc(entries, func(e os.DirEntry) bool {
			return !e.IsDir() && strings.HasSuffix(e.Name(), ".go")
		})
	}
}

// selectPackages applies the -changed and -full-on flags to pkgs. Without a
// changed files list every package is selected.
func selectPackages(pkgs []pkg, changedPath string, fullOn string, readersSpec string, stderr io.Writer) (selection, error) {
	if changedPath == "" {
		return selection{pkgs: pkgs, everything: true, reason: "no changed files list was given"}, nil
	}

	changed, err := readChanged(changedPath)
	if err != nil {
		return selection{}, err
	}

	mod, err := currentModule(stderr)
	if err != nil {
		return selection{}, err
	}

	var entries []string
	if fullOn != "" {
		entries = strings.Split(fullOn, ",")
	}

	readers, err := parseReaders(readersSpec)
	if err != nil {
		return selection{}, err
	}

	return affected(pkgs, mod.Path, changed, entries, readers, dirHasGo(mod.Dir)), nil
}

// parseReaders reads a -readers value: comma-separated <file>=<package
// directory> pairs, both relative to the module root. A file may be repeated.
func parseReaders(spec string) (map[string][]string, error) {
	readers := map[string][]string{}
	if spec == "" {
		return readers, nil
	}

	for pair := range strings.SplitSeq(spec, ",") {
		file, dir, ok := strings.Cut(pair, "=")
		if !ok || file == "" || dir == "" {
			return nil, fmt.Errorf("reader %q is not in <file>=<package directory> form", pair)
		}

		readers[file] = append(readers[file], dir)
	}

	return readers, nil
}

func totalWeight(pkgs []pkg) int64 {
	var sum int64
	for _, p := range pkgs {
		sum += p.weight
	}

	return sum
}

// shardCount returns how many of at most limit shards a selection needs. A
// shard's share of the whole suite is the budget: the selection gets as many
// shards as it takes to stay within it, and none when it is empty.
func shardCount(selected int64, total int64, limit int) int {
	if selected <= 0 {
		return 0
	}

	budget := max(total/int64(limit), 1)

	return int(min((selected+budget-1)/budget, int64(limit)))
}

// plan is the JSON document the plan subcommand prints.
type plan struct {
	// Shards lists the shard indexes to run, e.g. [1, 2]. It is empty when no
	// test package is selected.
	Shards []int `json:"shards"`
	// Selected and Total count test packages.
	Selected int `json:"selected"`
	Total    int `json:"total"`
	// Reason says why the whole suite runs, when it does.
	Reason string `json:"reason,omitempty"`
}

const planUsage = `Usage: shard plan -max <shards> [flags] [packages...]

Prints, as JSON, how many shards the selected test packages need and why.

Flags:
`

func runPlan(argv []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("shard plan", flag.ContinueOnError)
	// Errors are reported by the caller so they are not printed twice.
	fs.SetOutput(io.Discard)

	limit := fs.Int("max", 0, "the most shards to use: the number the whole suite runs on")
	tags := fs.String("tags", "", "comma-separated build tags to pass to 'go list'")
	timingsPath := fs.String("timings", "", "timings file to weight packages by (default: the checked-in ci/cmd/shard/timings.txt)")
	changedPath := fs.String("changed", "", "file listing changed paths relative to the module root; selects only the packages they belong to and those packages' direct importers")
	fullOn := fs.String("full-on", "", "comma-separated paths (a trailing slash matches a directory tree) whose change selects every package")
	readers := fs.String("readers", "", "comma-separated <file>=<package directory> pairs: a change to the file also selects that package")

	if err := fs.Parse(argv); err != nil {
		fs.SetOutput(stderr)
		fmt.Fprint(stderr, planUsage)
		fs.PrintDefaults()

		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return err
	}

	if *limit < 1 {
		return fmt.Errorf("-max %d must be at least 1", *limit)
	}

	patterns := fs.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	for i, p := range patterns {
		patterns[i] = expandPattern(p)
	}

	pkgs, err := listTestPackages(*tags, patterns, stderr)
	if err != nil {
		return err
	}

	timings, err := loadTimings(*timingsPath)
	if err != nil {
		return err
	}

	weigh(pkgs, timings)

	chosen, err := selectPackages(pkgs, *changedPath, *fullOn, *readers, stderr)
	if err != nil {
		return err
	}

	count := *limit
	if !chosen.everything {
		count = shardCount(totalWeight(chosen.pkgs), totalWeight(pkgs), *limit)
	}

	result := plan{Shards: []int{}, Selected: len(chosen.pkgs), Total: len(pkgs), Reason: chosen.reason}
	for i := 1; i <= count; i++ {
		result.Shards = append(result.Shards, i)
	}

	if chosen.everything {
		fmt.Fprintf(stderr, "plan: every test package on %d shards: %s\n", count, chosen.reason)
	} else {
		fmt.Fprintf(stderr, "plan: %d of %d test packages on %d shards\n", len(chosen.pkgs), len(pkgs), count)
		for _, p := range chosen.pkgs {
			fmt.Fprintf(stderr, "  %s\n", p.ImportPath)
		}
	}

	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return fmt.Errorf("write plan: %w", err)
	}

	return nil
}
