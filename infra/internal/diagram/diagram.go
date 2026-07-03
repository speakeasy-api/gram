// Package diagram generates a single mermaid diagram (plus text tables) that
// ties Gram's proto-declared Pub/Sub topology to the Go and Python call sites
// that publish to topics and consume subscriptions.
//
// The topology comes from the compiled proto descriptors (the same source of
// truth that produces infra/gen/kcc.yaml). The call sites come from ast-grep
// scans of the Go (server/) and Python (pystreams/) trees: ast-grep parses each
// language natively and reports the proto message/subscription type used at each
// publish/consume site. Those captured symbols are resolved back to proto full
// names via each file's import block and joined against the topology, so a
// publisher whose topic is missing — or a topic/subscription with no code site —
// is surfaced rather than hidden.
package diagram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/speakeasy-api/gram/infra/internal/gcp"
)

// protoModuleRoot is the repo-relative root of the proto module (see buf.yaml),
// prepended to descriptor file names to form on-disk paths.
const protoModuleRoot = "infra/proto/"

// astGrepBin is the ast-grep executable. It is resolved from PATH; when run via
// the mise task the tool is already activated. Override with ASTGREP_BIN.
func astGrepBin() string {
	if v := strings.TrimSpace(os.Getenv("ASTGREP_BIN")); v != "" {
		return v
	}
	return "ast-grep"
}

// scan describes one ast-grep query: a pattern, the language to parse, the
// directories to search (repo-relative), an optional metavariable constraint, and
// which captured metavariables hold the topic message and (for subscribers) the
// subscription marker.
type scan struct {
	id         string // ast-grep rule id; routes matches back when rules share one walk
	role       string // "publish" | "subscribe"
	lang       string // "go" | "python"
	pattern    string
	dirs       []string
	constraint *scanConstraint // optional metavariable regex constraint
	msgVar     string          // single metavariable holding the topic message type expression
	subVar     string          // single metavariable holding the subscription marker expression ("" for publishers)
	hdlrVar    string          // multi metavariable whose first element is the handler ("" if none)
	batch      bool            // true when the site consumes via ReceiveBatch (batch handler)
}

// scanConstraint restricts a captured metavariable to an anchored regex, so the
// scan only emits matches whose type is one of the known topic messages — letting
// ast-grep prune in Rust instead of returning every literal for Go to filter.
type scanConstraint struct {
	metavar string
	regex   string
}

// scanExclusionGlobs prune files at scan time (ast-grep already honors
// .gitignore by default, so generated/ignored files never reach us).
//
// Beyond test files, this also drops the emulator-only load generator
// (cmd/presidio_load.py): it constructs a PresidioAnalysis purely to publish
// synthetic traffic at the local emulator for profiling, so treating it as a
// production publisher of gram-risk-v1-presidio-analysis would misrepresent the
// committed topology.
var scanExclusionGlobs = []string{
	"!**/*_test.go",
	"!**/*_test.py",
	"!**/test_*.py",
	"!**/tests/**",
	"!**/cmd/presidio_load.py",
}

// subscribeScans are static: subscriptions are registered at a single, specific
// call shape per language.
var subscribeScans = []scan{
	// Go: subscription registered in the streams runner.
	{
		id:      "subscribe-go",
		role:    "subscribe",
		lang:    "go",
		pattern: "mustReceive($G, $MSG, $SUB, $$$HANDLER)",
		dirs:    []string{"server"},
		msgVar:  "MSG",
		subVar:  "SUB",
		hdlrVar: "HANDLER",
	},
	// Go: batch subscription registered in the streams runner. mustReceiveBatch
	// carries an extra BatchReceiveSettings argument ($S) between the subscription
	// marker and the handler, and binds a streams.BatchHandler. A distinct
	// identifier from mustReceive, so the single-message scan never matches it.
	{
		id:      "subscribe-batch-go",
		role:    "subscribe",
		lang:    "go",
		pattern: "mustReceiveBatch($G, $MSG, $SUB, $S, $$$HANDLER)",
		dirs:    []string{"server"},
		msgVar:  "MSG",
		subVar:  "SUB",
		hdlrVar: "HANDLER",
		batch:   true,
	},
	// Python: subscription registered with the receiver group.
	{
		id:      "subscribe-python",
		role:    "subscribe",
		lang:    "python",
		pattern: "$R.receive($MSG, $SUB, $$$HANDLER)",
		dirs:    []string{"pystreams"},
		msgVar:  "MSG",
		subVar:  "SUB",
		hdlrVar: "HANDLER",
	},
}

