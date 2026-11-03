package authz

import (
	"bytes"
	"cmp"
	"encoding/csv"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var updateFoundationInventory = flag.Bool("update-foundation-inventory", false, "rewrite testdata/foundation_compatibility/consumers.csv from the server source tree")

const (
	foundationCompatibilityDir = "testdata/foundation_compatibility"
	authzImportPath            = "github.com/speakeasy-api/gram/server/internal/authz"
)

// consumerCategory says what a consumer does with the authorization package.
// Each category is migrated differently, so the inventory keeps them apart.
type consumerCategory string

const (
	// categoryCheck builds a Check (or grant expression) that the Engine or
	// an engine-free evaluator then decides.
	categoryCheck consumerCategory = "check"
	// categoryEngine calls an Engine method whose name identifies it
	// unambiguously: admission, preparation, mandatory decisions, overrides.
	categoryEngine consumerCategory = "engine"
	// categoryEvaluator decides grants without the Engine.
	categoryEvaluator consumerCategory = "evaluator"
	// categoryGate is an independent gate kept outside ordinary grant
	// evaluation (sensitive content, upstream selector writes).
	categoryGate consumerCategory = "gate"
	// categoryProjection reads scope metadata: catalogue, implications,
	// exclusions, grant surfaces, selector validation, role defaults.
	categoryProjection consumerCategory = "projection"
	// categoryLoader loads principals or grants, or moves them through the
	// request context.
	categoryLoader consumerCategory = "loader"
	// categoryWriter writes or validates stored grants.
	categoryWriter consumerCategory = "writer"
	// categoryWiring constructs or configures authorization components.
	categoryWiring consumerCategory = "wiring"
)

// consumerEntryPoints classifies every exported authz function a consumer may
// call. A call to an exported function missing from this map fails
// TestFoundationInventory, so a new entry point gets classified before it
// spreads. Pure value constructors (NewGrant, NewSelector, selector helpers)
// are listed as "" and not inventoried: they decide nothing on their own.
var consumerEntryPoints = map[string]consumerCategory{
	// Check and expression builders.
	"MCPToolCallCheck":        categoryCheck,
	"MCPCheck":                categoryCheck,
	"AssistantCheck":          categoryCheck,
	"HooksIngestCheck":        categoryCheck,
	"MCPAuthClientCheck":      categoryCheck,
	"RiskPolicyEvaluateCheck": categoryCheck,
	"RiskPolicyApplies":       categoryCheck,
	"ChatReadCheck":           categoryCheck,
	"ChatWriteCheck":          categoryCheck,
	"RiskPolicyBypassCheck":   categoryCheck,
	"InferenceExecuteCheck":   categoryCheck,

	// Engine-free evaluation helpers.
	"GrantsSatisfy":                 categoryEvaluator,
	"GrantsAuthorize":               categoryEvaluator,
	"GrantsContainSelector":         categoryEvaluator,
	"GrantsContainSelectorFor":      categoryEvaluator,
	"GrantAllows":                   categoryEvaluator,
	"ExplainGrantCheck":             categoryEvaluator,
	"DirectOverrideGrants":          categoryEvaluator,
	"IsDirectGrant":                 categoryEvaluator,
	"ExclusionYieldsToDirectGrants": categoryEvaluator,
	"IsBlocklistScope":              categoryEvaluator,
	"RequirePluginWrite":            categoryEvaluator,
	"APIKeyReachesMCP":              categoryEvaluator,

	// Independent gates.
	"CheckSensitiveContentAccess":            categoryGate,
	"RedactSessionTitle":                     categoryGate,
	"UpstreamSelectorWritesAllowed":          categoryGate,
	"RequireUpstreamSelectorWrites":          categoryGate,
	"RequireUpstreamSelectorWritesRaw":       categoryGate,
	"RequireUpstreamSelectorWritesForGrants": categoryGate,

	// Scope metadata projections.
	"ScopeImplicationClosure":   categoryProjection,
	"ExclusionScopeFor":         categoryProjection,
	"CalculateSubScopes":        categoryProjection,
	"ScopeVisibilityFor":        categoryProjection,
	"RegisteredScopes":          categoryProjection,
	"ResourceKindForScope":      categoryProjection,
	"GrantSurfaceForScope":      categoryProjection,
	"ValidateScopeGrantSurface": categoryProjection,
	"ValidateGrantSurface":      categoryProjection,
	"ValidateSelector":          categoryProjection,
	"ValidateAPIKeyPolicyGrant": categoryProjection,
	"GrantsToScopedGrants":      categoryProjection,
	"DemoScopeGrants":           categoryProjection,
	"GrantsFromOverrides":       categoryProjection,
	"IsSharedAuthClientType":    categoryProjection,
	"IsAgentKeyAuthClientType":  categoryProjection,
	"ParseScopeOverrides":       categoryProjection,

	// Principal and grant loading, and request-context plumbing.
	"LoadGrants":                     categoryLoader,
	"LoadUserGrants":                 categoryLoader,
	"ResolveActorPrincipals":         categoryLoader,
	"ResolveAgentPrincipals":         categoryLoader,
	"ResolveUserPrincipals":          categoryLoader,
	"ListGrantsForResource":          categoryLoader,
	"ListGrantsForResourceIDs":       categoryLoader,
	"GrantsForRole":                  categoryLoader,
	"GrantsFromContext":              categoryLoader,
	"GrantsToContext":                categoryLoader,
	"ResolveOrganizationAdminEmails": categoryLoader,
	"AdmittedWorkloadAgent":          categoryLoader,
	"SelectorFromRow":                categoryLoader,

	// Stored grant writers and their validation.
	"SeedSystemRoleGrants":            categoryWriter,
	"SeedSystemRoleGrantsTx":          categoryWriter,
	"PatchRoleGrantsTx":               categoryWriter,
	"PatchPrincipalGrants":            categoryWriter,
	"DeleteRoleGrants":                categoryWriter,
	"ReplaceGrantAudience":            categoryWriter,
	"ReplaceResourceAudience":         categoryWriter,
	"ReplaceGrantsForResource":        categoryWriter,
	"GrantResourceToPrincipals":       categoryWriter,
	"RevokeResourceFromPrincipals":    categoryWriter,
	"SeedPersonalAuthClientDefaultTx": categoryWriter,
	"MarkAuthPermissionTakenOverTx":   categoryWriter,
	"LockAuthPermissionBackfillTx":    categoryWriter,
	"TakeOverPrincipalPolicyTx":       categoryWriter,
	"InsertPrincipalGrantsIfAbsentTx": categoryWriter,
	"ValidatePrincipal":               categoryWriter,

	// Construction and configuration.
	"NewEngine":                     categoryWiring,
	"NewChallengeCHWriter":          categoryWiring,
	"ChallengeLoggingEnabled":       categoryWiring,
	"NewProvisioner":                categoryWiring,
	"SetUpstreamSelectorWriteGate":  categoryWiring,
	"WithUpstreamSelectorWriteGate": categoryWiring,

	// Value constructors: not inventoried.
	"NewGrant":                     "",
	"NewGrantWithSelector":         "",
	"NewSelector":                  "",
	"AllUsersPrincipal":            "",
	"Scope":                        "",
	"PersonalAuthClientSelector":   "",
	"AuthClientSelector":           "",
	"ConnectionAuthClientSelector": "",
	"AuthClientTypeSelector":       "",
	"ProjectServersSelector":       "",
	"NamesUpstreamClient":          "",
	"Denied":                       "",
	"InvalidCheck":                 "",
}

// engineEntryPoints are Engine methods whose names are distinctive enough to
// inventory by name alone. Require, RequireAny, Evaluate and Filter are not:
// their checks are inventoried where they are built instead.
var engineEntryPoints = map[string]bool{
	"PrepareContext":           true,
	"RefreshContext":           true,
	"CaptureAdmissionBoundary": true,
	"PrepareMandatory":         true,
	"AuthorizeMandatory":       true,
	"DecideServerAdmission":    true,
	"ShouldEnforce":            true,
	"GetScopeOverrides":        true,
	"EvaluateLoadedGrants":     true,
	"RequireAnyUnblocked":      true,
	"FindMatched":              true,
	"InvalidateRoleCache":      true,
	"InvalidateAllRoleCaches":  true,
}

// enforcingMethods annotate a check with the Engine method that decides it.
var enforcingMethods = map[string]bool{
	"Require":               true,
	"RequireAny":            true,
	"RequireAnyUnblocked":   true,
	"Evaluate":              true,
	"Filter":                true,
	"FindMatched":           true,
	"AuthorizeMandatory":    true,
	"EvaluateLoadedGrants":  true,
	"Decide":                true,
	"DecideServerAdmission": true,
	"GrantsAuthorize":       true,
	"GrantsSatisfy":         true,
	"ExplainGrantCheck":     true,
}

type inventoryConsumer struct {
	File     string
	Function string
	Category consumerCategory
	Entry    string
	Scope    string
	Via      string
	Count    int
	Owner    string
}

func (c inventoryConsumer) key() string {
	return strings.Join([]string{c.File, c.Function, string(c.Category), c.Entry, c.Scope, c.Via}, "\x00")
}

// TestFoundationInventory freezes every authorization consumer outside the
// authz package and assigns each one to the migration batch that moves it
// (docs/rbac-foundation-migration.md). It fails when a consumer appears, moves
// or disappears without the inventory following, and when a consumer has no
// owning batch. Refresh it with
//
//	go test ./internal/authz -run TestFoundationInventory -update-foundation-inventory
func TestFoundationInventory(t *testing.T) {
	t.Parallel()

	consumers, unknown := scanAuthorizationConsumers(t, filepath.Join("..", ".."))
	require.Empty(t, unknown, "classify these exported authz functions in consumerEntryPoints")

	var unowned []string
	for i := range consumers {
		consumers[i].Owner = foundationOwner(consumers[i])
		if consumers[i].Owner == "" {
			unowned = append(unowned, consumers[i].File+" "+consumers[i].Function)
		}
	}
	require.Empty(t, unowned, "assign these consumers to a Backend Revamp ticket in foundationOwners")
	require.Empty(t, staleFoundationOwners(consumers), "remove owner rules that no longer match a consumer")

	got := encodeInventory(t, consumers)
	path := filepath.Join(foundationCompatibilityDir, "consumers.csv")
	if *updateFoundationInventory {
		require.NoError(t, os.WriteFile(path, got, 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got), "authorization consumers changed; refresh with -update-foundation-inventory and update docs/rbac-foundation-migration.md")
}

func scanAuthorizationConsumers(t *testing.T, root string) ([]inventoryConsumer, []string) {
	t.Helper()

	byKey := map[string]*inventoryConsumer{}
	unknownSet := map[string]struct{}{}
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return fmt.Errorf("relative path: %w", relErr)
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch {
			case rel == "gen", rel == "internal/authz", rel == "internal/authztest", strings.HasSuffix(rel, "/testdata"), d.Name() == "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return fmt.Errorf("parse %s: %w", rel, parseErr)
		}
		alias, ok := authzImportName(file)
		if !ok {
			return nil
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			scanFunction(fset, rel, alias, fn, byKey, unknownSet)
		}
		return nil
	})
	require.NoError(t, err)

	consumers := make([]inventoryConsumer, 0, len(byKey))
	for _, c := range byKey {
		consumers = append(consumers, *c)
	}
	slices.SortFunc(consumers, func(a, b inventoryConsumer) int { return cmp.Compare(a.key(), b.key()) })
	unknown := make([]string, 0, len(unknownSet))
	for name := range unknownSet {
		unknown = append(unknown, name)
	}
	slices.Sort(unknown)
	return consumers, unknown
}

