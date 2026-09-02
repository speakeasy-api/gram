package hostedinference

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

const (
	gramModulePath       = "github.com/speakeasy-api/gram"
	openRouterPackage    = gramModulePath + "/server/internal/thirdparty/openrouter"
	hostedPackage        = gramModulePath + "/server/internal/killswitches/hostedinference"
	chatPackage          = gramModulePath + "/server/internal/chat"
	contextValuesPackage = gramModulePath + "/server/internal/contextvalues"
	openRouterSDKPrefix  = "github.com/OpenRouterTeam/go-sdk"
)

type inventoryIssue struct {
	Kind     string
	Path     string
	Function string
	Detail   string
}

func (i inventoryIssue) String() string {
	return fmt.Sprintf("%s: %s:%s: %s", i.Kind, i.Path, i.Function, i.Detail)
}

type packageImport struct {
	packagePath string
	importPath  string
}

type typedCall struct {
	path        string
	packagePath string
	function    string
	callee      *types.Func
	call        ssa.CallInstruction
}

type inventoryAnalysis struct {
	serverRoot        string
	program           *ssa.Program
	functions         []*ssa.Function
	calls             []typedCall
	imports           []packageImport
	completionMethods map[string][]*types.Signature
	flowCategories    map[ssa.Value]map[CallCategory]struct{}
	flowUnclassified  map[ssa.Value]bool
	constructors      map[*ssa.Function]ConstructorKind
	callers           map[*ssa.Function][]ssa.CallInstruction
	escapedFunctions  map[*ssa.Function]bool
}

var (
	repositoryAnalysisOnce sync.Once
	repositoryAnalysis     *inventoryAnalysis
	errRepositoryAnalysis  error
)

func loadRepositoryAnalysis(t *testing.T) *inventoryAnalysis {
	t.Helper()
	repositoryAnalysisOnce.Do(func() {
		root := repositoryRoot(t)
		patterns, err := repositoryInventoryPatterns(root)
		if err != nil {
			errRepositoryAnalysis = err
			return
		}
		repositoryAnalysis, errRepositoryAnalysis = loadInventoryAnalysis(root, patterns...)
	})
	require.NoError(t, errRepositoryAnalysis)
	return repositoryAnalysis
}

func repositoryInventoryPatterns(root string) ([]string, error) {
	serverRoot := filepath.Join(root, "server")
	patterns := map[string]bool{
		"./server/internal/killswitches/hostedinference": true,
		"./server/internal/thirdparty/openrouter":        true,
	}
	err := filepath.WalkDir(serverRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" || path == filepath.Join(serverRoot, "gen") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read inventory candidate %s: %w", path, err)
		}
		text := string(source)
		relevant := strings.Contains(text, "NewUnifiedClient") ||
			strings.Contains(text, "NewUncheckedUnifiedClient") ||
			strings.Contains(text, "ChatClient") ||
			strings.Contains(text, ".GetCompletion(") ||
			strings.Contains(text, ".GetCompletionStream(") ||
			strings.Contains(text, ".GetObjectCompletion(") ||
			strings.Contains(text, ".CreateEmbeddings(") ||
			strings.Contains(text, "WithValidatedGramSession(") ||
			strings.Contains(text, "WithValidatedChatSessionActingUser(") ||
			strings.Contains(text, hostedPackage) ||
			strings.Contains(text, openRouterSDKPrefix) ||
			strings.Contains(text, "OpenRouterBaseURL") ||
			strings.Contains(text, "openrouter.ai/")
		if !relevant && strings.Contains(text, "\"net/http\"") {
			for _, operation := range []string{".Do(", ".RoundTrip(", ".Get(", ".Head(", ".Post(", ".PostForm("} {
				if strings.Contains(text, operation) {
					relevant = true
					break
				}
			}
		}
		if relevant {
			relative, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return fmt.Errorf("make inventory package path relative: %w", err)
			}
			patterns["./"+filepath.ToSlash(relative)] = true
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover inventory packages: %w", err)
	}
	result := make([]string, 0, len(patterns))
	for pattern := range patterns {
		result = append(result, pattern)
	}
	sort.Strings(result)
	return result, nil
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	require.NoError(t, err)
	return root
}

