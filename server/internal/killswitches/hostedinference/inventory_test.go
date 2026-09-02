package hostedinference

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var inventoriedMethods = map[string]struct{}{
	"GetCompletion": {}, "GetCompletionStream": {},
	"GetObjectCompletion": {}, "CreateEmbeddings": {},
}

// Transparent wrappers preserve the owning caller's classification and are not
// independent inventory claims. Their forwarding behavior has focused tests in chat.
var transparentForwarders = map[string]struct{}{
	"chat/agent_client.go:GetCompletion":       {},
	"chat/agent_client.go:GetCompletionStream": {},
	"chat/agent_client.go:GetObjectCompletion": {},
	"chat/agent_client.go:CreateEmbeddings":    {},
}

// classificationOwners explicitly links forwarding/fallback call sites to the
// function that owns their classification. Every unlisted call site must
// classify in its own function.
var classificationOwners = map[string]string{
	"chat/impl.go:HandleCompletion":                     "chat/hosted_inference.go:classifyChatInference",
	"chat/turnstream_tee.go:teedCompletion":             "chat/hosted_inference.go:classifyChatInference",
	"scanners/promptinjection/openrouter/judge.go:call": "scanners/promptinjection/openrouter/judge.go:Classify",
	"scanners/promptpolicy/openrouter/judge.go:call":    "scanners/promptpolicy/openrouter/judge.go:Evaluate",
	"skills/efficacy/judge.go:call":                     "skills/efficacy/judge.go:Judge",
}

func TestProductionCallSiteInventoryIsSynchronized(t *testing.T) {
	t.Parallel()
	internalRoot := filepath.Clean(filepath.Join("..", ".."))
	actual := map[string]int{}
	err := filepath.WalkDir(internalRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if entry.IsDir() {
			rel, _ := filepath.Rel(internalRoot, path)
			if rel == filepath.Join("thirdparty", "openrouter") || rel == "gen" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, parseErr)
		rel, relErr := filepath.Rel(internalRoot, path)
		require.NoError(t, relErr)
		for _, declaration := range parsed.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if _, transparent := transparentForwarders[filepath.ToSlash(rel)+":"+fn.Name.Name]; transparent {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				method := selector.Sel.Name
				if _, tracked := inventoriedMethods[method]; tracked {
					actual[filepath.ToSlash(rel)+":"+fn.Name.Name+":"+method]++
				}
				return true
			})
		}
		return nil
	})
	require.NoError(t, err)

	expected := map[string]int{}
	governedClaimed := map[CallCategory]bool{}
	for _, claim := range productionCallSiteInventory {
		require.NoError(t, validateCategoryClass(claim.Category, categoryClasses[claim.Category]), claim)
		callSiteOwner := claim.Path + ":" + claim.Function
		classificationOwner := callSiteOwner
		if linked, ok := classificationOwners[callSiteOwner]; ok {
			classificationOwner = linked
		}
		ownerPath, ownerFunction, ok := strings.Cut(classificationOwner, ":")
		require.True(t, ok, "invalid classification owner: %s", classificationOwner)
		require.True(t, functionReferencesIdentifier(t, filepath.Join(internalRoot, filepath.FromSlash(ownerPath)), ownerFunction, categoryIdentifier(claim.Category)),
			"inventory claim is not classified by its linked function: %v owner=%s", claim, classificationOwner)

		key := claim.Path + ":" + claim.Function + ":" + claim.Method
		expected[key]++
		if isGovernedCategory(claim.Category) {
			governedClaimed[claim.Category] = true
		}
	}
	require.Equal(t, expected, actual)
	for category, class := range categoryClasses {
		if class == CallClassGovernedUser {
			require.True(t, governedClaimed[category], "registered governed category has no production coverage claim: %s", category)
		}
	}
}

func functionReferencesIdentifier(t *testing.T, path, function, identifier string) bool {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	require.NoError(t, err)
	for _, declaration := range parsed.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != function || fn.Body == nil {
			continue
		}
		found := false
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == identifier {
				found = true
			}
			return !found
		})
		return found
	}
	return false
}