// publishScans locates publishers at the site where a topic's protobuf message is
// *constructed* — almost always the same place it is published, but far more
// reliable to match than chasing `.Publish(...)` through a receiver whose type
// ast-grep cannot see. The scans are constrained to the known topic type names so
// ast-grep only walks the tree once and emits a handful of matches. On Go the match
// is the message struct or its generated `_builder`; an empty literal body
// (`pkg.Type{}`) is a zero-value type marker, not a construction, and is skipped.
// Import resolution then rejects same-named types from other packages.
func publishScans(topicTypeNames []string) []scan {
	alt := regexAlternation(topicTypeNames)
	return []scan{
		{
			id:         "publish-go",
			role:       "publish",
			lang:       "go",
			pattern:    "$P.$T{$$$F}",
			dirs:       []string{"server"},
			constraint: &scanConstraint{metavar: "T", regex: "^(" + alt + ")(_builder)?$"},
		},
		{
			id:         "publish-python",
			role:       "publish",
			lang:       "python",
			pattern:    "$P.$T($$$A)",
			dirs:       []string{"pystreams"},
			constraint: &scanConstraint{metavar: "T", regex: "^(" + alt + ")$"},
		},
	}
}

// topicMessageNames returns the unique short (proto) type names of all declared
// topic messages (DLQ topics carry their subscription marker's name and are
// excluded), used to constrain the publisher scans.
func topicMessageNames(topics []gcp.DesiredTopic) []string {
	seen := map[string]bool{}
	var names []string
	for _, t := range topics {
		if isDLQ(t) {
			continue
		}
		short := t.ProtoMessage[strings.LastIndex(t.ProtoMessage, ".")+1:]
		if short != "" && !seen[short] {
			seen[short] = true
			names = append(names, short)
		}
	}
	sort.Strings(names)
	return names
}

func regexAlternation(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = regexp.QuoteMeta(n)
	}
	return strings.Join(quoted, "|")
}

// callSite is one resolved publish/consume location.
type callSite struct {
	role    string
	lang    string
	file    string // repo-relative
	line    int    // 1-based
	msgFull string // resolved proto full name of the topic message
	subFull string // resolved proto full name of the subscription marker (subscribe only)
	handler string // short handler label (subscribe only)
	batch   bool   // true when consumed via ReceiveBatch (subscribe only)
}

// Generate runs the scans, joins them with the topology discovered from the
// descriptor set, and returns the markdown document (mermaid diagram + tables).
func Generate(ctx context.Context, descriptors []byte, repoRoot string) (string, error) {
	topics, subs, err := gcp.DiscoverPubSub(descriptors)
	if err != nil {
		return "", fmt.Errorf("discover pubsub topology: %w", err)
	}

	allScans := append(publishScans(topicMessageNames(topics)), subscribeScans...)
	sites, unresolved, err := collectCallSites(ctx, repoRoot, allScans)
	if err != nil {
		return "", err
	}

	locs, err := protoLocations(descriptors)
	if err != nil {
		return "", err
	}

	return render(topics, subs, sites, unresolved, locs), nil
}

