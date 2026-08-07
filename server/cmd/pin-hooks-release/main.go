// Command pin-hooks-release rewrites the generated hooks release pins after a
// speakeasy-hooks release is published. The release workflow runs it with the
// version it tagged and the checksums goreleaser produced, then opens a pull
// request with the result: the pin has to land as a reviewed change because
// pushing straight to main is not permitted.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

// releaseTargets is the full set of platforms every release publishes. A
// checksums file missing any of them means the release is incomplete, and
// pinning it would leave those platforms unable to install.
var releaseTargets = []string{
	"darwin-amd64", "darwin-arm64",
	"linux-amd64", "linux-arm64",
	"windows-amd64", "windows-arm64",
}

type pins struct {
	BinaryVersion string
	SHA256s       map[string]string
	Retired       map[string]map[string]string
}

func main() {
	version := flag.String("version", "", "released hooks version, without the hooks@ prefix")
	checksums := flag.String("checksums", "", "path to the release checksums.txt")
	path := flag.String("pins", "server/internal/plugins/hooks_release_pins.go", "path to the generated pins file")
	generate := flag.String("generate", "server/internal/plugins/generate.go", "path to the file owning hooksGeneratorVersion")
	flag.Parse()

	if err := run(*version, *checksums, *path, *generate); err != nil {
		fmt.Fprintf(os.Stderr, "pin-hooks-release: %v\n", err)
		os.Exit(1)
	}
}

func run(version, checksums, path, generate string) error {
	if version == "" || checksums == "" {
		return fmt.Errorf("--version and --checksums are required")
	}
	version = strings.TrimPrefix(strings.TrimSpace(version), "hooks@")
	if !releaseVersionPattern.MatchString(version) {
		return fmt.Errorf("version %q is not a MAJOR.MINOR.PATCH release version", version)
	}

	current, err := readPins(path)
	if err != nil {
		return err
	}
	if current.BinaryVersion == version {
		return fmt.Errorf("%s is already pinned", version)
	}

	sha256s, err := readChecksums(checksums)
	if err != nil {
		return err
	}

	// The outgoing release stays served: bootstrap scripts already installed on
	// user machines keep requesting it until their repo republishes.
	retired := current.Retired
	if retired == nil {
		retired = map[string]map[string]string{}
	}
	retired[current.BinaryVersion] = current.SHA256s

	if err := writePins(path, pins{
		BinaryVersion: version,
		SHA256s:       sha256s,
		Retired:       retired,
	}); err != nil {
		return err
	}

	// New checksums always change the rendered bootstrap script, and connected
	// repos only republish when this constant moves — so the pin is incomplete
	// without it. It lives in hand-written source because non-release changes to
	// hooks generation have to bump it too, with no release to regenerate from.
	return bumpGeneratorVersion(generate)
}

var (
	releaseVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	// Digests are compared verbatim against locally computed lowercase hex, so
	// anything else in the pin can never verify.
	sha256Pattern        = regexp.MustCompile(`^[0-9a-f]{64}$`)
	generatorVersionLine = regexp.MustCompile(`(?m)^const hooksGeneratorVersion = "(\d+)"$`)
)

func bumpGeneratorVersion(path string) error {
	source, err := os.ReadFile(path) // #nosec G304 -- release-workflow-supplied path inside the repo checkout
	if err != nil {
		return fmt.Errorf("read generator version: %w", err)
	}
	matches := generatorVersionLine.FindAllSubmatch(source, -1)
	if len(matches) != 1 {
		return fmt.Errorf("expected exactly one hooksGeneratorVersion declaration in %s, found %d", path, len(matches))
	}
	current, err := strconv.Atoi(string(matches[0][1]))
	if err != nil {
		return fmt.Errorf("parse generator version %q: %w", matches[0][1], err)
	}
	next := fmt.Sprintf("const hooksGeneratorVersion = %q", strconv.Itoa(current+1))
	updated := generatorVersionLine.ReplaceAll(source, []byte(next))
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		return fmt.Errorf("write generator version: %w", err)
	}
	return nil
}

// readChecksums parses goreleaser's checksums.txt ("<sha256>  <asset>") and
// keys the digests by target, rejecting anything that is not a complete
// release archive set.
func readChecksums(path string) (map[string]string, error) {
	file, err := os.Open(path) // #nosec G304 -- release-workflow-supplied checksums path
	if err != nil {
		return nil, fmt.Errorf("open checksums: %w", err)
	}
	defer func() { _ = file.Close() }()

	out := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		sha, asset := fields[0], fields[1]
		name, ok := strings.CutPrefix(asset, "speakeasy-hooks_")
		if !ok {
			continue
		}
		name, ok = strings.CutSuffix(name, ".zip")
		if !ok {
			continue
		}
		if !sha256Pattern.MatchString(sha) {
			return nil, fmt.Errorf("checksum for %s is not a lowercase hex sha256 digest", asset)
		}
		out[strings.ReplaceAll(name, "_", "-")] = sha
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read checksums: %w", err)
	}
	for _, target := range releaseTargets {
		if out[target] == "" {
			return nil, fmt.Errorf("checksums are missing an archive for %s", target)
		}
	}
	return out, nil
}