func categoryIdentifier(category CallCategory) string {
	return map[CallCategory]string{
		CallCategoryUserChatCompletion:              "CallCategoryUserChatCompletion",
		CallCategoryChatSummary:                     "CallCategoryChatSummary",
		CallCategoryToolCallSummary:                 "CallCategoryToolCallSummary",
		CallCategoryRiskAuthoring:                   "CallCategoryRiskAuthoring",
		CallCategoryBusinessMemorySearchEmbedding:   "CallCategoryBusinessMemorySearchEmbedding",
		CallCategoryAPIKeyChat:                      "CallCategoryAPIKeyChat",
		CallCategoryChatSessionChat:                 "CallCategoryChatSessionChat",
		CallCategoryNonOrdinaryGramSessionChat:      "CallCategoryNonOrdinaryGramSessionChat",
		CallCategoryAPIKeyChatSummary:               "CallCategoryAPIKeyChatSummary",
		CallCategoryAPIKeyToolCallSummary:           "CallCategoryAPIKeyToolCallSummary",
		CallCategoryAPIKeyRiskAuthoring:             "CallCategoryAPIKeyRiskAuthoring",
		CallCategoryAPIKeyBusinessMemorySearch:      "CallCategoryAPIKeyBusinessMemorySearch",
		CallCategoryNonOrdinarySessionChatSummary:   "CallCategoryNonOrdinarySessionChatSummary",
		CallCategoryNonOrdinarySessionToolSummary:   "CallCategoryNonOrdinarySessionToolSummary",
		CallCategoryNonOrdinarySessionRiskAuthoring: "CallCategoryNonOrdinarySessionRiskAuthoring",
		CallCategoryNonOrdinarySessionMemorySearch:  "CallCategoryNonOrdinarySessionMemorySearch",
		CallCategoryAutomaticChatTitle:              "CallCategoryAutomaticChatTitle",
		CallCategoryChatResolution:                  "CallCategoryChatResolution",
		CallCategoryChatAnalysis:                    "CallCategoryChatAnalysis",
		CallCategoryPromptScanner:                   "CallCategoryPromptScanner",
		CallCategorySkillJudge:                      "CallCategorySkillJudge",
		CallCategoryBusinessMemoryJudge:             "CallCategoryBusinessMemoryJudge",
		CallCategoryRAGIndexing:                     "CallCategoryRAGIndexing",
		CallCategoryAssistantChat:                   "CallCategoryAssistantChat",
		CallCategoryAssistantMemory:                 "CallCategoryAssistantMemory",
		CallCategoryAssistantResearch:               "CallCategoryAssistantResearch",
		CallCategoryAssistantRAG:                    "CallCategoryAssistantRAG",
	}[category]
}

func TestProductionCompositionsInjectCheckpoint(t *testing.T) {
	t.Parallel()
	serverRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	production := []string{"cmd/gram/start.go", "cmd/gram/worker.go", "cmd/gram/streams.go"}
	for _, rel := range production {
		body, err := os.ReadFile(filepath.Join(serverRoot, rel))
		require.NoError(t, err)
		text := string(body)
		require.Equal(t, 1, strings.Count(text, "NewUnifiedClient("), rel)
		require.Contains(t, text, "WithHostedInferenceCheckpoint(", rel)
	}
	for _, command := range standaloneCommandExclusions {
		matches, err := filepath.Glob(filepath.Join(serverRoot, "cmd", command, "*.go"))
		require.NoError(t, err)
		joined := strings.Builder{}
		for _, match := range matches {
			body, readErr := os.ReadFile(match)
			require.NoError(t, readErr)
			joined.Write(body)
		}
		require.Equal(t, 1, strings.Count(joined.String(), "NewUnifiedClient("), command)
		require.NotContains(t, joined.String(), "WithHostedInferenceCheckpoint(", command)
	}
}

func TestProductionAIAccessCompositionSharesRegistryAndEvaluator(t *testing.T) {
	t.Parallel()
	serverRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	helper, err := os.ReadFile(filepath.Join(serverRoot, "cmd/gram/hosted_inference.go"))
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(helper), "mcptoolexecution.NewRegistry("))
	require.Equal(t, 1, strings.Count(string(helper), "killswitches.NewEvaluator("))

	start, err := os.ReadFile(filepath.Join(serverRoot, "cmd/gram/start.go"))
	require.NoError(t, err)
	text := string(start)
	require.NotContains(t, text, "mcptoolexecution.NewRegistry(")
	require.NotContains(t, text, "killswitches.NewEvaluator(")
	require.Contains(t, text, "NewHookAIAccessCheckpoint(aiAccess.registry, aiAccess.evaluator")
	require.Contains(t, text, "NewLiteLLMAIAccessCheckpoint(aiAccess.registry, aiAccess.evaluator")
	require.Contains(t, text, "WithHostedInferenceCheckpoint(aiAccess.hostedInference)")

	checkpoint, err := os.ReadFile(filepath.Join(serverRoot, "internal/killswitches/hostedinference/checkpoint.go"))
	require.NoError(t, err)
	require.NotContains(t, string(checkpoint), "NewEvaluator(", "surface checkpoints must not construct a parallel evaluator")
}

