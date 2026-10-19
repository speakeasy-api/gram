package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// Affected-package selection narrows a pull request's test run to the packages
// its changes can reach. A test package is affected when its test binary links
// a package whose files changed. Changed files that cannot be traced to a
// package this way (a schema read at runtime, go.mod, CI config) make every
// package affected instead, so a miss costs speed and never coverage.

// listed holds the fields of `go list -deps -test -json` output that affected
// selection cares about.
type listed struct {
	ImportPath      string
	Dir             string
	ForTest         string
	Deps            []string
	EmbedFiles      []string
	TestEmbedFiles  []string
	XTestEmbedFiles []string
	Module          *struct{ Main bool }
}

// graph is the part of the build graph that affected selection reads.
type graph struct {
	// dirs maps the directory of each main-module package, relative to the
	// repository root with forward slashes, to its import path.
	dirs map[string]string

	// embeds maps each file a main-module package embeds, relative to the
	// repository root, to the import paths that embed it.
	embeds map[string][]string

	// links maps the import path of each package with tests to every package
	// its test binary links, itself included.
	links map[string][]string
}

// selection is the outcome of matching changed files against a graph: either
// every package (full, with the reason) or the packages in changed.
type selection struct {
	full    string
	changed map[string]bool
}

// readChangedFiles reads one repository-relative path per line, as printed by
// `git diff --name-only`.
func readChangedFiles(name string) ([]string, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open changed files: %w", err)
	}
	defer f.Close()

	var files []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			files = append(files, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read changed files: %w", err)
	}

	return files, nil
}

// loadGraph lists the packages matching patterns, their test variants and
// everything they link.
func loadGraph(tags string, patterns []string, stderr io.Writer) (graph, error) {
	root, err := repoRoot()
	if err != nil {
		return graph{}, err
	}

	args := []string{"list", "-e", "-deps", "-test", "-json=ImportPath,Dir,ForTest,Deps,EmbedFiles,TestEmbedFiles,XTestEmbedFiles,Module"}
	if tags != "" {
		args = append(args, "-tags="+tags)
	}
	args = append(args, patterns...)

	cmd := exec.Command("go", args...)
	cmd.Stderr = stderr

	out, err := cmd.Output()
	if err != nil {
		return graph{}, fmt.Errorf("go list: %w", err)
	}

	var pkgs []listed
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listed
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return graph{}, fmt.Errorf("decode go list output: %w", err)
		}

		pkgs = append(pkgs, p)
	}

	return buildGraph(root, pkgs)
}

