// Command authzcoverage reports which management API endpoints were seen
// making an authorization decision in tests.
//
// Test runs record coverage when GRAM_AUTHZ_COVERAGE_DIR is set: every authz
// decision appends the server methods on its call stack to a file in that
// directory (see server/internal/authz/decision_coverage.go). This command
// lists every Goa endpoint the server implements, matches it against those
// records, and fails when an endpoint that was never seen deciding is missing
// from the allowlist. It warns when the allowlist names an endpoint that no longer
// needs to be there (a warning only).
//
// Usage:
//
//	go run ./ci/cmd/authzcoverage -records <dir> [-allowlist <file>] [-write]
//
// -write rewrites the allowlist from the current report, keeping the reasons
// already written and marking new entries as unreviewed. Only use it after a
// full test run; a partial run reports every endpoint it did not reach.
package main

import (
	"bufio"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"go/constant"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

const (
	modulePath       = "github.com/speakeasy-api/gram"
	genPrefix        = modulePath + "/server/gen/"
	internalPrefix   = modulePath + "/server/internal/"
	defaultAllowlist = "server/internal/authz/testdata/endpoint_coverage_allowlist.txt"
	unreviewedReason = "unreviewed: no test records an authz decision for this endpoint"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "authzcoverage: %v\n", err)
		os.Exit(1)
	}
}

// endpoint is one Goa method and the server method that implements it.
type endpoint struct {
	// Name is service.Method, for example skills.List.
	Name string
	// Symbol is the implementation as the runtime names it, for example
	// github.com/speakeasy-api/gram/server/internal/skills.(*Service).List.
	Symbol string
}

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("authzcoverage", flag.ContinueOnError)
	records := flags.String("records", "", "directory of coverage records written by test runs")
	allowlistPath := flags.String("allowlist", defaultAllowlist, "allowlist of endpoints with no recorded decision")
	write := flags.Bool("write", false, "rewrite the allowlist from this report")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if *records == "" {
		return errors.New("-records is required")
	}

	endpoints, err := listEndpoints()
	if err != nil {
		return err
	}
	seen, err := readRecords(*records)
	if err != nil {
		return err
	}
	allowlist, err := readAllowlist(*allowlistPath)
	if err != nil {
		return err
	}

	// An endpoint counts as covered when any type implementing it was seen.
	coveredNames := make(map[string]bool)
	for _, e := range endpoints {
		if _, ok := seen[e.Symbol]; ok {
			coveredNames[e.Name] = true
		}
	}
	var uncovered []endpoint
	for _, e := range endpoints {
		if !coveredNames[e.Name] {
			uncovered = append(uncovered, e)
		}
	}
	uncovered = slices.CompactFunc(uncovered, func(a, b endpoint) bool { return a.Name == b.Name })
	covered := len(coveredNames)

	if *write {
		return writeAllowlist(*allowlistPath, uncovered, allowlist)
	}

	known := make(map[string]bool, len(endpoints))
	for _, e := range endpoints {
		known[e.Name] = true
	}
	total := len(known)
	var missing []string
	for _, e := range uncovered {
		if _, ok := allowlist[e.Name]; !ok {
			missing = append(missing, e.Name)
		}
	}
	uncoveredNames := make(map[string]bool, len(uncovered))
	for _, e := range uncovered {
		uncoveredNames[e.Name] = true
	}
	var stale []string
	for name := range allowlist {
		if !known[name] || !uncoveredNames[name] {
			stale = append(stale, name)
		}
	}
	slices.Sort(stale)

	fmt.Fprintf(stdout, "authz endpoint coverage: %d of %d endpoints recorded a decision in tests, %d allowlisted\n", covered, total, len(uncovered)-len(missing))
	// Stale entries only warn: a test that is skipped on one machine and runs
	// on another must not fail the check in either direction.
	for _, name := range stale {
		fmt.Fprintf(stdout, "  warning: allowlisted but now covered or removed, delete the entry: %s\n", name)
	}
	if len(missing) == 0 {
		return nil
	}
	for _, name := range missing {
		fmt.Fprintf(stdout, "  no recorded decision and not allowlisted: %s\n", name)
	}
	fmt.Fprintf(stdout, "\nAdd a test that calls the endpoint and reaches its authz check, or add the endpoint to %s with the reason it makes no decision.\n", *allowlistPath)
	return fmt.Errorf("%d endpoints have no recorded authz decision and are not allowlisted", len(missing))
}