func authzImportName(file *ast.File) (string, bool) {
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != authzImportPath {
			continue
		}
		if spec.Name != nil {
			return spec.Name.Name, spec.Name.Name != "_"
		}
		return "authz", true
	}
	return "", false
}

func scanFunction(fset *token.FileSet, rel, alias string, fn *ast.FuncDecl, byKey map[string]*inventoryConsumer, unknown map[string]struct{}) {
	function := fn.Name.Name
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		function = nodeSource(fset, fn.Recv.List[0].Type) + "." + function
	}

	record := func(category consumerCategory, entry, scope, via string) {
		c := inventoryConsumer{File: rel, Function: function, Category: category, Entry: entry, Scope: scope, Via: via, Count: 0, Owner: ""}
		existing, ok := byKey[c.key()]
		if !ok {
			existing = &c
			byKey[c.key()] = existing
		}
		existing.Count++
	}

	var stack []ast.Node
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)

		switch x := n.(type) {
		case *ast.CompositeLit:
			if isAuthzSelector(x.Type, alias, "Check") {
				record(categoryCheck, "Check", compositeScope(fset, x, alias), enclosingMethod(stack))
			}
			// Elements of []authz.Check{{...}} and map[K]authz.Check{k: {...}}
			// elide their type, so record them through the outer literal.
			if elidesCheck(x.Type, alias) {
				for _, elt := range x.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						elt = kv.Value
					}
					if u, ok := elt.(*ast.UnaryExpr); ok && u.Op == token.AND {
						elt = u.X
					}
					if lit, ok := elt.(*ast.CompositeLit); ok && lit.Type == nil {
						record(categoryCheck, "Check", compositeScope(fset, lit, alias), enclosingMethod(stack))
					}
				}
			}
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == alias {
				name := sel.Sel.Name
				category, known := consumerEntryPoints[name]
				switch {
				case !ast.IsExported(name):
				case !known:
					unknown[name] = struct{}{}
				case category == "":
				case category == categoryCheck:
					record(category, name, callScope(fset, x, alias), enclosingMethod(stack[:len(stack)-1]))
				default:
					record(category, name, "", "")
				}
				return true
			}
			if engineEntryPoints[sel.Sel.Name] {
				record(categoryEngine, sel.Sel.Name, "", "")
			}
		}
		return true
	})
}