func loadInventoryAnalysis(root string, patterns ...string) (*inventoryAnalysis, error) {
	config := &packages.Config{
		Dir: root,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports | packages.NeedTypesSizes,
		Tests: false,
	}
	loaded, err := packages.Load(config, patterns...)
	if err != nil {
		return nil, fmt.Errorf("load inventory packages: %w", err)
	}
	var packageErrors []string
	for _, pkg := range loaded {
		for _, packageErr := range pkg.Errors {
			packageErrors = append(packageErrors, packageErr.Error())
		}
	}
	if len(packageErrors) > 0 {
		sort.Strings(packageErrors)
		if len(packageErrors) > 8 {
			packageErrors = packageErrors[:8]
		}
		return nil, fmt.Errorf("load inventory packages: %s", strings.Join(packageErrors, "; "))
	}

	program, ssaPackages := ssautil.Packages(loaded, ssa.InstantiateGenerics)
	program.Build()

	serverRoot := filepath.Join(root, "server")
	analysis := &inventoryAnalysis{
		serverRoot:        serverRoot,
		program:           program,
		completionMethods: map[string][]*types.Signature{},
		flowCategories:    map[ssa.Value]map[CallCategory]struct{}{},
		flowUnclassified:  map[ssa.Value]bool{},
		constructors:      map[*ssa.Function]ConstructorKind{},
		callers:           map[*ssa.Function][]ssa.CallInstruction{},
		escapedFunctions:  map[*ssa.Function]bool{},
	}
	for _, pkg := range loaded {
		analysis.registerOpenRouterSymbols(pkg)
		if !strings.HasPrefix(pkg.PkgPath, gramModulePath+"/server/") {
			continue
		}
		for importPath := range pkg.Imports {
			analysis.imports = append(analysis.imports, packageImport{packagePath: pkg.PkgPath, importPath: importPath})
		}
	}
	seenFunctions := map[*ssa.Function]bool{}
	var addFunction func(*ssa.Function)
	addFunction = func(fn *ssa.Function) {
		if fn == nil || seenFunctions[fn] || len(fn.Blocks) == 0 {
			return
		}
		seenFunctions[fn] = true
		analysis.functions = append(analysis.functions, fn)
		for _, anonymous := range fn.AnonFuncs {
			addFunction(anonymous)
		}
	}
	packages.Visit(loaded, func(pkg *packages.Package) bool {
		analysis.registerOpenRouterSymbols(pkg)
		return len(analysis.completionMethods) == 0 || len(analysis.constructors) == 0
	}, nil)
	for index, pkg := range loaded {
		if index < len(ssaPackages) && ssaPackages[index] != nil {
			for _, member := range ssaPackages[index].Members {
				if fn, ok := member.(*ssa.Function); ok {
					addFunction(fn)
				}
			}
		}
		for _, object := range pkg.TypesInfo.Defs {
			if fnObject, ok := object.(*types.Func); ok {
				addFunction(program.FuncValue(fnObject))
			}
		}
	}
	for _, fn := range analysis.functions {
		path := analysis.functionPath(fn)
		for _, block := range fn.Blocks {
			for _, instruction := range block.Instrs {
				call, _ := instruction.(ssa.CallInstruction)
				var direct *ssa.Function
				if call != nil {
					direct = call.Common().StaticCallee()
					if direct != nil {
						analysis.callers[direct] = append(analysis.callers[direct], call)
					}
				}
				for _, operand := range instruction.Operands(nil) {
					referenced, ok := (*operand).(*ssa.Function)
					if ok && referenced != direct {
						analysis.escapedFunctions[referenced] = true
					}
				}
				if call != nil {
					if callee := calledObject(call.Common()); callee != nil {
						ownerPackage := ""
						if fn.Package() != nil && fn.Package().Pkg != nil {
							ownerPackage = fn.Package().Pkg.Path()
						}
						analysis.calls = append(analysis.calls, typedCall{path: path, packagePath: ownerPackage, function: sourceFunctionName(fn), callee: callee, call: call})
					}
				}
			}
		}
	}
	analysis.computeCategoryFlow()
	return analysis, nil
}

func (a *inventoryAnalysis) registerOpenRouterSymbols(pkg *packages.Package) {
	if pkg == nil || pkg.PkgPath != openRouterPackage || pkg.Types == nil {
		return
	}
	for _, kind := range []ConstructorKind{ConstructorProduction, ConstructorUnchecked} {
		if object, ok := pkg.Types.Scope().Lookup(string(kind)).(*types.Func); ok {
			a.constructors[a.program.FuncValue(object)] = kind
		}
	}
	if len(a.completionMethods) > 0 {
		return
	}
	object := pkg.Types.Scope().Lookup("CompletionClient")
	if object == nil {
		return
	}
	named, ok := types.Unalias(object.Type()).(*types.Named)
	if !ok {
		return
	}
	contract, ok := named.Underlying().(*types.Interface)
	if !ok {
		return
	}
	contract.Complete()
	for method := range contract.Methods() {
		switch method.Name() {
		case "GetCompletion", "GetCompletionStream", "GetObjectCompletion", "CreateEmbeddings":
			signature, _ := method.Type().(*types.Signature)
			a.completionMethods[method.Name()] = append(a.completionMethods[method.Name()], signature)
		}
	}
}

func (a *inventoryAnalysis) functionPath(fn *ssa.Function) string {
	filename := a.program.Fset.Position(fn.Pos()).Filename
	rel, err := filepath.Rel(a.serverRoot, filename)
	if err != nil {
		return filepath.ToSlash(filename)
	}
	return filepath.ToSlash(rel)
}

func sourceFunctionName(fn *ssa.Function) string {
	for fn.Parent() != nil {
		fn = fn.Parent()
	}
	return fn.Name()
}

func calledObject(common *ssa.CallCommon) *types.Func {
	if common == nil {
		return nil
	}
	if common.Method != nil {
		return common.Method
	}
	if callee := common.StaticCallee(); callee != nil {
		if object, ok := callee.Object().(*types.Func); ok {
			return object
		}
	}
	return nil
}

func packagePath(object *types.Func) string {
	if object == nil || object.Pkg() == nil {
		return ""
	}
	return object.Pkg().Path()
}

func isKnownContextPreserver(object *types.Func) bool {
	if object == nil {
		return false
	}
	switch packagePath(object) {
	case "context":
		switch object.Name() {
		case "WithCancel", "WithCancelCause", "WithDeadline", "WithDeadlineCause", "WithTimeout", "WithTimeoutCause", "WithoutCancel":
			return true
		}
	case "go.opentelemetry.io/otel/trace":
		return object.Name() == "Start"
	case chatPackage:
		return object.Name() == "wrapHostedInferenceClassification"
	}
	return false
}