// listEndpoints loads the generated Goa service interfaces and the server
// packages, and pairs each interface method with every type implementing it.
func listEndpoints() ([]endpoint, error) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps}
	pkgs, err := packages.Load(cfg, modulePath+"/server/gen/...", modulePath+"/server/internal/...")
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		return nil, fmt.Errorf("%d package load errors", n)
	}

	type service struct {
		name  string
		iface *types.Interface
	}
	var services []service
	var impls []*types.Package
	for _, pkg := range pkgs {
		switch {
		case strings.HasPrefix(pkg.PkgPath, genPrefix) && !strings.Contains(strings.TrimPrefix(pkg.PkgPath, genPrefix), "/"):
			obj := pkg.Types.Scope().Lookup("Service")
			if obj == nil {
				continue
			}
			iface, ok := obj.Type().Underlying().(*types.Interface)
			if !ok {
				continue
			}
			name := pkg.Name
			if c, ok := pkg.Types.Scope().Lookup("ServiceName").(*types.Const); ok && c.Val().Kind() == constant.String {
				name = constant.StringVal(c.Val())
			}
			services = append(services, service{name: name, iface: iface})
		case strings.HasPrefix(pkg.PkgPath, internalPrefix):
			impls = append(impls, pkg.Types)
		}
	}

	var out []endpoint
	for _, svc := range services {
		for _, pkg := range impls {
			scope := pkg.Scope()
			for _, typeName := range scope.Names() {
				tn, ok := scope.Lookup(typeName).(*types.TypeName)
				if !ok || tn.IsAlias() {
					continue
				}
				if _, isIface := tn.Type().Underlying().(*types.Interface); isIface {
					continue
				}
				if !types.Implements(types.NewPointer(tn.Type()), svc.iface) {
					continue
				}
				// Explicit methods only: the embedded Auther methods are
				// security handlers, not endpoints.
				for i := range svc.iface.NumExplicitMethods() {
					method := svc.iface.ExplicitMethod(i).Name()
					out = append(out, endpoint{
						Name:   svc.name + "." + method,
						Symbol: fmt.Sprintf("%s.(*%s).%s", pkg.Path(), tn.Name(), method),
					})
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b endpoint) int { return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Symbol, b.Symbol)) })
	out = slices.CompactFunc(out, func(a, b endpoint) bool { return a.Name == b.Name && a.Symbol == b.Symbol })
	return out, nil
}

func readRecords(dir string) (map[string]struct{}, error) {
	seen := make(map[string]struct{})
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".txt") {
			return nil
		}
		f, err := os.Open(path) //nolint:gosec // reading the records directory named on the command line
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		defer func() { _ = f.Close() }()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			if line := strings.TrimSpace(scanner.Text()); line != "" {
				seen[line] = struct{}{}
			}
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read records: %w", err)
	}
	return seen, nil
}

// readAllowlist reads "service.Method reason..." lines. Blank lines and lines
// starting with # are ignored.
func readAllowlist(path string) (map[string]string, error) {
	out := make(map[string]string)
	data, err := os.ReadFile(path) //nolint:gosec // reading the allowlist named on the command line
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read allowlist: %w", err)
	}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, reason, _ := strings.Cut(line, " ")
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return nil, fmt.Errorf("%s:%d: %s has no reason", path, i+1, name)
		}
		out[name] = reason
	}
	return out, nil
}

func writeAllowlist(path string, uncovered []endpoint, existing map[string]string) error {
	var b strings.Builder
	b.WriteString("# Management API endpoints no test has seen making an authz decision.\n")
	b.WriteString("# Format: <service>.<Method> <reason>. Checked by ci/cmd/authzcoverage.\n")
	b.WriteString("#\n")
	b.WriteString("# Remove an entry by adding a test that reaches the endpoint's authz check.\n")
	b.WriteString("# Keep one only with the reason the endpoint makes no decision, for\n")
	b.WriteString("# example \"public: serves the login page\".\n")
	names := make([]string, 0, len(uncovered))
	for _, e := range uncovered {
		names = append(names, e.Name)
	}
	slices.Sort(names)
	names = slices.Compact(names)
	for _, name := range names {
		reason, ok := existing[name]
		if !ok {
			reason = unreviewedReason
		}
		fmt.Fprintf(&b, "%s %s\n", name, reason)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create allowlist directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("write allowlist: %w", err)
	}
	return nil
}
