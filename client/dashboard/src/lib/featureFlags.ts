export const FEATURE_FLAGS = {
  inferenceWorkloadSetup: "inference-workload-setup",
  // UI-only visibility; saved runtime selection and backend authorization are independent.
  assistantKitRuntime: "assistant-kit-runtime",
  // UI-only rollout gate; Channels operations enforce organization grants.
  channels: "channels",
  budgets: "gram-budgets",
  // Gates saving a service's enabled identity provider list; Speakeasy vouching follows its issuer state.
  emaProviders: "ema-providers",
  explore: "gram-explore",
  headlessModeSwitcher: "headless-mode-switcher",
  killswitches: "gram-killswitches",
  // UI-only rollout gate for the MCP scope picker in the risk policy editor;
  // not enforced server-side.
  mcpScopedPolicies: "gram-mcp-scoped-policies",
  // UI-only rollout gate for the Sync from Okta option of Add MCP server; the
  // Okta application and suggestion APIs behind it are not gated.
  oktaMcpImport: "okta-mcp-import",
  promptPolicies: "gram-prompt-policies",
  // Org-targeted, read server-side under the same key when a private network
  // connection is created: the tailnet gateway when enabled, the Tailscale
  // operator otherwise. The setup sheet shows the matching steps.
  privateNetworkGateway: "private-network-gateway",
  // Multivariate (`off` | `shadow` | `llm`), org-targeted: which risk engine
  // the organization runs. Read through `useFeatureFlagVariant`; the boolean
  // read is true for every variant, `off` included. `useDetectorMode` maps it
  // to the policy editor mode.
  riskLlmAnalyzer: "gram-risk-llm-analyzer",
  // Creating tool approval policies and the Tool approvals tab. The server
  // enforces the same key on policy create and update.
  toolApprovals: "risk-tool-approvals",
} as const;

export type FeatureFlag = (typeof FEATURE_FLAGS)[keyof typeof FEATURE_FLAGS];