func isContextType(value types.Type) bool {
	return isNamedType(value, "context", "Context")
}

func isNamedType(value types.Type, packagePath, name string) bool {
	named, ok := types.Unalias(value).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == packagePath && named.Obj().Name() == name
}

func isPointerToNamedType(value types.Type, packagePath, name string) bool {
	pointer, ok := types.Unalias(value).(*types.Pointer)
	return ok && isNamedType(pointer.Elem(), packagePath, name)
}

func (a *inventoryAnalysis) completionIssues() []inventoryIssue {
	type actualCall struct {
		typedCall
		categories   map[CallCategory]struct{}
		unclassified bool
	}
	actual := map[string][]actualCall{}
	for _, call := range a.calls {
		if !a.isCompletionOperation(call.callee) || call.packagePath == openRouterPackage || isUncheckedConstructorAllowedPath(call.path) {
			continue
		}
		shortPath := strings.TrimPrefix(call.path, "internal/")
		owner := shortPath + ":" + call.function
		if _, transparent := transparentForwarders[owner]; transparent {
			continue
		}
		key := owner + ":" + call.callee.Name()
		categories, unclassified := a.callClassification(call.call.Common())
		actual[key] = append(actual[key], actualCall{typedCall: call, categories: categories, unclassified: unclassified})
	}

	expected := map[string][]CallSiteClaim{}
	var issues []inventoryIssue
	governedClaimed := map[CallCategory]bool{}
	for _, claim := range productionCallSiteInventory {
		key := claim.Path + ":" + claim.Function + ":" + claim.Method
		expected[key] = append(expected[key], claim)
		if err := validateCategoryClass(claim.Category, categoryClasses[claim.Category]); err != nil {
			issues = append(issues, inventoryIssue{Kind: "invalid-class-claim", Path: claim.Path, Function: claim.Function, Detail: err.Error()})
		}
		if isGovernedCategory(claim.Category) {
			governedClaimed[claim.Category] = true
		}
	}
	for key, calls := range actual {
		claims := expected[key]
		if len(claims) != len(calls) {
			issues = append(issues, inventoryIssue{Kind: "completion-inventory", Path: calls[0].path, Function: calls[0].function, Detail: fmt.Sprintf("%s actual=%d claimed=%d", calls[0].callee.Name(), len(calls), len(claims))})
		}
		for index, call := range calls {
			if len(call.categories) == 0 || call.unclassified {
				issues = append(issues, inventoryIssue{Kind: "completion-context", Path: call.path, Function: call.function, Detail: call.callee.Name() + " may receive an unclassified context"})
			}
			if index >= len(claims) {
				continue
			}
			if !categoriesMatchClaim(call.categories, call.unclassified, claims[index]) {
				issues = append(issues, inventoryIssue{Kind: "completion-context", Path: call.path, Function: call.function, Detail: fmt.Sprintf("%s receives categories %v (may be unclassified: %t), want %v", call.callee.Name(), sortedCategories(call.categories), call.unclassified, sortedCategories(allowedCategories(claims[index].Category)))})
			}
		}
		delete(expected, key)
	}
	for key, claims := range expected {
		if len(claims) > 0 {
			issues = append(issues, inventoryIssue{Kind: "completion-inventory", Path: claims[0].Path, Function: claims[0].Function, Detail: key + " is claimed but absent"})
		}
	}
	for category, class := range categoryClasses {
		if class == CallClassGovernedUser && !governedClaimed[category] {
			issues = append(issues, inventoryIssue{Kind: "missing-governed-claim", Detail: string(category)})
		}
	}
	return sortedIssues(issues)
}

func categoriesMatchClaim(actual map[CallCategory]struct{}, unclassified bool, claim CallSiteClaim) bool {
	return !unclassified && maps.Equal(actual, allowedCategories(claim.Category))
}

func allowedCategories(primary CallCategory) map[CallCategory]struct{} {
	categories := []CallCategory{primary}
	switch primary {
	case CallCategoryUserChatCompletion:
		categories = []CallCategory{CallCategoryUserChatCompletion, CallCategoryAPIKeyChat, CallCategoryNonOrdinaryGramSessionChat, CallCategoryAssistantChat, CallCategoryChatSessionChat}
	case CallCategoryChatSummary:
		categories = []CallCategory{CallCategoryChatSummary, CallCategoryAPIKeyChatSummary, CallCategoryNonOrdinarySessionChatSummary}
	case CallCategoryToolCallSummary:
		categories = []CallCategory{CallCategoryToolCallSummary, CallCategoryAPIKeyToolCallSummary, CallCategoryNonOrdinarySessionToolSummary}
	case CallCategoryRiskAuthoring:
		categories = []CallCategory{CallCategoryRiskAuthoring, CallCategoryAPIKeyRiskAuthoring, CallCategoryNonOrdinarySessionRiskAuthoring}
	case CallCategoryBusinessMemorySearchEmbedding:
		categories = []CallCategory{CallCategoryBusinessMemorySearchEmbedding, CallCategoryAPIKeyBusinessMemorySearch, CallCategoryNonOrdinarySessionMemorySearch}
	default:
	}
	result := make(map[CallCategory]struct{}, len(categories))
	for _, category := range categories {
		result[category] = struct{}{}
	}
	return result
}

func sortedCategories(categories map[CallCategory]struct{}) []string {
	result := make([]string, 0, len(categories))
	for category := range categories {
		result = append(result, string(category))
	}
	sort.Strings(result)
	return result
}