func TestManagementAuditAndPlatformControlPathsDoNotDependOnHostedInferenceCheckpoint(t *testing.T) {
	t.Parallel()

	serverRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	for _, rel := range []string{"internal/killswitchapi", "internal/audit", "internal/auditapi", "internal/platformmcp"} {
		err := filepath.WalkDir(filepath.Join(serverRoot, rel), func(path string, entry fs.DirEntry, walkErr error) error {
			require.NoError(t, walkErr)
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			body, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			text := string(body)
			require.NotContains(t, text, "killswitches/hostedinference", path)
			require.NotContains(t, text, "PreflightHostedInference", path)
			require.NotContains(t, text, "WithHostedInferenceCheckpoint", path)
			return nil
		})
		require.NoError(t, err)
	}
}

// TestValidatedSessionProvenanceMintingIsAuthBoundaryOwned prevents ordinary
// production packages from manufacturing the opaque provenance consumed by
// hosted-inference policy. Tests may mint it directly; production calls stay
// inside the credential validators that established the underlying facts.
func TestValidatedSessionProvenanceMintingIsAuthBoundaryOwned(t *testing.T) {
	t.Parallel()

	serverRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	actual := map[string][]string{}
	tracked := map[string]struct{}{
		"WithValidatedGramSession":           {},
		"WithValidatedChatSessionActingUser": {},
	}
	err := filepath.WalkDir(serverRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, parseErr)
		rel, relErr := filepath.Rel(serverRoot, path)
		require.NoError(t, relErr)
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if _, ok := tracked[selector.Sel.Name]; ok {
				actual[selector.Sel.Name] = append(actual[selector.Sel.Name], filepath.ToSlash(rel))
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, map[string][]string{
		"WithValidatedGramSession": {
			"internal/auth/sessions/sessions.go",
			"internal/auth/sessions/sessions.go",
		},
		"WithValidatedChatSessionActingUser": {
			"internal/auth/chatsessions/manager.go",
		},
	}, actual)
}

func TestHostedProviderTransportsAreRepoWideAllowlisted(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	matches := map[string]int{}
	err := filepath.WalkDir(repoRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "gen" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, parseErr)
		rel, relErr := filepath.Rel(repoRoot, path)
		require.NoError(t, relErr)
		for _, declaration := range parsed.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				switch value := node.(type) {
				case *ast.BasicLit:
					if strings.Contains(value.Value, "/v1/chat/completions") {
						matches[filepath.ToSlash(rel)+":"+fn.Name.Name+":raw_chat_completions"]++
					}
					if strings.Contains(value.Value, "/endpoints") {
						matches[filepath.ToSlash(rel)+":"+fn.Name.Name+":raw_model_endpoints"]++
					}
				case *ast.CallExpr:
					selector, ok := value.Fun.(*ast.SelectorExpr)
					if !ok {
						break
					}
					if selector.Sel.Name == "Generate" {
						if owner, ok := selector.X.(*ast.SelectorExpr); ok && owner.Sel.Name == "Embeddings" {
							matches[filepath.ToSlash(rel)+":"+fn.Name.Name+":sdk_embeddings"]++
						}
					}
					if ident, ok := selector.X.(*ast.Ident); ok && ident.Name == "or_base" && selector.Sel.Name == "New" {
						matches[filepath.ToSlash(rel)+":"+fn.Name.Name+":sdk_client"]++
					}
				}
				return true
			})
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, map[string]int{
		"server/internal/thirdparty/openrouter/context_window.go:fetchMin:raw_model_endpoints":         1,
		"server/internal/thirdparty/openrouter/unified_client.go:createEmbeddings:sdk_client":          1,
		"server/internal/thirdparty/openrouter/unified_client.go:createEmbeddings:sdk_embeddings":      1,
		"server/internal/thirdparty/openrouter/unified_client.go:makeHTTPRequest:raw_chat_completions": 1,
	}, matches)
}