func collectCallSites(ctx context.Context, repoRoot string, scans []scan) (sites []callSite, unresolved []string, err error) {
	importCache := map[string]map[string]string{} // file -> (qualifier -> proto pkg)

	// All scans for one language share the same directories, so run them as a
	// single multi-rule ast-grep pass: the tree is parsed once per language rather
	// than once per pattern, and each match carries its rule id for routing back.
	for _, lang := range scanLanguages(scans) {
		group := scansForLanguage(scans, lang)
		byID := map[string]scan{}
		for _, s := range group {
			byID[s.id] = s
		}

		matches, err := runASTGrepRules(ctx, repoRoot, lang, group)
		if err != nil {
			return nil, nil, err
		}

		for _, m := range matches {
			s, ok := byID[m.RuleID]
			if !ok {
				continue
			}

			imports, ok := importCache[m.File]
			if !ok {
				imports, err = loadImports(filepath.Join(repoRoot, m.File), repoRoot, s.lang)
				if err != nil {
					return nil, nil, fmt.Errorf("resolve imports for %s: %w", m.File, err)
				}
				importCache[m.File] = imports
			}

			// Publishers are located where the topic message is constructed. The
			// captured type is `$P.$T` (Go) or `$P.$T(...)` (Python); resolve it and
			// let the topology join (in render) keep only topic messages.
			if s.role == "publish" {
				typeName := m.meta("T")
				if s.lang == "go" {
					// `pkg.Type{}` with an empty body is a zero-value type marker
					// (used for type dispatch), not a constructed message.
					if m.multiCount("F") == 0 {
						continue
					}
					// The generated builder builds its message: Type_builder -> Type.
					typeName = strings.TrimSuffix(typeName, "_builder")
				}

				full, ok := resolveSymbol(imports, m.meta("P")+"."+typeName)
				if !ok {
					// Not an infra/gen proto — an unrelated literal/constructor.
					continue
				}
				sites = append(sites, callSite{
					role:    s.role,
					lang:    s.lang,
					file:    m.File,
					line:    m.line(),
					msgFull: full,
				})
				continue
			}

			msgSym := normalizeType(m.meta(s.msgVar))
			msgFull, ok := resolveSymbol(imports, msgSym)
			if !ok {
				unresolved = append(unresolved, fmt.Sprintf("%s:%d %s (%s)", m.File, m.line(), msgSym, s.role))
				continue
			}

			site := callSite{
				role:    s.role,
				lang:    s.lang,
				file:    m.File,
				line:    m.line(),
				msgFull: msgFull,
				batch:   s.batch,
			}

			if s.subVar != "" {
				subSym := normalizeType(m.meta(s.subVar))
				subFull, ok := resolveSymbol(imports, subSym)
				if !ok {
					unresolved = append(unresolved, fmt.Sprintf("%s:%d %s (%s)", m.File, m.line(), subSym, s.role))
					continue
				}
				site.subFull = subFull
			}
			if s.hdlrVar != "" {
				site.handler = shortHandler(m.firstMulti(s.hdlrVar))
			}

			sites = append(sites, site)
		}
	}

	sort.Slice(sites, func(i, j int) bool {
		if sites[i].file != sites[j].file {
			return sites[i].file < sites[j].file
		}
		return sites[i].line < sites[j].line
	})
	sort.Strings(unresolved)
	return sites, unresolved, nil
}

// --- ast-grep invocation -----------------------------------------------------

type asgMatch struct {
	RuleID string `json:"ruleId"`
	File   string `json:"file"`
	Range  struct {
		Start struct {
			Line int `json:"line"` // 0-based
		} `json:"start"`
	} `json:"range"`
	MetaVariables struct {
		Single map[string]struct {
			Text string `json:"text"`
		} `json:"single"`
		Multi map[string][]struct {
			Text string `json:"text"`
		} `json:"multi"`
	} `json:"metaVariables"`
}

func (m asgMatch) line() int { return m.Range.Start.Line + 1 }

func (m asgMatch) meta(name string) string {
	return strings.TrimSpace(m.MetaVariables.Single[name].Text)
}

// firstMulti returns the text of the first node captured by a `$$$` multi
// metavariable (e.g. the handler argument that leads the trailing args).
func (m asgMatch) firstMulti(name string) string {
	nodes := m.MetaVariables.Multi[name]
	if len(nodes) == 0 {
		return ""
	}
	return strings.TrimSpace(nodes[0].Text)
}

// multiCount reports how many nodes a `$$$` multi metavariable captured (e.g. the
// number of elements in a composite literal body).
func (m asgMatch) multiCount(name string) int {
	return len(m.MetaVariables.Multi[name])
}