func (a *inventoryAnalysis) isCompletionOperation(object *types.Func) bool {
	if object == nil {
		return false
	}
	signature, ok := object.Type().(*types.Signature)
	if !ok {
		return false
	}
	for _, expected := range a.completionMethods[object.Name()] {
		if sameCallableSignature(signature, expected) {
			return true
		}
	}
	return false
}

func sameCallableSignature(actual, expected *types.Signature) bool {
	return types.Identical(actual, expected)
}

func (a *inventoryAnalysis) callClassification(common *ssa.CallCommon) (map[CallCategory]struct{}, bool) {
	for _, argument := range common.Args {
		if isContextType(argument.Type()) {
			return a.flowCategories[argument], a.flowUnclassified[argument]
		}
	}
	return nil, true
}

func (a *inventoryAnalysis) computeCategoryFlow() {
	relevant := map[*ssa.Function]bool{}
	for _, call := range a.calls {
		if a.isCompletionOperation(call.callee) {
			for fn := call.call.Parent(); fn != nil; fn = fn.Parent() {
				relevant[fn] = true
			}
		}
	}
	changed := true
	for changed {
		changed = false
		for fn := range relevant {
			for _, caller := range a.callers[fn] {
				if parent := caller.Parent(); parent != nil && !relevant[parent] {
					relevant[parent] = true
					changed = true
				}
			}
			for _, block := range fn.Blocks {
				for _, instruction := range block.Instrs {
					call, ok := instruction.(ssa.CallInstruction)
					if !ok {
						continue
					}
					callee := call.Common().StaticCallee()
					if callee != nil && functionReturnsContext(callee) && len(directClassificationCategories(call.Common())) == 0 && !isKnownContextPreserver(calledObject(call.Common())) && !relevant[callee] {
						relevant[callee] = true
						changed = true
					}
				}
			}
		}
	}

	edges := map[ssa.Value][]ssa.Value{}
	var phis []*ssa.Phi
	addEdge := func(from, to ssa.Value) {
		if from != nil && to != nil {
			edges[from] = append(edges[from], to)
		}
	}
	seed := func(value ssa.Value, categories map[CallCategory]struct{}) {
		if value == nil || len(categories) == 0 {
			return
		}
		target := a.flowCategories[value]
		if target == nil {
			target = map[CallCategory]struct{}{}
			a.flowCategories[value] = target
		}
		maps.Copy(target, categories)
	}

	for fn := range relevant {
		for _, block := range fn.Blocks {
			for _, instruction := range block.Instrs {
				switch value := instruction.(type) {
				case *ssa.Phi:
					phis = append(phis, value)
					for _, incoming := range value.Edges {
						addEdge(incoming, value)
					}
				case *ssa.ChangeInterface:
					addEdge(value.X, value)
				case *ssa.ChangeType:
					addEdge(value.X, value)
				case *ssa.Convert:
					addEdge(value.X, value)
				case *ssa.MakeInterface:
					addEdge(value.X, value)
				case *ssa.Store:
					addEdge(value.Val, value.Addr)
				case *ssa.UnOp:
					if value.Op == token.MUL {
						addEdge(value.X, value)
					}
				case *ssa.MakeClosure:
					closure, ok := value.Fn.(*ssa.Function)
					if ok {
						for index, binding := range value.Bindings {
							if index < len(closure.FreeVars) {
								addEdge(binding, closure.FreeVars[index])
							}
						}
					}
				}

				call, ok := instruction.(ssa.CallInstruction)
				if !ok {
					continue
				}
				common := call.Common()
				outputs := contextCallOutputs(call)
				if categories := directClassificationCategories(common); len(categories) > 0 {
					for _, output := range outputs {
						seed(output.value, categories)
					}
				}
				if isKnownContextPreserver(calledObject(common)) {
					for _, argument := range common.Args {
						if isContextType(argument.Type()) {
							for _, output := range outputs {
								addEdge(argument, output.value)
							}
						}
					}
				}
				callee := common.StaticCallee()
				if callee == nil || !relevant[callee] || len(directClassificationCategories(common)) > 0 || isKnownContextPreserver(calledObject(common)) {
					continue
				}
				for index, argument := range common.Args {
					if index < len(callee.Params) {
						addEdge(argument, callee.Params[index])
					}
				}
				for _, calleeBlock := range callee.Blocks {
					for _, calleeInstruction := range calleeBlock.Instrs {
						returned, ok := calleeInstruction.(*ssa.Return)
						if !ok {
							continue
						}
						for _, output := range outputs {
							if output.index < len(returned.Results) {
								addEdge(returned.Results[output.index], output.value)
							}
						}
					}
				}
			}
		}
	}

	queue := make([]ssa.Value, 0, len(a.flowCategories))
	for value := range a.flowCategories {
		queue = append(queue, value)
	}
	for len(queue) > 0 {
		value := queue[0]
		queue = queue[1:]
		for _, target := range edges[value] {
			targetCategories := a.flowCategories[target]
			if targetCategories == nil {
				targetCategories = map[CallCategory]struct{}{}
				a.flowCategories[target] = targetCategories
			}
			before := len(targetCategories)
			maps.Copy(targetCategories, a.flowCategories[value])
			if len(targetCategories) != before {
				queue = append(queue, target)
			}
		}
	}

	for _, phi := range phis {
		for _, incoming := range phi.Edges {
			if isContextType(phi.Type()) && len(a.flowCategories[incoming]) == 0 {
				a.flowUnclassified[phi] = true
				break
			}
		}
	}
	for fn := range relevant {
		for index, parameter := range fn.Params {
			if !isContextType(parameter.Type()) {
				continue
			}
			if a.escapedFunctions[fn] {
				a.flowUnclassified[parameter] = true
			}
			for _, caller := range a.callers[fn] {
				if index >= len(caller.Common().Args) {
					continue
				}
				argument := caller.Common().Args[index]
				if len(a.flowCategories[argument]) == 0 || a.flowUnclassified[argument] {
					a.flowUnclassified[parameter] = true
					break
				}
			}
		}
	}
	queue = queue[:0]
	for value := range a.flowUnclassified {
		queue = append(queue, value)
	}
	for len(queue) > 0 {
		value := queue[0]
		queue = queue[1:]
		for _, target := range edges[value] {
			if !isContextType(value.Type()) || !isContextType(target.Type()) {
				continue
			}
			if !a.flowUnclassified[target] {
				a.flowUnclassified[target] = true
				queue = append(queue, target)
			}
		}
	}
}

