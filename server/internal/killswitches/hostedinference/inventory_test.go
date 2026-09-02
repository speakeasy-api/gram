package hostedinference

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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
	requireNoInventoryIssues(t, loadRepositoryAnalysis(t).completionIssues())
}

func TestRepositoryConstructorInventoryIsSynchronized(t *testing.T) {
	t.Parallel()
	requireNoInventoryIssues(t, loadRepositoryAnalysis(t).constructorIssues())
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
	require.Contains(t, text, "openrouter.NewUnifiedClient(")
	require.NotContains(t, text, "WithHostedInferenceCheckpoint(aiAccess.hostedInference)")

	checkpoint, err := os.ReadFile(filepath.Join(serverRoot, "internal/killswitches/hostedinference/checkpoint.go"))
	require.NoError(t, err)
	require.NotContains(t, string(checkpoint), "NewEvaluator(", "surface checkpoints must not construct a parallel evaluator")
}

func TestManagementAuditAndPlatformControlPathsDoNotDependOnHostedInferenceCheckpoint(t *testing.T) {
	t.Parallel()
	analysis := loadRepositoryAnalysis(t)
	restricted := []string{"/internal/killswitchapi", "/internal/audit", "/internal/auditapi", "/internal/platformmcp"}
	for _, imported := range analysis.imports {
		for _, suffix := range restricted {
			if strings.HasPrefix(imported.packagePath, gramModulePath+"/server"+suffix) {
				require.NotEqual(t, hostedPackage, imported.importPath, imported.packagePath)
			}
		}
	}
	for _, call := range analysis.calls {
		for _, suffix := range restricted {
			if strings.HasPrefix(call.packagePath, gramModulePath+"/server"+suffix) {
				require.NotEqual(t, hostedPackage, packagePath(call.callee), call.path)
			}
		}
	}
}