// normalizeType strips language wrappers from a captured type expression so it
// reduces to a bare qualified type: Go's `&pingv2.Message{}` -> `pingv2.Message`.
func normalizeType(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "&")
	if i := strings.IndexByte(s, '{'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// scanLanguages returns the distinct languages across scans in first-seen order,
// keeping the per-language passes deterministic.
func scanLanguages(scans []scan) []string {
	seen := map[string]bool{}
	var langs []string
	for _, s := range scans {
		if !seen[s.lang] {
			seen[s.lang] = true
			langs = append(langs, s.lang)
		}
	}
	return langs
}

func scansForLanguage(scans []scan, lang string) []scan {
	var out []scan
	for _, s := range scans {
		if s.lang == lang {
			out = append(out, s)
		}
	}
	return out
}

// runASTGrepRules runs every scan for one language as a single ast-grep pass: the
// scans are combined into one multi-rule `--inline-rules` document (rules joined by
// the YAML `---` separator) over the union of their directories, so the source tree
// is parsed only once per language. Each returned match is tagged with its rule id.
func runASTGrepRules(ctx context.Context, repoRoot, lang string, scans []scan) ([]asgMatch, error) {
	rules := make([]string, 0, len(scans))
	dirSet := map[string]bool{}
	for _, s := range scans {
		rules = append(rules, ruleYAML(s))
		for _, d := range s.dirs {
			dirSet[d] = true
		}
	}
	dirs := make([]string, 0, len(dirSet))
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	args := []string{"scan", "--inline-rules", strings.Join(rules, "---\n"), "--json=compact"}
	for _, g := range scanExclusionGlobs {
		args = append(args, "--globs", g)
	}
	args = append(args, dirs...)

	cmd := exec.CommandContext(ctx, astGrepBin(), args...)
	cmd.Dir = repoRoot

	// ast-grep is grep-style: it exits 1 when there are no matches while still
	// printing a valid empty JSON array. So parse stdout first and only treat a
	// run error as fatal when the output is not parseable JSON.
	out, runErr := cmd.Output()

	var matches []asgMatch
	if jsonErr := json.Unmarshal(out, &matches); jsonErr != nil {
		stderr := ""
		if exitErr, ok := errors.AsType[*exec.ExitError](runErr); ok {
			stderr = string(exitErr.Stderr)
		}
		if runErr != nil {
			return nil, fmt.Errorf("ast-grep scan (%s): %w: %s", lang, runErr, stderr)
		}
		return nil, fmt.Errorf("parse ast-grep json (%s): %w", lang, jsonErr)
	}
	return matches, nil
}

// ruleYAML renders one scan as an ast-grep rule document. Pattern and regex contain
// no single quotes, so YAML single-quoted scalars are safe.
func ruleYAML(s scan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "id: %s\nlanguage: %s\nrule:\n  pattern: '%s'\n", s.id, s.lang, s.pattern)
	if s.constraint != nil {
		fmt.Fprintf(&b, "constraints:\n  %s: {regex: '%s'}\n", s.constraint.metavar, s.constraint.regex)
	}
	return b.String()
}

// --- symbol resolution -------------------------------------------------------

// resolveSymbol turns a qualified type reference like "pingv2.Message" or
// "ping_pb2.Message" into its proto full name "gram.ping.v2.Message" using the
// file's import map (qualifier -> proto package).
func resolveSymbol(imports map[string]string, symbol string) (string, bool) {
	parts := strings.Split(symbol, ".")
	if len(parts) < 2 {
		return "", false
	}
	pkg, ok := imports[parts[0]]
	if !ok {
		return "", false
	}
	return pkg + "." + parts[len(parts)-1], true
}

func loadImports(absPath, repoRoot, lang string) (map[string]string, error) {
	switch lang {
	case "go":
		return goImports(absPath, repoRoot)
	case "python":
		return pyImports(absPath)
	default:
		return nil, fmt.Errorf("unsupported language %q", lang)
	}
}

const genMarker = "/infra/gen/"

// goImports maps each generated-proto import's local qualifier to its proto
// package. Only imports under .../infra/gen/ are kept; the proto package is the
// path tail (e.g. ".../infra/gen/gram/ping/v2" -> "gram.ping.v2").
//
// The qualifier is the import alias when one is given. Otherwise it is the
// imported package's *declared* name (`package pingv2`), which for these
// generated packages differs from the directory basename (`v2`) — using the
// basename would resolve `pingv2.Message` against the wrong key and silently drop
// the publisher/consumer. The basename is only a fallback when the package source
// cannot be read.
func goImports(absPath, repoRoot string) (map[string]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, absPath, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}

	out := map[string]string{}
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		_, after, ok := strings.Cut(path, genMarker)
		if !ok {
			continue
		}
		pkg := strings.ReplaceAll(after, "/", ".")

		var alias string
		switch {
		case imp.Name != nil:
			alias = imp.Name.Name
		default:
			alias = path[strings.LastIndex(path, "/")+1:] // basename fallback
			if name, ok := declaredPackageName(filepath.Join(repoRoot, "infra", "gen", after)); ok {
				alias = name
			}
		}
		out[alias] = pkg
	}
	return out, nil
}