type indexedContextValue struct {
	index int
	value ssa.Value
}

func contextCallOutputs(call ssa.CallInstruction) []indexedContextValue {
	var result []indexedContextValue
	if value, ok := call.(ssa.Value); ok && isContextType(value.Type()) {
		result = append(result, indexedContextValue{index: 0, value: value})
	}
	value, hasValue := call.(ssa.Value)
	if hasValue && value.Referrers() != nil {
		for _, reference := range *value.Referrers() {
			if extracted, ok := reference.(*ssa.Extract); ok && isContextType(extracted.Type()) {
				result = append(result, indexedContextValue{index: extracted.Index, value: extracted})
			}
		}
	}
	return result
}

func functionReturnsContext(fn *ssa.Function) bool {
	if fn == nil {
		return false
	}
	signature := fn.Signature
	for v := range signature.Results().Variables() {
		if isContextType(v.Type()) {
			return true
		}
	}
	return false
}

func directClassificationCategories(common *ssa.CallCommon) map[CallCategory]struct{} {
	object := calledObject(common)
	if packagePath(object) != hostedPackage {
		return nil
	}
	switch object.Name() {
	case "WithGovernedUser", "WithGovernedUserOrUnsupported", "WithInternal", "WithBackground", "WithUnsupported":
	default:
		return nil
	}
	result := map[CallCategory]struct{}{}
	for _, argument := range common.Args {
		constantValue, ok := argument.(*ssa.Const)
		if !ok || constantValue.Value == nil || constantValue.Value.Kind() != constant.String {
			continue
		}
		category := CallCategory(constant.StringVal(constantValue.Value))
		if _, registered := categoryClasses[category]; registered {
			result[category] = struct{}{}
		}
	}
	return result
}

func (a *inventoryAnalysis) constructorIssues() []inventoryIssue {
	actual := map[string]int{}
	factories := map[string]int{}
	allocations := map[string]int{}
	checkpointWrites := map[string]int{}
	var issues []inventoryIssue
	for _, fn := range a.functions {
		constructionKey := a.functionPath(fn) + ":" + sourceFunctionName(fn)
		if functionReturnsChatClient(fn) {
			factories[constructionKey]++
		}
		for _, block := range fn.Blocks {
			for _, instruction := range block.Instrs {
				allocation, ok := instruction.(*ssa.Alloc)
				if ok && isPointerToNamedType(allocation.Type(), openRouterPackage, "ChatClient") {
					allocations[constructionKey]++
				}
				store, ok := instruction.(*ssa.Store)
				field, fieldOK := storeAddrField(store, ok)
				if fieldOK && field == "inferenceCheckpoint" {
					checkpointWrites[constructionKey]++
				}
			}
		}
		for _, block := range fn.Blocks {
			for _, instruction := range block.Instrs {
				direct, _ := instruction.(ssa.CallInstruction)
				for _, operand := range instruction.Operands(nil) {
					constructor, ok := (*operand).(*ssa.Function)
					if !ok {
						continue
					}
					kind, tracked := a.constructors[constructor]
					if !tracked || (direct != nil && direct.Common().StaticCallee() == constructor) {
						continue
					}
					issues = append(issues, inventoryIssue{Kind: "constructor-reference", Path: a.functionPath(fn), Function: sourceFunctionName(fn), Detail: string(kind) + " must be called directly"})
				}
			}
		}
	}
	constructionExpected := map[string]ClientConstructionClaim{}
	for _, claim := range clientConstructionInventory {
		constructionExpected[claim.Path+":"+claim.Function] = claim
	}
	constructionKeys := map[string]bool{}
	for key := range factories {
		constructionKeys[key] = true
	}
	for key := range allocations {
		constructionKeys[key] = true
	}
	for key := range checkpointWrites {
		constructionKeys[key] = true
	}
	for key := range constructionKeys {
		claim, found := constructionExpected[key]
		if !found || factories[key] != 1 || allocations[key] != claim.Allocations || checkpointWrites[key] != claim.CheckpointWrites {
			issues = append(issues, inventoryIssue{Kind: "client-construction-inventory", Detail: fmt.Sprintf("%s factories=%d allocations=%d checkpoint-writes=%d", key, factories[key], allocations[key], checkpointWrites[key])})
		}
		delete(constructionExpected, key)
	}
	for key, claim := range constructionExpected {
		issues = append(issues, inventoryIssue{Kind: "client-construction-inventory", Detail: fmt.Sprintf("%s factories=0 allocations=0 checkpoint-writes=0 want allocations=%d checkpoint-writes=%d", key, claim.Allocations, claim.CheckpointWrites)})
	}

	for _, call := range a.calls {
		if packagePath(call.callee) != openRouterPackage {
			continue
		}
		kind := ConstructorKind(call.callee.Name())
		if kind != ConstructorProduction && kind != ConstructorUnchecked {
			continue
		}
		if kind == ConstructorUnchecked && call.path == "internal/thirdparty/openrouter/unified_client.go" && call.function == "NewUnifiedClient" {
			continue
		}
		key := call.path + ":" + string(kind)
		actual[key]++
		if kind == ConstructorProduction && call.packagePath != gramModulePath+"/server/cmd/gram" {
			issues = append(issues, inventoryIssue{Kind: "production-constructor-boundary", Path: call.path, Function: call.function, Detail: string(kind)})
		}
		if kind == ConstructorUnchecked && !isUncheckedConstructorAllowedPath(call.path) {
			issues = append(issues, inventoryIssue{Kind: "unchecked-constructor", Path: call.path, Function: call.function, Detail: string(kind)})
		}
	}
	expected := map[string]int{}
	for _, claim := range repositoryConstructorInventory {
		expected[claim.Path+":"+string(claim.Kind)]++
	}
	for key, count := range actual {
		if expected[key] != count {
			issues = append(issues, inventoryIssue{Kind: "constructor-inventory", Detail: fmt.Sprintf("%s actual=%d claimed=%d", key, count, expected[key])})
		}
		delete(expected, key)
	}
	for key, count := range expected {
		issues = append(issues, inventoryIssue{Kind: "constructor-inventory", Detail: fmt.Sprintf("%s actual=0 claimed=%d", key, count)})
	}
	return sortedIssues(issues)
}

