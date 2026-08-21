// Command shard splits the Go test packages under one or more directories into
// deterministic, roughly balanced shards so CI can spread a suite over several
// runners.
//
// Usage:
//
//	go run ./ci/cmd/shard -i 1/4 ./server
//
// It prints the import paths of the packages assigned to the requested shard,
// one per line. Packages without test files are never printed: they are already
// compiled by the build and lint jobs, so testing them buys nothing.
//
// Packages are weighted by the size of their test sources — a stand-in for how
// long they take to run — and packed largest first into whichever shard is
// lightest so far. The assignment is a pure function of the package list, so
// every shard reaches the same answer without sharing state or recorded
// timings.
package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const usage = `Usage: shard -i <index>/<total> [flags] [packages...]

Prints the test packages assigned to one shard, one import path per line.
Directory arguments are expanded to match the tree below them, so "./server"
lists everything under the server directory. Defaults to "./..." when no
packages are given.

Flags:
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "shard: %v\n", err)
		os.Exit(1)
	}
}

// pkg holds the fields of `go list -json` output that shard cares about.
type pkg struct {
	ImportPath   string
	Dir          string
	TestGoFiles  []string
	XTestGoFiles []string

	// weight approximates how long the package takes to test.
	weight int64
}

func run(argv []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("shard", flag.ContinueOnError)
	// Errors are reported by the caller so they are not printed twice.
	fs.SetOutput(io.Discard)

	spec := fs.String("i", "", "shard to print, as <index>/<total> (1-indexed), e.g. 1/4")
	tags := fs.String("tags", "", "comma-separated build tags to pass to 'go list'")

	if err := fs.Parse(argv); err != nil {
		fs.SetOutput(stderr)
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()

		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return err
	}

	index, total, err := parseShard(*spec)
	if err != nil {
		return err
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

	selected := assign(pkgs, index, total)

	var selectedWeight, totalWeight int64
	for _, p := range pkgs {
		totalWeight += p.weight
	}
	for _, p := range selected {
		selectedWeight += p.weight
	}

	fmt.Fprintf(stderr, "shard %d/%d: %d of %d test packages, %dKB of %dKB test sources\n",
		index, total, len(selected), len(pkgs), selectedWeight/1000, totalWeight/1000)

	for _, p := range selected {
		if _, err := fmt.Fprintln(stdout, p.ImportPath); err != nil {
			return fmt.Errorf("write package list: %w", err)
		}
	}

	return nil
}

// parseShard reads an "<index>/<total>" shard spec, where index is 1-indexed.
func parseShard(spec string) (index int, total int, err error) {
	if spec == "" {
		return 0, 0, errors.New("missing -i: expected a shard like -i 1/4")
	}

	rawIndex, rawTotal, ok := strings.Cut(spec, "/")
	if !ok {
		return 0, 0, fmt.Errorf("shard %q is not in <index>/<total> form, e.g. 1/4", spec)
	}

	index, err = strconv.Atoi(rawIndex)
	if err != nil {
		return 0, 0, fmt.Errorf("shard index %q is not a number", rawIndex)
	}

	total, err = strconv.Atoi(rawTotal)
	if err != nil {
		return 0, 0, fmt.Errorf("shard total %q is not a number", rawTotal)
	}

	if total < 1 {
		return 0, 0, fmt.Errorf("shard total %d must be at least 1", total)
	}

	if index < 1 || index > total {
		return 0, 0, fmt.Errorf("shard index %d must be between 1 and %d", index, total)
	}

	return index, total, nil
}

// expandPattern turns a directory argument such as "./server" into the package
// pattern "./server/...", so callers can name a tree rather than spell out a
// pattern. Arguments that already end in "..." are left alone.
func expandPattern(pattern string) string {
	if strings.HasSuffix(pattern, "...") {
		return pattern
	}

	return strings.TrimSuffix(pattern, "/") + "/..."
}

// listTestPackages returns the packages matching patterns that have test files,
// each weighted by the size of its test sources.
func listTestPackages(tags string, patterns []string, stderr io.Writer) ([]pkg, error) {
	args := []string{"list", "-json=ImportPath,Dir,TestGoFiles,XTestGoFiles"}
	if tags != "" {
		args = append(args, "-tags="+tags)
	}
	args = append(args, patterns...)

	cmd := exec.Command("go", args...)
	cmd.Stderr = stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}

	var pkgs []pkg
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p pkg
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode go list output: %w", err)
		}

		if len(p.TestGoFiles)+len(p.XTestGoFiles) == 0 {
			continue
		}

		p.weight, err = weigh(p)
		if err != nil {
			return nil, err
		}

		pkgs = append(pkgs, p)
	}

	return pkgs, nil
}

// weigh sums the size of a package's test sources. Each file also contributes a
// byte of its own so that packages of empty test files still differ in weight.
func weigh(p pkg) (int64, error) {
	var weight int64

	for _, name := range slices.Concat(p.TestGoFiles, p.XTestGoFiles) {
		path := filepath.Join(p.Dir, name)

		info, err := os.Stat(path)
		if err != nil {
			return 0, fmt.Errorf("stat test file %s: %w", path, err)
		}

		weight += info.Size() + 1
	}

	return weight, nil
}

// assign packs pkgs into total shards, heaviest package first into the shard
// carrying the least weight so far, and returns the packages that landed in
// shard index (1-indexed), sorted by import path.
func assign(pkgs []pkg, index int, total int) []pkg {
	ordered := slices.Clone(pkgs)
	slices.SortFunc(ordered, func(a, b pkg) int {
		// Ties break on import path so the order never depends on how the
		// packages happened to arrive.
		return cmp.Or(
			cmp.Compare(b.weight, a.weight),
			cmp.Compare(a.ImportPath, b.ImportPath),
		)
	})

	// Weights are always positive, so an empty shard is lighter than any shard
	// holding a package: with more shards than packages, the shards past the
	// last package are always empty. Only tracking the ones that can win keeps
	// a spec like -i 1/1000000000 from allocating its way to an OOM.
	if total > len(ordered) {
		if index > len(ordered) {
			return nil
		}

		total = len(ordered)
	}

	loads := make([]int64, total)
	var selected []pkg

	for _, p := range ordered {
		lightest := 0
		for i := 1; i < total; i++ {
			if loads[i] < loads[lightest] {
				lightest = i
			}
		}

		loads[lightest] += p.weight

		if lightest == index-1 {
			selected = append(selected, p)
		}
	}

	slices.SortFunc(selected, func(a, b pkg) int {
		return cmp.Compare(a.ImportPath, b.ImportPath)
	})

	return selected
}