func readPins(path string) (pins, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return pins{}, fmt.Errorf("parse pins: %w", err)
	}

	out := pins{BinaryVersion: "", SHA256s: map[string]string{}, Retired: map[string]map[string]string{}}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
				continue
			}
			switch value.Names[0].Name {
			case "hooksBinaryVersion":
				out.BinaryVersion = stringLiteral(value.Values[0])
			case "hooksBinarySHA256s":
				out.SHA256s = stringMap(value.Values[0])
			case "hooksRetiredSHA256s":
				maps.Copy(out.Retired, nestedMap(value.Values[0]))
			}
		}
	}
	if out.BinaryVersion == "" || len(out.SHA256s) == 0 {
		return pins{}, fmt.Errorf("pins file %s is missing expected declarations", path)
	}
	return out, nil
}

func stringLiteral(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return value
}

func stringMap(expr ast.Expr) map[string]string {
	composite, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	out := map[string]string{}
	for _, element := range composite.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key := stringLiteral(pair.Key); key != "" {
			out[key] = stringLiteral(pair.Value)
		}
	}
	return out
}

func nestedMap(expr ast.Expr) map[string]map[string]string {
	composite, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	out := map[string]map[string]string{}
	for _, element := range composite.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key := stringLiteral(pair.Key); key != "" {
			out[key] = stringMap(pair.Value)
		}
	}
	return out
}

func writePins(path string, data pins) error {
	rendered := strings.Builder{}
	if err := pinsTemplate.Execute(&rendered, templateData(data)); err != nil {
		return fmt.Errorf("render pins: %w", err)
	}
	formatted, err := format.Source([]byte(rendered.String()))
	if err != nil {
		return fmt.Errorf("format pins: %w", err)
	}
	if err := os.WriteFile(path, formatted, 0o600); err != nil {
		return fmt.Errorf("write pins: %w", err)
	}
	return nil
}

type templateEntry struct {
	Version string
	SHA256s []templateSHA
}

type templateSHA struct {
	Target string
	SHA256 string
}

func templateData(data pins) struct {
	BinaryVersion string
	SHA256s       []templateSHA
	Retired       []templateEntry
} {
	retired := make([]templateEntry, 0, len(data.Retired))
	for version, sha256s := range data.Retired {
		retired = append(retired, templateEntry{Version: version, SHA256s: sortedSHAs(sha256s)})
	}
	sort.Slice(retired, func(i, j int) bool { return semverLess(retired[i].Version, retired[j].Version) })

	return struct {
		BinaryVersion string
		SHA256s       []templateSHA
		Retired       []templateEntry
	}{
		BinaryVersion: data.BinaryVersion,
		SHA256s:       sortedSHAs(data.SHA256s),
		Retired:       retired,
	}
}

// semverLess orders MAJOR.MINOR.PATCH strings numerically; plain string
// comparison would put "0.10.0" before "0.2.0".
func semverLess(a, b string) bool {
	pa, pb := semverParts(a), semverParts(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func semverParts(v string) [3]int {
	out := [3]int{}
	for i, part := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(part)
	}
	return out
}

func sortedSHAs(sha256s map[string]string) []templateSHA {
	out := make([]templateSHA, 0, len(sha256s))
	for target, sha := range sha256s {
		out = append(out, templateSHA{Target: target, SHA256: sha})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

var pinsTemplate = template.Must(template.New("pins").Parse(`// Code generated by server/cmd/pin-hooks-release. DO NOT EDIT.
//
// The hooks release workflow regenerates this file after publishing a
// speakeasy-hooks release and opens a pull request with the result. Everything
// here is derived from that release: the version it tagged and the checksums
// goreleaser computed. Editing it by hand is how the pin silently drifts from
// the released artifacts. The rollout signal that makes connected repos
// republish, hooksGeneratorVersion, stays in generate.go — the release bumps it
// there, and changes unrelated to a release have to bump it by hand.

package plugins

// hooksBinaryVersion is the release every newly rendered bootstrap script
// installs.
const hooksBinaryVersion = {{ printf "%q" .BinaryVersion }}

// hooksBinarySHA256s pins the archive digests for hooksBinaryVersion. Bootstrap
// scripts verify the same digests client-side after downloading.
var hooksBinarySHA256s = map[string]string{
{{- range .SHA256s }}
	{{ printf "%q" .Target }}: {{ printf "%q" .SHA256 }},
{{- end }}
}

// hooksRetiredSHA256s keeps previously pinned releases fetchable. Bootstrap
// scripts already installed on user machines keep requesting the version they
// were rendered with until their repo republishes, so dropping an entry here
// breaks those installations' cold installs.
var hooksRetiredSHA256s = map[string]map[string]string{
{{- range .Retired }}
	{{ printf "%q" .Version }}: {
	{{- range .SHA256s }}
		{{ printf "%q" .Target }}: {{ printf "%q" .SHA256 }},
	{{- end }}
	},
{{- end }}
}
`))