func storeAddrField(store *ssa.Store, ok bool) (string, bool) {
	if !ok {
		return "", false
	}
	fieldAddr, ok := store.Addr.(*ssa.FieldAddr)
	if !ok {
		return "", false
	}
	pointer, ok := types.Unalias(fieldAddr.X.Type()).(*types.Pointer)
	if !ok {
		return "", false
	}
	named, ok := types.Unalias(pointer.Elem()).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != openRouterPackage || named.Obj().Name() != "ChatClient" {
		return "", false
	}
	structure, ok := named.Underlying().(*types.Struct)
	if !ok || fieldAddr.Field >= structure.NumFields() {
		return "", false
	}
	return structure.Field(fieldAddr.Field).Name(), true
}

func functionReturnsChatClient(fn *ssa.Function) bool {
	if fn == nil || fn.Signature == nil {
		return false
	}
	results := fn.Signature.Results()
	for v := range results.Variables() {
		if isPointerToNamedType(v.Type(), openRouterPackage, "ChatClient") {
			return true
		}
	}
	return false
}

func isUncheckedConstructorAllowedPath(filePath string) bool {
	for _, claim := range repositoryConstructorInventory {
		if claim.Kind == ConstructorUnchecked && filepath.Dir(claim.Path) == filepath.Dir(filePath) {
			return true
		}
	}
	return false
}

func (a *inventoryAnalysis) providerIssues() []inventoryIssue {
	actual := map[string]int{}
	var issues []inventoryIssue
	for _, imported := range a.imports {
		if imported.packagePath == openRouterPackage || !strings.HasPrefix(imported.importPath, openRouterSDKPrefix) || isOpenRouterSDKDataPackage(imported.importPath) {
			continue
		}
		issues = append(issues, inventoryIssue{Kind: "provider-import-boundary", Path: strings.TrimPrefix(imported.packagePath, gramModulePath+"/server/"), Detail: imported.importPath})
	}
	for _, call := range a.calls {
		operation := providerOperation(call.callee)
		if operation == "" {
			continue
		}
		inside := call.packagePath == openRouterPackage
		if inside {
			actual[call.path+":"+call.function+":"+operation]++
			continue
		}
		if operation == ProviderOperationOpenRouterSDK || a.callTargetsOpenRouter(call.call.Common()) {
			issues = append(issues, inventoryIssue{Kind: "provider-boundary", Path: call.path, Function: call.function, Detail: operation})
		}
	}

	expected := map[string]int{}
	claimedClass := map[string]string{}
	for class, claims := range map[string][]ProviderOperationClaim{
		"governed": governedProviderOperationInventory,
		"excluded": excludedProviderOperationInventory,
	} {
		for _, claim := range claims {
			key := claim.Path + ":" + claim.Function + ":" + claim.Operation
			if previous, duplicate := claimedClass[key]; duplicate {
				issues = append(issues, inventoryIssue{Kind: "duplicate-provider-classification", Path: claim.Path, Function: claim.Function, Detail: previous + " and " + class})
			}
			claimedClass[key] = class
			expected[key]++
		}
	}
	for key, count := range actual {
		if expected[key] != count {
			issues = append(issues, inventoryIssue{Kind: "provider-inventory", Detail: fmt.Sprintf("%s actual=%d claimed=%d", key, count, expected[key])})
		}
		delete(expected, key)
	}
	for key, count := range expected {
		issues = append(issues, inventoryIssue{Kind: "provider-inventory", Detail: fmt.Sprintf("%s actual=0 claimed=%d", key, count)})
	}
	return sortedIssues(issues)
}

