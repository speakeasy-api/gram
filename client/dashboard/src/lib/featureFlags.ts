export const FEATURE_FLAGS = {
  // UI-only visibility; saved runtime selection and backend authorization are independent.
  assistantKitRuntime: "assistant-kit-runtime",
  budgets: "gram-budgets",
  // Gates saving a service's enabled identity provider list; Speakeasy vouching follows its issuer state.
  emaProviders: "ema-providers",
  gatewayEndpoints: "gram-gateway-endpoints",
  headlessModeSwitcher: "headless-mode-switcher",
  killswitches: "gram-killswitches",
  mcpResearch: "gram-mcp-research",
  // UI-only rollout gate for the MCP scope picker in the risk policy editor;
  // not enforced server-side.
  mcpScopedPolicies: "gram-mcp-scoped-policies",
  oktaConnections: "okta-connections",
  paygSelfServeBilling: "gram-payg-self-serve-billing",
  promptPolicies: "gram-prompt-policies",
  // Multivariate (`off` | `shadow` | `llm`), org-targeted: which risk engine
  // the organization runs. Read through `useFeatureFlagVariant`; the boolean
  // read is true for every variant, `off` included. `useDetectorMode` maps it
  // to the policy editor mode.
  riskLlmAnalyzer: "gram-risk-llm-analyzer",
  // UI-only rollout gate for the organization's Speakeasy authorization
  // servers page; the APIs behind it are not gated.
  userSessionIssuerDashboard: "gram-user-session-issuer-dashboard",
} as const;

export type FeatureFlag = (typeof FEATURE_FLAGS)[keyof typeof FEATURE_FLAGS];
