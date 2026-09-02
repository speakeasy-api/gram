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
	for _, claim := range ProductionCallSiteInventory {
		require.NoError(t, validateCategoryClass(claim.Category, categoryClasses[claim.Category]), claim)
		classificationPath := claim.Path
		if claim.Path == "chat/turnstream_tee.go" || claim.Category == CallCategoryUserChatCompletion {
			classificationPath = "chat/hosted_inference.go"
		}
		body, readErr := os.ReadFile(filepath.Join(internalRoot, filepath.FromSlash(classificationPath)))
		require.NoError(t, readErr)
		require.Contains(t, string(body), categoryIdentifier(claim.Category), "inventory claim has no static owning-surface classification: %v", claim)

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

func categoryIdentifier(category CallCategory) string {
	return map[CallCategory]string{
		CallCategoryUserChatCompletion:            "CallCategoryUserChatCompletion",
		CallCategoryChatSummary:                   "CallCategoryChatSummary",
		CallCategoryToolCallSummary:               "CallCategoryToolCallSummary",
		CallCategoryRiskAuthoring:                 "CallCategoryRiskAuthoring",
		CallCategoryBusinessMemorySearchEmbedding: "CallCategoryBusinessMemorySearchEmbedding",
		CallCategoryAutomaticChatTitle:            "CallCategoryAutomaticChatTitle",
		CallCategoryChatResolution:                "CallCategoryChatResolution",
		CallCategoryChatAnalysis:                  "CallCategoryChatAnalysis",
		CallCategoryPromptScanner:                 "CallCategoryPromptScanner",
		CallCategorySkillJudge:                    "CallCategorySkillJudge",
		CallCategoryBusinessMemoryJudge:           "CallCategoryBusinessMemoryJudge",
		CallCategoryRAGIndexing:                   "CallCategoryRAGIndexing",
		CallCategoryAssistantChat:                 "CallCategoryAssistantChat",
		CallCategoryAssistantMemory:               "CallCategoryAssistantMemory",
		CallCategoryAssistantResearch:             "CallCategoryAssistantResearch",
		CallCategoryAssistantRAG:                  "CallCategoryAssistantRAG",
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
	for _, command := range StandaloneCommandExclusions {
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

func TestDirectOpenRouterSDKInferenceIsTransportOwned(t *testing.T) {
	t.Parallel()
	serverRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	matches := []string{}
	err := filepath.WalkDir(filepath.Join(serverRoot, "internal"), func(path string, entry fs.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		if strings.Contains(string(body), ".Embeddings.Generate(") || strings.Contains(string(body), ".Chat.Completions") {
			rel, relErr := filepath.Rel(serverRoot, path)
			require.NoError(t, relErr)
			matches = append(matches, filepath.ToSlash(rel))
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"internal/thirdparty/openrouter/unified_client.go"}, matches)
}