// TestValidatedSessionProvenanceMintingIsAuthBoundaryOwned prevents ordinary
// production packages from manufacturing the opaque provenance consumed by
// hosted-inference policy. Tests may mint it directly; production calls stay
// inside the credential validators that established the underlying facts.
func TestValidatedSessionProvenanceMintingIsAuthBoundaryOwned(t *testing.T) {
	t.Parallel()

	actual := map[string][]string{}
	tracked := map[string]struct{}{
		"WithValidatedGramSession":           {},
		"WithValidatedChatSessionActingUser": {},
	}
	for _, call := range loadRepositoryAnalysis(t).calls {
		if packagePath(call.callee) != contextValuesPackage {
			continue
		}
		if _, ok := tracked[call.callee.Name()]; ok {
			actual[call.callee.Name()] = append(actual[call.callee.Name()], call.path)
		}
	}
	for _, paths := range actual {
		sort.Strings(paths)
	}
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

func TestProviderOperationInventoryIsSynchronized(t *testing.T) {
	t.Parallel()
	requireNoInventoryIssues(t, loadRepositoryAnalysis(t).providerIssues())
}

func TestTypeAwareInventoryDetectsRegressions(t *testing.T) {
	directory := writeMutationPackage(t, `package inventorymutation

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	_ "github.com/OpenRouterTeam/go-sdk/retry"
	"github.com/speakeasy-api/gram/server/internal/killswitches/hostedinference"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func droppedClassification(ctx context.Context, client openrouter.CompletionClient) {
	classified, _ := hostedinference.WithInternal(ctx, hostedinference.CallCategoryPromptScanner)
	_ = classified
	_, _ = client.GetCompletion(ctx, openrouter.CompletionRequest{})
}

func mergedClassification(ctx context.Context, client openrouter.CompletionClient, background bool) {
	classified, _ := hostedinference.WithInternal(ctx, hostedinference.CallCategoryPromptScanner)
	if background {
		classified, _ = hostedinference.WithBackground(ctx, hostedinference.CallCategoryAutomaticChatTitle)
	}
	_, _ = client.GetCompletion(classified, openrouter.CompletionRequest{})
}

func conditionallyDroppedClassification(ctx context.Context, client openrouter.CompletionClient, drop bool) {
	classified, _ := hostedinference.WithInternal(ctx, hostedinference.CallCategoryPromptScanner)
	if drop {
		classified = ctx
	}
	_, _ = client.GetCompletion(classified, openrouter.CompletionRequest{})
}

func forwardCompletion(ctx context.Context, client openrouter.CompletionClient) {
	_, _ = client.GetCompletion(ctx, openrouter.CompletionRequest{})
}

func classifiedForwardingCaller(ctx context.Context, client openrouter.CompletionClient) {
	classified, _ := hostedinference.WithInternal(ctx, hostedinference.CallCategoryPromptScanner)
	forwardCompletion(classified, client)
}

var forwardAlias = forwardCompletion

func rawForwardingCaller(ctx context.Context, client openrouter.CompletionClient) {
	forwardAlias(ctx, client)
}

var uncheckedFactory = openrouter.NewUncheckedUnifiedClient

func uncheckedProductionConstruction() {
	_ = openrouter.NewUncheckedUnifiedClient(nil, nil, nil, nil, nil, nil, nil, nil)
	_ = uncheckedFactory(nil, nil, nil, nil, nil, nil, nil, nil)
}

func misplacedProductionConstruction() {
	_, _ = openrouter.NewUnifiedClient(nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func rogueClientFactory() openrouter.CompletionClient {
	return &openrouter.ChatClient{}
}

func directProviderEgress(ctx context.Context) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, openrouter.OpenRouterBaseURL+"/v1/chat/completions", nil)
	_, _ = http.DefaultClient.Do(req)
}

func providerRequest(ctx context.Context) *http.Request {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, openrouter.OpenRouterBaseURL+"/v1/chat/completions", nil)
	return req
}

func helperProviderEgress(ctx context.Context) {
	_, _ = http.DefaultTransport.RoundTrip(providerRequest(ctx))
}

func sendProviderRequest(req *http.Request) {
	_, _ = http.DefaultClient.Do(req)
}

func parameterProviderEgress(ctx context.Context) {
	sendProviderRequest(providerRequest(ctx))
}

func formattedProviderEgress(ctx context.Context) {
	providerURL := fmt.Sprintf("%s/v1/chat/completions", openrouter.OpenRouterBaseURL)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, providerURL, nil)
	_, _ = http.DefaultClient.Do(req)
}

func parsedProviderEgress(ctx context.Context) {
	providerURL, _ := url.Parse(openrouter.OpenRouterBaseURL)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, providerURL.JoinPath("v1", "chat", "completions").String(), nil)
	_, _ = http.DefaultClient.Do(req)
}

func clientGetProviderEgress() {
	_, _ = http.DefaultClient.Get(openrouter.OpenRouterBaseURL + "/v1/models")
}

func clientHeadProviderEgress() {
	_, _ = http.DefaultClient.Head(openrouter.OpenRouterBaseURL + "/v1/models")
}

func clientPostProviderEgress() {
	_, _ = http.DefaultClient.Post(openrouter.OpenRouterBaseURL+"/v1/chat/completions", "application/json", nil)
}

func clientPostFormProviderEgress() {
	_, _ = http.DefaultClient.PostForm(openrouter.OpenRouterBaseURL+"/v1/chat/completions", nil)
}

func packageProviderEgress() {
	_, _ = http.Post(openrouter.OpenRouterBaseURL+"/v1/chat/completions", "application/json", nil)
}
`)

	root := repositoryRoot(t)
	relative, err := filepath.Rel(root, directory)
	require.NoError(t, err)
	analysis, err := loadInventoryAnalysis(root, "./"+filepath.ToSlash(relative))
	require.NoError(t, err)
	issues := append(analysis.completionIssues(), analysis.constructorIssues()...)
	issues = append(issues, analysis.providerIssues()...)

	kinds := map[string]bool{}
	boundaryFunctions := map[string]bool{}
	contextIssueFunctions := map[string]bool{}
	for _, issue := range issues {
		kinds[issue.Kind] = true
		if issue.Kind == "provider-boundary" {
			boundaryFunctions[issue.Function] = true
		}
		if issue.Kind == "completion-context" {
			contextIssueFunctions[issue.Function] = true
		}
	}
	for _, function := range []string{"droppedClassification", "conditionallyDroppedClassification", "forwardCompletion"} {
		require.True(t, contextIssueFunctions[function], "%s context regression was not detected: %v", function, issues)
	}
	require.True(t, kinds["unchecked-constructor"], "unchecked production constructor was not detected: %v", issues)
	require.True(t, kinds["production-constructor-boundary"], "misplaced production constructor was not detected: %v", issues)
	require.True(t, kinds["client-construction-inventory"], "rogue client factory was not detected: %v", issues)
	require.True(t, kinds["constructor-reference"], "aliased constructor was not detected: %v", issues)
	for _, function := range []string{"directProviderEgress", "helperProviderEgress", "sendProviderRequest", "formattedProviderEgress", "parsedProviderEgress", "clientGetProviderEgress", "clientHeadProviderEgress", "clientPostProviderEgress", "clientPostFormProviderEgress", "packageProviderEgress"} {
		require.True(t, boundaryFunctions[function], "%s was not detected: %v", function, issues)
	}
	require.True(t, kinds["provider-import-boundary"], "operational OpenRouter SDK import was not detected: %v", issues)

	matched := map[string]int{}
	for _, call := range analysis.calls {
		if !analysis.isCompletionOperation(call.callee) {
			continue
		}
		switch call.function {
		case "mergedClassification":
			matched[call.function]++
			categories, unclassified := analysis.callClassification(call.call.Common())
			require.Contains(t, categories, CallCategoryPromptScanner)
			require.Contains(t, categories, CallCategoryAutomaticChatTitle)
			require.False(t, categoriesMatchClaim(categories, unclassified, CallSiteClaim{Category: CallCategoryPromptScanner}), "mixed-category flow must not satisfy an exact claim")
		case "conditionallyDroppedClassification", "forwardCompletion":
			matched[call.function]++
			categories, unclassified := analysis.callClassification(call.call.Common())
			require.Contains(t, categories, CallCategoryPromptScanner)
			require.True(t, unclassified, "%s must retain an unclassified path", call.function)
			require.False(t, categoriesMatchClaim(categories, unclassified, CallSiteClaim{Category: CallCategoryPromptScanner}))
		}
	}
	require.Equal(t, map[string]int{
		"mergedClassification":               1,
		"conditionallyDroppedClassification": 1,
		"forwardCompletion":                  1,
	}, matched)
}