// declaredPackageName reads the `package` clause of the Go package in dir, used to
// resolve unaliased imports of generated packages whose name differs from their
// directory basename.
func declaredPackageName(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.PackageClauseOnly)
		if err != nil || f.Name == nil || f.Name.Name == "" {
			continue
		}
		return f.Name.Name, true
	}
	return "", false
}

var pyFromImport = regexp.MustCompile(`(?m)^\s*from\s+([\w.]+)\s+import\s+(.+?)\s*$`)

// pyImports maps each imported module name to its proto package by reading
// `from <pkg> import a, b as c` statements. Only packages that look like proto
// packages (contain a dot) are kept.
func pyImports(absPath string) (map[string]string, error) {
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	out := map[string]string{}
	for _, match := range pyFromImport.FindAllStringSubmatch(string(content), -1) {
		pkg := match[1]
		if !strings.Contains(pkg, ".") {
			continue
		}
		for item := range strings.SplitSeq(match[2], ",") {
			item = strings.TrimSpace(strings.Trim(strings.TrimSpace(item), "()"))
			if item == "" {
				continue
			}
			// Handle "name as alias": the alias is the local qualifier.
			name := item
			if fields := strings.Fields(item); len(fields) == 3 && fields[1] == "as" {
				name = fields[2]
			}
			out[name] = pkg
		}
	}
	return out, nil
}