func isAuthzSelector(expr ast.Expr, alias, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == alias && sel.Sel.Name == name
}

// elidesCheck reports whether typ is a slice, array or map literal type whose
// elements are authz.Check or *authz.Check.
func elidesCheck(typ ast.Expr, alias string) bool {
	var elem ast.Expr
	switch t := typ.(type) {
	case *ast.ArrayType:
		elem = t.Elt
	case *ast.MapType:
		elem = t.Value
	default:
		return false
	}
	if star, ok := elem.(*ast.StarExpr); ok {
		elem = star.X
	}
	return isAuthzSelector(elem, alias, "Check")
}

// enclosingMethod names the innermost enforcing call around the node on top of
// stack, or "" when the check is built for a later call.
func enclosingMethod(stack []ast.Node) string {
	for i := len(stack) - 1; i >= 0; i-- {
		call, ok := stack[i].(*ast.CallExpr)
		if !ok {
			continue
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && enforcingMethods[sel.Sel.Name] {
			return sel.Sel.Name
		}
	}
	return ""
}

func compositeScope(fset *token.FileSet, lit *ast.CompositeLit, alias string) string {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Scope" {
			return strings.TrimPrefix(nodeSource(fset, kv.Value), alias+".")
		}
	}
	return ""
}

func callScope(fset *token.FileSet, call *ast.CallExpr, alias string) string {
	if len(call.Args) == 0 {
		return ""
	}
	arg := nodeSource(fset, call.Args[0])
	if !strings.HasPrefix(arg, alias+".Scope") {
		return ""
	}
	return strings.TrimPrefix(arg, alias+".")
}

