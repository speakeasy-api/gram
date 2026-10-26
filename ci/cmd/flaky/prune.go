package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// shortSHALen is how many characters of a commit SHA a prune comment shows,
// matching what GitHub shows for a commit.
const shortSHALen = 10

// removal is the commit that deleted a test.
type removal struct {
	// SHA is the full commit SHA.
	SHA string

	// Subject is the first line of the commit message.
	Subject string
}

type pruneOptions struct {
	// Root is the repository checkout that tests are looked up in.
	Root string

	// Repo is the GitHub owner/name used to link the removal commit; empty
	// leaves the commit unlinked.
	Repo string

	// DryRun prints the tickets that would be canceled instead of canceling
	// them.
	DryRun bool
}

// runPrune cancels every open flaky-test ticket whose test no longer exists in
// the checkout, noting the commit that removed it. Those tickets can never be
// worked, and leaving them open clutters the flaky-test backlog.
func runPrune(ctx context.Context, linear *linearClient, opts pruneOptions, stdout io.Writer) error {
	if err := checkModuleRoot(opts.Root); err != nil {
		return err
	}

	issues, err := linear.openFlakyIssues(ctx)
	if err != nil {
		return err
	}

	keys := make([]testKey, 0, len(issues))
	for k := range issues {
		keys = append(keys, k)
	}
	keys = sortedUnique(keys)

	fsys := os.DirFS(opts.Root)
	stateID := ""
	pruned := 0
	for _, k := range keys {
		issue := issues[k]

		exists, err := testExists(fsys, k)
		if err != nil {
			// Keep a ticket whose test cannot be checked; canceling it on a
			// parse error would hide a test that may still be flaky.
			annotate(stdout, "Flaky ticket not checked", fmt.Sprintf("%s: %v", issue.Identifier, err))
			continue
		}
		if exists {
			continue
		}

		removedBy, err := findRemoval(ctx, opts.Root, k)
		if err != nil {
			annotate(stdout, "Flaky test removal commit not found", fmt.Sprintf("%s: %v", issue.Identifier, err))
		}
		body := pruneComment(k, removedBy, opts.Repo)

		if opts.DryRun {
			fmt.Fprintf(stdout, "deleted   %s (%s): %s\n", k, issue.Identifier, body)
			continue
		}

		if stateID == "" {
			if stateID, err = linear.canceledStateID(ctx); err != nil {
				return err
			}
		}
		if err := linear.cancel(ctx, issue, stateID); err != nil {
			return err
		}
		if err := linear.comment(ctx, issue, body); err != nil {
			annotate(stdout, "Flaky ticket cancellation not explained", fmt.Sprintf("%s: %v", issue.Identifier, err))
		}
		pruned++
		fmt.Fprintf(stdout, "canceled  %s (%s)\n", k, issue.Identifier)
	}

	fmt.Fprintf(stdout, "checked %d open flaky tickets: %d canceled for deleted tests\n", len(keys), pruned)
	return nil
}

// checkModuleRoot fails unless root is the repository's module root. Ticket
// packages are paths from that root, so pruning from any other directory would
// find no tests and cancel every ticket.
func checkModuleRoot(root string) error {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return fmt.Errorf("open go.mod in %s: %w", root, err)
	}
	defer func() { _ = f.Close() }()

	want := strings.TrimSuffix(modulePrefix, "/")
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if mod, ok := strings.CutPrefix(strings.TrimSpace(scanner.Text()), "module "); ok {
			if got := strings.TrimSpace(mod); got != want {
				return fmt.Errorf("%s is module %s, not %s", root, got, want)
			}
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read go.mod in %s: %w", root, err)
	}
	return fmt.Errorf("go.mod in %s declares no module", root)
}

// testExists reports whether the test's package still declares it as a
// top-level function in one of its _test.go files. A missing package directory
// means the test is gone.
func testExists(fsys fs.FS, k testKey) (bool, error) {
	if !fs.ValidPath(k.Package) || !token.IsIdentifier(k.Test) {
		return false, fmt.Errorf("ticket names an invalid test %q", k.String())
	}

	entries, err := fs.ReadDir(fsys, k.Package)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read package %s: %w", k.Package, err)
	}

	// A file that fails to parse might be the one declaring the test, so its
	// error only matters when no other file declares it.
	var parseErr error
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}

		name := path.Join(k.Package, e.Name())
		src, err := fs.ReadFile(fsys, name)
		if err != nil {
			return false, fmt.Errorf("read %s: %w", name, err)
		}

		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			parseErr = errors.Join(parseErr, fmt.Errorf("parse %s: %w", name, err))
			continue
		}
		if slices.ContainsFunc(file.Decls, func(d ast.Decl) bool {
			fn, ok := d.(*ast.FuncDecl)
			return ok && fn.Recv == nil && fn.Name.Name == k.Test
		}) {
			return true, nil
		}
	}
	return false, parseErr
}

// findRemoval returns the last commit that changed the test's declaration line
// in its package. Once the test is gone, that is the commit that removed it.
// It needs the checkout's history: a shallow clone finds no commit.
func findRemoval(ctx context.Context, root string, k testKey) (removal, error) {
	// [(] matches a literal parenthesis in both basic and extended regex.
	pattern := "^func " + k.Test + "[(]"
	cmd := exec.CommandContext(ctx, "git", "log", "-1", "--format=%H%x00%s", "-G", pattern, "--", k.Package)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return removal{}, fmt.Errorf("git log for %s: %w", k, err)
	}

	sha, subject, ok := strings.Cut(strings.TrimSpace(string(out)), "\x00")
	if !ok || sha == "" {
		return removal{}, fmt.Errorf("no commit in history removes %s", k)
	}
	return removal{SHA: sha, Subject: subject}, nil
}

// pruneComment explains on the ticket why it was canceled, linking the removal
// commit when one was found.
func pruneComment(k testKey, removedBy removal, repo string) string {
	msg := fmt.Sprintf("Canceling: `%s` no longer exists in `%s` on `main`.", k.Test, k.Package)
	if removedBy.SHA == "" {
		return msg
	}

	ref := "`" + removedBy.SHA[:min(len(removedBy.SHA), shortSHALen)] + "`"
	if repo != "" {
		ref = fmt.Sprintf("[%s](https://github.com/%s/commit/%s)", ref, repo, removedBy.SHA)
	}
	return fmt.Sprintf("%s It was removed in %s (%s).", msg, ref, removedBy.Subject)
}