// shortHandler trims a handler expression down to a readable label, e.g.
// "ping.NewHandler(logger, lvl)" -> "ping.NewHandler" and
// "PingHandler(logger).handle" -> "PingHandler.handle".
func shortHandler(expr string) string {
	expr = strings.TrimSpace(expr)
	var b strings.Builder
	depth := 0
	for _, r := range expr {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// --- proto declaration locations ---------------------------------------------

// protoLoc is where a proto message is declared: a repo-relative file path. Line
// numbers are deliberately omitted — they shift on every unrelated proto edit and
// churn the committed diagram.
type protoLoc struct {
	file string
}

// protoLocations maps each proto message's full name to the `.proto` file it is
// declared in. It is how topic/subscription marker messages are linked back to
// their source.
func protoLocations(descriptors []byte) (map[string]protoLoc, error) {
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(descriptors, &fds); err != nil {
		return nil, fmt.Errorf("unmarshal descriptor set: %w", err)
	}

	out := map[string]protoLoc{}
	for _, fd := range fds.GetFile() {
		file := protoModuleRoot + fd.GetName()
		for _, msg := range fd.GetMessageType() {
			collectProtoLoc(fd.GetPackage(), file, msg, out)
		}
	}
	return out, nil
}

func collectProtoLoc(prefix, file string, msg *descriptorpb.DescriptorProto, out map[string]protoLoc) {
	full := prefix + "." + msg.GetName()
	out[full] = protoLoc{file: file}

	for _, nested := range msg.GetNestedType() {
		collectProtoLoc(full, file, nested, out)
	}
}

// --- rendering ---------------------------------------------------------------

func render(topics []gcp.DesiredTopic, subs []gcp.DesiredSubscription, sites []callSite, unresolved []string, locs map[string]protoLoc) string {
	sort.Slice(topics, func(i, j int) bool { return topics[i].Name < topics[j].Name })
	sort.Slice(subs, func(i, j int) bool { return subs[i].Name < subs[j].Name })

	topicByProto := map[string]gcp.DesiredTopic{}
	for _, t := range topics {
		topicByProto[t.ProtoMessage] = t
	}
	subByProto := map[string]gcp.DesiredSubscription{}
	for _, s := range subs {
		subByProto[s.ProtoMessage] = s
	}

	// Group call sites by the topology resource they touch.
	publishers := map[string][]callSite{} // topic name -> publisher sites
	consumers := map[string][]callSite{}  // subscription name -> consumer sites
	for _, s := range sites {
		switch s.role {
		case "publish":
			// Only constructions of topic messages are publishers; constructing any
			// other infra/gen proto (e.g. a subscription marker) is not.
			if t, ok := topicByProto[s.msgFull]; ok {
				publishers[t.Name] = append(publishers[t.Name], s)
			}
		case "subscribe":
			if sub, ok := subByProto[s.subFull]; ok {
				consumers[sub.Name] = append(consumers[sub.Name], s)
			} else {
				unresolved = append(unresolved, fmt.Sprintf("%s:%d consumes undeclared subscription %s", s.file, s.line, s.subFull))
			}
		}
	}

	var b strings.Builder
	b.WriteString("<!-- Code generated by `infra gen-diagram`. DO NOT EDIT. -->\n\n")
	b.WriteString("# Pub/Sub Topology\n\n")
	b.WriteString("Generated from the proto-declared topology (`infra/gen` descriptors) joined\n")
	b.WriteString("with ast-grep scans of Go (`server/`) and Python (`pystreams/`) call sites.\n")
	b.WriteString("Run `mise run gen:infra` to regenerate.\n\n")

	writeMermaid(&b, topics, subs, publishers, consumers)
	writeTables(&b, topics, subs, publishers, consumers, locs)
	writeNotes(&b, topics, subs, publishers, consumers, unresolved)

	return b.String()
}

func writeMermaid(b *strings.Builder, topics []gcp.DesiredTopic, subs []gcp.DesiredSubscription, publishers, consumers map[string][]callSite) {
	b.WriteString("```mermaid\n")
	b.WriteString("flowchart LR\n")
	b.WriteString("  classDef topic fill:#dbeafe,stroke:#3b82f6,color:#1e3a8a;\n")
	b.WriteString("  classDef dlq fill:#fee2e2,stroke:#ef4444,color:#7f1d1d;\n")
	b.WriteString("  classDef sub fill:#dcfce7,stroke:#22c55e,color:#14532d;\n")
	b.WriteString("  classDef go fill:#e0f2fe,stroke:#0284c7,color:#075985;\n")
	b.WriteString("  classDef python fill:#fef9c3,stroke:#ca8a04,color:#713f12;\n")
	b.WriteString("  classDef deprecated stroke-dasharray:4 3,opacity:0.55;\n\n")

	var deprecated []string
	siteID := 0

	// Topics (split regular vs DLQ for shape/class).
	for _, t := range topics {
		id := nodeID("t_", t.Name)
		label := t.Name
		class := "topic"
		if isDLQ(t) {
			label += "<br/>(dlq)"
			class = "dlq"
		} else {
			label += "<br/>(topic)"
		}
		fmt.Fprintf(b, "  %s([\"%s\"]):::%s\n", id, label, class)
		if isDeprecated(t.Labels) {
			deprecated = append(deprecated, id)
		}
	}

	// Subscriptions.
	for _, s := range subs {
		id := nodeID("s_", s.Name)
		fmt.Fprintf(b, "  %s[\"%s<br/>(sub)\"]:::sub\n", id, s.Name)
		if isDeprecated(s.Labels) {
			deprecated = append(deprecated, id)
		}
	}

	b.WriteString("\n")

	// Publisher nodes + edges to topics.
	for _, t := range topics {
		for _, site := range publishers[t.Name] {
			pid := fmt.Sprintf("p%d", siteID)
			siteID++
			fmt.Fprintf(b, "  %s[/\"📤<br/>%s\"/]:::%s\n", pid, site.file, site.lang)
			fmt.Fprintf(b, "  %s --> %s\n", pid, nodeID("t_", t.Name))
		}
	}

	// Topic -> subscription edges.
	for _, s := range subs {
		fmt.Fprintf(b, "  %s --> %s\n", nodeID("t_", s.Topic), nodeID("s_", s.Name))
		// Subscription -> DLQ edge.
		if s.DeadLetterTopic != "" {
			fmt.Fprintf(b, "  %s -. dead-letter .-> %s\n", nodeID("s_", s.Name), nodeID("t_", s.DeadLetterTopic))
		}
	}

	// Consumer nodes + edges from subscriptions.
	for _, s := range subs {
		for _, site := range consumers[s.Name] {
			cid := fmt.Sprintf("c%d", siteID)
			siteID++
			label := fmt.Sprintf("📥<br/>%s", site.file)
			if site.handler != "" {
				label += "<br/>" + site.handler
			}
			if site.batch {
				label += "<br/>(batch)"
			}
			fmt.Fprintf(b, "  %s[\\\"%s\"\\]:::%s\n", cid, label, site.lang)
			fmt.Fprintf(b, "  %s --> %s\n", nodeID("s_", s.Name), cid)
		}
	}

	if len(deprecated) > 0 {
		fmt.Fprintf(b, "\n  class %s deprecated;\n", strings.Join(deprecated, ","))
	}

	b.WriteString("```\n\n")
}

func writeTables(b *strings.Builder, topics []gcp.DesiredTopic, subs []gcp.DesiredSubscription, publishers, consumers map[string][]callSite, locs map[string]protoLoc) {
	b.WriteString("## Topics\n\n")
	b.WriteString("| Topic | Kind | Retention | Published by |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, t := range topics {
		kind := "topic"
		if isDLQ(t) {
			kind = "DLQ"
		}
		if isDeprecated(t.Labels) {
			kind += " (deprecated)"
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s |\n", protoNameCell(t.Name, t.ProtoMessage, locs), kind, orDash(humanDur(t.Retention)), siteList(publishers[t.Name]))
	}

	b.WriteString("\n## Subscriptions\n\n")
	b.WriteString("| Subscription | Topic | Ack | DLQ | Consumed by |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, s := range subs {
		name := protoNameCell(s.Name, s.ProtoMessage, locs)
		if isDeprecated(s.Labels) {
			name += " _(deprecated)_"
		}
		fmt.Fprintf(b, "| %s | `%s` | %s | %s | %s |\n", name, s.Topic, orDash(humanDur(s.AckDeadline)), orDash(codeOrDash(s.DeadLetterTopic)), siteList(consumers[s.Name]))
	}
	b.WriteString("\n")
}

func writeNotes(b *strings.Builder, topics []gcp.DesiredTopic, subs []gcp.DesiredSubscription, publishers, consumers map[string][]callSite, unresolved []string) {
	var notes []string
	for _, t := range topics {
		if isDLQ(t) || isDeprecated(t.Labels) {
			continue
		}
		if len(publishers[t.Name]) == 0 {
			notes = append(notes, fmt.Sprintf("Topic `%s` has no publisher in `server/` or `pystreams/`.", t.Name))
		}
	}
	for _, s := range subs {
		if isDeprecated(s.Labels) {
			continue
		}
		if len(consumers[s.Name]) == 0 {
			notes = append(notes, fmt.Sprintf("Subscription `%s` has no consumer in `server/` or `pystreams/`.", s.Name))
		}
	}
	notes = append(notes, unresolved...)

	if len(notes) == 0 {
		return
	}
	b.WriteString("## Notes\n\n")
	for _, n := range notes {
		fmt.Fprintf(b, "- %s\n", n)
	}
	b.WriteString("\n")
}

// --- small helpers -----------------------------------------------------------

func nodeID(prefix, name string) string {
	return prefix + strings.NewReplacer("-", "_", ".", "_").Replace(name)
}

func isDLQ(t gcp.DesiredTopic) bool { return t.Labels["dlq_for"] != "" }

func isDeprecated(labels map[string]string) bool { return labels["deprecated"] == "true" }

func humanDur(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	s := int64(d.Seconds())
	switch {
	case s%86400 == 0:
		return fmt.Sprintf("%dd", s/86400)
	case s%3600 == 0:
		return fmt.Sprintf("%dh", s/3600)
	case s%60 == 0:
		return fmt.Sprintf("%dm", s/60)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func codeOrDash(s string) string {
	if s == "" {
		return ""
	}
	return "`" + s + "`"
}

func siteList(sites []callSite) string {
	if len(sites) == 0 {
		return "—"
	}
	parts := make([]string, 0, len(sites))
	for _, s := range sites {
		// Link to the file (no line anchor): line numbers shift on every unrelated
		// edit, churning the committed diagram for no benefit.
		label := fmt.Sprintf("[`%s`](%s)", s.file, sourceLink(s))
		parts = append(parts, label)
	}
	return strings.Join(parts, "<br/>")
}

// sourceLink builds a link from docs/pubsub-topology.md to a call site's source
// file. The path is repo-relative, so one `../` reaches the repo root.
func sourceLink(s callSite) string {
	return "../" + s.file
}

// protoLink builds a link from docs/pubsub-topology.md to a proto declaration file.
func protoLink(loc protoLoc) string {
	return "../" + loc.file
}

// protoNameCell renders a topology resource's name as a code span linking to the
// proto message that declares it, falling back to a plain code span when the
// declaration site is unknown (e.g. a synthesized DLQ with no own marker).
func protoNameCell(name, protoMessage string, locs map[string]protoLoc) string {
	if loc, ok := locs[protoMessage]; ok {
		return fmt.Sprintf("[`%s`](%s)", name, protoLink(loc))
	}
	return "`" + name + "`"
}