// repoRoot returns the top of the git checkout, which changed-file paths are
// relative to.
func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("find repository root: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

func buildGraph(root string, pkgs []listed) (graph, error) {
	g := graph{
		dirs:   map[string]string{},
		embeds: map[string][]string{},
		links:  map[string][]string{},
	}

	for _, p := range pkgs {
		// Test variants ("pkg [pkg.test]") repeat a package's directory and
		// embeds; the plain entry is enough for those.
		if p.Module == nil || !p.Module.Main || p.ForTest != "" || strings.Contains(p.ImportPath, " ") {
			continue
		}

		// The generated test main ("pkg.test") lives in the build cache, so
		// its directory is not the package's. Its Deps are what the test
		// binary links.
		if forTest, ok := strings.CutSuffix(p.ImportPath, ".test"); ok && len(p.Deps) > 0 {
			var links []string
			for _, dep := range p.Deps {
				dep, _, _ = strings.Cut(dep, " ")
				links = append(links, dep)
			}
			g.links[forTest] = links
			continue
		}

		dir, err := filepath.Rel(root, p.Dir)
		if err != nil {
			return graph{}, fmt.Errorf("package %s is outside the repository: %w", p.ImportPath, err)
		}
		dir = filepath.ToSlash(dir)
		g.dirs[dir] = p.ImportPath

		for _, name := range append(append(append([]string(nil), p.EmbedFiles...), p.TestEmbedFiles...), p.XTestEmbedFiles...) {
			file := path.Join(dir, filepath.ToSlash(name))
			g.embeds[file] = append(g.embeds[file], p.ImportPath)
		}
	}

	return g, nil
}

// fullRunPrefixes are directories whose non-Go files force a full run even
// when a package embeds them, because tests elsewhere read them at runtime too.
// The OpenAPI document under server/gen is embedded by the server binary and
// read from disk by API contract tests.
var fullRunPrefixes = []string{"server/gen/"}

// inert reports whether a non-Go file that no package embeds can be ignored:
// nothing a Go test builds or reads depends on it.
func inert(file string) bool {
	switch {
	case strings.HasSuffix(file, ".md"):
		// Documentation. Markdown that a package does read (Platform MCP
		// skills, for example) is embedded, and embeds are matched first.
		return true
	case path.Base(file) == "queries.sql":
		// sqlc input. The generated queries.sql.go is what compiles, and
		// server-checks fails when it is out of date.
		return true
	case strings.HasPrefix(file, "infra/proto/"), strings.HasPrefix(file, "infra/gen_py/"):
		// Protobuf sources and their Python output. The Go they generate
		// is checked in under infra/gen.
		return true
	case file == "infra/gen/kcc.yaml":
		// Generated Config Connector manifests, read only by the infra
		// generator's own tests.
		return true
	case strings.HasPrefix(file, "agents/runner/"):
		// The Rust assistant runner. Go tests talk to a stand-in, never to a
		// build of it.
		return true
	}

	return false
}

// classify matches changed files against g.
func classify(g graph, files []string) selection {
	sel := selection{changed: map[string]bool{}}

	for _, file := range files {
		file = path.Clean(filepath.ToSlash(file))

		if strings.HasSuffix(file, ".go") {
			// A Go file belongs to the package in its directory. One under
			// testdata is data for the package that owns the testdata. Any
			// other Go file is not compiled into a test binary in this run.
			if pkg, ok := g.dirs[path.Dir(file)]; ok {
				sel.changed[pkg] = true
			} else if pkg, ok := testdataOwner(g, file); ok {
				sel.changed[pkg] = true
			}
			continue
		}

		for _, prefix := range fullRunPrefixes {
			if strings.HasPrefix(file, prefix) {
				sel.full = fmt.Sprintf("%s is read by tests outside the package that embeds it", file)
				return sel
			}
		}

		if pkgs, ok := g.embeds[file]; ok {
			for _, pkg := range pkgs {
				sel.changed[pkg] = true
			}
			continue
		}

		if pkg, ok := testdataOwner(g, file); ok {
			sel.changed[pkg] = true
			continue
		}

		if inert(file) {
			continue
		}

		sel.full = fmt.Sprintf("%s is not part of any package", file)
		return sel
	}

	return sel
}

// testdataOwner returns the package whose testdata directory holds file.
func testdataOwner(g graph, file string) (string, bool) {
	dir := path.Dir(file)
	for dir != "." && dir != "/" {
		if path.Base(dir) == "testdata" {
			pkg, ok := g.dirs[path.Dir(dir)]
			return pkg, ok
		}
		dir = path.Dir(dir)
	}

	return "", false
}

// affected reports whether the test binary of pkg links any changed package.
func (g graph) affected(pkg string, changed map[string]bool) bool {
	if changed[pkg] {
		return true
	}

	for _, dep := range g.links[pkg] {
		if changed[dep] {
			return true
		}
	}

	return false
}

// keepAffected narrows pkgs to those whose tests the changed files can reach,
// returning the reason when every package has to run.
func keepAffected(pkgs []pkg, tags string, patterns []string, changedFiles string, stderr io.Writer) ([]pkg, string, error) {
	files, err := readChangedFiles(changedFiles)
	if err != nil {
		return nil, "", err
	}

	g, err := loadGraph(tags, patterns, stderr)
	if err != nil {
		return nil, "", err
	}

	sel := classify(g, files)
	if sel.full != "" {
		return pkgs, sel.full, nil
	}

	var kept []pkg
	for _, p := range pkgs {
		if g.affected(p.ImportPath, sel.changed) {
			kept = append(kept, p)
		}
	}

	return kept, "", nil
}