func isOpenRouterSDKDataPackage(path string) bool {
	return path == openRouterSDKPrefix+"/models/components" || path == openRouterSDKPrefix+"/optionalnullable"
}

func providerOperation(object *types.Func) string {
	if isHTTPClientDo(object) {
		return ProviderOperationHTTPDo
	}
	if isHTTPRoundTrip(object) {
		return ProviderOperationHTTPRoundTrip
	}
	if isHTTPConvenienceEgress(object) {
		return ProviderOperationHTTPPackage
	}
	if isOpenRouterSDKOperation(object) {
		return ProviderOperationOpenRouterSDK
	}
	return ""
}

func isHTTPClientDo(object *types.Func) bool {
	return isHTTPRequestMethod(object, "Do")
}

func isHTTPRoundTrip(object *types.Func) bool {
	return isHTTPRequestMethod(object, "RoundTrip")
}

func isHTTPRequestMethod(object *types.Func, name string) bool {
	if object == nil || object.Name() != name {
		return false
	}
	signature, ok := object.Type().(*types.Signature)
	return ok && signature.Params().Len() == 1 && signature.Results().Len() >= 1 &&
		isPointerToNamedType(signature.Params().At(0).Type(), "net/http", "Request") &&
		isPointerToNamedType(signature.Results().At(0).Type(), "net/http", "Response")
}

func isHTTPConvenienceEgress(object *types.Func) bool {
	if object == nil || packagePath(object) != "net/http" {
		return false
	}
	signature, ok := object.Type().(*types.Signature)
	if !ok || signature.Results().Len() == 0 || !isPointerToNamedType(signature.Results().At(0).Type(), "net/http", "Response") {
		return false
	}
	if receiver := signature.Recv(); receiver != nil && !isPointerToNamedType(receiver.Type(), "net/http", "Client") {
		return false
	}
	switch object.Name() {
	case "Get", "Head", "Post", "PostForm":
		return true
	default:
		return false
	}
}

func isOpenRouterSDKOperation(object *types.Func) bool {
	if object == nil || !strings.HasPrefix(packagePath(object), openRouterSDKPrefix) {
		return false
	}
	signature, ok := object.Type().(*types.Signature)
	if !ok {
		return false
	}
	for v := range signature.Params().Variables() {
		if isContextType(v.Type()) {
			return true
		}
	}
	return false
}

func (a *inventoryAnalysis) callTargetsOpenRouter(common *ssa.CallCommon) bool {
	for _, argument := range common.Args {
		if isPointerToNamedType(argument.Type(), "net/http", "Request") && a.requestUsesOpenRouterURL(argument, map[ssa.Value]bool{}) {
			return true
		}
		if isStringType(argument.Type()) && a.valueUsesOpenRouterURL(argument, map[ssa.Value]bool{}) {
			return true
		}
	}
	return false
}

func isStringType(value types.Type) bool {
	basic, ok := types.Unalias(value).Underlying().(*types.Basic)
	return ok && basic.Kind() == types.String
}

func (a *inventoryAnalysis) requestUsesOpenRouterURL(value ssa.Value, seen map[ssa.Value]bool) bool {
	if value == nil || seen[value] {
		return false
	}
	seen[value] = true
	switch typed := value.(type) {
	case *ssa.Extract:
		call, ok := typed.Tuple.(ssa.CallInstruction)
		return ok && (a.requestCallUsesOpenRouterURL(call.Common(), typed.Index, seen) || a.requestResultUsesOpenRouterURL(call.Common(), typed.Index, seen))
	case *ssa.Call:
		return a.requestCallUsesOpenRouterURL(typed.Common(), 0, seen) || a.requestResultUsesOpenRouterURL(typed.Common(), 0, seen)
	case *ssa.Phi:
		for _, edge := range typed.Edges {
			if a.requestUsesOpenRouterURL(edge, seen) {
				return true
			}
		}
	case *ssa.ChangeType:
		return a.requestUsesOpenRouterURL(typed.X, seen)
	case *ssa.Convert:
		return a.requestUsesOpenRouterURL(typed.X, seen)
	case *ssa.Parameter:
		callee := typed.Parent()
		for _, call := range a.callers[callee] {
			for index, parameter := range callee.Params {
				if parameter == typed && index < len(call.Common().Args) && a.requestUsesOpenRouterURL(call.Common().Args[index], seen) {
					return true
				}
			}
		}
	}
	return false
}

func (a *inventoryAnalysis) requestCallUsesOpenRouterURL(common *ssa.CallCommon, _ int, seen map[ssa.Value]bool) bool {
	object := calledObject(common)
	if packagePath(object) != "net/http" || (object.Name() != "NewRequest" && object.Name() != "NewRequestWithContext") {
		return false
	}
	for _, argument := range common.Args {
		if isStringType(argument.Type()) && a.valueUsesOpenRouterURL(argument, seen) {
			return true
		}
	}
	return false
}

func (a *inventoryAnalysis) requestResultUsesOpenRouterURL(common *ssa.CallCommon, resultIndex int, seen map[ssa.Value]bool) bool {
	callee := common.StaticCallee()
	if callee == nil {
		return false
	}
	for _, block := range callee.Blocks {
		for _, instruction := range block.Instrs {
			returned, ok := instruction.(*ssa.Return)
			if ok && resultIndex < len(returned.Results) && a.requestUsesOpenRouterURL(returned.Results[resultIndex], seen) {
				return true
			}
		}
	}
	return false
}