func nodeSource(fset *token.FileSet, node ast.Node) string {
	var b bytes.Buffer
	if err := printer.Fprint(&b, fset, node); err != nil {
		return ""
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func encodeInventory(t *testing.T, consumers []inventoryConsumer) []byte {
	t.Helper()

	var b bytes.Buffer
	w := csv.NewWriter(&b)
	require.NoError(t, w.Write([]string{"file", "function", "category", "entry", "scope", "via", "count", "owner"}))
	for _, c := range consumers {
		require.NoError(t, w.Write([]string{c.File, c.Function, string(c.Category), c.Entry, c.Scope, c.Via, strconv.Itoa(c.Count), c.Owner}))
	}
	w.Flush()
	require.NoError(t, w.Error())
	return b.Bytes()
}

// staleFoundationOwners lists owner rules that match no consumer, so the rules
// shrink as migrations remove call sites instead of silently rotting.
func staleFoundationOwners(consumers []inventoryConsumer) []string {
	used := map[string]bool{}
	for _, c := range consumers {
		used[path.Dir(c.File)] = true
		used[c.File] = true
		used[c.File+" "+c.Function] = true
	}
	var stale []string
	for _, rules := range []map[string]string{foundationPackageOwners, foundationFileOwners, foundationFunctionOwners} {
		for key := range rules {
			if !used[key] {
				stale = append(stale, key)
			}
		}
	}
	slices.Sort(stale)
	return stale
}