func (a *inventoryAnalysis) valueUsesOpenRouterURL(value ssa.Value, seen map[ssa.Value]bool) bool {
	if value == nil || seen[value] {
		return false
	}
	seen[value] = true
	if constantValue, ok := value.(*ssa.Const); ok && constantValue.Value != nil && constantValue.Value.Kind() == constant.String {
		return strings.HasPrefix(constant.StringVal(constantValue.Value), "https://openrouter.ai/")
	}
	switch typed := value.(type) {
	case *ssa.Phi:
		for _, edge := range typed.Edges {
			if a.valueUsesOpenRouterURL(edge, seen) {
				return true
			}
		}
	case *ssa.ChangeInterface:
		return a.valueUsesOpenRouterURL(typed.X, seen)
	case *ssa.ChangeType:
		return a.valueUsesOpenRouterURL(typed.X, seen)
	case *ssa.Convert:
		return a.valueUsesOpenRouterURL(typed.X, seen)
	case *ssa.MakeInterface:
		return a.valueUsesOpenRouterURL(typed.X, seen)
	case *ssa.Slice:
		return a.valueUsesOpenRouterURL(typed.X, seen)
	case *ssa.Alloc, *ssa.IndexAddr:
		return a.aggregateUsesOpenRouterURL(value, seen)
	case *ssa.BinOp:
		return typed.Op == token.ADD && (a.valueUsesOpenRouterURL(typed.X, seen) || a.valueUsesOpenRouterURL(typed.Y, seen))
	case *ssa.Extract:
		if call, ok := typed.Tuple.(ssa.CallInstruction); ok {
			return a.stringCallUsesOpenRouterURL(call.Common(), seen) || a.stringResultUsesOpenRouterURL(call.Common(), typed.Index, seen)
		}
	case *ssa.Call:
		return a.stringCallUsesOpenRouterURL(typed.Common(), seen) || a.stringResultUsesOpenRouterURL(typed.Common(), 0, seen)
	case *ssa.Parameter:
		callee := typed.Parent()
		for _, call := range a.callers[callee] {
			for index, parameter := range callee.Params {
				if parameter == typed && index < len(call.Common().Args) && a.valueUsesOpenRouterURL(call.Common().Args[index], seen) {
					return true
				}
			}
		}
	}
	return false
}

func (a *inventoryAnalysis) aggregateUsesOpenRouterURL(value ssa.Value, seen map[ssa.Value]bool) bool {
	referrers := value.Referrers()
	if referrers == nil {
		return false
	}
	for _, instruction := range *referrers {
		var candidate ssa.Value
		switch typed := instruction.(type) {
		case *ssa.Store:
			candidate = typed.Val
		case *ssa.IndexAddr:
			candidate = typed
		case *ssa.Slice:
			candidate = typed
		}
		if candidate != nil && a.valueUsesOpenRouterURL(candidate, seen) {
			return true
		}
	}
	return false
}

func (a *inventoryAnalysis) stringCallUsesOpenRouterURL(common *ssa.CallCommon, seen map[ssa.Value]bool) bool {
	object := calledObject(common)
	if object == nil {
		return false
	}
	preservesInput := false
	switch packagePath(object) {
	case "fmt":
		preservesInput = object.Name() == "Sprint" || object.Name() == "Sprintf" || object.Name() == "Sprintln"
	case "strings":
		switch object.Name() {
		case "Join", "Replace", "ReplaceAll", "ToLower", "ToUpper", "Trim", "TrimFunc", "TrimLeft", "TrimLeftFunc", "TrimPrefix", "TrimRight", "TrimRightFunc", "TrimSpace", "TrimSuffix":
			preservesInput = true
		}
	case "path":
		preservesInput = object.Name() == "Join"
	case "net/url":
		preservesInput = true
	}
	if !preservesInput {
		return false
	}
	for _, argument := range common.Args {
		if a.valueUsesOpenRouterURL(argument, seen) {
			return true
		}
	}
	return false
}

func (a *inventoryAnalysis) stringResultUsesOpenRouterURL(common *ssa.CallCommon, resultIndex int, seen map[ssa.Value]bool) bool {
	callee := common.StaticCallee()
	if callee == nil {
		return false
	}
	for _, block := range callee.Blocks {
		for _, instruction := range block.Instrs {
			returned, ok := instruction.(*ssa.Return)
			if ok && resultIndex < len(returned.Results) && a.valueUsesOpenRouterURL(returned.Results[resultIndex], seen) {
				return true
			}
		}
	}
	return false
}

func sortedIssues(issues []inventoryIssue) []inventoryIssue {
	sort.Slice(issues, func(i, j int) bool { return issues[i].String() < issues[j].String() })
	return issues
}

func requireNoInventoryIssues(t *testing.T, issues []inventoryIssue) {
	t.Helper()
	if len(issues) == 0 {
		return
	}
	formatted := make([]string, len(issues))
	for index, issue := range issues {
		formatted[index] = issue.String()
	}
	require.Empty(t, issues, strings.Join(formatted, "\n"))
}

func writeMutationPackage(t *testing.T, source string) string {
	t.Helper()
	fixtureRoot := filepath.Join(repositoryRoot(t), "server", "internal", "thirdparty", "openrouter", "testdata")
	directory, err := os.MkdirTemp(fixtureRoot, "inventorymutation") //nolint:usetesting // packages.Load requires the fixture beneath the repository module.
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(directory)) })
	require.NoError(t, os.WriteFile(filepath.Join(directory, "mutation.go"), []byte(source), 0o600))
	return directory
}
